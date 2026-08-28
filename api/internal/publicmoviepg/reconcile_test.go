package publicmoviepg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
)

func TestBuildComponentsUsesStrictEvidenceOnly(t *testing.T) {
	first := &source{key: sourceKey{provider: "ugc", id: "1"}, title: "Même titre", runtime: 90}
	second := &source{key: sourceKey{provider: "kinepolis", id: "A"}, title: "Même titre", runtime: 90}
	components := buildComponents(map[sourceKey]*source{first.key: first, second.key: second})
	if len(components) != 2 {
		t.Fatalf("title/runtime merged %d components", len(components))
	}
	first.confirmedTMDB, second.confirmedTMDB = 42, 42
	components = buildComponents(map[sourceKey]*source{first.key: first, second.key: second})
	if len(components) != 1 || len(components[0].members) != 2 {
		t.Fatalf("confirmed TMDB evidence did not merge: %+v", components)
	}
}

func TestChooseMetadataPrecedenceAndFieldFallback(t *testing.T) {
	primaryOverview := "Résumé principal"
	fallbackPoster := "https://example.test/fallback.jpg"
	primary := &source{key: sourceKey{provider: "ugc", id: "2"}, title: "Principal", runtime: 100, overview: &primaryOverview, localPrimary: true}
	fallback := &source{key: sourceKey{provider: "kinepolis", id: "A"}, title: "Fallback", runtime: 95, poster: &fallbackPoster}
	component := &component{members: []*source{fallback, primary}, tmdbID: 42}
	tmdbOverview := "Résumé TMDB"
	imdbID := "tt1234567"
	trailerVFKey, trailerVOKey := "FRoff123456", "ENoff123456"
	chosen := chooseMetadata(component, tmdbMetadata{title: "Titre TMDB", runtime: 110, imdbID: &imdbID, trailerVFYouTubeKey: &trailerVFKey, trailerVOYouTubeKey: &trailerVOKey, overview: &tmdbOverview})
	if chosen.title != "Titre TMDB" || chosen.runtime != 110 || chosen.imdbID == nil || *chosen.imdbID != imdbID || chosen.trailerVFYouTubeKey == nil || *chosen.trailerVFYouTubeKey != trailerVFKey || chosen.trailerVOYouTubeKey == nil || *chosen.trailerVOYouTubeKey != trailerVOKey || chosen.overview == nil || *chosen.overview != tmdbOverview || chosen.poster == nil || *chosen.poster != fallbackPoster || chosen.tmdbID != 42 {
		t.Fatalf("chosen metadata=%+v", chosen)
	}
}

func TestSourceOrderingKeepsUGCFirstThenLexicalProviders(t *testing.T) {
	keys := []sourceKey{{provider: "pathe", id: "B"}, {provider: "ugc", id: "9"}, {provider: "kinepolis", id: "A"}, {provider: "pathe", id: "A"}}
	sort.Slice(keys, func(i, j int) bool { return lessSourceKey(keys[i], keys[j]) })
	want := []sourceKey{{provider: "ugc", id: "9"}, {provider: "kinepolis", id: "A"}, {provider: "pathe", id: "A"}, {provider: "pathe", id: "B"}}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("ordered sources=%+v want=%+v", keys, want)
	}
	component := &component{members: []*source{{key: sourceKey{provider: "pathe", id: "A"}}, {key: sourceKey{provider: "ugc", id: "9"}}}}
	if anchor := chooseAnchor(component); anchor != want[0] {
		t.Fatalf("anchor=%+v want=%+v", anchor, want[0])
	}
}

func TestReconcileMergeSplitIntegration(t *testing.T) {
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
	schema := "movieflow_public_movie_test_" + hex.EncodeToString(nonce)
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
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run integration migrations failed")
	}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO schedule_snapshot
    (version,schema_version,provider,scope,generated_at,timezone,window_from,window_through)
VALUES (1,1,'combined','all_cinemas',$1,'Europe/Paris','2026-08-23','2026-08-24');
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes,source_overview,source_genres) VALUES
    (1,'ugc','1','ugc-film-1','Même titre',90,'Résumé UGC',ARRAY['Drame']),
    (1,'kinepolis','A','kinepolis-film-A','Même titre',90,'Résumé Kinepolis',ARRAY['Comédie']);`, pgx.QueryExecModeSimpleProtocol, now); err != nil {
		t.Fatal("insert active source fixtures failed")
	}
	reconcile := func() {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin reconcile failed")
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if err := Reconcile(ctx, tx); err != nil {
			t.Fatalf("reconcile failed: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal("commit reconcile failed")
		}
	}
	reconcile()
	var ugcID, kinepolisID int64
	if err := pool.QueryRow(ctx, `SELECT
    max(public_movie_id) FILTER (WHERE source_provider='ugc'),
    max(public_movie_id) FILTER (WHERE source_provider='kinepolis')
