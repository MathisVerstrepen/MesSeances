package database

import "testing"

func TestWatchlistPreferencesMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade_from_054"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "watchlist_preferences_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 54, "054_account_watchlist_tag_colors.sql")
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
			exec(`INSERT INTO account_watchlist_tags(account_id,id,name,name_key,color) OVERRIDING SYSTEM VALUE VALUES(1,100,'Été','été','blue'),(2,200,'Été','été','rose')`)
			exec(`INSERT INTO account_watchlist_item_tags VALUES(1,10,100),(2,10,200)`)
			snapshot := func() string {
				t.Helper()
				var data string
				err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,id) FROM account_watchlist_tags t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,public_movie_id,tag_id) FROM account_watchlist_item_tags t),
 (SELECT jsonb_agg(to_jsonb(t)-'view_mode'-'filter_tag_id' ORDER BY account_id) FROM account_watchlist_state t),
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
				t.Fatal("migration changed existing data or initialized absent state")
			}
			var valid bool
			if err := pool.QueryRow(ctx, `SELECT view_mode='list' AND filter_tag_id IS NULL FROM account_watchlist_state WHERE account_id=1`).Scan(&valid); err != nil || !valid {
				t.Fatal("missing preference defaults", err)
			}
			for _, mode := range []any{nil, "", "LIST", "tags ", "grouped"} {
				if _, err := pool.Exec(ctx, `UPDATE account_watchlist_state SET view_mode=$1 WHERE account_id=1`, mode); err == nil {
					t.Fatal("invalid SQL view mode accepted")
				}
			}
			for _, id := range []int64{0, -1, 200, 999} {
				if _, err := pool.Exec(ctx, `UPDATE account_watchlist_state SET filter_tag_id=$1 WHERE account_id=1`, id); err == nil {
					t.Fatal("dangling or foreign filter accepted")
				}
			}
			exec(`UPDATE account_watchlist_state SET view_mode='tags',filter_tag_id=100 WHERE account_id=1`)
			exec(`DELETE FROM account_watchlist_tags WHERE account_id=1 AND id=100`)
			if err := pool.QueryRow(ctx, `SELECT revision=7 AND view_mode='tags' AND filter_tag_id IS NULL AND sort_order='release_asc' FROM account_watchlist_state WHERE account_id=1`).Scan(&valid); err != nil || !valid {
				t.Fatal("column-specific SET NULL lost owner/state/mode/sort", err)
			}
			// Account deletion traverses both direct state cascade and tag SET NULL.
			exec(`INSERT INTO account_watchlist_state(account_id,revision,view_mode,filter_tag_id) VALUES(2,9,'tags',200)`)
			exec(`DELETE FROM accounts`)
			var count int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watchlist_state)+(SELECT count(*) FROM account_watchlist_tags)+(SELECT count(*) FROM account_watchlist_items)+(SELECT count(*) FROM account_watchlist_item_tags)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("account cascade failed", err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM public_movies`).Scan(&count); err != nil || count != 1 {
				t.Fatal("account deletion removed public movie", err)
			}
		})
	}
}
