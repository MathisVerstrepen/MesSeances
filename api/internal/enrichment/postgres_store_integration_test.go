package enrichment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
	"messeances/api/internal/tmdb"
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
	if _, err := pool.Exec(ctx, `INSERT INTO schedule_snapshot (version, schema_version, provider, scope, generated_at, timezone, window_from, window_through)
VALUES (1,1,'combined','all_cinemas',$1,'Europe/Paris','2026-08-16','2026-08-23')`, now); err != nil {
		t.Fatal("insert active schedule snapshot failed")
	}
	t.Run("current unresolved rerun selection", func(t *testing.T) {
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM local_movie_groups WHERE primary_source_provider='ugc' AND primary_source_movie_id='406'")
			_, _ = pool.Exec(context.Background(), "DELETE FROM movie_matches WHERE source_movie_id IN ('401','402','403','404','405','406','499')")
			_, _ = pool.Exec(context.Background(), "DELETE FROM showtimes WHERE theater_id='ugc-9901'")
			_, _ = pool.Exec(context.Background(), "DELETE FROM theater_dates WHERE theater_id='ugc-9901'")
			_, _ = pool.Exec(context.Background(), "DELETE FROM theaters WHERE id='ugc-9901'")
			_, _ = pool.Exec(context.Background(), "DELETE FROM movies WHERE provider_id IN ('401','402','403','404','405','406','499')")
		}()
		if _, err := pool.Exec(ctx, `INSERT INTO theaters (generation_id,id,provider_id,slug,name,address,city,postal_code) VALUES
			(1,'ugc-9901','9901','ugc-9901','Test active','Test','Lille','59000'),
			(2,'ugc-9901','9901','ugc-9901','Test inactive','Test','Lille','59000')`); err != nil {
			t.Fatalf("insert rerun theaters failed: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO theater_dates (generation_id,theater_id,service_date) VALUES (1,'ugc-9901','2026-08-17'),(2,'ugc-9901','2026-08-17')`); err != nil {
			t.Fatalf("insert rerun dates failed: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES
			(1,'ugc','401','ugc-film-401','Missing',90),
			(1,'kinepolis','401','kinepolis-film-401','Missing Kinepolis',91),
			(1,'ugc','402','ugc-film-402','Unmatched',92),
			(1,'ugc','403','ugc-film-403','Review',93),
			(1,'ugc','404','ugc-film-404','Matched',94),
			(1,'ugc','405','ugc-film-405','Rejected',95),
			(1,'ugc','406','ugc-film-406','Local',96),
			(2,'ugc','499','ugc-film-499','Inactive',97)`); err != nil {
			t.Fatalf("insert rerun movies failed: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES
			(1,'ugc-showing-4011','4011','2026-08-17','ugc-9901','401','2026-08-17T18:00:00Z','2026-08-17T19:30:00Z','VF','VF','2D','1','https://example.test/4011','ugc'),
			(1,'ugc-showing-4010','4010','2026-08-17','ugc-9901','401','2026-08-17T10:00:00Z','2026-08-17T11:30:00Z','VF','VF','2D','1','https://example.test/4010','ugc'),
			(1,'kinepolis-showing-4012','4012','2026-08-17','ugc-9901','401','2026-08-17T11:00:00Z','2026-08-17T12:31:00Z','VF','VF','2D','1','https://example.test/4012','kinepolis'),
			(1,'ugc-showing-4020','4020','2026-08-17','ugc-9901','402','2026-08-17T12:00:00Z','2026-08-17T13:32:00Z','VF','VF','2D','1','https://example.test/4020','ugc'),
			(1,'ugc-showing-4030','4030','2026-08-17','ugc-9901','403','2026-08-17T13:00:00Z','2026-08-17T14:33:00Z','VF','VF','2D','1','https://example.test/4030','ugc'),
			(1,'ugc-showing-4040','4040','2026-08-17','ugc-9901','404','2026-08-17T14:00:00Z','2026-08-17T15:34:00Z','VF','VF','2D','1','https://example.test/4040','ugc'),
			(1,'ugc-showing-4050','4050','2026-08-17','ugc-9901','405','2026-08-17T15:00:00Z','2026-08-17T16:35:00Z','VF','VF','2D','1','https://example.test/4050','ugc'),
			(1,'ugc-showing-4060','4060','2026-08-17','ugc-9901','406','2026-08-17T16:00:00Z','2026-08-17T17:36:00Z','VF','VF','2D','1','https://example.test/4060','ugc'),
			(2,'ugc-showing-4990','4990','2026-08-17','ugc-9901','499','2026-08-17T17:00:00Z','2026-08-17T18:37:00Z','VF','VF','2D','1','https://example.test/4990','ugc')`); err != nil {
			t.Fatalf("insert rerun showtimes failed: %v", err)
		}
		for movieID, status := range map[string]string{"402": StatusUnmatched, "403": StatusReviewRequired, "404": StatusMatched, "405": StatusRejected} {
			decision := Match{SourceProvider: SourceUGC, SourceMovieID: movieID, MetadataProvider: ProviderTMDB, Status: status, NormalizedSourceTitle: strings.ToLower(map[string]string{"402": "Unmatched", "403": "Review", "404": "Matched", "405": "Rejected"}[movieID]), SourceRuntimeMinutes: map[string]int{"402": 92, "403": 93, "404": 94, "405": 95}[movieID], Candidates: []Candidate{}, EvaluatedAt: now, RetryAfter: now.Add(time.Hour)}
			if status == StatusMatched {
				decision.MetadataMovieID, decision.Score = 4040, 1
				decision.Candidates = []Candidate{{ID: 4040, Title: "Matched", Runtime: 94, Score: 1}}
			}
			if err := store.SaveDecision(ctx, decision); err != nil {
				t.Fatalf("save %s fixture failed: %v", status, err)
			}
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var localID int64
		if err := tx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider,primary_source_movie_id) VALUES ('ugc','406') RETURNING id").Scan(&localID); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id,source_provider,source_movie_id) VALUES ($1,'ugc','406')", localID); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}

		movies, err := store.UnresolvedMovies(ctx)
		want := []struct{ provider, id string }{{SourceKinepolis, "401"}, {SourceUGC, "401"}, {SourceUGC, "402"}, {SourceUGC, "403"}}
		if err != nil || len(movies) != len(want) {
			t.Fatalf("movies=%+v err=%v", movies, err)
		}
		for index, expected := range want {
			if movies[index].SourceProvider != expected.provider || movies[index].ProviderID != expected.id {
				t.Fatalf("movie[%d]=%+v want=%s/%s", index, movies[index], expected.provider, expected.id)
			}
		}
		if wantFirst := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC); !movies[1].FirstShowingAt.Equal(wantFirst) {
			t.Fatalf("first showing=%s want=%s", movies[1].FirstShowingAt, wantFirst)
		}
	})
	unmatched := Match{SourceProvider: SourceUGC, SourceMovieID: "200", MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 721, Candidates: []Candidate{}, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL)}
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
	matched.Candidates = []Candidate{{ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: 721, Score: 1}}
	matched.RetryAfter = now.Add(metadataTTL)
	metadata := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", Overview: "Résumé", ReleaseDate: "2026-01-02", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg", TrailerYouTubeKey: "FRoff123456", RuntimeMinutes: 721, Genres: []string{"Drame"}, FetchedAt: now, RefreshAfter: now.Add(metadataTTL)}
	if err := store.Publish(ctx, matched, metadata); err != nil {
		t.Fatal(err)
	}
	loadedMetadata, found, err := store.Metadata(ctx, ProviderTMDB, 42, LocaleFrench)
	if err != nil || !found || loadedMetadata.RuntimeMinutes != 721 || loadedMetadata.Overview != "Résumé" || loadedMetadata.BackdropURL != metadata.BackdropURL || loadedMetadata.TrailerYouTubeKey != metadata.TrailerYouTubeKey || len(loadedMetadata.Genres) != 1 {
		t.Fatalf("metadata=%+v found=%v error=%v", loadedMetadata, found, err)
	}
	var publicTrailerYouTubeKey string
	if err := pool.QueryRow(ctx, "SELECT trailer_youtube_key FROM public_movies WHERE confirmed_tmdb_id=42 AND redirect_to_id IS NULL").Scan(&publicTrailerYouTubeKey); err != nil || publicTrailerYouTubeKey != metadata.TrailerYouTubeKey {
		t.Fatalf("public trailer key=%q error=%v", publicTrailerYouTubeKey, err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 1 {
		t.Fatalf("published version=%d error=%v", version, err)
	}
	for index, runtime := range []int{719, 723, 718} {
		donor := matched
		donor.SourceProvider = SourceKinepolis
		donor.SourceMovieID = fmt.Sprintf("DONOR-%d", index)
		donor.SourceRuntimeMinutes = runtime
		donor.MetadataMovieID = int64(100 + index)
		donor.Candidates = []Candidate{{ID: donor.MetadataMovieID, Title: "Film", Runtime: runtime, Score: 1}}
		if err := store.SaveDecision(ctx, donor); err != nil {
			t.Fatal(err)
		}
	}
	confirmed, err := store.ConfirmedMatches(ctx, SourceUGC, ProviderTMDB, 719, 723)
	if err != nil || len(confirmed) != 2 || confirmed[0].SourceRuntimeMinutes != 719 || confirmed[1].SourceRuntimeMinutes != 723 {
		t.Fatalf("confirmed=%+v error=%v", confirmed, err)
	}

	invalid := metadata
	invalid.ProviderMovieID = 0
	if err := store.Publish(ctx, matched, invalid); err == nil {
		t.Fatal("invalid metadata publication accepted")
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed publication changed version=%d error=%v", version, err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider_id, slug, title, runtime_minutes, poster_url) VALUES
	(1,'50','ugc-film-50','000 alpha',90,NULL),
	(1,'51','ugc-film-51','000 Alpha',90,NULL),
	(1,'52','ugc-film-52','000 beta',90,NULL),
	(1,'17950','ugc-film-17950','Film historique',100,NULL),
	(1,'201','ugc-film-201','Film à revoir',100,'https://static.ugc.fr/posters/201.jpg'),
	(1,'202','ugc-film-202','Titre modifié',90,NULL),
	(1,'203','ugc-film-203','Film refusé',95,NULL),
	(1,'205','ugc-film-205','Ancien film',88,NULL),
	(1,'206','ugc-film-206','Film sans correspondance',97,NULL),
	(1,'207','ugc-film-207','Film déjà associé',99,NULL),
	(1,'208','ugc-film-208','Film déjà refusé',96,NULL),
	(1,'209','ugc-film-209','Film qui change',102,NULL),
	(1,'210','ugc-film-210','Film historique refusé',103,NULL)`); err != nil {
		t.Fatalf("insert review movies failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider, provider_id, slug, title, runtime_minutes) VALUES
	(1,'kinepolis','CASE-K','kinepolis-film-CASE-K','000 ALPHA',90),
	(1,'kinepolis','HO00016099','kinepolis-film-HO00016099','Film historique',100)`); err != nil {
		t.Fatal("insert missing Kinepolis review movie failed")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider_id, slug, title, runtime_minutes) VALUES (2,'201','ugc-film-201','Inactive poison',1)`); err != nil {
		t.Fatal("insert inactive duplicate movie failed")
	}
	pendingActive, err := store.PendingMatches(ctx, PendingMatchFilterUnresolved, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	active201 := 0
	for _, item := range pendingActive {
		if item.SourceProvider == SourceUGC && item.SourceMovieID == "201" {
			active201++
			if item.SourceTitle != "Film à revoir" || item.SourceRuntimeMinutes != 100 {
				t.Fatalf("inactive movie leaked into pending review: %+v", item)
			}
		}
	}
	if active201 != 1 {
		t.Fatalf("active movie 201 count=%d", active201)
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
	items, err := store.PendingMatches(ctx, PendingMatchFilterUnresolved, 50, 0)
	if err != nil || len(items) != 13 {
		t.Fatalf("pending=%+v err=%v", items, err)
	}
	expectedOrder := []struct {
		provider string
		movieID  string
	}{
		{SourceKinepolis, "CASE-K"},
		{SourceUGC, "50"},
		{SourceUGC, "51"},
		{SourceUGC, "52"},
	}
	for index, expected := range expectedOrder {
		if items[index].SourceProvider != expected.provider || items[index].SourceMovieID != expected.movieID {
			t.Fatalf("pending order[%d]=%s/%s want=%s/%s", index, items[index].SourceProvider, items[index].SourceMovieID, expected.provider, expected.movieID)
		}
	}
	pendingByID := make(map[string]PendingMatch, len(items))
	for _, item := range items {
		pendingByID[item.SourceProvider+"/"+item.SourceMovieID] = item
	}
	item201 := pendingByID[SourceUGC+"/201"]
	item205 := pendingByID[SourceUGC+"/205"]
	item206 := pendingByID[SourceUGC+"/206"]
	itemKinepolis := pendingByID[SourceKinepolis+"/HO00016099"]
	itemUGC := pendingByID[SourceUGC+"/17950"]
	if item201.Status != StatusReviewRequired || item201.SourceTitle != "Film à revoir" || item201.SourcePosterURL != "https://static.ugc.fr/posters/201.jpg" || item201.Candidates[0].PosterURL != "https://image.tmdb.org/t/p/w500/52.jpg" || item205.Status != StatusReviewRequired || item205.SourcePosterURL != "" || item205.Candidates[0].PosterURL != "" || item206.Status != StatusUnmatched || itemKinepolis.Status != StatusReviewRequired || len(itemKinepolis.Candidates) != 0 || itemKinepolis.EvaluatedAt.IsZero() || itemUGC.Status != StatusReviewRequired || len(itemUGC.Candidates) != 0 || itemUGC.EvaluatedAt.IsZero() {
		t.Fatalf("pending by ID=%+v", pendingByID)
	}
	if _, exists := pendingByID[SourceUGC+"/208"]; exists {
		t.Fatalf("rejected match leaked into unresolved results: %+v", pendingByID[SourceUGC+"/208"])
	}
	rejectedItems, err := store.PendingMatches(ctx, PendingMatchFilterRejected, 50, 0)
	if err != nil || len(rejectedItems) != 1 || rejectedItems[0].SourceProvider != SourceUGC || rejectedItems[0].SourceMovieID != "208" || rejectedItems[0].Status != StatusRejected {
		t.Fatalf("rejected=%+v err=%v", rejectedItems, err)
	}
	paged, err := store.PendingMatches(ctx, PendingMatchFilterUnresolved, 2, 1)
	if err != nil || len(paged) != 2 || paged[0].SourceProvider != SourceUGC || paged[0].SourceMovieID != "50" || paged[1].SourceProvider != SourceUGC || paged[1].SourceMovieID != "51" {
		t.Fatalf("paged pending=%+v err=%v", paged, err)
	}
	for _, source := range []struct {
		provider string
		movieID  string
	}{
		{SourceKinepolis, "HO00016099"},
		{SourceUGC, "17950"},
	} {
		candidate, runtime, err := store.ReviewCandidate(ctx, source.provider, source.movieID, 999)
		if err != nil || candidate.ID != 999 || candidate.Score != 1 || runtime != 100 {
			t.Fatalf("missing-row candidate source=%+v candidate=%+v runtime=%d error=%v", source, candidate, runtime, err)
		}
	}
	manualCandidate, _, err := store.ReviewCandidate(ctx, SourceUGC, "201", 999)
	if err != nil || manualCandidate.ID != 999 || manualCandidate.Score != 1 {
		t.Fatalf("manual candidate=%+v error=%v", manualCandidate, err)
	}
	storedCandidate, _, err := store.ReviewCandidate(ctx, SourceUGC, "201", 52)
	if err != nil || storedCandidate.ID != 52 || storedCandidate.Score != .92 {
		t.Fatalf("stored candidate=%+v error=%v", storedCandidate, err)
	}
	approvedMetadata := metadata
	approvedMetadata.ProviderMovieID = 52
	approvedMetadata.ProviderTitle = "Film à revoir"
	approvedMetadata.LocalizedTitle = "Film à revoir"
	if err := store.ApproveReview(ctx, SourceUGC, "201", 52, approvedMetadata, 0, now.Add(time.Minute)); err != nil {
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
	if err := store.ApproveReview(ctx, SourceUGC, "206", 999, manualMetadata, 0, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT score FROM movie_matches WHERE source_movie_id='206'").Scan(&score); err != nil || score != 1 {
		t.Fatalf("unmatched manual score=%v error=%v", score, err)
	}
	manualMetadata.ProviderMovieID, manualMetadata.ProviderTitle, manualMetadata.LocalizedTitle, manualMetadata.RuntimeMinutes = 888, "Ancien film", "Ancien film", 88
	if err := store.ApproveReview(ctx, SourceUGC, "205", 888, manualMetadata, 0, now.Add(3*time.Minute)); err != nil {
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
	if _, _, err := store.ReviewCandidate(ctx, SourceUGC, "202", 52); !errors.Is(err, ErrReviewConflict) {
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
	if _, _, err := store.ReviewCandidate(ctx, SourceUGC, "209", 79); err != nil {
		t.Fatalf("changing preflight error=%v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE movies SET title='Film changé' WHERE provider_id='209'"); err != nil {
		t.Fatal(err)
	}
	changingMetadata := metadata
	changingMetadata.ProviderMovieID, changingMetadata.ProviderTitle, changingMetadata.LocalizedTitle, changingMetadata.RuntimeMinutes = 79, "Film changé", "Film changé", 102
	if err := store.ApproveReview(ctx, SourceUGC, "209", 79, changingMetadata, 0, now.Add(3*time.Minute)); !errors.Is(err, ErrReviewConflict) {
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
	if _, _, err := store.ReviewCandidate(ctx, SourceUGC, "203", 999); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("rejected assignment error=%v", err)
	}
	if _, _, err := store.ReviewCandidate(ctx, SourceUGC, "207", 999); !errors.Is(err, ErrReviewConflict) {
		t.Fatalf("matched assignment error=%v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider_id, slug, title, runtime_minutes) VALUES (1,'204','ugc-film-204','Film concurrent',105)`); err != nil {
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
		results <- store.ApproveReview(ctx, SourceUGC, "204", 72, concurrentMetadata, 0, now.Add(5*time.Minute))
	}()
	go func() { results <- store.RejectReview(ctx, SourceUGC, "204", now.Add(5*time.Minute)) }()
	first, second := <-results, <-results
	if (first == nil) == (second == nil) || first != nil && !errors.Is(first, ErrReviewConflict) || second != nil && !errors.Is(second, ErrReviewConflict) {
		t.Fatalf("concurrent decisions first=%v second=%v", first, second)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 6 {
		t.Fatalf("concurrent review version=%d error=%v", version, err)
	}

	for index, source := range []struct {
		provider string
		movieID  string
		tmdbID   int64
	}{
		{SourceKinepolis, "HO00016099", 16099},
		{SourceUGC, "17950", 179500},
	} {
		resolvedMetadata := metadata
		resolvedMetadata.ProviderMovieID = source.tmdbID
		resolvedMetadata.ProviderTitle = "Film historique"
		resolvedMetadata.LocalizedTitle = "Film historique"
		if err := store.ApproveReview(ctx, source.provider, source.movieID, source.tmdbID, resolvedMetadata, 0, now.Add(time.Duration(6+index)*time.Minute)); err != nil {
			t.Fatalf("approve missing-row source=%+v error=%v", source, err)
		}
		resolved, found, err := store.Match(ctx, source.provider, source.movieID, ProviderTMDB)
		if err != nil || !found || resolved.Status != StatusMatched || resolved.MetadataMovieID != source.tmdbID || resolved.Score != 1 {
			t.Fatalf("resolved missing-row source=%+v match=%+v found=%t error=%v", source, resolved, found, err)
		}
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 8 {
		t.Fatalf("missing-row approvals version=%d error=%v", version, err)
	}
	if err := store.RejectReview(ctx, SourceUGC, "210", now.Add(8*time.Minute)); err != nil {
		t.Fatalf("reject missing-row error=%v", err)
	}
	rejectedMissing, found, err := store.Match(ctx, SourceUGC, "210", ProviderTMDB)
	if err != nil || !found || rejectedMissing.Status != StatusRejected {
		t.Fatalf("rejected missing-row match=%+v found=%t error=%v", rejectedMissing, found, err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != 9 {
		t.Fatalf("missing-row rejection version=%d error=%v", version, err)
	}

	t.Run("local movie groups", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider_id, slug, title, runtime_minutes, poster_url) VALUES
			(1,'301','ugc-film-301','Local principal',101,'https://static.ugc.fr/posters/301.jpg'),
			(1,'302','ugc-film-302','Local secondaire',102,NULL),
			(1,'303','ugc-film-303','Local rejeté associé',102,NULL),
			(1,'310','ugc-film-310','Concurrent commun',100,NULL),
			(1,'311','ugc-film-311','Concurrent A',100,NULL),
			(1,'312','ugc-film-312','Concurrent B',100,NULL),
			(1,'320','ugc-film-320','Décision concurrente',103,NULL),
			(1,'321','ugc-film-321','Fusion concurrente',103,NULL)`); err != nil {
			t.Fatal("insert local UGC movies failed")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider, provider_id, slug, title, runtime_minutes, poster_url) VALUES
			(1,'kinepolis','LOCAL-A','kinepolis-film-LOCAL-A','Local Kinepolis',99,'https://cdn.kinepolis.fr/images/local-a.jpg')`); err != nil {
			t.Fatal("insert local Kinepolis movie failed")
		}
		rejected := Match{SourceProvider: SourceUGC, SourceMovieID: "302", MetadataProvider: ProviderTMDB, Status: StatusRejected, NormalizedSourceTitle: NormalizeTitle("Local secondaire"), SourceRuntimeMinutes: 102, Candidates: []Candidate{}, EvaluatedAt: now, RetryAfter: now}
		if err := store.SaveDecision(ctx, rejected); err != nil {
			t.Fatal("save rejected local candidate failed")
		}
		before := version
		primary := LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "301"}
		secondary := LocalMovieSource{SourceProvider: SourceKinepolis, SourceMovieID: "LOCAL-A"}
		group, err := store.MergeLocalMovies(ctx, []LocalMovieSource{secondary, primary}, primary)
		if err != nil || group.ID <= 0 || group.LocalMovieID != LocalMovieID(group.ID) || len(group.Members) != 2 || group.MetadataSource == nil || *group.MetadataSource != primary {
			t.Fatalf("merged group=%+v error=%v", group, err)
		}
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != before+1 {
			t.Fatalf("merge version=%d before=%d error=%v", version, before, err)
		}
		rejectedGroup, err := store.MergeLocalMovies(ctx, []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "302"}, {SourceProvider: SourceUGC, SourceMovieID: "303"}}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "302"})
		if err != nil || rejectedGroup.ID <= group.ID {
			t.Fatalf("rejected candidate group=%+v first=%+v error=%v", rejectedGroup, group, err)
		}
		groups, err := store.LocalMovieGroups(ctx, 1, 0)
		if err != nil || len(groups) != 1 || groups[0].ID != group.ID || len(groups[0].Members) != 2 {
			t.Fatalf("groups=%+v error=%v", groups, err)
		}
		paged, err := store.LocalMovieGroups(ctx, 1, 1)
		if err != nil || len(paged) != 1 || paged[0].ID != rejectedGroup.ID {
			t.Fatalf("paged groups=%+v error=%v", paged, err)
		}
		pending, err := store.PendingMatches(ctx, PendingMatchFilterUnresolved, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range pending {
			if item.SourceProvider == primary.SourceProvider && item.SourceMovieID == primary.SourceMovieID || item.SourceProvider == secondary.SourceProvider && item.SourceMovieID == secondary.SourceMovieID {
				t.Fatalf("local member remained pending: %+v", item)
			}
		}
		if _, _, err := store.ReviewCandidate(ctx, primary.SourceProvider, primary.SourceMovieID, 42); !errors.Is(err, ErrReviewConflict) {
			t.Fatalf("local review preflight error=%v", err)
		}
		if err := store.RejectReview(ctx, primary.SourceProvider, primary.SourceMovieID, now.Add(time.Hour)); !errors.Is(err, ErrReviewConflict) {
			t.Fatalf("local rejection error=%v", err)
		}
		blocked := Match{SourceProvider: primary.SourceProvider, SourceMovieID: primary.SourceMovieID, MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: NormalizeTitle("Local principal"), SourceRuntimeMinutes: 101, Candidates: []Candidate{}, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL)}
		if err := store.SaveDecision(ctx, blocked); !errors.Is(err, ErrLocalMovieConflict) {
			t.Fatalf("local matcher decision error=%v", err)
		}
		blocked.Status, blocked.MetadataMovieID, blocked.Score = StatusMatched, 3010, 1
		blocked.RetryAfter = now.Add(metadataTTL)
		blockedMetadata := metadata
		blockedMetadata.ProviderMovieID, blockedMetadata.ProviderTitle, blockedMetadata.LocalizedTitle, blockedMetadata.RuntimeMinutes = 3010, "Local principal", "Local principal", 101
		if err := store.Publish(ctx, blocked, blockedMetadata); !errors.Is(err, ErrLocalMovieConflict) {
			t.Fatalf("local matcher publication error=%v", err)
		}
		if err := store.UnmergeLocalMovie(ctx, group.ID); err != nil {
			t.Fatal("unmerge local group failed")
		}
		if err := store.UnmergeLocalMovie(ctx, rejectedGroup.ID); err != nil {
			t.Fatal("unmerge rejected candidate group failed")
		}
		if err := store.UnmergeLocalMovie(ctx, group.ID); !errors.Is(err, ErrLocalMovieNotFound) {
			t.Fatalf("second unmerge error=%v", err)
		}
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != before+4 {
			t.Fatalf("unmerge version=%d before=%d error=%v", version, before, err)
		}

		before = version
		if _, err := store.MergeLocalMovies(ctx, []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "302"}, {SourceProvider: SourceUGC, SourceMovieID: "207"}}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "302"}); !errors.Is(err, ErrLocalMovieConflict) {
			t.Fatalf("matched source merge error=%v", err)
		}
		if _, err := store.MergeLocalMovies(ctx, []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "302"}, {SourceProvider: SourceUGC, SourceMovieID: "202"}}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "302"}); !errors.Is(err, ErrLocalMovieConflict) {
			t.Fatalf("stale source merge error=%v", err)
		}
		if _, err := store.MergeLocalMovies(ctx, []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "302"}, {SourceProvider: SourceUGC, SourceMovieID: "999"}}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "302"}); !errors.Is(err, ErrLocalMovieConflict) {
			t.Fatalf("missing source merge error=%v", err)
		}
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != before {
			t.Fatalf("failed merge version=%d before=%d error=%v", version, before, err)
		}

		common := LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "310"}
		before = version
		mergeResults := make(chan struct {
			group LocalMovieGroup
			err   error
		}, 2)
		go func() {
			group, err := store.MergeLocalMovies(ctx, []LocalMovieSource{common, {SourceProvider: SourceUGC, SourceMovieID: "311"}}, common)
			mergeResults <- struct {
				group LocalMovieGroup
				err   error
			}{group, err}
		}()
		go func() {
			group, err := store.MergeLocalMovies(ctx, []LocalMovieSource{common, {SourceProvider: SourceUGC, SourceMovieID: "312"}}, common)
			mergeResults <- struct {
				group LocalMovieGroup
				err   error
			}{group, err}
		}()
		mergeFirst, mergeSecond := <-mergeResults, <-mergeResults
		if (mergeFirst.err == nil) == (mergeSecond.err == nil) || mergeFirst.err != nil && !errors.Is(mergeFirst.err, ErrLocalMovieConflict) || mergeSecond.err != nil && !errors.Is(mergeSecond.err, ErrLocalMovieConflict) {
			t.Fatalf("concurrent merges first=%+v second=%+v", mergeFirst, mergeSecond)
		}
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != before+1 {
			t.Fatalf("concurrent merge version=%d before=%d error=%v", version, before, err)
		}
		winner := mergeFirst.group
		if mergeFirst.err != nil {
			winner = mergeSecond.group
		}
		if err := store.UnmergeLocalMovie(ctx, winner.ID); err != nil {
			t.Fatal("cleanup concurrent merge failed")
		}

		raceReview := Match{SourceProvider: SourceUGC, SourceMovieID: "320", MetadataProvider: ProviderTMDB, Status: StatusReviewRequired, NormalizedSourceTitle: NormalizeTitle("Décision concurrente"), SourceRuntimeMinutes: 103, Candidates: []Candidate{{ID: 3200, Title: "Décision concurrente", Runtime: 103, Score: .9}}, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL)}
		if err := store.SaveDecision(ctx, raceReview); err != nil {
			t.Fatal("save race review failed")
		}
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&before); err != nil {
			t.Fatal("read pre-race version failed")
		}
		raceMetadata := metadata
		raceMetadata.ProviderMovieID, raceMetadata.ProviderTitle, raceMetadata.LocalizedTitle, raceMetadata.RuntimeMinutes = 3200, "Décision concurrente", "Décision concurrente", 103
		raceResults := make(chan error, 2)
		go func() {
			_, err := store.MergeLocalMovies(ctx, []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "320"}, {SourceProvider: SourceUGC, SourceMovieID: "321"}}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "320"})
			raceResults <- err
		}()
		go func() {
			raceResults <- store.ApproveReview(ctx, SourceUGC, "320", 3200, raceMetadata, 0, now.Add(2*time.Hour))
		}()
		raceFirst, raceSecond := <-raceResults, <-raceResults
		if (raceFirst == nil) == (raceSecond == nil) || raceFirst != nil && !errors.Is(raceFirst, ErrLocalMovieConflict) && !errors.Is(raceFirst, ErrReviewConflict) || raceSecond != nil && !errors.Is(raceSecond, ErrLocalMovieConflict) && !errors.Is(raceSecond, ErrReviewConflict) {
			t.Fatalf("merge/approval race first=%v second=%v", raceFirst, raceSecond)
		}
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true").Scan(&version); err != nil || version != before+1 {
			t.Fatalf("merge/approval version=%d before=%d error=%v", version, before, err)
		}
	})

	t.Run("reject unmatched Kinepolis review", func(t *testing.T) {
		const movieID = "HO00016253"
		const title = "Film Kinepolis sans correspondance"
		if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id, provider, provider_id, slug, title, runtime_minutes)
VALUES (1,'kinepolis',$1,'kinepolis-film-HO00016253',$2,104)`, movieID, title); err != nil {
			t.Fatal("insert unmatched Kinepolis movie failed")
		}
		decision := Match{
			SourceProvider:        SourceKinepolis,
			SourceMovieID:         movieID,
			MetadataProvider:      ProviderTMDB,
			Status:                StatusUnmatched,
			NormalizedSourceTitle: NormalizeTitle(title),
			SourceRuntimeMinutes:  104,
			Candidates:            []Candidate{},
			EvaluatedAt:           now,
			RetryAfter:            now.Add(decisionTTL),
		}
		if err := store.SaveDecision(ctx, decision); err != nil {
			t.Fatal("save unmatched Kinepolis decision failed")
		}
		if err := store.RejectReview(ctx, SourceKinepolis, movieID, now.Add(3*time.Hour)); err != nil {
			t.Fatalf("reject unmatched Kinepolis review error=%v", err)
		}
		rejected, found, err := store.Match(ctx, SourceKinepolis, movieID, ProviderTMDB)
		if err != nil || !found || rejected.Status != StatusRejected {
			t.Fatalf("rejected Kinepolis match=%+v found=%t error=%v", rejected, found, err)
		}
		if err := store.RejectReview(ctx, SourceKinepolis, movieID, now.Add(4*time.Hour)); !errors.Is(err, ErrReviewConflict) {
			t.Fatalf("second Kinepolis rejection error=%v", err)
		}
		pending, err := store.PendingMatches(ctx, PendingMatchFilterRejected, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range pending {
			if item.SourceProvider == SourceKinepolis && item.SourceMovieID == movieID {
				if item.Status != StatusRejected {
					t.Fatalf("pending Kinepolis status=%q", item.Status)
				}
				return
			}
		}
		t.Fatal("rejected Kinepolis movie missing from pending matches")
	})

	t.Run("generation promotion lock serializes local merge", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id,provider_id,slug,title,runtime_minutes) VALUES (1,'901','ugc-film-901','Lock A',90),(1,'902','ugc-film-902','Lock B',90)`); err != nil {
			t.Fatal("insert lock test movies failed")
		}
		publicationTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(publicationTx)
		if _, err := publicationTx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, scheduleGenerationLockID); err != nil {
			t.Fatal("acquire publication lock failed")
		}
		result := make(chan error, 1)
		go func() {
			_, mergeErr := store.MergeLocalMovies(context.Background(), []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "901"}, {SourceProvider: SourceUGC, SourceMovieID: "902"}}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "901"})
			result <- mergeErr
		}()
		deadline := time.Now().Add(time.Second)
		blocked := false
		for time.Now().Before(deadline) {
			if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock%')`).Scan(&blocked); err != nil {
				t.Fatal("inspect advisory wait failed")
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("local merge did not wait for generation promotion lock")
		}
		if err := publicationTx.Commit(ctx); err != nil {
			t.Fatal("release publication lock failed")
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("serialized merge failed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("serialized merge did not resume")
		}
	})

	t.Run("generation promotion lock serializes matcher publish", func(t *testing.T) {
		publicationTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(publicationTx)
		if _, err := publicationTx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, scheduleGenerationLockID); err != nil {
			t.Fatal("acquire publication lock failed")
		}
		match := Match{SourceProvider: SourceUGC, SourceMovieID: "50", MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: 9950, Score: 1, NormalizedSourceTitle: "000 alpha", SourceRuntimeMinutes: 90, Candidates: []Candidate{{ID: 9950, Title: "000 alpha", Runtime: 90, Score: 1}}, EvaluatedAt: now, RetryAfter: now.Add(metadataTTL)}
		publishedMetadata := metadata
		publishedMetadata.ProviderMovieID, publishedMetadata.ProviderTitle, publishedMetadata.LocalizedTitle, publishedMetadata.RuntimeMinutes = 9950, "000 alpha", "000 alpha", 90
		result := make(chan error, 1)
		go func() { result <- store.Publish(context.Background(), match, publishedMetadata) }()
		deadline := time.Now().Add(time.Second)
		blocked := false
		for time.Now().Before(deadline) {
			if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock%')`).Scan(&blocked); err != nil {
				t.Fatal("inspect advisory wait failed")
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("matcher publish did not wait for generation promotion lock")
		}
		if err := publicationTx.Commit(ctx); err != nil {
			t.Fatal("release publication lock failed")
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("serialized matcher publish failed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("serialized matcher publish did not resume")
		}
	})

	t.Run("publish metadata with zero genres", func(t *testing.T) {
		match := Match{
			SourceProvider:        SourceUGC,
			SourceMovieID:         "13546",
			MetadataProvider:      ProviderTMDB,
			Status:                StatusMatched,
			MetadataMovieID:       577599,
			Score:                 1,
			NormalizedSourceTitle: "film sans genre",
			SourceRuntimeMinutes:  90,
			Candidates:            []Candidate{{ID: 577599, Title: "Film sans genre", Runtime: 90, Score: 1}},
			EvaluatedAt:           now,
			RetryAfter:            now.Add(metadataTTL),
		}
		metadata := metadataFromDetails(tmdb.Details{ID: 577599, Title: "Film sans genre", OriginalTitle: "Film sans genre", Runtime: 90}, 90, now)
		metadata.Genres = nil
		if err := store.Publish(ctx, match, metadata); err != nil {
			t.Fatalf("publish zero-genre metadata failed: %v", err)
		}
		var genres []string
		if err := pool.QueryRow(ctx, `SELECT genres FROM movie_metadata_cache WHERE provider=$1 AND provider_movie_id=$2 AND locale=$3`, ProviderTMDB, int64(577599), LocaleFrench).Scan(&genres); err != nil {
			t.Fatalf("read zero-genre metadata failed: %v", err)
		}
		if genres == nil || len(genres) != 0 {
			t.Fatalf("stored genres=%#v", genres)
		}
	})

	t.Run("approve unknown runtime", func(t *testing.T) {
		const movieID = "9090"
		const tmdbID int64 = 909000
		if _, err := pool.Exec(ctx, `INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes)
VALUES (1,'cgr',$1,'cgr-film-9090','Film à venir',0)`, movieID); err != nil {
			t.Fatalf("insert unknown-runtime movie failed: %v", err)
		}
		provider := &reviewProviderStub{details: tmdb.Details{ID: tmdbID, Title: "Film à venir", OriginalTitle: "Film à venir", Runtime: 0, Genres: []string{}}}
		if err := NewReviewService(store, provider, func() time.Time { return now.Add(5 * time.Hour) }).Approve(ctx, SourceCGR, movieID, tmdbID); err != nil {
			t.Fatalf("approve unknown-runtime movie failed: %v", err)
		}
		if provider.calls != 1 {
			t.Fatalf("provider detail calls=%d", provider.calls)
		}
		var status string
		var sourceRuntime, metadataRuntime, publicRuntime int
		if err := pool.QueryRow(ctx, `SELECT status, source_runtime_minutes FROM movie_matches WHERE source_provider='cgr' AND source_movie_id=$1 AND metadata_provider='tmdb'`, movieID).Scan(&status, &sourceRuntime); err != nil || status != StatusMatched || sourceRuntime != 0 {
			t.Fatalf("stored match status=%q runtime=%d error=%v", status, sourceRuntime, err)
		}
		if err := pool.QueryRow(ctx, `SELECT runtime_minutes FROM movie_metadata_cache WHERE provider='tmdb' AND provider_movie_id=$1 AND locale='fr-FR'`, tmdbID).Scan(&metadataRuntime); err != nil || metadataRuntime != 0 {
			t.Fatalf("stored metadata runtime=%d error=%v", metadataRuntime, err)
		}
		if err := pool.QueryRow(ctx, `SELECT runtime_minutes FROM public_movies WHERE confirmed_tmdb_id=$1 AND redirect_to_id IS NULL`, tmdbID).Scan(&publicRuntime); err != nil || publicRuntime != 0 {
			t.Fatalf("public movie runtime=%d error=%v", publicRuntime, err)
		}
	})
}
