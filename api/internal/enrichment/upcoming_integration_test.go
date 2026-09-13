package enrichment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/database"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/syncschedule"
	"messeances/api/internal/tmdb"
)

func upcomingIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	name := pgx.Identifier{"upcoming_test_" + hex.EncodeToString(nonce)}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("connect disposable database failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := bootstrap.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Error("cleanup isolated schema failed")
		}
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("parse disposable database failed")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create disposable pool failed")
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func upcomingProviderDataset(now time.Time) schedule.Dataset {
	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 9, 14, 18, 0, 0, 0, location)
	return schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: schedule.ScopeAll, GeneratedAt: now, Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-09-14", Through: "2026-09-14"}, Theaters: []schedule.TheaterRecord{{Provider: schedule.ProviderUGC, ID: "ugc-1", ProviderID: "1", Slug: "ugc-1", Name: "Cinéma test", City: "Paris", Address: "1 rue test", PostalCode: "75001", AvailableDates: []string{"2026-09-14"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []schedule.ShowtimeRecord{{Provider: schedule.ProviderUGC, ID: "ugc-showing-100", ProviderShowingID: "100", ServiceDate: "2026-09-14", TheaterID: "ugc-1", Movie: schedule.MovieRecord{Provider: schedule.ProviderUGC, ProviderID: "10", Slug: "ugc-film-10", Title: "Provider title", RuntimeMinutes: 90}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, Room: "1", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=100"}}}
}

func upcomingMatch(now time.Time, id int64) Match {
	return Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: id, Score: 1, NormalizedSourceTitle: NormalizeTitle("Provider title"), SourceRuntimeMinutes: 90, Candidates: []Candidate{}, EvaluatedAt: now, RetryAfter: now.Add(metadataTTL)}
}

func upcomingTestRelease(id int64, date string, active bool) UpcomingRelease {
	rows := []tmdb.FrenchReleaseRow{}
	if date != "" {
		rows = append(rows, tmdb.FrenchReleaseRow{Type: 3, Date: date})
	}
	return UpcomingRelease{TMDBID: id, FrenchReleaseDate: date, Active: active, FrenchReleases: rows, ReasonCodes: AssessUpcoming(rows, date)}
}

