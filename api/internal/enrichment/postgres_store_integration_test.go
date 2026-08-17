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

	"movieflow/api/internal/database"
)

func TestPostgresStoreIntegration(t *testing.T) {
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
	schema := "movieflow_enrichment_test_" + hex.EncodeToString(nonce)
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
		if !strings.HasPrefix(schema, "movieflow_enrichment_test_") {
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
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run integration migrations failed")
	}

	store := NewPostgresStore(pool)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	unmatched := Match{SourceProvider: SourceUGC, SourceMovieID: "200", MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 100, Candidates: []Candidate{}, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL)}
	if err := store.SaveDecision(ctx, unmatched); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 0 {
		t.Fatalf("audit-only version=%d error=%v", version, err)
	}
	loadedMatch, found, err := store.Match(ctx, SourceUGC, "200", ProviderTMDB)
	if err != nil || !found || loadedMatch.Status != StatusUnmatched {
		t.Fatalf("match=%+v found=%v error=%v", loadedMatch, found, err)
	}

	matched := unmatched
	matched.Status, matched.MetadataMovieID, matched.Score = StatusMatched, 42, 1
	matched.Candidates = []Candidate{{ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: 100, Score: 1}}
	matched.RetryAfter = now.Add(metadataTTL)
	metadata := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", Overview: "Résumé", ReleaseDate: "2026-01-02", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg", RuntimeMinutes: 100, Genres: []string{"Drame"}, FetchedAt: now, RefreshAfter: now.Add(metadataTTL)}
	if err := store.Publish(ctx, matched, metadata); err != nil {
		t.Fatal(err)
	}
	loadedMetadata, found, err := store.Metadata(ctx, ProviderTMDB, 42, LocaleFrench)
	if err != nil || !found || loadedMetadata.Overview != "Résumé" || loadedMetadata.BackdropURL != metadata.BackdropURL || len(loadedMetadata.Genres) != 1 {
		t.Fatalf("metadata=%+v found=%v error=%v", loadedMetadata, found, err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 1 {
		t.Fatalf("published version=%d error=%v", version, err)
	}

	invalid := metadata
	invalid.ProviderMovieID = 0
	if err := store.Publish(ctx, matched, invalid); err == nil {
		t.Fatal("invalid metadata publication accepted")
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed publication changed version=%d error=%v", version, err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO movies (provider_id, slug, title, runtime_minutes, poster_url) VALUES
	('201','ugc-film-201','Film à revoir',100,'https://static.ugc.fr/posters/201.jpg'),
	('202','ugc-film-202','Titre modifié',90,NULL),
	('203','ugc-film-203','Film refusé',95,NULL),
	('205','ugc-film-205','Ancien film',88,NULL),
	('206','ugc-film-206','Film sans correspondance',97,NULL),
	('207','ugc-film-207','Film déjà associé',99,NULL),
	('208','ugc-film-208','Film déjà refusé',96,NULL),
	('209','ugc-film-209','Film qui change',102,NULL)`); err != nil {
		t.Fatal("insert review movies failed")
	}
	review := Match{SourceProvider: SourceUGC, SourceMovieID: "201", MetadataProvider: ProviderTMDB, Status: StatusReviewRequired, NormalizedSourceTitle: NormalizeTitle("Film à revoir"), SourceRuntimeMinutes: 100, Candidates: []Candidate{{ID: 52, Title: "Film à revoir", Runtime: 100, Score: .92, PosterURL: "https://image.tmdb.org/t/p/w500/52.jpg"}}, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL)}
	if err := store.SaveDecision(ctx, review); err != nil {
		t.Fatal(err)
	}
	oldReview := review
	oldReview.SourceMovieID, oldReview.NormalizedSourceTitle, oldReview.SourceRuntimeMinutes = "205", NormalizeTitle("Ancien film"), 88
	oldReview.Candidates = []Candidate{{ID: 53, Title: "Ancien film", Runtime: 88, Score: .91}}
	if err := store.SaveDecision(ctx, oldReview); err != nil {
		t.Fatal(err)
	}
	unmatchedReview := review
	unmatchedReview.SourceMovieID, unmatchedReview.Status, unmatchedReview.NormalizedSourceTitle, unmatchedReview.SourceRuntimeMinutes = "206", StatusUnmatched, NormalizeTitle("Film sans correspondance"), 97
	unmatchedReview.Candidates = []Candidate{}
	if err := store.SaveDecision(ctx, unmatchedReview); err != nil {
		t.Fatal(err)
	}
	alreadyMatched := review
	alreadyMatched.SourceMovieID, alreadyMatched.Status, alreadyMatched.MetadataMovieID, alreadyMatched.Score = "207", StatusMatched, 2070, .8
	alreadyMatched.NormalizedSourceTitle, alreadyMatched.SourceRuntimeMinutes = NormalizeTitle("Film déjà associé"), 99
	alreadyMatched.Candidates = []Candidate{{ID: 2070, Title: "Film déjà associé", Runtime: 99, Score: .8}}
	if err := store.SaveDecision(ctx, alreadyMatched); err != nil {
		t.Fatal(err)
	}
	alreadyRejected := review
	alreadyRejected.SourceMovieID, alreadyRejected.Status = "208", StatusRejected
	alreadyRejected.NormalizedSourceTitle, alreadyRejected.SourceRuntimeMinutes = NormalizeTitle("Film déjà refusé"), 96
	if err := store.SaveDecision(ctx, alreadyRejected); err != nil {
		t.Fatal(err)
	}
	items, err := store.PendingMatches(ctx, 50, 0)
	if err != nil || len(items) != 3 || items[0].SourceMovieID != "201" || items[0].Status != StatusReviewRequired || items[0].SourceTitle != "Film à revoir" || items[0].SourcePosterURL != "https://static.ugc.fr/posters/201.jpg" || items[0].Candidates[0].PosterURL != "https://image.tmdb.org/t/p/w500/52.jpg" || items[1].SourceMovieID != "205" || items[1].Status != StatusReviewRequired || items[1].SourcePosterURL != "" || items[1].Candidates[0].PosterURL != "" || items[2].SourceMovieID != "206" || items[2].Status != StatusUnmatched {
		t.Fatalf("pending=%+v err=%v", items, err)
	}
	paged, err := store.PendingMatches(ctx, 2, 1)
	if err != nil || len(paged) != 2 || paged[0].SourceMovieID != "205" || paged[1].SourceMovieID != "206" {
		t.Fatalf("paged pending=%+v err=%v", paged, err)
	}
	manualCandidate, err := store.ReviewCandidate(ctx, SourceUGC, "201", 999)
	if err != nil || manualCandidate.ID != 999 || manualCandidate.Score != 1 {
		t.Fatalf("manual candidate=%+v error=%v", manualCandidate, err)
	}
	storedCandidate, err := store.ReviewCandidate(ctx, SourceUGC, "201", 52)
	if err != nil || storedCandidate.ID != 52 || storedCandidate.Score != .92 {
		t.Fatalf("stored candidate=%+v error=%v", storedCandidate, err)
	}
	if err := store.RejectReview(ctx, SourceUGC, "206", now.Add(30*time.Second)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("unmatched rejection error=%v", err)
	}
	approvedMetadata := metadata
	approvedMetadata.ProviderMovieID = 52
	approvedMetadata.ProviderTitle = "Film à revoir"
	approvedMetadata.LocalizedTitle = "Film à revoir"
	if err := store.ApproveReview(ctx, SourceUGC, "201", 52, approvedMetadata, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 2 {
		t.Fatalf("review approval version=%d error=%v", version, err)
	}
	var score float64
	if err := pool.QueryRow(ctx, "SELECT score FROM movie_matches WHERE source_movie_id='201'").Scan(&score); err != nil || score != .92 {
		t.Fatalf("stored candidate score=%v error=%v", score, err)
	}
	if err := store.RejectReview(ctx, SourceUGC, "201", now.Add(2*time.Minute)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("second decision error=%v", err)
	}
	manualMetadata := metadata
	manualMetadata.ProviderMovieID, manualMetadata.ProviderTitle, manualMetadata.LocalizedTitle, manualMetadata.RuntimeMinutes = 999, "Film sans correspondance", "Film sans correspondance", 97
	if err := store.ApproveReview(ctx, SourceUGC, "206", 999, manualMetadata, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT score FROM movie_matches WHERE source_movie_id='206'").Scan(&score); err != nil || score != 1 {
		t.Fatalf("unmatched manual score=%v error=%v", score, err)
	}
	manualMetadata.ProviderMovieID, manualMetadata.ProviderTitle, manualMetadata.LocalizedTitle, manualMetadata.RuntimeMinutes = 888, "Ancien film", "Ancien film", 88
	if err := store.ApproveReview(ctx, SourceUGC, "205", 888, manualMetadata, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT score FROM movie_matches WHERE source_movie_id='205'").Scan(&score); err != nil || score != 1 {
		t.Fatalf("review-required manual score=%v error=%v", score, err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 4 {
		t.Fatalf("manual approval version=%d error=%v", version, err)
	}

	stale := review
	stale.SourceMovieID, stale.NormalizedSourceTitle, stale.SourceRuntimeMinutes = "202", NormalizeTitle("Ancien titre"), 90
	if err := store.SaveDecision(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReviewCandidate(ctx, SourceUGC, "202", 52); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("stale candidate error=%v", err)
	}
	if err := store.RejectReview(ctx, SourceUGC, "202", now.Add(3*time.Minute)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("stale rejection error=%v", err)
	}
	changing := review
	changing.SourceMovieID, changing.NormalizedSourceTitle, changing.SourceRuntimeMinutes = "209", NormalizeTitle("Film qui change"), 102
	changing.Candidates = []Candidate{{ID: 79, Title: "Film qui change", Runtime: 102, Score: .9}}
	if err := store.SaveDecision(ctx, changing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReviewCandidate(ctx, SourceUGC, "209", 79); err != nil {
		t.Fatalf("changing preflight error=%v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE movies SET title='Film changé' WHERE provider_id='209'"); err != nil {
		t.Fatal(err)
	}
	changingMetadata := metadata
	changingMetadata.ProviderMovieID, changingMetadata.ProviderTitle, changingMetadata.LocalizedTitle, changingMetadata.RuntimeMinutes = 79, "Film changé", "Film changé", 102
	if err := store.ApproveReview(ctx, SourceUGC, "209", 79, changingMetadata, now.Add(3*time.Minute)); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("locked stale approval error=%v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 4 {
		t.Fatalf("stale approval version=%d error=%v", version, err)
	}

	rejected := review
	rejected.SourceMovieID, rejected.NormalizedSourceTitle, rejected.SourceRuntimeMinutes = "203", NormalizeTitle("Film refusé"), 95
	if err := store.SaveDecision(ctx, rejected); err != nil {
		t.Fatal(err)
	}
	if err := store.RejectReview(ctx, SourceUGC, "203", now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	loadedMatch, found, err = store.Match(ctx, SourceUGC, "203", ProviderTMDB)
	if err != nil || !found || loadedMatch.Status != StatusRejected {
		t.Fatalf("rejected match=%+v found=%v err=%v", loadedMatch, found, err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 5 {
		t.Fatalf("review rejection version=%d error=%v", version, err)
	}
	overwrite := rejected
	overwrite.Status = StatusReviewRequired
	if err := store.SaveDecision(ctx, overwrite); err != nil {
		t.Fatal(err)
	}
	loadedMatch, found, err = store.Match(ctx, SourceUGC, "203", ProviderTMDB)
	if err != nil || !found || loadedMatch.Status != StatusRejected {
		t.Fatalf("rejection overwritten by decision: match=%+v err=%v", loadedMatch, err)
	}
	if _, err := store.ReviewCandidate(ctx, SourceUGC, "203", 999); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("rejected assignment error=%v", err)
	}
	if _, err := store.ReviewCandidate(ctx, SourceUGC, "207", 999); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("matched assignment error=%v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO movies (provider_id, slug, title, runtime_minutes) VALUES ('204','ugc-film-204','Film concurrent',105)`); err != nil {
		t.Fatal(err)
	}
	concurrent := review
	concurrent.SourceMovieID, concurrent.NormalizedSourceTitle, concurrent.SourceRuntimeMinutes = "204", NormalizeTitle("Film concurrent"), 105
	concurrent.Candidates = []Candidate{{ID: 72, Title: "Film concurrent", Runtime: 105, Score: .95}}
	if err := store.SaveDecision(ctx, concurrent); err != nil {
		t.Fatal(err)
	}
	concurrentMetadata := metadata
	concurrentMetadata.ProviderMovieID, concurrentMetadata.ProviderTitle, concurrentMetadata.LocalizedTitle, concurrentMetadata.RuntimeMinutes = 72, "Film concurrent", "Film concurrent", 105
	results := make(chan error, 2)
	go func() {
		results <- store.ApproveReview(ctx, SourceUGC, "204", 72, concurrentMetadata, now.Add(5*time.Minute))
	}()
	go func() { results <- store.RejectReview(ctx, SourceUGC, "204", now.Add(5*time.Minute)) }()
	first, second := <-results, <-results
	if (first == nil) == (second == nil) || first != nil && !errors.Is(first, ErrReviewConflict) || second != nil && !errors.Is(second, ErrReviewConflict) {
		t.Fatalf("concurrent decisions first=%v second=%v", first, second)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 6 {
		t.Fatalf("concurrent review version=%d error=%v", version, err)
	}
}
