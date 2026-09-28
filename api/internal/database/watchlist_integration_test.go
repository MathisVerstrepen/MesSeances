package database

import "testing"

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