func TestUpcomingPersistenceLifecycleIntegration(t *testing.T) {
	for _, sourceFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "TMDB first", true: "source first"}[sourceFirst], func(t *testing.T) {
			pool := upcomingIntegrationPool(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			store := NewPostgresStore(pool)
			reader := schedulepg.NewStore(pool)
			now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
			metadata := metadataFromDetails(tmdb.Details{ID: 42, Title: "Catalog title", OriginalTitle: "Original", ReleaseDate: "2000-01-01", Runtime: 90, TrailerVFYouTubeKey: "abcdefghijk", Genres: []string{"Drame"}}, 0, now)
			publication := UpcomingPublication{CompletedAt: now, Window: schedule.UpcomingWindow(now), Metadata: []Metadata{metadata}, Releases: []UpcomingRelease{upcomingTestRelease(42, "2026-10-07", true)}}
			var beforeID int64
			if sourceFirst {
				if _, err := reader.Replace(ctx, []schedule.Dataset{upcomingProviderDataset(now)}); err != nil {
					t.Fatal(err)
				}
				if err := store.Publish(ctx, upcomingMatch(now, 42), metadata); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, "SELECT id FROM public_movies WHERE confirmed_tmdb_id=42 AND redirect_to_id IS NULL").Scan(&beforeID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.PublishUpcoming(ctx, publication); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "excluded", ExpectedRevision: 1}, now); err != nil {
				t.Fatal(err)
			}
			data, revision, err := reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var catalogID int64
			if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM tmdb_upcoming_movies WHERE tmdb_id=42").Scan(&catalogID); err != nil {
				t.Fatal(err)
			}
			if sourceFirst && catalogID != beforeID {
				t.Fatal("source-first identity changed")
			}
			if !sourceFirst && (revision.ScheduleVersion != 0 || revision.EnrichmentVersion != 2 || len(data.Theaters) != 0 || len(data.Showtimes) != 0 || data.Window != (schedule.Window{}) || !data.GeneratedAt.Equal(now)) {
				t.Fatalf("bootstrap data=%+v revision=%+v", data, revision)
			}
			source, err := schedule.NewPostgresSource(ctx, reader)
			if err != nil {
				t.Fatal(err)
			}
			service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			if !service.HasCatalog() || service.HasSnapshot() != sourceFirst {
				t.Fatal("wrong catalog readiness")
			}
			if _, err := pool.Exec(ctx, `INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title,release_date_overridden,release_date) VALUES($1,true,'Manual title',true,'2001-02-03')`, catalogID); err != nil {
				t.Fatal(err)
			}
			if err := store.PublishUpcoming(ctx, publication); err != nil {
				t.Fatal(err)
			}
			if !sourceFirst {
				if _, err := reader.Replace(ctx, []schedule.Dataset{upcomingProviderDataset(now)}); err != nil {
					t.Fatal(err)
				}
				if err := store.Publish(ctx, upcomingMatch(now, 42), metadata); err != nil {
					t.Fatal(err)
				}
			}
			source, err = schedule.NewPostgresSource(ctx, reader)
			if err != nil {
				t.Fatal(err)
			}
			service, err = schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			if !service.HasSnapshot() {
				t.Fatal("catalog did not transition to schedule")
			}
			data, _, err = reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if publicSourceID(data, schedule.ProviderUGC, "10") != catalogID {
				t.Fatal("provider did not attach to existing catalog ID")
			}
			movie := publicMovieByID(data, catalogID)
			if movie.Title != "Manual title" || movie.ReleaseDate != "2001-02-03" || movie.FrenchReleaseDate != "2026-10-07" || movie.TrailerVFYouTubeKey != "abcdefghijk" {
				t.Fatalf("metadata/overrides lost: %+v", movie)
			}
			// A correction must not carry the old French release to the replacement identity.
			items, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "", 10, 0)
			if err != nil || len(items) != 1 {
				t.Fatalf("pending=%+v err=%v", items, err)
			}
			provider := &matchedCorrectionProvider{details: tmdb.Details{ID: 99, Title: "Correction", OriginalTitle: "Correction", Runtime: 91}}
			if err := NewReviewService(store, provider, func() time.Time { return now.Add(time.Hour) }).Correct(ctx, SourceUGC, "10", 99, *items[0].UpdatedAt); err != nil {
				t.Fatal(err)
			}
			data, _, err = reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var retainedID int64
			if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM tmdb_upcoming_movies WHERE tmdb_id=42").Scan(&retainedID); err != nil {
				t.Fatal(err)
			}
			old := publicMovieByID(data, retainedID)
			corrected := publicMovieByID(data, publicSourceID(data, schedule.ProviderUGC, "10"))
			if old.TMDBID != 42 || old.RedirectToID != 0 || old.FrenchReleaseDate != "2026-10-07" || old.Title != "Manual title" || !old.UpcomingExcluded || corrected.TMDBID != 99 || corrected.HasUpcomingRelease || corrected.UpcomingExcluded || retainedID == corrected.ID {
				t.Fatalf("old=%+v corrected=%+v", old, corrected)
			}
			if !sourceFirst && retainedID != catalogID {
				t.Fatal("TMDB-only anchor moved on provider correction")
			}
			var aliasID int64
			if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM movie_slug_aliases WHERE slug='tmdb-film-42'").Scan(&aliasID); err != nil || aliasID != retainedID {
				t.Fatalf("TMDB alias=%d err=%v", aliasID, err)
			}
			ids, err := store.MatchedTMDBIDs(ctx)
			if err != nil || !reflect.DeepEqual(ids, []int64{42, 99}) {
				t.Fatalf("refresh IDs=%v err=%v", ids, err)
			}
			metadata.LocalizedTitle = "Refreshed base"
			reviewBefore, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Search: "42", Limit: 50}, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.RefreshMetadata(ctx, []Metadata{metadata}); err != nil {
				t.Fatal(err)
			}
			reviewAfter, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Search: "42", Limit: 50}, now)
			if err != nil || !reflect.DeepEqual(reviewBefore, reviewAfter) {
				t.Fatal("metadata refresh altered assessment")
			}
			// Withdrawal retains identity, overrides and general release-date cache.
			publication.CompletedAt = now.Add(2 * time.Hour)
			publication.Metadata = nil
			publication.Releases[0].Active = false
			publication.Releases[0].FrenchReleaseDate = ""
			publication.Releases[0].FrenchReleases = nil
			if err := store.PublishUpcoming(ctx, publication); err != nil {
				t.Fatal(err)
			}
			source, err = schedule.NewPostgresSource(ctx, reader)
			if err != nil {
				t.Fatal(err)
			}
			service, err = schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.UpcomingMovies(schedule.UpcomingMoviesQuery{})
			if err != nil || result.Total != 0 {
				t.Fatalf("withdrawn list=%+v err=%v", result, err)
			}
			data, _, err = reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			old = publicMovieByID(data, retainedID)
			if old.FrenchReleaseDate != "" || !old.HasUpcomingRelease || old.Title != "Manual title" || old.TMDBID != 42 {
				t.Fatalf("withdrawal lost record: %+v", old)
			}
			publication.Releases[0] = upcomingTestRelease(42, "2026-11-04", true)
			if err := store.PublishUpcoming(ctx, publication); err != nil {
				t.Fatal(err)
			}
			var reappeared int64
			if err := pool.QueryRow(ctx, "SELECT public_movie_id FROM tmdb_upcoming_movies WHERE tmdb_id=42").Scan(&reappeared); err != nil || reappeared != retainedID {
				t.Fatal("reappearance allocated a new identity")
			}
			reviewAfter, err = store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "excluded", Search: "42", Limit: 50}, now)
			if err != nil || reviewAfter.Total != 1 || reviewAfter.Items[0].PubliclyVisible || reviewAfter.Items[0].Decision != "excluded" {
				t.Fatal("reappearance lost decision")
			}
		})
	}
}

