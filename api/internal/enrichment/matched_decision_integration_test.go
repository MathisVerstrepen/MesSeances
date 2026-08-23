package enrichment

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

	"messeances/api/internal/database"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

func TestSaveDecisionMatchedEvidenceRemovalIntegration(t *testing.T) {
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
	schema := "movieflow_matched_decision_test_" + hex.EncodeToString(nonce)
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
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dataset := func(provider schedule.Provider, theaterID, theaterProviderID, movieID, showingID, title string) schedule.Dataset {
		bookingURL := "https://www.ugc.fr/reservationSeances.html?id=" + showingID
		acceptedPasses := []string{"UGC_ILLIMITE"}
		if provider == schedule.ProviderKinepolis {
			bookingURL = "https://kinepolis.fr/direct-vista-redirect/" + showingID + "/0/" + theaterProviderID + "/0"
			acceptedPasses = []string{}
		}
		return schedule.Dataset{
			SchemaVersion: schedule.SchemaVersion,
			Provider:      provider,
			Scope:         schedule.ScopeAll,
			GeneratedAt:   now,
			Timezone:      schedule.Timezone,
			Window:        schedule.Window{From: "2026-08-24", Through: "2026-08-24"},
			Theaters:      []schedule.TheaterRecord{{Provider: provider, ID: theaterID, ProviderID: theaterProviderID, Slug: theaterID, Name: "Cinéma test", Address: "1 rue test", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-24"}, AcceptedPasses: acceptedPasses}},
			Showtimes:     []schedule.ShowtimeRecord{{Provider: provider, ID: string(provider) + "-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: "2026-08-24", TheaterID: theaterID, Movie: schedule.MovieRecord{Provider: provider, ProviderID: movieID, Slug: string(provider) + "-film-" + movieID, Title: title, RuntimeMinutes: 90}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, Room: "1", BookingURL: bookingURL}},
		}
	}
	pgStore := schedulepg.NewStore(pool)
	if _, err := pgStore.Replace(ctx, []schedule.Dataset{
		dataset(schedule.ProviderUGC, "ugc-1", "1", "10", "100", "Source UGC"),
		dataset(schedule.ProviderKinepolis, "kinepolis-K", "K", "K10", "K100", "Source Kinepolis"),
	}); err != nil {
		t.Fatalf("publish source fixtures failed: %v", err)
	}
	store := NewPostgresStore(pool)
	metadata := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "TMDB", LocalizedTitle: "Canonique TMDB", Overview: "Résumé TMDB", RuntimeMinutes: 91, Genres: []string{"Drame"}, FetchedAt: now, RefreshAfter: now.Add(metadataTTL)}
	match := func(provider, id, normalized string) Match {
		return Match{SourceProvider: provider, SourceMovieID: id, MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: 42, Score: 1, NormalizedSourceTitle: normalized, SourceRuntimeMinutes: 90, Candidates: []Candidate{{ID: 42, Title: "Canonique TMDB", Runtime: 91, Score: 1}}, EvaluatedAt: now, RetryAfter: now.Add(metadataTTL)}
	}
	if err := store.Publish(ctx, match(SourceUGC, "10", "source ugc"), metadata); err != nil {
		t.Fatal("publish UGC match failed")
	}
	if err := store.Publish(ctx, match(SourceKinepolis, "K10", "source kinepolis"), metadata); err != nil {
		t.Fatal("publish Kinepolis match failed")
	}
	before, beforeRevision, err := pgStore.Load(ctx)
	if err != nil {
		t.Fatalf("load merged catalog failed: %v", err)
	}
	ugcBefore, kinepolisBefore := publicSourceID(before, schedule.ProviderUGC, "10"), publicSourceID(before, schedule.ProviderKinepolis, "K10")
	if ugcBefore <= 0 || ugcBefore != kinepolisBefore {
		t.Fatalf("matched sources not merged: ugc=%d kinepolis=%d", ugcBefore, kinepolisBefore)
	}

	changed := match(SourceUGC, "10", "source ugc changed")
	changed.Status, changed.MetadataMovieID, changed.Score = StatusReviewRequired, 0, 0
	changed.SourceRuntimeMinutes = 92
	changed.Candidates = []Candidate{{ID: 99, Title: "Nouvelle empreinte", Runtime: 92, Score: .8}}
	changed.EvaluatedAt = now.Add(time.Minute)
	if err := store.SaveDecision(ctx, changed); err != nil {
		t.Fatalf("replace matched decision failed: %v", err)
	}
	after, afterRevision, err := pgStore.Load(ctx)
	if err != nil {
		t.Fatalf("reload split catalog failed: %v", err)
	}
	if afterRevision.EnrichmentVersion != beforeRevision.EnrichmentVersion+1 {
		t.Fatalf("enrichment revision before=%d after=%d", beforeRevision.EnrichmentVersion, afterRevision.EnrichmentVersion)
	}
	ugcAfter, kinepolisAfter := publicSourceID(after, schedule.ProviderUGC, "10"), publicSourceID(after, schedule.ProviderKinepolis, "K10")
	if ugcAfter <= 0 || kinepolisAfter <= 0 || ugcAfter == kinepolisAfter {
		t.Fatalf("matched evidence removal did not split: ugc=%d kinepolis=%d", ugcAfter, kinepolisAfter)
	}
	ugcMovie, kinepolisMovie := publicMovieByID(after, ugcAfter), publicMovieByID(after, kinepolisAfter)
	if ugcMovie.Title != "Source UGC" || ugcMovie.TMDBID != 0 || kinepolisMovie.Title != "Canonique TMDB" || kinepolisMovie.TMDBID != 42 {
		t.Fatalf("split metadata ugc=%+v kinepolis=%+v", ugcMovie, kinepolisMovie)
	}
}

func publicSourceID(data schedule.Dataset, provider schedule.Provider, sourceID string) int64 {
	for _, source := range data.MovieSources {
		if source.Provider == provider && source.SourceMovieID == sourceID {
			return source.PublicMovieID
		}
	}
	return 0
}

func publicMovieByID(data schedule.Dataset, id int64) schedule.PublicMovieRecord {
	for _, movie := range data.PublicMovies {
		if movie.ID == id {
			return movie
		}
	}
	return schedule.PublicMovieRecord{}
}
