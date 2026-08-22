package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationsIntegration(t *testing.T) {
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
	if err != nil || len(migrations) != 11 {
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
	if _, err := pool.Exec(ctx, `INSERT INTO schedule_snapshot (version, schema_version, provider, scope, generated_at, timezone, window_from, window_through)
VALUES (1,1,'ugc','all_cinemas',$1,'Europe/Paris','2026-08-16','2026-08-23')`, databaseNow); err != nil {
		t.Fatal("insert pre-migration snapshot failed")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO movies (provider_id, slug, title, runtime_minutes) VALUES ('10','ugc-film-10','Marathon',600)"); err != nil {
		t.Fatal("insert pre-migration movie failed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ('ugc','10','tmdb','unmatched','marathon',600,'[]',$1,$1,$1)`, databaseNow); err != nil {
		t.Fatal("insert pre-migration match failed")
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("run pending migrations failed")
	}
	for table, column := range map[string]string{
		"movies":               "runtime_minutes",
		"movie_matches":        "source_runtime_minutes",
		"movie_metadata_cache": "runtime_minutes",
	} {
		var dataType, nullable string
		if err := pool.QueryRow(ctx, `SELECT data_type, is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`, table, column).Scan(&dataType, &nullable); err != nil || dataType != "integer" || nullable != "NO" {
			t.Fatalf("%s.%s type=%q nullable=%q err=%v", table, column, dataType, nullable, err)
		}
	}
	for _, constraint := range []string{
		"movies_runtime_minutes_positive_check",
		"movie_matches_source_runtime_minutes_positive_check",
		"movie_metadata_cache_runtime_minutes_positive_check",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND conname=$1)`, constraint).Scan(&exists); err != nil || !exists {
			t.Fatalf("constraint %q exists=%t err=%v", constraint, exists, err)
		}
	}
	updates := []string{
		"UPDATE movies SET runtime_minutes=%d WHERE provider_id='10'",
		"UPDATE movie_matches SET source_runtime_minutes=%d WHERE source_movie_id='10'",
		"UPDATE movie_metadata_cache SET runtime_minutes=%d WHERE provider_movie_id=41",
	}
	for _, statement := range updates {
		if _, err := pool.Exec(ctx, fmt.Sprintf(statement, 721)); err != nil {
			t.Fatalf("marathon runtime rejected by %s: %v", statement, err)
		}
	}
	for _, runtime := range []int{0, -1} {
		for _, statement := range updates {
			query := fmt.Sprintf(statement, runtime)
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatalf("invalid runtime accepted: %s", query)
			}
		}
	}
	for _, query := range []string{
		"SELECT runtime_minutes FROM movies WHERE provider_id='10'",
		"SELECT source_runtime_minutes FROM movie_matches WHERE source_movie_id='10'",
		"SELECT runtime_minutes FROM movie_metadata_cache WHERE provider_movie_id=41",
	} {
		var runtime int
		if err := pool.QueryRow(ctx, query).Scan(&runtime); err != nil || runtime != 721 {
			t.Fatalf("marathon runtime not preserved by %s: runtime=%d err=%v", query, runtime, err)
		}
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
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movieflow_schema_migrations").Scan(&migrationCount); err != nil || migrationCount != 11 {
		t.Fatalf("migration count=%d err=%v", migrationCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT refresh_after FROM movie_metadata_cache WHERE provider_movie_id=42").Scan(&repeatedRefresh); err != nil || !repeatedRefresh.Equal(staleRefresh) {
		t.Fatalf("repeat run changed stale refresh=%s err=%v", repeatedRefresh, err)
	}
}

func TestScheduleGenerationMigrationRejectsOrphanRowsIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate schema nonce failed")
	}
	schema := "movieflow_orphan_migration_test_" + hex.EncodeToString(nonce)
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE")
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	migrations, err := embeddedMigrations()
	if err != nil || len(migrations) != 11 {
		t.Fatalf("migrations=%d err=%v", len(migrations), err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range migrations[:9] {
		if _, err := pool.Exec(ctx, migration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply migration %d failed: %v", migration.version, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO movieflow_schema_migrations (version,name) VALUES ($1,$2)`, migration.version, migration.name); err != nil {
			t.Fatal("record migration failed")
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movies (provider_id,slug,title,runtime_minutes,provider) VALUES ('10','ugc-film-10','Orphan',90,'ugc')`); err != nil {
		t.Fatal("insert orphan schedule row failed")
	}
	if err := RunMigrations(ctx, pool); err == nil {
		t.Fatal("orphan schedule migration succeeded")
	}
	var recorded, generationColumns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=10`).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("migration recorded=%d err=%v", recorded, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND column_name='generation_id'`).Scan(&generationColumns); err != nil || generationColumns != 0 {
		t.Fatalf("generation columns after rollback=%d err=%v", generationColumns, err)
	}
}