func TestUpcomingPublicationRollbackAndLeaseIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := NewPostgresStore(pool)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p := UpcomingPublication{CompletedAt: now, Window: schedule.UpcomingWindow(now), Releases: []UpcomingRelease{upcomingTestRelease(42, "2026-10-07", true)}, Metadata: []Metadata{metadataFromDetails(tmdb.Details{ID: 42, Title: "Original", OriginalTitle: "Original"}, 0, now)}}
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Fail at COMMIT, after metadata, evidence, marker and reconciliation have run.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_upcoming_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture commit failure'; END $$; CREATE CONSTRAINT TRIGGER upcoming_commit_failure AFTER UPDATE ON tmdb_upcoming_state DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_upcoming_commit()`); err != nil {
		t.Fatal(err)
	}
	p.CompletedAt = now.Add(time.Hour)
	p.Metadata[0].LocalizedTitle = "Must roll back"
	p.Releases[0] = UpcomingRelease{TMDBID: 42}
	if err := store.PublishUpcoming(ctx, p); err == nil {
		t.Fatal("commit failure accepted")
	}
	data, revision, err := schedulepg.NewStore(pool).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if revision.EnrichmentVersion != 1 || !data.UpcomingCompletedAt.Equal(now) || data.PublicMovies[0].Title != "Original" || !data.PublicMovies[0].UpcomingActive {
		t.Fatalf("partial publication survived: %+v %+v", data, revision)
	}
	locker := NewPostgresUpcomingLocker(pool)
	lease, err := locker.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPostgresUpcomingLocker(pool).Acquire(ctx); !errors.Is(err, syncschedule.ErrInProgress) {
		t.Fatalf("cross-session contention=%v", err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	lease, err = locker.Acquire(ctx)
	if err != nil {
		t.Fatal("lease not reusable")
	}
	dead, cancelDead := context.WithCancel(ctx)
	cancelDead()
	if err := lease.Release(dead); err == nil {
		t.Fatal("canceled unlock accepted")
	}
	lease, err = locker.Acquire(ctx)
	if err != nil {
		t.Fatalf("discarded session leaked lock: %v", err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUpcomingFixtureImportDispatchIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/discover/movie":
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"total_results":1,"results":[{"id":42}]}`))
		case "/3/movie/42/release_dates":
			_, _ = w.Write([]byte(`{"id":42,"results":[{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"2000-01-01T00:00:00Z"}]},{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-07T00:00:00Z"}]}]}`))
		case "/3/movie/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Fixture film","original_title":"Fixture film","release_date":"2000-01-01","runtime":0,"genres":[]}`))
		default:
			t.Errorf("unexpected fixture request path")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := tmdb.NewClientWithConfig("fixture-only", tmdb.Config{BaseURL: server.URL, HTTPClient: server.Client(), RequestInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	manager, err := NewUpcomingManager(ctx, NewUpcomingService(store, client, func() time.Time { return now }, NewTMDBRunGate()), NewPostgresUpcomingLocker(pool))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	var scheduleID int64
	if err := pool.QueryRow(ctx, `INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('tmdb_upcoming_movies',true,'daily','14:00') RETURNING id`).Scan(&scheduleID); err != nil {
		t.Fatal(err)
	}
	claims := syncschedule.NewPostgresStore(pool)
	claim := func(ctx context.Context) (bool, error) {
		return claims.ClaimOccurrence(ctx, syncschedule.Occurrence{ScheduleID: scheduleID, Target: syncschedule.TargetUpcomingMovies, Revision: 1, ScheduledFor: now})
	}
	done, err := manager.StartScheduled(claim)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if !result.Succeeded {
			t.Fatal("fixture import failed")
		}
	case <-ctx.Done():
		t.Fatal("fixture import timed out")
	}
	source, err := schedule.NewPostgresSource(ctx, schedulepg.NewStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.UpcomingMovies(schedule.UpcomingMoviesQuery{})
	if err != nil || result.Total != 1 || result.Items[0].FrenchReleaseDate == nil || *result.Items[0].FrenchReleaseDate != "2026-10-07" || result.Items[0].ReleaseDate == nil || *result.Items[0].ReleaseDate != "2000-01-01" || service.HasSnapshot() {
		t.Fatalf("fixture catalog=%+v err=%v", result, err)
	}
	if _, err := manager.StartScheduled(claim); !errors.Is(err, syncschedule.ErrOccurrenceClaimed) {
		t.Fatalf("durable duplicate dispatch accepted: %v", err)
	}
}

func TestUpcomingExistingOwnerAndLocalGroupsIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	store, reader := NewPostgresStore(pool), schedulepg.NewStore(pool)
	metadata := metadataFromDetails(tmdb.Details{ID: 42, Title: "Original", OriginalTitle: "Original", Runtime: 90}, 0, now)
	other := metadata
	other.ProviderMovieID = 99
	publication := UpcomingPublication{CompletedAt: now, Window: schedule.UpcomingWindow(now), Metadata: []Metadata{metadata, other}, Releases: []UpcomingRelease{upcomingTestRelease(42, "2026-10-07", true), upcomingTestRelease(99, "2026-11-04", true)}}
	if err := store.PublishUpcoming(ctx, publication); err != nil {
		t.Fatal(err)
	}
	data, _, err := reader.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner42, owner99 := data.PublicMovies[0].ID, data.PublicMovies[1].ID
	if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "excluded", ExpectedRevision: 1}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetUpcomingDecision(ctx, 99, UpcomingDecisionUpdate{Decision: "approved", ExpectedRevision: 1}, now); err != nil {
		t.Fatal(err)
	}
	dataset := upcomingProviderDataset(now)
	extra := dataset.Showtimes[0]
	extra.ID, extra.ProviderShowingID = "ugc-showing-101", "101"
	extra.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=101"
	extra.Movie.ProviderID, extra.Movie.Slug = "11", "ugc-film-11"
	dataset.Showtimes = append(dataset.Showtimes, extra)
	if _, err := reader.Replace(ctx, []schedule.Dataset{dataset}); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(ctx, upcomingMatch(now, 42), metadata); err != nil {
		t.Fatal(err)
	}
	items, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "", 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("matched source unavailable: %v", err)
	}
	provider := &matchedCorrectionProvider{details: tmdb.Details{ID: 99, Title: "Corrected", OriginalTitle: "Corrected", Runtime: 90}}
	if err := NewReviewService(store, provider, func() time.Time { return now }).Correct(ctx, SourceUGC, "10", 99, *items[0].UpdatedAt); err != nil {
		t.Fatal(err)
	}
	data, _, err = reader.Load(ctx)
	if err != nil || publicSourceID(data, schedule.ProviderUGC, "10") != owner99 || publicMovieByID(data, owner42).FrenchReleaseDate != "2026-10-07" {
		t.Fatalf("correction did not retain existing owners: %v", err)
	}
	// A changed source fingerprint reopens review; rejecting it detaches only source evidence.
	match := upcomingMatch(now.Add(time.Hour), 0)
	match.Status, match.Score = StatusUnmatched, 0
	if err := store.SaveDecision(ctx, match); err != nil {
		t.Fatal(err)
	}
	if err := NewReviewService(store, nil, func() time.Time { return now.Add(2 * time.Hour) }).Reject(ctx, SourceUGC, "10"); err != nil {
		t.Fatal(err)
	}
	local := NewLocalMovieService(store)
	members := []LocalMovieSource{{SourceProvider: SourceUGC, SourceMovieID: "10"}, {SourceProvider: SourceUGC, SourceMovieID: "11"}}
	group, err := local.Merge(ctx, members, members[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := local.Unmerge(ctx, group.LocalMovieID); err != nil {
		t.Fatal(err)
	}
	// An unrelated provider publication cannot erase retained TMDB-only evidence.
	if _, err := reader.Replace(ctx, []schedule.Dataset{dataset}); err != nil {
		t.Fatal(err)
	}
	data, _, err = reader.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{owner42, owner99} {
		movie := publicMovieByID(data, id)
		if movie.RedirectToID != 0 || movie.TMDBID == 0 || !movie.UpcomingActive || movie.FrenchReleaseDate == "" {
			t.Fatalf("local grouping lost catalog identity: %+v", movie)
		}
	}
	reviews, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Limit: 50}, now)
	if err != nil || reviews.Total != 2 {
		t.Fatal(err)
	}
	for _, item := range reviews.Items {
		if item.Revision != 2 || item.Decision != map[int64]string{42: "excluded", 99: "approved"}[item.TMDBID] {
			t.Fatalf("correction/rejection/merge/unmerge altered decision %+v", item)
		}
	}
}
