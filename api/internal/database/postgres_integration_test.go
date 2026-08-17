package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigration004Integration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate test schema nonce failed")
	}
	schema := "movieflow_migration_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		if !strings.HasPrefix(schema, "movieflow_migration_test_") {
			t.Error("unsafe integration schema cleanup rejected")
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("drop integration schema failed")
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	var currentSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("isolated schema assertion failed: schema=%q err=%v", currentSchema, err)
	}

	migrations, err := embeddedMigrations()
	if err != nil || len(migrations) != 8 {
		t.Fatalf("embedded migrations=%d err=%v", len(migrations), err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range migrations[:3] {
		if _, err := pool.Exec(ctx, migration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply fixture migration %d failed: %v", migration.version, err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO movieflow_schema_migrations (version, name) VALUES ($1, $2)", migration.version, migration.name); err != nil {
			t.Fatal("record fixture migration failed")
		}
	}

	var databaseNow time.Time
	if err := pool.QueryRow(ctx, "SELECT CURRENT_TIMESTAMP").Scan(&databaseNow); err != nil {
		t.Fatal("read database time failed")
	}
	fetchedAt := databaseNow.Add(-48 * time.Hour)
	futureRefresh := databaseNow.Add(24 * time.Hour)
	staleRefresh := databaseNow.Add(-24 * time.Hour)
	for id, refresh := range map[int64]time.Time{41: futureRefresh, 42: staleRefresh} {
		if _, err := pool.Exec(ctx, `INSERT INTO movie_metadata_cache (provider, provider_movie_id, locale, provider_title, localized_title, overview, poster_url, runtime_minutes, genres, fetched_at, refresh_after)
VALUES ('tmdb',$1,'fr-FR','Original','Localisé','Résumé','https://image.tmdb.org/t/p/w500/a.jpg',90,ARRAY['Drame'],$2,$3)`, id, fetchedAt, refresh); err != nil {
			t.Fatal("insert pre-migration metadata failed")
		}
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("run migration 004 failed")
	}
	var freshAfter, staleAfter, appliedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT refresh_after FROM movie_metadata_cache WHERE provider_movie_id=41").Scan(&freshAfter); err != nil {
		t.Fatal("read refreshed future row failed")
	}
	if err := pool.QueryRow(ctx, "SELECT refresh_after FROM movie_metadata_cache WHERE provider_movie_id=42").Scan(&staleAfter); err != nil {
		t.Fatal("read stale row failed")
	}
	if err := pool.QueryRow(ctx, "SELECT applied_at FROM movieflow_schema_migrations WHERE version=4 AND name='004_movie_backdrop.sql'").Scan(&appliedAt); err != nil {
		t.Fatal("migration 004 was not recorded")
	}
	if !freshAfter.Equal(appliedAt) || !staleAfter.Equal(staleRefresh) {
		t.Fatalf("refresh eligibility future=%s applied=%s stale=%s want-stale=%s", freshAfter, appliedAt, staleAfter, staleRefresh)
	}
	var backdrop *string
	var overview, poster string
	var preservedFetched time.Time
	if err := pool.QueryRow(ctx, "SELECT backdrop_url, overview, poster_url, fetched_at FROM movie_metadata_cache WHERE provider_movie_id=41").Scan(&backdrop, &overview, &poster, &preservedFetched); err != nil {
		t.Fatal("read migrated metadata failed")
	}
	if backdrop != nil || overview != "Résumé" || poster != "https://image.tmdb.org/t/p/w500/a.jpg" || !preservedFetched.Equal(fetchedAt) {
		t.Fatalf("metadata changed backdrop=%v overview=%q poster=%q fetched=%s", backdrop, overview, poster, preservedFetched)
	}
	var enrichmentVersion int64
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&enrichmentVersion); err != nil || enrichmentVersion != 0 {
		t.Fatalf("enrichment version=%d err=%v", enrichmentVersion, err)
	}

	valid := "https://image.tmdb.org/t/p/w780/backdrop.jpg"
	if _, err := pool.Exec(ctx, "UPDATE movie_metadata_cache SET backdrop_url=$1 WHERE provider_movie_id=41", valid); err != nil {
		t.Fatalf("valid backdrop rejected: %v", err)
	}
	for _, invalid := range []string{
		"http://image.tmdb.org/t/p/w780/a.jpg",
		"https://evil.example/t/p/w780/a.jpg",
		"https://image.tmdb.org:443/t/p/w780/a.jpg",
		"https://user@image.tmdb.org/t/p/w780/a.jpg",
		"https://image.tmdb.org/t/p/w500/a.jpg",
		"https://image.tmdb.org/t/p/w780/",
		"https://image.tmdb.org/t/p/w780//a.jpg",
		"https://image.tmdb.org/t/p/w780/../a.jpg",
		"https://image.tmdb.org/t/p/w780/%2e%2e/a.jpg",
		"https://image.tmdb.org/t/p/w780/a\\b.jpg",
		"https://image.tmdb.org/t/p/w780/a.jpg?x=1",
		"https://image.tmdb.org/t/p/w780/a.jpg#x",
	} {
		if _, err := pool.Exec(ctx, "UPDATE movie_metadata_cache SET backdrop_url=$1 WHERE provider_movie_id=42", invalid); err == nil {
			t.Fatalf("invalid backdrop accepted: %q", invalid)
		}
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("repeat migration run failed")
	}
	var migrationCount int
	var repeatedRefresh time.Time
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movieflow_schema_migrations").Scan(&migrationCount); err != nil || migrationCount != 8 {
		t.Fatalf("migration count=%d err=%v", migrationCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT refresh_after FROM movie_metadata_cache WHERE provider_movie_id=42").Scan(&repeatedRefresh); err != nil || !repeatedRefresh.Equal(staleRefresh) {
		t.Fatalf("repeat run changed stale refresh=%s err=%v", repeatedRefresh, err)
	}
}
