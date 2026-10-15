package database

import "testing"

func TestWatchedMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade_from_061"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "watched_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 61, "061_public_page_content.sql")
			} else if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			exec := func(sql string) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO accounts(id,email,created_at,email_verified_at,verification_source) OVERRIDING SYSTEM VALUE VALUES(1,'one@example.com',now(),now(),'email'),(2,'two@example.com',now(),now(),'email');
 INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES(10,10,'Film',0);
 INSERT INTO account_watchlist_state(account_id,revision,sort_order,view_mode) VALUES(1,7,'release_asc','tags');
 INSERT INTO account_watchlist_items(account_id,public_movie_id,added_at) VALUES(1,10,'2026-09-01T12:00:00.123456Z');
 INSERT INTO account_watchlist_tags(account_id,id,name,name_key,color) OVERRIDING SYSTEM VALUE VALUES(1,100,'Keep','keep','blue');
 INSERT INTO account_watchlist_item_tags VALUES(1,10,100);
 UPDATE account_watchlist_state SET filter_tag_id=100`)
			snapshot := func() string {
				t.Helper()
				var data string
				if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM accounts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM public_movies t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id) FROM account_watchlist_state t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,public_movie_id) FROM account_watchlist_items t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,id) FROM account_watchlist_tags t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,public_movie_id,tag_id) FROM account_watchlist_item_tags t))::text`).Scan(&data); err != nil {
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
				t.Fatal("migration changed existing owner/catalog/watchlist data")
			}
			var n int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watched_state)+(SELECT count(*) FROM account_watched_items)`).Scan(&n); err != nil || n != 0 {
				t.Fatal("backfilled private state", err)
			}
			exec(`INSERT INTO account_watched_state(account_id,revision) VALUES(1,1); INSERT INTO account_watched_items(account_id,public_movie_id) VALUES(1,10)`)
			var valid bool
			if err := pool.QueryRow(ctx, `SELECT revision=1 AND default_visibility='hide' AND search_visibility='inherit' AND planning_visibility='inherit' AND films_visibility='inherit' AND upcoming_visibility='inherit' AND home_visibility='inherit' AND cinema_city_visibility='inherit' AND NOT auto_remove_watchlist FROM account_watched_state`).Scan(&valid); err != nil || !valid {
				t.Fatal("defaults", err)
			}
			if err := pool.QueryRow(ctx, `SELECT watched_at IS NOT NULL FROM account_watched_items`).Scan(&valid); err != nil || !valid {
				t.Fatal("marking instant missing", err)
			}
			for _, field := range []string{"default_visibility", "search_visibility", "planning_visibility", "films_visibility", "upcoming_visibility", "home_visibility", "cinema_city_visibility"} {
				for _, value := range []any{nil, "", "SHOW", "show ", "auto"} {
					if _, err := pool.Exec(ctx, "UPDATE account_watched_state SET "+field+"=$1", value); err == nil {
						t.Fatal("invalid enum accepted", field)
					}
				}
				if field == "default_visibility" {
					if _, err := pool.Exec(ctx, "UPDATE account_watched_state SET default_visibility='inherit'"); err == nil {
						t.Fatal("inherit default accepted")
					}
				}
			}
			for _, revision := range []any{nil, int64(0), int64(-1), int64(9007199254740992)} {
				if _, err := pool.Exec(ctx, `UPDATE account_watched_state SET revision=$1`, revision); err == nil {
					t.Fatal("invalid revision accepted")
				}
			}
			for _, sql := range []string{
				`INSERT INTO account_watched_state(account_id,revision) VALUES(1,2)`,
				`INSERT INTO account_watched_state(account_id,revision) VALUES(999,1)`,
				`INSERT INTO account_watched_items(account_id,public_movie_id) VALUES(1,10)`,
				`INSERT INTO account_watched_items(account_id,public_movie_id) VALUES(999,10)`,
				`INSERT INTO account_watched_items(account_id,public_movie_id) VALUES(1,999)`,
				`UPDATE account_watched_state SET auto_remove_watchlist=NULL`,
				`UPDATE account_watched_items SET watched_at=NULL`,
				`DELETE FROM public_movies WHERE id=10`,
			} {
				if _, err := pool.Exec(ctx, sql); err == nil {
					t.Fatal("constraint missing", sql)
				}
			}
			exec(`UPDATE account_watched_state SET revision=9007199254740991,default_visibility='show',auto_remove_watchlist=true; INSERT INTO account_watched_items(account_id,public_movie_id) VALUES(2,10)`)
			for range 2 {
				if err := RunMigrations(ctx, pool); err != nil {
					t.Fatal(err)
				}
			}
			if err := pool.QueryRow(ctx, `SELECT revision=9007199254740991 AND default_visibility='show' AND auto_remove_watchlist FROM account_watched_state`).Scan(&valid); err != nil || !valid {
				t.Fatal("rerun changed watched state", err)
			}
			exec(`DELETE FROM accounts`)
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watched_state)+(SELECT count(*) FROM account_watched_items)`).Scan(&n); err != nil || n != 0 {
				t.Fatal("watched cascade failed", err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM public_movies`).Scan(&n); err != nil || n != 1 {
				t.Fatal("cascade removed catalog", err)
			}
		})
	}
}