FROM public_movie_sources`).Scan(&ugcID, &kinepolisID); err != nil || ugcID <= 0 || kinepolisID <= 0 || ugcID == kinepolisID {
		t.Fatalf("strict singleton IDs ugc=%d kinepolis=%d err=%v", ugcID, kinepolisID, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movie_metadata_cache
	    (provider,provider_movie_id,imdb_id,locale,provider_title,localized_title,overview,trailer_vf_youtube_key,trailer_vo_youtube_key,runtime_minutes,genres,fetched_at,refresh_after)
VALUES ('tmdb',42,'tt1234567','fr-FR','Original','Canonique TMDB','Résumé TMDB','FRoff123456','ENoff123456',91,ARRAY['Action'],$1,$2);
INSERT INTO movie_matches
    (source_provider,source_movie_id,metadata_provider,status,metadata_movie_id,score,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at)
VALUES
    ('ugc','1','tmdb','matched',42,1,'même titre',90,'[]',$1,$2,$1),
    ('kinepolis','A','tmdb','matched',42,1,'même titre',90,'[]',$1,$2,$1);`, pgx.QueryExecModeSimpleProtocol, now, now.Add(24*time.Hour)); err != nil {
		t.Fatal("insert confirmed TMDB evidence failed")
	}
	reconcile()
	survivor := ugcID
	loser := kinepolisID
	if kinepolisID < ugcID {
		survivor, loser = kinepolisID, ugcID
	}
	var mergedUGC, mergedKinepolis, redirect int64
	var canonicalTitle, canonicalIMDBID, canonicalTrailerVFKey, canonicalTrailerVOKey string
	if err := pool.QueryRow(ctx, `SELECT
    max(public_movie_id) FILTER (WHERE source_provider='ugc'),
    max(public_movie_id) FILTER (WHERE source_provider='kinepolis')
FROM public_movie_sources`).Scan(&mergedUGC, &mergedKinepolis); err != nil || mergedUGC != survivor || mergedKinepolis != survivor {
		t.Fatalf("merged IDs ugc=%d kinepolis=%d survivor=%d err=%v", mergedUGC, mergedKinepolis, survivor, err)
	}
	if err := pool.QueryRow(ctx, "SELECT redirect_to_id FROM public_movies WHERE id=$1", loser).Scan(&redirect); err != nil || redirect != survivor {
		t.Fatalf("loser redirect=%d survivor=%d err=%v", redirect, survivor, err)
	}
	if err := pool.QueryRow(ctx, "SELECT title FROM public_movies WHERE id=$1", survivor).Scan(&canonicalTitle); err != nil || canonicalTitle != "Canonique TMDB" {
		t.Fatalf("canonical title=%q err=%v", canonicalTitle, err)
	}
	if err := pool.QueryRow(ctx, "SELECT imdb_id FROM public_movies WHERE id=$1", survivor).Scan(&canonicalIMDBID); err != nil || canonicalIMDBID != "tt1234567" {
		t.Fatalf("canonical IMDb ID=%q err=%v", canonicalIMDBID, err)
	}
	var loserIMDBNull bool
	if err := pool.QueryRow(ctx, "SELECT imdb_id IS NULL FROM public_movies WHERE id=$1", loser).Scan(&loserIMDBNull); err != nil || !loserIMDBNull {
		t.Fatalf("redirect tombstone IMDb null=%t err=%v", loserIMDBNull, err)
	}
	if err := pool.QueryRow(ctx, "SELECT trailer_vf_youtube_key, trailer_vo_youtube_key FROM public_movies WHERE id=$1", survivor).Scan(&canonicalTrailerVFKey, &canonicalTrailerVOKey); err != nil || canonicalTrailerVFKey != "FRoff123456" || canonicalTrailerVOKey != "ENoff123456" {
		t.Fatalf("canonical trailer keys VF=%q VO=%q err=%v", canonicalTrailerVFKey, canonicalTrailerVOKey, err)
	}
	oldUpdatedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, "UPDATE public_movies SET updated_at=$2 WHERE id=$1; UPDATE movie_metadata_cache SET imdb_id='tt2345678' WHERE provider_movie_id=42", pgx.QueryExecModeSimpleProtocol, survivor, oldUpdatedAt); err != nil {
		t.Fatal("prepare IMDb-only canonical change failed")
	}
	reconcile()
	var imdbOnlyUpdatedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT imdb_id, updated_at FROM public_movies WHERE id=$1", survivor).Scan(&canonicalIMDBID, &imdbOnlyUpdatedAt); err != nil || canonicalIMDBID != "tt2345678" || !imdbOnlyUpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("IMDb-only canonical update ID=%q updated_at=%v err=%v", canonicalIMDBID, imdbOnlyUpdatedAt, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movie_metadata_cache
	    (provider,provider_movie_id,imdb_id,locale,provider_title,localized_title,runtime_minutes,genres,fetched_at,refresh_after)
