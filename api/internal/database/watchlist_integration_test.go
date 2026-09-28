package database

import "testing"

func TestWatchlistReleaseMigrationIntegration(t *testing.T) {
	for _, prefix := range []int{0, 50} {
		t.Run(map[int]string{0: "fresh", 50: "upgrade"}[prefix], func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "watchlist_release_")
			if prefix != 0 {
				installMigrationPrefix(t, ctx, pool, 50, "050_account_watchlist.sql")
			}
			for range 2 {
				if err := RunMigrations(ctx, pool); err != nil {
					t.Fatal(err)
				}
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM tmdb_french_release_cache`).Scan(&count); err != nil || count != 0 {
				t.Fatal("nonempty cache", err)
			}
			for _, values := range []string{
				`(0,NULL,'[]',NULL,now(),1)`,
				`(1,NULL,'{}',NULL,now(),1)`,
				`(1,NULL,(SELECT jsonb_agg(n) FROM generate_series(1,65)n),now(),now(),1)`,
				`(1,'1998-10-14','[]',NULL,now(),1)`,
				`(1,NULL,'[{}]',NULL,now(),1)`,
				`(1,NULL,'[]',NULL,now(),0)`,
				`(1,NULL,'[]',NULL,now(),9007199254740992)`,
			} {
				if _, err := pool.Exec(ctx, `INSERT INTO tmdb_french_release_cache(tmdb_id,french_release_date,french_releases,verified_at,retry_after,attempt_revision) VALUES `+values); err == nil {
					t.Fatal("invalid cache row accepted", values)
				}
			}
			if _, err := pool.Exec(ctx, `INSERT INTO tmdb_french_release_cache(tmdb_id,verified_at,retry_after,attempt_revision) VALUES(1,now(),now(),1),(2,NULL,now(),1)`); err != nil {
				t.Fatal(err)
			}
			for _, purpose := range []string{"watchlist_release_fetch", "watchlist_search", "watchlist_write", "watchlist_import", "avatar_import", "login"} {
				if _, err := pool.Exec(ctx, `INSERT INTO account_rate_limits(purpose,key_digest,window_seconds,window_start,count,expires_at) VALUES($1,decode(repeat('00',32),'hex'),60,now(),1,now()+interval '1 hour')`, purpose); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestWatchlistMigrationUpgradeAndConstraintsIntegration(t *testing.T) {
	ctx := t.Context()
	pool, _ := newMigrationTestPool(t, ctx, "watchlist_upgrade_")
	installMigrationPrefix(t, ctx, pool, 49, "049_account_theater_preferences.sql")
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
	for _, table := range []string{"account_watchlist_state", "account_watchlist_items", "tmdb_catalog_imports"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("migration did not create empty watchlist tables")
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO account_watchlist_state(account_id,revision) VALUES(999,1)`); err == nil {
		t.Fatal("account FK absent")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tmdb_catalog_imports(tmdb_id,public_movie_id,published_at) VALUES(42,999,now())`); err == nil {
		t.Fatal("public movie FK absent")
	}
	for _, purpose := range []string{"watchlist_write", "watchlist_search", "watchlist_import", "avatar_import", "login"} {
		if _, err := pool.Exec(ctx, `INSERT INTO account_rate_limits(purpose,key_digest,window_seconds,window_start,count,expires_at) VALUES($1,decode(repeat('00',32),'hex'),60,now(),1,now()+interval '1 hour')`, purpose); err != nil {
			t.Fatalf("quota purpose %s: %v", purpose, err)
		}
	}
}
