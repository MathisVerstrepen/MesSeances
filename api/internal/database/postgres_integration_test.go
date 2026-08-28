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

func mustEmbeddedMigrations(t *testing.T) []migration {
	t.Helper()
	migrations, err := embeddedMigrations()
	if err != nil {
		t.Fatalf("discover embedded migrations: %v", err)
	}
	return migrations
}

func requireMigrationPrefix(t *testing.T, migrations []migration, throughVersion int64, throughName string) []migration {
	t.Helper()
	if len(migrations) < int(throughVersion) {
		t.Fatalf("embedded migrations=%d, need prefix through %03d", len(migrations), throughVersion)
	}
	prefix := migrations[:int(throughVersion)]
	last := prefix[len(prefix)-1]
	if last.version != throughVersion || last.name != throughName {
		t.Fatalf("migration prefix ends at (%d,%q), want=(%d,%q)", last.version, last.name, throughVersion, throughName)
	}
	return prefix
}

func assertCompleteMigrationHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want []migration) {
	t.Helper()
	rows, err := pool.Query(ctx, "SELECT version, name FROM movieflow_schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("read migration history: %v", err)
	}
	defer rows.Close()

	type appliedMigration struct {
		version int64
		name    string
	}
	got := make([]appliedMigration, 0, len(want))
	for rows.Next() {
		var item appliedMigration
		if err := rows.Scan(&item.version, &item.name); err != nil {
			t.Fatalf("scan migration history: %v", err)
		}
		got = append(got, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migration history: %v", err)
	}
	rows.Close()

	if len(got) != len(want) {
		t.Fatalf("migration history count=%d want=%d", len(got), len(want))
	}
	for i, item := range got {
		if item.version != want[i].version || item.name != want[i].name {
			t.Fatalf("migration history[%d]=(%d,%q) want=(%d,%q)", i, item.version, item.name, want[i].version, want[i].name)
		}
	}
}

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

	migrations := mustEmbeddedMigrations(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range requireMigrationPrefix(t, migrations, 3, "003_admin_match_review.sql") {
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
		"movies_runtime_minutes_nonnegative_check",
		"movie_matches_source_runtime_minutes_nonnegative_check",
		"movie_metadata_cache_runtime_minutes_nonnegative_check",
		"public_movies_runtime_minutes_nonnegative_check",
		"public_movie_sources_runtime_minutes_nonnegative_check",
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
		"UPDATE public_movies SET runtime_minutes=%d WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'",
		"UPDATE public_movie_sources SET runtime_minutes=%d WHERE source_provider='ugc' AND source_movie_id='10'",
	}
	for _, statement := range updates {
		if _, err := pool.Exec(ctx, fmt.Sprintf(statement, 721)); err != nil {
			t.Fatalf("marathon runtime rejected by %s: %v", statement, err)
		}
	}
	for _, statement := range updates {
		query := fmt.Sprintf(statement, 0)
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatalf("unknown runtime rejected: %s: %v", query, err)
		}
	}
	for _, statement := range updates {
		query := fmt.Sprintf(statement, -1)
		if _, err := pool.Exec(ctx, query); err == nil {
			t.Fatalf("negative runtime accepted: %s", query)
		}
	}
	for _, statement := range updates {
		query := fmt.Sprintf(statement, 721)
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatalf("marathon runtime restore rejected by %s: %v", statement, err)
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
	if _, err := pool.Exec(ctx, "UPDATE public_movies SET trailer_vf_youtube_key=$1 WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'", "FRoff123456"); err == nil {
		t.Fatal("public trailer YouTube key without confirmed TMDB identity accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE public_movies SET confirmed_tmdb_id=41 WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'"); err != nil {
		t.Fatalf("set public movie TMDB identity failed: %v", err)
	}
	for _, check := range []struct {
		statement string
		reset     string
	}{
		{statement: "UPDATE movie_metadata_cache SET trailer_vf_youtube_key=$1 WHERE provider_movie_id=41", reset: "UPDATE movie_metadata_cache SET trailer_vf_youtube_key=NULL WHERE provider_movie_id=41"},
		{statement: "UPDATE movie_metadata_cache SET trailer_vo_youtube_key=$1 WHERE provider_movie_id=41", reset: "UPDATE movie_metadata_cache SET trailer_vo_youtube_key=NULL WHERE provider_movie_id=41"},
		{statement: "UPDATE public_movies SET trailer_vf_youtube_key=$1 WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'", reset: "UPDATE public_movies SET trailer_vf_youtube_key=NULL WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'"},
		{statement: "UPDATE public_movies SET trailer_vo_youtube_key=$1 WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'", reset: "UPDATE public_movies SET trailer_vo_youtube_key=NULL WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'"},
	} {
		if _, err := pool.Exec(ctx, check.statement, "FRoff123456"); err != nil {
			t.Fatalf("valid trailer YouTube key rejected by %s: %v", check.statement, err)
		}
		for _, invalid := range []string{"short", "FRoff12345!", "FRoff1234567"} {
			if _, err := pool.Exec(ctx, check.statement, invalid); err == nil {
				t.Fatalf("invalid trailer YouTube key accepted by %s: %q", check.statement, invalid)
			}
		}
		if _, err := pool.Exec(ctx, check.reset); err != nil {
			t.Fatalf("reset trailer key after %s: %v", check.statement, err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE movie_metadata_cache SET trailer_vf_youtube_key='FRoff123456', trailer_vo_youtube_key='FRoff123456' WHERE provider_movie_id=41"); err == nil {
		t.Fatal("duplicate metadata trailer variants accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE public_movies SET trailer_vf_youtube_key='FRoff123456', trailer_vo_youtube_key='FRoff123456' WHERE identity_anchor_provider='ugc' AND identity_anchor_source_movie_id='10'"); err == nil {
		t.Fatal("duplicate public trailer variants accepted")
	}
	if _, err := pool.Exec(ctx, "SELECT trailer_youtube_key FROM movie_metadata_cache LIMIT 1"); err == nil {
		t.Fatal("legacy metadata trailer column still exists")
	}
	if _, err := pool.Exec(ctx, "SELECT trailer_youtube_key FROM public_movies LIMIT 1"); err == nil {
		t.Fatal("legacy public trailer column still exists")
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("repeat migration run failed")
	}
	var repeatedRefresh time.Time
	assertCompleteMigrationHistory(t, ctx, pool, migrations)
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
	migrations := mustEmbeddedMigrations(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range requireMigrationPrefix(t, migrations, 9, "009_widen_runtime_minutes.sql") {
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
	migrations := mustEmbeddedMigrations(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range requireMigrationPrefix(t, migrations, 13, "013_unbounded_schedule_windows.sql") {
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

func TestSyncSchedulesMigrationIntegration(t *testing.T) {
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
	schema := "movieflow_sync_schedule_migration_test_" + hex.EncodeToString(nonce)
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
	migrations := mustEmbeddedMigrations(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range requireMigrationPrefix(t, migrations, 14, "014_public_movie_catalog.sql") {
		if _, err := pool.Exec(ctx, migration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply fixture migration %d failed: %v", migration.version, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO movieflow_schema_migrations (version,name) VALUES ($1,$2)`, migration.version, migration.name); err != nil {
			t.Fatal("record fixture migration failed")
		}
	}
	var oldRunID int64
	if err := pool.QueryRow(ctx, `INSERT INTO sync_runs
        (target,state,started_at,finished_at,window_from,window_through,providers)
        VALUES ('ugc','failed','2026-08-24T08:00:00Z','2026-08-24T08:01:00Z','2026-08-24','2026-08-24','{}') RETURNING id`).Scan(&oldRunID); err != nil {
		t.Fatal("insert pre-015 sync run failed")
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("run migration 015 failed")
	}
	var trigger string
	var revision *int64
	var scheduledFor *time.Time
	var attempt *int16
	if err := pool.QueryRow(ctx, `SELECT trigger_source,schedule_revision,scheduled_for,schedule_attempt FROM sync_runs WHERE id=$1`, oldRunID).Scan(&trigger, &revision, &scheduledFor, &attempt); err != nil || trigger != "manual" || revision != nil || scheduledFor != nil || attempt != nil {
		t.Fatalf("old run backfill trigger=%q revision=%v scheduled=%v attempt=%v err=%v", trigger, revision, scheduledFor, attempt, err)
	}
	var scheduleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules`).Scan(&scheduleCount); err != nil || scheduleCount != 0 {
		t.Fatalf("initial schedules=%d err=%v", scheduleCount, err)
	}
	var defaultRevision int64
	if err := pool.QueryRow(ctx, `INSERT INTO sync_schedules (provider,enabled,schedule_kind,local_time) VALUES ('ugc',false,'daily','08:05') RETURNING revision`).Scan(&defaultRevision); err != nil || defaultRevision != 1 {
		t.Fatalf("daily default revision=%d err=%v", defaultRevision, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sync_schedules (provider,revision,enabled,schedule_kind,local_time,weekdays) VALUES ('kinepolis',2,true,'weekly','23:59',ARRAY['mon','sun'])`); err != nil {
		t.Fatalf("valid weekly schedule rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sync_schedules SET schedule_kind='cron',local_time=NULL,weekdays=NULL,cron_expression='5 4 * * *' WHERE provider='kinepolis'`); err != nil {
		t.Fatalf("valid cron schedule rejected: %v", err)
	}
	invalidSchedules := []string{
		`INSERT INTO sync_schedules (provider,enabled,schedule_kind,local_time) VALUES ('all',true,'daily','08:00')`,
		`INSERT INTO sync_schedules (provider,revision,enabled,schedule_kind,local_time) VALUES ('ugc',0,true,'daily','08:00') ON CONFLICT (provider) DO UPDATE SET revision=excluded.revision`,
		`UPDATE sync_schedules SET local_time='24:00' WHERE provider='ugc'`,
		`UPDATE sync_schedules SET weekdays=ARRAY['mon'] WHERE provider='ugc'`,
		`UPDATE sync_schedules SET schedule_kind='weekly',weekdays=ARRAY[]::text[] WHERE provider='ugc'`,
		`UPDATE sync_schedules SET schedule_kind='weekly',weekdays=ARRAY['noday'] WHERE provider='ugc'`,
		`UPDATE sync_schedules SET schedule_kind='cron',local_time=NULL,cron_expression='' WHERE provider='ugc'`,
		`UPDATE sync_schedules SET schedule_kind='cron',local_time=NULL,cron_expression=repeat('x',256) WHERE provider='ugc'`,
	}
	for _, query := range invalidSchedules {
		if _, err := pool.Exec(ctx, query); err == nil {
			t.Fatalf("invalid schedule accepted: %s", query)
		}
	}

	baseRun := `INSERT INTO sync_runs
        (target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_revision,scheduled_for,schedule_attempt)
        VALUES ($1,'failed','2026-08-24T09:00:00Z','2026-08-24T09:01:00Z','2026-08-24','2026-08-24','{}',$2,$3,$4,$5)`
	if _, err := pool.Exec(ctx, baseRun, "ugc", "scheduled", 1, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), 0); err != nil {
		t.Fatalf("valid scheduled base run rejected: %v", err)
	}
	for _, retry := range []int{1, 2} {
		if _, err := pool.Exec(ctx, baseRun, "ugc", "scheduled", 1, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), retry); err != nil {
			t.Fatalf("valid scheduled retry %d rejected: %v", retry, err)
		}
	}
	if _, err := pool.Exec(ctx, baseRun, "ugc", "scheduled", 1, time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), 0); err != nil {
		t.Fatalf("distinct fall-back UTC occurrence rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, baseRun, "ugc", "scheduled", 2, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), 0); err != nil {
		t.Fatalf("distinct revision occurrence rejected: %v", err)
	}
	invalidRuns := []struct {
		target   string
		trigger  string
		revision any
		at       any
		attempt  any
	}{
		{target: "ugc", trigger: "manual", revision: 1, at: time.Now(), attempt: 0},
		{target: "all", trigger: "scheduled", revision: 1, at: time.Now(), attempt: 0},
		{target: "ugc", trigger: "scheduled", revision: 0, at: time.Now(), attempt: 0},
		{target: "ugc", trigger: "scheduled", revision: 1, at: nil, attempt: 0},
		{target: "ugc", trigger: "scheduled", revision: 1, at: time.Now(), attempt: 3},
		{target: "ugc", trigger: "other", revision: nil, at: nil, attempt: nil},
	}
	for _, invalid := range invalidRuns {
		if _, err := pool.Exec(ctx, baseRun, invalid.target, invalid.trigger, invalid.revision, invalid.at, invalid.attempt); err == nil {
			t.Fatalf("invalid run accepted: %+v", invalid)
		}
	}
	if _, err := pool.Exec(ctx, baseRun, "ugc", "scheduled", 1, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), 0); err == nil {
		t.Fatal("duplicate scheduled attempt accepted")
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("repeat migration run failed")
	}
	var migrationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=15 AND name='015_sync_schedules.sql'`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("migration 015 bookkeeping count=%d err=%v", migrationCount, err)
	}
	var oldRunCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_runs WHERE id=$1 AND trigger_source='manual' AND schedule_revision IS NULL AND scheduled_for IS NULL AND schedule_attempt IS NULL`, oldRunID).Scan(&oldRunCount); err != nil || oldRunCount != 1 {
		t.Fatalf("repeat migration changed old run count=%d err=%v", oldRunCount, err)
	}
}

func TestPatheProviderMigrationIntegration(t *testing.T) {
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
	schema := "movieflow_pathe_migration_test_" + hex.EncodeToString(nonce)
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
	migrations := mustEmbeddedMigrations(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for _, migration := range requireMigrationPrefix(t, migrations, 15, "015_sync_schedules.sql") {
		if _, err := pool.Exec(ctx, migration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply fixture migration %d failed: %v", migration.version, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO movieflow_schema_migrations (version,name) VALUES ($1,$2)`, migration.version, migration.name); err != nil {
			t.Fatal("record fixture migration failed")
		}
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
INSERT INTO schedule_snapshot (version,schema_version,provider,scope,generated_at,timezone,window_from,window_through) VALUES (1,1,'combined','all_cinemas',$1,'Europe/Paris','2026-08-24','2026-08-24');
INSERT INTO provider_snapshots (generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES
    (1,'ugc',1,'all_cinemas',$1,'Europe/Paris','2026-08-24','2026-08-24'),
    (1,'kinepolis',1,'all_cinemas',$1,'Europe/Paris','2026-08-24','2026-08-24');
INSERT INTO passes (code) VALUES ('UGC_ILLIMITE');
INSERT INTO theaters (generation_id,id,provider_id,slug,name,address,city,postal_code,provider) VALUES
    (1,'ugc-1','1','ugc-1','UGC historique','1 rue','Lille','59000','ugc'),
    (1,'kinepolis-K','K','kinepolis-K','Kinepolis historique','','Lomme','','kinepolis');
INSERT INTO theater_dates (generation_id,theater_id,service_date) VALUES (1,'ugc-1','2026-08-24'),(1,'kinepolis-K','2026-08-24');
INSERT INTO theater_passes (generation_id,theater_id,pass_code) VALUES (1,'ugc-1','UGC_ILLIMITE');
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES
    (1,'ugc','10','ugc-film-10','Film UGC',90),
    (1,'kinepolis','K10','kinepolis-film-K10','Film Kinepolis',95);
INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES
    (1,'ugc-showing-100','100','2026-08-24','ugc-1','10','2026-08-24T18:00:00+02','2026-08-24T19:30:00+02','VF','VF','2D','1','https://www.ugc.fr/reservationSeances.html?id=100','ugc'),
    (1,'kinepolis-showing-K100','K100','2026-08-24','kinepolis-K','K10','2026-08-24T20:00:00+02','2026-08-24T21:35:00+02','VF','VF','IMAX','2','https://kinepolis.fr/direct-vista-redirect/K100/0/K/0','kinepolis');
INSERT INTO sync_schedules (provider,enabled,schedule_kind,local_time) VALUES ('ugc',false,'daily','08:00'),('kinepolis',false,'daily','09:00');
INSERT INTO sync_runs (target,state,started_at,finished_at,window_from,window_through,providers) VALUES ('all','succeeded',$1,$1,'2026-08-24','2026-08-24','{}');
`, pgx.QueryExecModeSimpleProtocol, now); err != nil {
		t.Fatalf("seed pre-016 rows failed: %v", err)
	}
	localTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin pre-016 local group failed")
	}
	var oldLocalID int64
	if err := localTx.QueryRow(ctx, `INSERT INTO local_movie_groups (primary_source_provider,primary_source_movie_id) VALUES ('ugc','10') RETURNING id`).Scan(&oldLocalID); err != nil {
		t.Fatal("insert pre-016 local group failed")
	}
	if _, err := localTx.Exec(ctx, `INSERT INTO local_movie_group_members (local_movie_id,source_provider,source_movie_id) VALUES ($1,'ugc','10'),($1,'kinepolis','K10')`, oldLocalID); err != nil {
		t.Fatal("insert pre-016 local members failed")
	}
	if err := localTx.Commit(ctx); err != nil {
		t.Fatal("commit pre-016 local group failed")
	}
	var oldPublicID int64
	if err := pool.QueryRow(ctx, `INSERT INTO public_movies (identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES ('ugc','10','Film UGC',90) RETURNING id`).Scan(&oldPublicID); err != nil {
		t.Fatal("insert pre-016 public movie failed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public_movie_sources (source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) VALUES ('ugc','10',$1,'ugc-film-10','Film UGC',90); INSERT INTO movie_slug_aliases (slug,public_movie_id,alias_kind,source_provider,source_movie_id) VALUES ('ugc-film-10',$1,'source','ugc','10')`, pgx.QueryExecModeSimpleProtocol, oldPublicID); err != nil {
		t.Fatal("insert pre-016 public source failed")
	}

	patheProviderMigration := migrations[15]
	if _, err := pool.Exec(ctx, patheProviderMigration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("run migration 016 failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movieflow_schema_migrations (version,name) VALUES ($1,$2)`, patheProviderMigration.version, patheProviderMigration.name); err != nil {
		t.Fatal("record migration 016 failed")
	}
	var oldRows int
	if err := pool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM theaters WHERE provider IN ('ugc','kinepolis')) +
    (SELECT count(*) FROM movies WHERE provider IN ('ugc','kinepolis')) +
    (SELECT count(*) FROM showtimes WHERE provider IN ('ugc','kinepolis')) +
    (SELECT count(*) FROM local_movie_groups WHERE id=$1) +
    (SELECT count(*) FROM public_movies WHERE id=$2)`, oldLocalID, oldPublicID).Scan(&oldRows); err != nil || oldRows != 8 {
		t.Fatalf("preserved pre-016 rows=%d err=%v", oldRows, err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO provider_snapshots (generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES (1,'pathe',1,'all_cinemas',$1,'Europe/Paris','2026-08-24','2026-08-24');
INSERT INTO theaters (generation_id,id,provider_id,slug,name,address,city,postal_code,provider) VALUES (1,'pathe-lille','lille','pathe-lille','Pathé Lille','1 rue','Lille','59000','pathe');
INSERT INTO theater_dates (generation_id,theater_id,service_date) VALUES (1,'pathe-lille','2026-08-24');
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES (1,'pathe','film-a','pathe-film-film-a','Film Pathé',100);
INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES (1,'pathe-showing-S135392','S135392','2026-08-24','pathe-lille','film-a','2026-08-24T21:00:00+02','2026-08-24T22:40:00+02','VF','vf','ICE','ICE','https://s.pathe.fr/fr/V3308S135392/booking','pathe');
`, pgx.QueryExecModeSimpleProtocol, now); err != nil {
		t.Fatalf("insert legacy Pathé showing row failed: %v", err)
	}
	var inboundShowtimeForeignKeys int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE contype='f' AND confrelid='showtimes'::regclass`).Scan(&inboundShowtimeForeignKeys); err != nil || inboundShowtimeForeignKeys != 0 {
		t.Fatalf("inbound showtime foreign keys=%d err=%v", inboundShowtimeForeignKeys, err)
	}
	var constraintsBefore, indexesBefore []string
	if err := pool.QueryRow(ctx, `SELECT coalesce(array_agg(conname || '=' || pg_get_constraintdef(oid) ORDER BY conname) FILTER (WHERE conname NOT IN ('showtimes_provider_identity_check','showtimes_provider_check','showtimes_language_check','showtimes_check1','showtimes_time_check')), ARRAY[]::text[]) FROM pg_constraint WHERE conrelid='showtimes'::regclass`).Scan(&constraintsBefore); err != nil {
		t.Fatal("read pre-017 showtime constraints failed")
	}
	if err := pool.QueryRow(ctx, `SELECT coalesce(array_agg(indexname || '=' || indexdef ORDER BY indexname), ARRAY[]::text[]) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='showtimes'`).Scan(&indexesBefore); err != nil {
		t.Fatal("read pre-017 showtime indexes failed")
	}
	assertMigrationRejected := func(label string) {
		t.Helper()
		if err := RunMigrations(ctx, pool); err == nil || err.Error() != "database migration 017 failed" {
			t.Fatalf("%s migration error=%v", label, err)
		}
		var migration17Count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=17`).Scan(&migration17Count); err != nil || migration17Count != 0 {
			t.Fatalf("%s failed migration recorded: count=%d err=%v", label, migration17Count, err)
		}
	}
	insertLegacyShowing := func(providerShowingID, bookingURL string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES (1,'pathe-showing-' || $1,$1,'2026-08-24','pathe-lille','film-a','2026-08-24T18:00:00+02','2026-08-24T19:40:00+02','VF','vf','ICE','1',$2,'pathe')`, providerShowingID, bookingURL); err != nil {
			t.Fatalf("insert %s legacy fixture failed", providerShowingID)
		}
	}
	assertLegacyShowing := func(providerShowingID, bookingURL string) {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM showtimes WHERE generation_id=1 AND provider='pathe' AND id='pathe-showing-' || $1 AND provider_showing_id=$1 AND booking_url=$2`, providerShowingID, bookingURL).Scan(&count); err != nil || count != 1 {
			t.Fatalf("legacy fixture %s changed: count=%d err=%v", providerShowingID, count, err)
		}
	}
	removeShowing := func(providerShowingID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `DELETE FROM showtimes WHERE generation_id=1 AND provider='pathe' AND provider_showing_id=$1`, providerShowingID); err != nil {
			t.Fatalf("remove %s invalid fixture failed", providerShowingID)
		}
	}

	malformedURL := "https://s.pathe.fr/fr/V1S200/booking?unexpected=1"
	insertLegacyShowing("S200", malformedURL)
	assertMigrationRejected("malformed URL")
	assertLegacyShowing("S135392", "https://s.pathe.fr/fr/V3308S135392/booking")
	assertLegacyShowing("S200", malformedURL)
	removeShowing("S200")

	mismatchedTokenURL := "https://s.pathe.fr/fr/V1S202/booking"
	insertLegacyShowing("S201", mismatchedTokenURL)
	assertMigrationRejected("mismatched token")
	assertLegacyShowing("S201", mismatchedTokenURL)
	removeShowing("S201")

	oversizedTokenURL := "https://s.pathe.fr/fr/V" + strings.Repeat("9", 112) + "S203/booking"
	insertLegacyShowing("S203", oversizedTokenURL)
	assertMigrationRejected("oversized derived identity")
	assertLegacyShowing("S203", oversizedTokenURL)
	removeShowing("S203")

	if _, err := pool.Exec(ctx, `
ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_identity_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_provider_identity_check CHECK (
    (provider = 'ugc' AND provider_showing_id ~ '^[1-9][0-9]*$') OR
    (provider = 'kinepolis' AND provider_showing_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (provider = 'pathe' AND (provider_showing_id ~ '^S[1-9][0-9]*$' OR provider_showing_id ~ '^V[1-9][0-9]*S[1-9][0-9]*$'))
);
INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES (1,'pathe-showing-V3308S135392','V3308S135392','2026-08-24','pathe-lille','film-a','2026-08-24T19:00:00+02','2026-08-24T20:40:00+02','VF','vf','ICE','2','https://s.pathe.fr/fr/V3308S135392/booking','pathe');
`, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatal("insert collision fixture failed")
	}
	assertMigrationRejected("derived identity collision")
	assertLegacyShowing("S135392", "https://s.pathe.fr/fr/V3308S135392/booking")
	var collisionRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM showtimes WHERE generation_id=1 AND provider='pathe' AND provider_showing_id='V3308S135392' AND id='pathe-showing-V3308S135392'`).Scan(&collisionRows); err != nil || collisionRows != 1 {
		t.Fatalf("collision fixture changed: count=%d err=%v", collisionRows, err)
	}
	removeShowing("V3308S135392")

	if _, err := pool.Exec(ctx, migrations[16].sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("upgrade legacy Pathé showing identity failed: %v", err)
	}
	var upgradedRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM showtimes WHERE generation_id=1 AND provider='pathe' AND id='pathe-showing-V3308S135392' AND provider_showing_id='V3308S135392' AND service_date='2026-08-24' AND theater_id='pathe-lille' AND movie_provider_id='film-a' AND start_time='2026-08-24T21:00:00+02' AND end_time='2026-08-24T22:40:00+02' AND language='VF' AND provider_version='vf' AND format='ICE' AND room='ICE' AND booking_url='https://s.pathe.fr/fr/V3308S135392/booking'`).Scan(&upgradedRows); err != nil || upgradedRows != 1 {
		t.Fatalf("upgraded Pathé row count=%d err=%v", upgradedRows, err)
	}
	if _, err := pool.Exec(ctx, migrations[16].sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("idempotent migration 017 rerun failed: %v", err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("apply recorded Pathé and CGR migrations failed: %v", err)
	}
	var constraintsAfter, indexesAfter []string
	if err := pool.QueryRow(ctx, `SELECT coalesce(array_agg(conname || '=' || pg_get_constraintdef(oid) ORDER BY conname) FILTER (WHERE conname NOT IN ('showtimes_provider_identity_check','showtimes_provider_check','showtimes_language_check','showtimes_check1','showtimes_time_check')), ARRAY[]::text[]) FROM pg_constraint WHERE conrelid='showtimes'::regclass`).Scan(&constraintsAfter); err != nil {
		t.Fatal("read post-017 showtime constraints failed")
	}
	if err := pool.QueryRow(ctx, `SELECT coalesce(array_agg(indexname || '=' || indexdef ORDER BY indexname), ARRAY[]::text[]) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='showtimes'`).Scan(&indexesAfter); err != nil {
		t.Fatal("read post-017 showtime indexes failed")
	}
	if strings.Join(constraintsBefore, "\n") != strings.Join(constraintsAfter, "\n") || strings.Join(indexesBefore, "\n") != strings.Join(indexesAfter, "\n") {
		t.Fatal("migration 017 changed unrelated showtime constraints or indexes")
	}
	if err := pool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM theaters WHERE provider IN ('ugc','kinepolis')) +
    (SELECT count(*) FROM movies WHERE provider IN ('ugc','kinepolis')) +
    (SELECT count(*) FROM showtimes WHERE provider IN ('ugc','kinepolis')) +
    (SELECT count(*) FROM local_movie_groups WHERE id=$1) +
    (SELECT count(*) FROM public_movies WHERE id=$2)`, oldLocalID, oldPublicID).Scan(&oldRows); err != nil || oldRows != 8 {
		t.Fatalf("migration 017 preserved pre-Pathé rows=%d err=%v", oldRows, err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO movie_matches (source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES ('pathe','film-a','tmdb','unmatched','film pathe',100,'[]',$1,$1,$1);
INSERT INTO sync_schedules (provider,enabled,schedule_kind,local_time) VALUES ('pathe',false,'daily','10:00');
INSERT INTO sync_runs (target,state,started_at,finished_at,window_from,window_through,providers) VALUES ('pathe','succeeded',$1,$1,'2026-08-24','2026-08-24','{}');
INSERT INTO sync_runs (target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_revision,scheduled_for,schedule_attempt) VALUES ('pathe','failed',$1,$1,'2026-08-24','2026-08-24','{}','scheduled',1,$1,0);
`, pgx.QueryExecModeSimpleProtocol, now); err != nil {
		t.Fatalf("insert Pathé provider rows failed: %v", err)
	}
	patheLocalTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin Pathé local group failed")
	}
	var patheLocalID int64
	if err := patheLocalTx.QueryRow(ctx, `INSERT INTO local_movie_groups (primary_source_provider,primary_source_movie_id) VALUES ('pathe','film-a') RETURNING id`).Scan(&patheLocalID); err != nil {
		t.Fatal("insert Pathé local group failed")
	}
	if _, err := patheLocalTx.Exec(ctx, `INSERT INTO local_movie_group_members (local_movie_id,source_provider,source_movie_id) VALUES ($1,'pathe','film-a')`, patheLocalID); err != nil {
		t.Fatal("insert Pathé local member failed")
	}
	if err := patheLocalTx.Commit(ctx); err != nil {
		t.Fatal("commit Pathé local group failed")
	}
	var pathePublicID int64
	if err := pool.QueryRow(ctx, `INSERT INTO public_movies (identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES ('pathe','film-a','Film Pathé',100) RETURNING id`).Scan(&pathePublicID); err != nil {
		t.Fatal("insert Pathé public movie failed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public_movie_sources (source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) VALUES ('pathe','film-a',$1,'pathe-film-film-a','Film Pathé',100); INSERT INTO movie_slug_aliases (slug,public_movie_id,alias_kind,source_provider,source_movie_id) VALUES ('pathe-film-film-a',$1,'source','pathe','film-a')`, pgx.QueryExecModeSimpleProtocol, pathePublicID); err != nil {
		t.Fatal("insert Pathé public source failed")
	}

	rejects := []string{
		`INSERT INTO provider_snapshots (generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES (2,'other',1,'all_cinemas',now(),'Europe/Paris','2026-08-24','2026-08-24')`,
		`INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES (1,'pathe','bad id','pathe-film-bad id','Bad',90)`,
		`INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES (1,'pathe-showing-S135393','S135393','2026-08-24','pathe-lille','film-a','2026-08-24T18:00:00+02','2026-08-24T19:40:00+02','VF','vf','ICE','1','https://s.pathe.fr/fr/V3308S135393/booking','pathe')`,
		`INSERT INTO movie_matches (source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES ('pathe','bad id','tmdb','unmatched','bad',90,'[]',now(),now(),now())`,
	}
	for _, query := range rejects {
		if _, err := pool.Exec(ctx, query); err == nil {
			t.Fatalf("invalid Pathé row accepted: %s", query)
		}
	}
	var cgrPublicID int64
	if err := pool.QueryRow(ctx, `INSERT INTO public_movies (identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES ('cgr','1001','Conférence CGR',0) RETURNING id`).Scan(&cgrPublicID); err != nil {
		t.Fatalf("insert CGR public movie failed: %v", err)
	}
	cgrShowingID := "W8010-" + strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, `
INSERT INTO provider_snapshots (generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES (1,'cgr',1,'all_cinemas',$1,'Europe/Paris','2026-08-24','2026-08-24');
INSERT INTO theaters (generation_id,id,provider_id,slug,name,address,city,postal_code,provider) VALUES (1,'cgr-W8010','W8010','cgr-W8010','CGR Lille','1 rue','Lille','59000','cgr');
INSERT INTO theater_dates (generation_id,theater_id,service_date) VALUES (1,'cgr-W8010','2026-08-24');
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes,poster_url) VALUES (1,'cgr','1001','cgr-film-1001','Conférence CGR',0,'https://images.acsta.net/posters/1001.jpg');
INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES (1,'cgr-showing-' || $2,$2,'2026-08-24','cgr-W8010','1001','2026-08-24T19:00:00+02','2026-08-24T19:00:00+02','SPANISH','Localization.Language.Spanish','2D','','https://www.cgrcinemas.fr/lille/reserver/test','cgr');
INSERT INTO movie_matches (source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES ('cgr','1001','tmdb','unmatched','conference cgr',90,'[]',$1,$1,$1);
INSERT INTO sync_schedules (provider,enabled,schedule_kind,local_time) VALUES ('cgr',false,'daily','05:30');
INSERT INTO sync_runs (target,state,started_at,finished_at,window_from,window_through,providers) VALUES ('cgr','succeeded',$1,$1,'2026-08-24','2026-08-24','{}');
INSERT INTO public_movie_sources (source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes,poster_url) VALUES ('cgr','1001',$3,'cgr-film-1001','Conférence CGR',0,'https://images.acsta.net/posters/1001.jpg');
INSERT INTO movie_slug_aliases (slug,public_movie_id,alias_kind,source_provider,source_movie_id) VALUES ('cgr-film-1001',$3,'source','cgr','1001');
`, pgx.QueryExecModeSimpleProtocol, now, cgrShowingID, cgrPublicID); err != nil {
		t.Fatalf("insert CGR provider rows failed: %v", err)
	}
	var migrationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=16 AND name='016_pathe_provider.sql'`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("migration 016 bookkeeping count=%d err=%v", migrationCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=17 AND name='017_pathe_showing_identity.sql'`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("migration 017 bookkeeping count=%d err=%v", migrationCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=18 AND name='018_cgr_provider.sql'`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("migration 018 bookkeeping count=%d err=%v", migrationCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movieflow_schema_migrations WHERE version=19 AND name='019_repair_cgr_unknown_runtime.sql'`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("migration 019 bookkeeping count=%d err=%v", migrationCount, err)
	}
}