VALUES
	    ('tmdb',1,'tt7654321','fr-FR','Earlier','Earlier corrected',90,'{}',$1,$2),
	    ('tmdb',2,NULL,'fr-FR','Anchor','Anchor corrected',90,'{}',$1,$2);
UPDATE movie_matches SET metadata_movie_id=1 WHERE source_provider='ugc' AND source_movie_id='1';
UPDATE movie_matches SET metadata_movie_id=2 WHERE source_provider='kinepolis' AND source_movie_id='A';`, pgx.QueryExecModeSimpleProtocol, now, now.Add(24*time.Hour)); err != nil {
		t.Fatal("replace corrected TMDB evidence failed")
	}
	reconcile()
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM public_movie_sources WHERE source_provider='kinepolis' AND source_movie_id='A'").Scan(&mergedKinepolis); err != nil || mergedKinepolis != survivor {
		t.Fatalf("anchor split ID=%d survivor=%d err=%v", mergedKinepolis, survivor, err)
	}
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='1'").Scan(&mergedUGC); err != nil || mergedUGC == survivor || mergedUGC == loser {
		t.Fatalf("non-anchor split ID=%d survivor=%d loser=%d err=%v", mergedUGC, survivor, loser, err)
	}
	var splitIMDBID *string
	if err := pool.QueryRow(ctx, "SELECT imdb_id FROM public_movies WHERE id=$1", mergedUGC).Scan(&splitIMDBID); err != nil || splitIMDBID == nil || *splitIMDBID != "tt7654321" {
		t.Fatalf("corrected split IMDb ID=%v err=%v", splitIMDBID, err)
	}
	if err := pool.QueryRow(ctx, "SELECT imdb_id FROM public_movies WHERE id=$1", survivor).Scan(&splitIMDBID); err != nil || splitIMDBID != nil {
		t.Fatalf("stale anchor IMDb ID not cleared: %v err=%v", splitIMDBID, err)
	}
	var aliasTarget int64
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM movie_slug_aliases WHERE slug='kinepolis-film-A'").Scan(&aliasTarget); err != nil || aliasTarget != mergedKinepolis {
		t.Fatalf("retargeted source alias=%d split=%d err=%v", aliasTarget, mergedKinepolis, err)
	}
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM movie_slug_aliases WHERE slug='tmdb-film-1'").Scan(&aliasTarget); err != nil || aliasTarget != mergedUGC {
		t.Fatalf("earlier TMDB alias=%d corrected split=%d err=%v", aliasTarget, mergedUGC, err)
	}
	if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM movie_slug_aliases WHERE slug='tmdb-film-2'").Scan(&aliasTarget); err != nil || aliasTarget != survivor {
		t.Fatalf("anchor TMDB alias=%d survivor=%d err=%v", aliasTarget, survivor, err)
	}

	cycleTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin redirect-cycle rollback test failed")
	}
	if _, err := cycleTx.Exec(ctx, "UPDATE public_movies SET redirect_to_id=$2 WHERE id=$1", survivor, loser); err != nil {
		_ = cycleTx.Rollback(ctx)
		t.Fatal("create redirect-cycle fixture failed")
	}
	if err := Reconcile(ctx, cycleTx); err == nil {
		_ = cycleTx.Rollback(ctx)
		t.Fatal("redirect cycle reconciliation succeeded")
	}
	if err := cycleTx.Rollback(ctx); err != nil {
		t.Fatal("rollback redirect-cycle fixture failed")
	}
	var survivorRedirect int64
	if err := pool.QueryRow(ctx, "SELECT COALESCE(redirect_to_id,0) FROM public_movies WHERE id=$1", survivor).Scan(&survivorRedirect); err != nil || survivorRedirect != 0 {
		t.Fatalf("redirect-cycle rollback redirect=%d err=%v", survivorRedirect, err)
	}

	aliasTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin alias-collision rollback test failed")
	}
	if _, err := aliasTx.Exec(ctx, `UPDATE movie_slug_aliases SET alias_kind='tmdb',source_provider=NULL,source_movie_id=NULL
WHERE slug='ugc-film-1'`); err != nil {
		_ = aliasTx.Rollback(ctx)
		t.Fatal("create alias-collision fixture failed")
	}
	if err := Reconcile(ctx, aliasTx); err == nil {
		_ = aliasTx.Rollback(ctx)
		t.Fatal("alias collision reconciliation succeeded")
	}
	if err := aliasTx.Rollback(ctx); err != nil {
		t.Fatal("rollback alias-collision fixture failed")
	}
	var aliasKind string
	if err := pool.QueryRow(ctx, "SELECT alias_kind FROM movie_slug_aliases WHERE slug='ugc-film-1'").Scan(&aliasKind); err != nil || aliasKind != "source" {
		t.Fatalf("alias-collision rollback kind=%q err=%v", aliasKind, err)
	}
}
