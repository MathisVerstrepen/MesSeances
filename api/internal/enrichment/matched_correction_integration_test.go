package enrichment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/tmdb"
)

type matchedCorrectionProvider struct {
	details tmdb.Details
	calls   int
}

func (p *matchedCorrectionProvider) Details(_ context.Context, _ int64) (tmdb.Details, error) {
	p.calls++
	return p.details, nil
}

func TestMatchedCorrectionIntegration(t *testing.T) {
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
	schema := "movieflow_matched_correction_test_" + hex.EncodeToString(nonce)
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

	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 24, 18, 0, 0, 0, location)
	fixtureNow := time.Date(2026, 8, 23, 12, 0, 0, 123456000, time.UTC)
	ugcDataset := schedule.Dataset{
		SchemaVersion: schedule.SchemaVersion,
		Provider:      schedule.ProviderUGC,
		Scope:         schedule.ScopeAll,
		GeneratedAt:   fixtureNow,
		Timezone:      schedule.Timezone,
		Window:        schedule.Window{From: "2026-08-24", Through: "2026-08-24"},
		Theaters: []schedule.TheaterRecord{{
			Provider: schedule.ProviderUGC, ID: "ugc-1", ProviderID: "1", Slug: "ugc-1", Name: "Cinéma test", Address: "1 rue test", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-24"}, AcceptedPasses: []string{"UGC_ILLIMITE"},
		}},
	}
	for index, movie := range []struct {
		id, title string
	}{{"10", "Bravo corrected source"}, {"11", "Charlie target source"}, {"12", "Alpha source 100%_cache"}} {
		showingID := "10" + movie.id
		ugcDataset.Showtimes = append(ugcDataset.Showtimes, schedule.ShowtimeRecord{
			Provider: schedule.ProviderUGC, ID: "ugc-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: "2026-08-24", TheaterID: "ugc-1",
			Movie:     schedule.MovieRecord{Provider: schedule.ProviderUGC, ProviderID: movie.id, Slug: "ugc-film-" + movie.id, Title: movie.title, RuntimeMinutes: 90 + index},
			StartTime: start.Add(time.Duration(index) * time.Hour), EndTime: start.Add(time.Duration(index)*time.Hour + 90*time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, Room: "1", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=" + showingID,
		})
	}
	kinepolisDataset := schedule.Dataset{
		SchemaVersion: schedule.SchemaVersion,
		Provider:      schedule.ProviderKinepolis,
		Scope:         schedule.ScopeAll,
		GeneratedAt:   fixtureNow,
		Timezone:      schedule.Timezone,
		Window:        schedule.Window{From: "2026-08-24", Through: "2026-08-24"},
		Theaters: []schedule.TheaterRecord{{
			Provider: schedule.ProviderKinepolis, ID: "kinepolis-K", ProviderID: "K", Slug: "kinepolis-K", Name: "Cinéma K", Address: "2 rue test", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-24"}, AcceptedPasses: []string{},
		}},
		Showtimes: []schedule.ShowtimeRecord{{
			Provider: schedule.ProviderKinepolis, ID: "kinepolis-showing-K100", ProviderShowingID: "K100", ServiceDate: "2026-08-24", TheaterID: "kinepolis-K",
			Movie:     schedule.MovieRecord{Provider: schedule.ProviderKinepolis, ProviderID: "K10", Slug: "kinepolis-film-K10", Title: "Delta remaining source", RuntimeMinutes: 93},
			StartTime: start.Add(3 * time.Hour), EndTime: start.Add(4*time.Hour + 33*time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, Room: "2", BookingURL: "https://kinepolis.fr/direct-vista-redirect/K100/0/K/0",
		}},
	}
	pgStore := schedulepg.NewStore(pool)
	if _, err := pgStore.Replace(ctx, []schedule.Dataset{ugcDataset, kinepolisDataset}); err != nil {
		t.Fatalf("publish correction fixtures failed: %v", err)
	}

	store := NewPostgresStore(pool)
	oldMetadata := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "Old Original", LocalizedTitle: "Ancienne identité", Overview: "Ancien résumé", RuntimeMinutes: 100, Genres: []string{"Drame"}, FetchedAt: fixtureNow, RefreshAfter: fixtureNow.Add(metadataTTL)}
	staleReplacement := Metadata{Provider: ProviderTMDB, ProviderMovieID: 99, Locale: LocaleFrench, ProviderTitle: "Stale Original", LocalizedTitle: "Identité périmée", Overview: "Résumé périmé", RuntimeMinutes: 101, Genres: []string{"Action"}, FetchedAt: fixtureNow, RefreshAfter: fixtureNow.Add(metadataTTL)}
	match := func(provider, id, title string, runtime int, tmdbID int64) Match {
		return Match{SourceProvider: provider, SourceMovieID: id, MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: tmdbID, Score: .8, NormalizedSourceTitle: NormalizeTitle(title), SourceRuntimeMinutes: runtime, Candidates: []Candidate{{ID: tmdbID, Title: "Candidate conservé", Score: .8}}, EvaluatedAt: fixtureNow, RetryAfter: fixtureNow.Add(metadataTTL)}
	}
	if err := store.Publish(ctx, match(SourceUGC, "10", "Bravo corrected source", 90, 42), oldMetadata); err != nil {
		t.Fatal("publish corrected-source fixture failed")
	}
	if err := store.Publish(ctx, match(SourceKinepolis, "K10", "Delta remaining source", 93, 42), oldMetadata); err != nil {
		t.Fatal("publish old-identity peer fixture failed")
	}
	if err := store.Publish(ctx, match(SourceUGC, "11", "Charlie target source", 91, 99), staleReplacement); err != nil {
		t.Fatal("publish replacement-identity peer fixture failed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movie_matches (source_provider,source_movie_id,metadata_provider,status,metadata_movie_id,score,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at)
VALUES ('ugc','12','tmdb','matched',777,0.7,$1,92,'[]',$2,$3,$2)`, NormalizeTitle("Alpha source 100%_cache"), fixtureNow, fixtureNow.Add(metadataTTL)); err != nil {
		t.Fatal("insert cacheless matched fixture failed")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES (2,'ugc','13','ugc-film-13','Echo inactive source',94)"); err != nil {
		t.Fatal("insert inactive movie fixture failed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movie_matches (source_provider,source_movie_id,metadata_provider,status,metadata_movie_id,score,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at)
VALUES ('ugc','13','tmdb','matched',888,0.6,$1,94,'[]',$2,$3,$2)`, NormalizeTitle("Echo inactive source"), fixtureNow, fixtureNow.Add(metadataTTL)); err != nil {
		t.Fatal("insert inactive matched fixture failed")
	}

	items, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "", 20, 0)
	if err != nil || len(items) != 4 {
		t.Fatalf("matched items=%+v err=%v", items, err)
	}
	var correctedItem, cachelessItem PendingMatch
	for _, item := range items {
		switch item.SourceMovieID {
		case "10":
			correctedItem = item
		case "12":
			cachelessItem = item
		}
	}
	if correctedItem.UpdatedAt == nil || correctedItem.CurrentMatch == nil || correctedItem.CurrentMatch.ID != 42 || correctedItem.CurrentMatch.Title != oldMetadata.LocalizedTitle || correctedItem.CurrentMatch.OriginalTitle != oldMetadata.ProviderTitle || correctedItem.CurrentMatch.Runtime != oldMetadata.RuntimeMinutes || correctedItem.CurrentMatch.Score != .8 {
		t.Fatalf("listed current match=%+v token=%v", correctedItem.CurrentMatch, correctedItem.UpdatedAt)
	}
	if cachelessItem.CurrentMatch == nil || cachelessItem.CurrentMatch.ID != 777 || cachelessItem.CurrentMatch.Title != "TMDB #777" || cachelessItem.UpdatedAt == nil {
		t.Fatalf("cacheless current match=%+v token=%v", cachelessItem.CurrentMatch, cachelessItem.UpdatedAt)
	}
	searchCases := []struct {
		name     string
		search   string
		limit    int
		offset   int
		wantKeys []string
	}{
		{name: "source title case insensitive", search: "BRAVO CORRECTED", limit: 20, wantKeys: []string{"ugc/10"}},
		{name: "localized TMDB title", search: "ANCIENNE IDENTITÉ", limit: 20, wantKeys: []string{"ugc/10", "kinepolis/K10"}},
		{name: "original TMDB title", search: "old ORIGINAL", limit: 20, wantKeys: []string{"ugc/10", "kinepolis/K10"}},
		{name: "source movie ID", search: "k1", limit: 20, wantKeys: []string{"kinepolis/K10"}},
		{name: "exact canonical TMDB ID", search: "99", limit: 20, wantKeys: []string{"ugc/11"}},
		{name: "leading-zero numeric is text only", search: "099", limit: 20, wantKeys: []string{}},
		{name: "cacheless exact TMDB ID", search: "777", limit: 20, wantKeys: []string{"ugc/12"}},
		{name: "inactive exact TMDB ID excluded", search: "888", limit: 20, wantKeys: []string{}},
		{name: "cacheless source title", search: "100%_CACHE", limit: 20, wantKeys: []string{"ugc/12"}},
		{name: "percent is literal", search: "%", limit: 20, wantKeys: []string{"ugc/12"}},
		{name: "underscore is literal", search: "_", limit: 20, wantKeys: []string{"ugc/12"}},
		{name: "ordering and pagination", search: "source", limit: 2, offset: 1, wantKeys: []string{"ugc/10", "ugc/11"}},
	}
	for _, test := range searchCases {
		t.Run("search "+test.name, func(t *testing.T) {
			found, err := store.PendingMatches(ctx, PendingMatchFilterMatched, test.search, test.limit, test.offset)
			if err != nil {
				t.Fatalf("search %q failed: %v", test.search, err)
			}
			keys := make([]string, len(found))
			for index, item := range found {
				keys[index] = item.SourceProvider + "/" + item.SourceMovieID
			}
			if strings.Join(keys, ",") != strings.Join(test.wantKeys, ",") {
				t.Fatalf("search %q keys=%v want=%v", test.search, keys, test.wantKeys)
			}
		})
	}

	before, beforeRevision, err := pgStore.Load(ctx)
	if err != nil {
		t.Fatalf("load catalog before correction failed: %v", err)
	}
	correctedBefore := publicSourceID(before, schedule.ProviderUGC, "10")
	oldPeerBefore := publicSourceID(before, schedule.ProviderKinepolis, "K10")
	replacementPeerBefore := publicSourceID(before, schedule.ProviderUGC, "11")
	if correctedBefore <= 0 || correctedBefore != oldPeerBefore || correctedBefore == replacementPeerBefore {
		t.Fatalf("unexpected pre-correction grouping corrected=%d old=%d replacement=%d", correctedBefore, oldPeerBefore, replacementPeerBefore)
	}

	correctionNow := fixtureNow.Add(2 * time.Hour)
	provider := &matchedCorrectionProvider{details: tmdb.Details{ID: 99, IMDBID: "tt1234567", Title: "Nouvelle identité", OriginalTitle: "New Original", Overview: "Nouveau résumé", PosterURL: "https://image.tmdb.org/t/p/w500/99.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/99.jpg", Runtime: 102, Genres: []string{"Comédie"}}}
	service := NewReviewService(store, provider, func() time.Time { return correctionNow })
	if err := service.Correct(ctx, SourceUGC, "10", 99, *correctedItem.UpdatedAt); err != nil {
		t.Fatalf("correct matched identity failed: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d", provider.calls)
	}

	var metadataMovieID int64
	var score float64
	var candidates string
	var evaluatedAt, retryAfter, updatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT metadata_movie_id,score,candidates::text,evaluated_at,retry_after,updated_at FROM movie_matches WHERE source_provider='ugc' AND source_movie_id='10' AND metadata_provider='tmdb'`).Scan(&metadataMovieID, &score, &candidates, &evaluatedAt, &retryAfter, &updatedAt); err != nil {
		t.Fatal("read corrected match failed")
	}
	if metadataMovieID != 99 || score != 1 || !strings.Contains(candidates, "Candidate conservé") || !evaluatedAt.Equal(correctionNow) || !updatedAt.Equal(correctionNow) || !retryAfter.Equal(correctionNow.Add(reviewMetadataTTL)) {
		t.Fatalf("corrected row id=%d score=%v candidates=%s evaluated=%s retry=%s updated=%s", metadataMovieID, score, candidates, evaluatedAt, retryAfter, updatedAt)
	}
	var oldTitle, replacementTitle, replacementOverview string
	var replacementFetchedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT localized_title FROM movie_metadata_cache WHERE provider='tmdb' AND provider_movie_id=42 AND locale='fr-FR'").Scan(&oldTitle); err != nil || oldTitle != oldMetadata.LocalizedTitle {
		t.Fatalf("old cache title=%q err=%v", oldTitle, err)
	}
	if err := pool.QueryRow(ctx, "SELECT localized_title,overview,fetched_at FROM movie_metadata_cache WHERE provider='tmdb' AND provider_movie_id=99 AND locale='fr-FR'").Scan(&replacementTitle, &replacementOverview, &replacementFetchedAt); err != nil || replacementTitle != "Nouvelle identité" || replacementOverview != "Nouveau résumé" || !replacementFetchedAt.Equal(correctionNow) {
		t.Fatalf("replacement cache title=%q overview=%q fetched=%s err=%v", replacementTitle, replacementOverview, replacementFetchedAt, err)
	}
	after, afterRevision, err := pgStore.Load(ctx)
	if err != nil {
		t.Fatalf("load catalog after correction failed: %v", err)
	}
	correctedAfter := publicSourceID(after, schedule.ProviderUGC, "10")
	oldPeerAfter := publicSourceID(after, schedule.ProviderKinepolis, "K10")
	replacementPeerAfter := publicSourceID(after, schedule.ProviderUGC, "11")
	if correctedAfter <= 0 || correctedAfter != replacementPeerAfter || correctedAfter == oldPeerAfter {
		t.Fatalf("correction did not split/merge corrected=%d old=%d replacement=%d", correctedAfter, oldPeerAfter, replacementPeerAfter)
	}
	if afterRevision.EnrichmentVersion != beforeRevision.EnrichmentVersion+1 {
		t.Fatalf("enrichment version before=%d after=%d", beforeRevision.EnrichmentVersion, afterRevision.EnrichmentVersion)
	}

	var versionBeforeConflict int64
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&versionBeforeConflict); err != nil {
		t.Fatal("read version before conflicts failed")
	}
	refreshedItems, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "", 20, 0)
	if err != nil {
		t.Fatalf("reload corrected match failed: %v", err)
	}
	var correctedToken time.Time
	for _, item := range refreshedItems {
		if item.SourceProvider == SourceUGC && item.SourceMovieID == "10" && item.UpdatedAt != nil {
			correctedToken = *item.UpdatedAt
		}
	}
	if correctedToken.IsZero() {
		t.Fatal("corrected optimistic token missing")
	}
	if err := service.Correct(ctx, SourceUGC, "10", 99, correctedToken); !errors.Is(err, ErrReviewConflict) || provider.calls != 1 {
		t.Fatalf("same-ID correction calls=%d err=%v", provider.calls, err)
	}
	assertCorrectionConflictState(t, ctx, pool, SourceUGC, "10", 99, 100, versionBeforeConflict)

	conflictMetadata := Metadata{Provider: ProviderTMDB, ProviderMovieID: 100, Locale: LocaleFrench, ProviderTitle: "Conflict", LocalizedTitle: "Conflict", RuntimeMinutes: 93, Genres: []string{}, FetchedAt: correctionNow.Add(time.Hour), RefreshAfter: correctionNow.Add(31 * 24 * time.Hour)}
	var oldPeerToken time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM movie_matches WHERE source_provider='kinepolis' AND source_movie_id='K10' AND metadata_provider='tmdb'").Scan(&oldPeerToken); err != nil {
		t.Fatal("read stale-conflict token failed")
	}
	if err := store.CorrectReview(ctx, SourceKinepolis, "K10", 100, oldPeerToken.Add(-time.Nanosecond), conflictMetadata, 0, correctionNow.Add(time.Hour)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("stale correction err=%v", err)
	}
	assertCorrectionConflictState(t, ctx, pool, SourceKinepolis, "K10", 42, 100, versionBeforeConflict)

	var cachelessToken time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM movie_matches WHERE source_provider='ugc' AND source_movie_id='12' AND metadata_provider='tmdb'").Scan(&cachelessToken); err != nil {
		t.Fatal("read fingerprint-conflict token failed")
	}
	if _, err := store.CorrectionSource(ctx, SourceUGC, "12", 102, cachelessToken); err != nil {
		t.Fatalf("fingerprint conflict preflight failed unexpectedly: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE movies SET title='Alpha modified source' WHERE generation_id=(SELECT version FROM schedule_snapshot WHERE singleton=true) AND provider='ugc' AND provider_id='12'"); err != nil {
		t.Fatal("change source fingerprint failed")
	}
	fingerprintMetadata := conflictMetadata
	fingerprintMetadata.ProviderMovieID = 102
	fingerprintMetadata.RuntimeMinutes = 92
	if err := store.CorrectReview(ctx, SourceUGC, "12", 102, cachelessToken, fingerprintMetadata, 0, correctionNow.Add(2*time.Hour)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("fingerprint correction err=%v", err)
	}
	assertCorrectionConflictState(t, ctx, pool, SourceUGC, "12", 777, 102, versionBeforeConflict)
	if _, err := pool.Exec(ctx, "UPDATE movies SET title='Alpha source 100%_cache' WHERE generation_id=(SELECT version FROM schedule_snapshot WHERE singleton=true) AND provider='ugc' AND provider_id='12'"); err != nil {
		t.Fatal("restore source fingerprint failed")
	}

	if _, err := store.CorrectionSource(ctx, SourceKinepolis, "K10", 101, oldPeerToken); err != nil {
		t.Fatalf("local conflict preflight failed unexpectedly: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin local fixture transaction failed")
	}
	var localID int64
	if err := tx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider,primary_source_movie_id) VALUES ('kinepolis','K10') RETURNING id").Scan(&localID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal("insert local group failed")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id,source_provider,source_movie_id) VALUES ($1,'kinepolis','K10')", localID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal("insert local member failed")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit local fixture failed")
	}
	localConflictMetadata := conflictMetadata
	localConflictMetadata.ProviderMovieID = 101
	if err := store.CorrectReview(ctx, SourceKinepolis, "K10", 101, oldPeerToken, localConflictMetadata, 0, correctionNow.Add(2*time.Hour)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("local-member correction err=%v", err)
	}
	assertCorrectionConflictState(t, ctx, pool, SourceKinepolis, "K10", 42, 101, versionBeforeConflict)
	matchedItems, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "", 20, 0)
	if err != nil {
		t.Fatalf("list after local conflict failed: %v", err)
	}
	for _, item := range matchedItems {
		if item.SourceProvider == SourceKinepolis && item.SourceMovieID == "K10" {
			t.Fatal("locally merged source remained in matched collection")
		}
	}
	localSearchItems, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "delta remaining", 20, 0)
	if err != nil || len(localSearchItems) != 0 {
		t.Fatalf("local-member search items=%+v err=%v", localSearchItems, err)
	}
}

func assertCorrectionConflictState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, provider, sourceID string, wantTMDBID, absentCacheID, wantVersion int64) {
	t.Helper()
	var tmdbID, version int64
	if err := pool.QueryRow(ctx, "SELECT metadata_movie_id FROM movie_matches WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb'", provider, sourceID).Scan(&tmdbID); err != nil {
		t.Fatal("read match after conflict failed")
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil {
		t.Fatal("read version after conflict failed")
	}
	var cacheCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movie_metadata_cache WHERE provider='tmdb' AND provider_movie_id=$1 AND locale='fr-FR'", absentCacheID).Scan(&cacheCount); err != nil {
		t.Fatal("count conflict cache failed")
	}
	if tmdbID != wantTMDBID || version != wantVersion || cacheCount != 0 {
		t.Fatalf("conflict state tmdb=%d version=%d cache=%d want=%d/%d/0", tmdbID, version, cacheCount, wantTMDBID, wantVersion)
	}
}
