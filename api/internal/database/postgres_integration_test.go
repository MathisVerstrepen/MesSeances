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

	"messeances/api/internal/publicmoviepg"
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
	if err != nil || len(migrations) != 14 {
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
	var publicMovieCount, publicSourceCount, publicAliasCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public_movies WHERE redirect_to_id IS NULL").Scan(&publicMovieCount); err != nil || publicMovieCount != 1 {
		t.Fatalf("backfilled public movies=%d err=%v", publicMovieCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='10'").Scan(&publicSourceCount); err != nil || publicSourceCount != 1 {
		t.Fatalf("backfilled public sources=%d err=%v", publicSourceCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movie_slug_aliases WHERE slug='ugc-film-10' AND alias_kind='source'").Scan(&publicAliasCount); err != nil || publicAliasCount != 1 {
		t.Fatalf("backfilled source aliases=%d err=%v", publicAliasCount, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE schedule_snapshot SET window_through='2026-09-30' WHERE singleton=true"); err != nil {
		t.Fatalf("long combined window rejected after migration: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE provider_snapshots SET window_through='2026-09-30' WHERE provider='ugc'"); err != nil {
		t.Fatalf("long provider window rejected after migration: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE schedule_snapshot SET window_through=window_from-1 WHERE singleton=true"); err == nil {
		t.Fatal("reverse combined window accepted after migration")
	}
	if _, err := pool.Exec(ctx, "UPDATE provider_snapshots SET window_through=window_from-1 WHERE provider='ugc'"); err == nil {
		t.Fatal("reverse provider window accepted after migration")
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
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movieflow_schema_migrations").Scan(&migrationCount); err != nil || migrationCount != 14 {
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
	if err != nil || len(migrations) != 14 {
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

func TestPublicMovieCatalogBackfillIntegration(t *testing.T) {
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
	schema := "movieflow_catalog_migration_test_" + hex.EncodeToString(nonce)
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
	if err != nil || len(migrations) != 14 {
		t.Fatalf("embedded migrations=%d err=%v", len(migrations), err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range migrations[:13] {
		if _, err := pool.Exec(ctx, migration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply fixture migration %d failed: %v", migration.version, err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO movieflow_schema_migrations (version,name) VALUES ($1,$2)", migration.version, migration.name); err != nil {
			t.Fatal("record fixture migration failed")
		}
	}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO schedule_snapshot
    (version,schema_version,provider,scope,generated_at,timezone,window_from,window_through)
VALUES (1,1,'combined','all_cinemas',$1,'Europe/Paris','2026-08-23','2026-08-24');
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES
    (1,'ugc','1','ugc-film-1','Même titre',90),
    (1,'kinepolis','A','kinepolis-film-A','Même titre',90),
    (1,'ugc','2','ugc-film-2','TMDB UGC',91),
    (1,'kinepolis','B','kinepolis-film-B','TMDB Kinepolis',92),
    (1,'ugc','3','ugc-film-3','Local UGC',93),
    (1,'kinepolis','C','kinepolis-film-C','Local principal',94),
    (1,'ugc','4','ugc-film-4','Local avec principal inactif',96);
INSERT INTO movie_metadata_cache
    (provider,provider_movie_id,locale,provider_title,localized_title,runtime_minutes,genres,fetched_at,refresh_after)
VALUES ('tmdb',42,'fr-FR','TMDB','Canonique TMDB',95,ARRAY['Drame'],$1,$2);
INSERT INTO movie_matches
    (source_provider,source_movie_id,metadata_provider,status,metadata_movie_id,score,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at)
VALUES
    ('ugc','2','tmdb','matched',42,1,'tmdb ugc',91,'[]',$1,$2,$1),
    ('kinepolis','B','tmdb','matched',42,1,'tmdb kinepolis',92,'[]',$1,$2,$1);`, pgx.QueryExecModeSimpleProtocol, now, now.Add(24*time.Hour)); err != nil {
		t.Fatal("insert catalog backfill fixture failed")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin local group fixture failed")
	}
	var localID, inactivePrimaryLocalID int64
	if err := tx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider,primary_source_movie_id) VALUES ('kinepolis','C') RETURNING id").Scan(&localID); err != nil {
		t.Fatal("insert local group failed")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id,source_provider,source_movie_id) VALUES ($1,'ugc','3'),($1,'kinepolis','C')", localID); err != nil {
		t.Fatal("insert local members failed")
	}
	if err := tx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider,primary_source_movie_id) VALUES ('kinepolis','D') RETURNING id").Scan(&inactivePrimaryLocalID); err != nil {
		t.Fatal("insert inactive-primary local group failed")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id,source_provider,source_movie_id) VALUES ($1,'ugc','4'),($1,'kinepolis','D')", inactivePrimaryLocalID); err != nil {
		t.Fatal("insert inactive-primary local members failed")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit local group failed")
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("run catalog migration failed")
	}
	var activeCount, aliasCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public_movies WHERE redirect_to_id IS NULL").Scan(&activeCount); err != nil || activeCount != 5 {
		t.Fatalf("strict backfill components=%d err=%v", activeCount, err)
	}
	var singletonUGC, singletonKinepolis, tmdbUGC, tmdbKinepolis, localUGC, localKinepolis int64
	if err := pool.QueryRow(ctx, `SELECT
    max(public_movie_id) FILTER (WHERE source_provider='ugc' AND source_movie_id='1'),
    max(public_movie_id) FILTER (WHERE source_provider='kinepolis' AND source_movie_id='A'),
    max(public_movie_id) FILTER (WHERE source_provider='ugc' AND source_movie_id='2'),
    max(public_movie_id) FILTER (WHERE source_provider='kinepolis' AND source_movie_id='B'),
    max(public_movie_id) FILTER (WHERE source_provider='ugc' AND source_movie_id='3'),
    max(public_movie_id) FILTER (WHERE source_provider='kinepolis' AND source_movie_id='C')
FROM public_movie_sources`).Scan(&singletonUGC, &singletonKinepolis, &tmdbUGC, &tmdbKinepolis, &localUGC, &localKinepolis); err != nil {
		t.Fatal("read backfilled source mappings failed")
	}
	if singletonUGC == singletonKinepolis || tmdbUGC != tmdbKinepolis || localUGC != localKinepolis {
		t.Fatalf("backfill mappings singleton=%d/%d tmdb=%d/%d local=%d/%d", singletonUGC, singletonKinepolis, tmdbUGC, tmdbKinepolis, localUGC, localKinepolis)
	}
	var title, anchorProvider, anchorID string
	if err := pool.QueryRow(ctx, "SELECT title FROM public_movies WHERE id=$1", tmdbUGC).Scan(&title); err != nil || title != "Canonique TMDB" {
		t.Fatalf("TMDB canonical title=%q err=%v", title, err)
	}
	if err := pool.QueryRow(ctx, "SELECT identity_anchor_provider,identity_anchor_source_movie_id FROM public_movies WHERE id=$1", localUGC).Scan(&anchorProvider, &anchorID); err != nil || anchorProvider != "kinepolis" || anchorID != "C" {
		t.Fatalf("local anchor=%s/%s err=%v", anchorProvider, anchorID, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movie_slug_aliases").Scan(&aliasCount); err != nil || aliasCount != 10 {
		t.Fatalf("backfilled aliases=%d err=%v", aliasCount, err)
	}
	var inactiveAnchorPublicID int64
	if err := pool.QueryRow(ctx, `SELECT source.public_movie_id
FROM public_movie_sources source
JOIN public_movies movie ON movie.id=source.public_movie_id
WHERE source.source_provider='ugc' AND source.source_movie_id='4'
  AND movie.identity_anchor_provider='kinepolis' AND movie.identity_anchor_source_movie_id='D'`).Scan(&inactiveAnchorPublicID); err != nil || inactiveAnchorPublicID <= 0 {
		t.Fatalf("inactive selected-primary anchor ID=%d err=%v", inactiveAnchorPublicID, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE public_movies SET genres=ARRAY[' '] WHERE id=$1", inactiveAnchorPublicID); err == nil {
		t.Fatal("blank canonical genre accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE public_movie_sources SET genres=ARRAY[repeat('x',257)] WHERE source_provider='ugc' AND source_movie_id='4'"); err == nil {
		t.Fatal("oversized durable source genre accepted")
	}

	reconcile := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM local_movie_groups WHERE id=$1", inactivePrimaryLocalID); err != nil {
		t.Fatal("unmerge inactive-primary group failed")
	}
	if err := reconcile(); err != nil {
		t.Fatalf("reconcile inactive-primary unmerge failed: %v", err)
	}
	var secondaryAfterSplit, oldRedirect int64
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='4'").Scan(&secondaryAfterSplit); err != nil || secondaryAfterSplit <= 0 || secondaryAfterSplit == inactiveAnchorPublicID {
		t.Fatalf("inactive-primary split secondary=%d old=%d err=%v", secondaryAfterSplit, inactiveAnchorPublicID, err)
	}
	if err := pool.QueryRow(ctx, "SELECT COALESCE(redirect_to_id,0) FROM public_movies WHERE id=$1", inactiveAnchorPublicID).Scan(&oldRedirect); err != nil || oldRedirect != 0 {
		t.Fatalf("inactive anchor was tombstoned redirect=%d err=%v", oldRedirect, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movies
    (generation_id,provider,provider_id,slug,title,runtime_minutes)
VALUES (1,'kinepolis','D','kinepolis-film-D','Principal revenu',97)`); err != nil {
		t.Fatal("reinsert selected primary source failed")
	}
	if err := reconcile(); err != nil {
		t.Fatalf("reconcile selected-primary reappearance failed: %v", err)
	}
	var reappearedPublicID int64
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM public_movie_sources WHERE source_provider='kinepolis' AND source_movie_id='D'").Scan(&reappearedPublicID); err != nil || reappearedPublicID != inactiveAnchorPublicID {
		t.Fatalf("reappeared primary ID=%d want=%d err=%v", reappearedPublicID, inactiveAnchorPublicID, err)
	}
}
