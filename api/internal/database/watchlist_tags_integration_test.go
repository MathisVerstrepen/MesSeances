package database

import "testing"

func TestWatchlistTagColorsMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade_from_053"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "watchlist_colors_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 53, "053_account_watchlist_tags.sql")
			} else if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			exec := func(sql string) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO accounts(id,email,created_at,email_verified_at,verification_source) OVERRIDING SYSTEM VALUE VALUES(1,'one@example.com',now(),now(),'email'),(2,'two@example.com',now(),now(),'email')`)
			exec(`INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES(10,10,'Film',0)`)
			exec(`INSERT INTO account_watchlist_state(account_id,revision,sort_order) VALUES(1,7,'release_asc')`)
			exec(`INSERT INTO account_watchlist_items(account_id,public_movie_id,added_at) VALUES(1,10,'2026-09-01T12:00:00.123456Z'),(2,10,'2026-09-02T12:00:00Z')`)
			exec(`INSERT INTO account_watchlist_tags(account_id,id,name,name_key) OVERRIDING SYSTEM VALUE VALUES(1,100,'Été','été'),(2,200,'Été','été')`)
			exec(`INSERT INTO account_watchlist_item_tags VALUES(1,10,100),(2,10,200)`)
			snapshot := func() string {
				t.Helper()
				var data string
				err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t)-'color' ORDER BY account_id,id) FROM account_watchlist_tags t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,public_movie_id,tag_id) FROM account_watchlist_item_tags t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id) FROM account_watchlist_state t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,public_movie_id) FROM account_watchlist_items t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM accounts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM public_movies t))::text`).Scan(&data)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			before := snapshot()
			for range 2 {
				if err := RunMigrations(ctx, pool); err != nil {
					t.Fatal(err)
				}
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if snapshot() != before {
				t.Fatal("color migration changed existing data or initialized absent state")
			}
			var neutral bool
			if err := pool.QueryRow(ctx, `SELECT bool_and(color='neutral') FROM account_watchlist_tags`).Scan(&neutral); err != nil || !neutral {
				t.Fatal("existing tags not neutral", err)
			}
			for _, color := range []string{"neutral", "red", "amber", "green", "teal", "blue", "violet", "rose"} {
				if _, err := pool.Exec(ctx, `UPDATE account_watchlist_tags SET color=$1 WHERE account_id=1`, color); err != nil {
					t.Fatal("palette rejected", err)
				}
			}
			for _, color := range []any{nil, "", "purple", "BLUE", " blue", "blue ", "#2563eb", "var(--blue)"} {
				if _, err := pool.Exec(ctx, `UPDATE account_watchlist_tags SET color=$1 WHERE account_id=1`, color); err == nil {
					t.Fatal("invalid SQL color accepted")
				}
			}
			var color string
			if err := pool.QueryRow(ctx, `INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,'Default','default') RETURNING color`).Scan(&color); err != nil || color != "neutral" {
				t.Fatal("SQL default absent", err)
			}
		})
	}
}

func TestWatchlistTagsMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade_from_052"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "watchlist_tags_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 52, "052_account_watchlist_sort.sql")
			} else if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			exec := func(sql string) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO accounts(id,email,created_at,email_verified_at,verification_source) OVERRIDING SYSTEM VALUE VALUES(1,'one@example.com',now(),now(),'email'),(2,'two@example.com',now(),now(),'email')`)
			exec(`INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES(10,10,'Film',0),(20,20,'Other',0)`)
			exec(`INSERT INTO account_watchlist_state(account_id,revision,sort_order) VALUES(1,7,'release_asc')`)
			exec(`INSERT INTO account_watchlist_items(account_id,public_movie_id,added_at) VALUES(1,10,'2026-09-01T12:00:00.123456Z'),(2,20,'2026-09-01T12:00:00Z')`)
			for range 2 {
				if err := RunMigrations(ctx, pool); err != nil {
					t.Fatal(err)
				}
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var valid bool
			if err := pool.QueryRow(ctx, `SELECT s.revision=7 AND s.sort_order='release_asc' AND i.added_at='2026-09-01T12:00:00.123456Z'::timestamptz FROM account_watchlist_state s JOIN account_watchlist_items i USING(account_id) WHERE s.account_id=1`).Scan(&valid); err != nil || !valid {
				t.Fatal("upgrade modified state", err)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watchlist_state WHERE account_id=2)+(SELECT count(*) FROM account_watchlist_tags)+(SELECT count(*) FROM account_watchlist_item_tags)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("migration backfilled", err)
			}
			exec(`INSERT INTO account_watchlist_tags(account_id,id,name,name_key) OVERRIDING SYSTEM VALUE VALUES(1,100,'Action','action'),(2,200,'Action','action')`)
			for _, sql := range []string{
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,'Other','action')`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(999,'Missing','missing')`,
				`INSERT INTO account_watchlist_tags(account_id,id,name,name_key) OVERRIDING SYSTEM VALUE VALUES(1,0,'Zero','zero')`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,'','empty')`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,'   ','blank')`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,repeat('é',41),'long')`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,'Key','')`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,'Key',repeat('x',121))`,
				`INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES(1,NULL,'null')`,
				`INSERT INTO account_watchlist_item_tags VALUES(1,10,200)`,
				`INSERT INTO account_watchlist_item_tags VALUES(1,20,100)`,
				`INSERT INTO account_watchlist_item_tags VALUES(1,999,100)`,
				`INSERT INTO account_watchlist_item_tags VALUES(1,10,999)`,
			} {
				if _, err := pool.Exec(ctx, sql); err == nil {
					t.Fatal("SQL constraint absent", sql)
				}
			}
			exec(`INSERT INTO account_watchlist_item_tags VALUES(1,10,100),(2,20,200)`)
			if _, err := pool.Exec(ctx, `INSERT INTO account_watchlist_item_tags VALUES(1,10,100)`); err == nil {
				t.Fatal("duplicate association")
			}
			exec(`DELETE FROM account_watchlist_tags WHERE account_id=1 AND id=100`)
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_items`).Scan(&count); err != nil || count != 2 {
				t.Fatal("tag deletion removed films", err)
			}
			exec(`DELETE FROM account_watchlist_items WHERE account_id=2`)
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_tags`).Scan(&count); err != nil || count != 1 {
				t.Fatal("film deletion removed tag", err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_item_tags`).Scan(&count); err != nil || count != 0 {
				t.Fatal("association cascade", err)
			}
			exec(`INSERT INTO account_watchlist_items(account_id,public_movie_id) VALUES(2,20)`)
			exec(`INSERT INTO account_watchlist_item_tags VALUES(2,20,200)`)
			exec(`DELETE FROM accounts`)
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watchlist_tags)+(SELECT count(*) FROM account_watchlist_item_tags)+(SELECT count(*) FROM account_watchlist_items)+(SELECT count(*) FROM account_watchlist_state)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("account cascade", err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM public_movies`).Scan(&count); err != nil || count != 2 {
				t.Fatal("public film deletion", err)
			}
		})
	}
}
