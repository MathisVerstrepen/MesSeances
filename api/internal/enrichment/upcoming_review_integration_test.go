package enrichment

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/tmdb"
)

func reviewPublication(now time.Time) UpcomingPublication {
	p := UpcomingPublication{CompletedAt: now, Window: schedule.UpcomingWindow(now)}
	for _, id := range []int64{42, 43, 44, 45} {
		p.Metadata = append(p.Metadata, metadataFromDetails(tmdb.Details{ID: id, Title: map[int64]string{42: "Flagged", 43: "Literal %_\\ title", 44: "Inactive", 45: "Pending"}[id], OriginalTitle: "Original"}, 0, now))
		p.Releases = append(p.Releases, upcomingTestRelease(id, "2026-10-07", true))
	}
	p.Releases[0].FrenchReleases = []tmdb.FrenchReleaseRow{{Type: 6, Date: "2026-10-01"}, {Type: 2, Date: "2026-10-07", Note: "France 2"}}
	p.Releases[0].ReasonCodes = AssessUpcoming(p.Releases[0].FrenchReleases, "2026-10-07")
	return p
}

func TestUpcomingReviewPosterPrecedenceIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx := t.Context()
	store := NewPostgresStore(pool)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p := reviewPublication(now)
	p.Metadata, p.Releases = p.Metadata[:1], p.Releases[:1]
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	tmdbPoster := "https://image.tmdb.org/t/p/w500/poster.jpg"
	sourcePoster := "https://static.ugc.fr/posters/poster.jpg"
	manualPoster := "https://images.example.com/uploads/manual-poster.jpg"
	for _, test := range []struct {
		name       string
		base       *string
		overridden bool
		override   *string
		want       *string
	}{
		{name: "missing"},
		{name: "canonical TMDB", base: &tmdbPoster, want: &tmdbPoster},
		{name: "canonical provider fallback", base: &sourcePoster, want: &sourcePoster},
		{name: "manual upload URL overrides canonical", base: &tmdbPoster, overridden: true, override: &manualPoster, want: &manualPoster},
		{name: "manual without canonical", overridden: true, override: &manualPoster, want: &manualPoster},
		{name: "explicit clear suppresses canonical", base: &tmdbPoster, overridden: true},
		{name: "reset restores canonical", base: &tmdbPoster, want: &tmdbPoster},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE public_movies SET poster_url=$1 WHERE confirmed_tmdb_id=42`, test.base); err != nil {
				t.Fatal(err)
			}
			// Keep an unrelated override present to exercise false poster flags as well as true ones.
			if _, err := pool.Exec(ctx, `INSERT INTO public_movie_metadata_overrides (public_movie_id,title,title_overridden,poster_url,poster_url_overridden)
SELECT public_movie_id,'Reviewed title',true,$1,$2 FROM tmdb_upcoming_movies WHERE tmdb_id=42
ON CONFLICT (public_movie_id) DO UPDATE SET poster_url=EXCLUDED.poster_url,poster_url_overridden=EXCLUDED.poster_url_overridden`, test.override, test.overridden); err != nil {
				t.Fatal(err)
			}
			list, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Limit: 50}, now)
			if err != nil || list.Total != 1 || len(list.Items) != 1 {
				t.Fatalf("list=%+v error=%v", list, err)
			}
			item := list.Items[0]
			if !reflect.DeepEqual(item.PosterURL, test.want) {
				t.Fatalf("poster=%v, want %v", item.PosterURL, test.want)
			}
			// Compare actual public detail output, not a second copy of its SQL expression.
			source, err := schedule.NewPostgresSource(ctx, schedulepg.NewStore(pool))
			if err != nil {
				t.Fatal(err)
			}
			service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			detail, err := service.MovieShowtimes(schedule.MovieShowtimesQuery{Slug: item.Slug, Date: "2026-09-13"})
			if err != nil || !reflect.DeepEqual(detail.Movie.PosterURL, item.PosterURL) {
				t.Fatalf("public poster=%v admin=%v error=%v", detail.Movie.PosterURL, item.PosterURL, err)
			}
			for _, decision := range []string{item.Decision, "approved", "excluded", "unreviewed"} {
				got, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: decision, ExpectedRevision: item.Revision}, now)
				if err != nil {
					t.Fatal(err)
				}
				if decision != item.Decision {
					item.Decision = decision
					item.Revision++
					item.PubliclyVisible = decision != "excluded"
				}
				if !reflect.DeepEqual(got, item) {
					t.Fatalf("decision changed poster or review fields: got=%+v want=%+v", got, item)
				}
			}
		})
	}
}

func TestUpcomingReviewDecisionsAndPublicationIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p := reviewPublication(now)
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	list := func(filter, search string, offset int) UpcomingReviewList {
		t.Helper()
		result, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: filter, Search: search, Limit: 50, Offset: offset}, now)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if q := list("needs_review", "", 0); q.Total != 1 || q.Items[0].TMDBID != 42 || !q.Items[0].PubliclyVisible {
		t.Fatalf("queue %+v", q)
	}
	if q := list("all", "%_\\", 0); q.Total != 1 || q.Items[0].TMDBID != 43 {
		t.Fatalf("literal search %+v", q)
	}
	if q := list("all", "42", 0); q.Total != 1 || q.Items[0].TMDBID != 42 {
		t.Fatalf("ID search %+v", q)
	}
	if q := list("all", "042", 0); q.Total != 0 {
		t.Fatal("noncanonical ID matched")
	}
	if q := list("all", "", 100); q.Total != 4 || len(q.Items) != 0 {
		t.Fatalf("offset %+v", q)
	}
	if q := list("all", "FLAG", 0); q.Total != 1 {
		t.Fatal("case insensitive search failed")
	}
	initial := list("all", "42", 0).Items[0]
	version := func() int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, "SELECT version FROM movie_enrichment_state").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	v := version()
	if got, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "unreviewed", ExpectedRevision: 1}, now); err != nil || !reflect.DeepEqual(got, initial) || version() != v {
		t.Fatalf("no-op %+v %v", got, err)
	}
	// Two independent same-revision editors serialize; precisely one succeeds.
	results := make(chan error, 2)
	for _, decision := range []string{"approved", "excluded"} {
		go func() {
			_, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: decision, ExpectedRevision: 1}, now)
			results <- err
		}()
	}
	a, b := <-results, <-results
	if a != nil {
		a, b = b, a
	}
	if a != nil || !errors.Is(b, ErrUpcomingReviewConflict) {
		t.Fatalf("concurrent edits %v %v", a, b)
	}
	current := list("all", "42", 0).Items[0]
	if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: current.Decision, ExpectedRevision: 1}, now); !errors.Is(err, ErrUpcomingReviewConflict) {
		t.Fatalf("stale no-op %v", err)
	}
	p.CompletedAt = now.Add(time.Hour)
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	identical := list("all", "42", 0).Items[0]
	if identical.Revision != current.Revision || identical.Decision != current.Decision || !identical.AssessedAt.Equal(p.CompletedAt) {
		t.Fatalf("timestamp-only conflict %+v", identical)
	}
	p.Releases[0] = upcomingTestRelease(42, "2026-11-04", true)
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "approved", ExpectedRevision: current.Revision}, now); !errors.Is(err, ErrUpcomingReviewConflict) {
		t.Fatalf("unseen evidence accepted %v", err)
	}
	changed := list("all", "42", 0).Items[0]
	if changed.Decision != current.Decision || len(changed.ReasonCodes) != 0 || changed.Revision != current.Revision+1 {
		t.Fatalf("decision changed by evidence %+v", changed)
	}
	excluded, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "excluded", ExpectedRevision: changed.Revision}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, release := range []UpcomingRelease{upcomingTestRelease(42, "2028-01-01", false), upcomingTestRelease(42, "", false), upcomingTestRelease(42, "2026-10-07", true)} {
		p.Releases[0] = release
		if err := store.PublishUpcoming(ctx, p); err != nil {
			t.Fatal(err)
		}
		got := list("excluded", "42", 0)
		if got.Total != 1 || got.Items[0].PubliclyVisible || got.Items[0].Decision != excluded.Decision {
			t.Fatalf("lost exclusion %+v", got)
		}
	}
	// Pending and inactive retained records are not silently restricted by filters.
	if _, err := pool.Exec(ctx, "UPDATE tmdb_upcoming_movies SET active=false,french_release_date=NULL WHERE tmdb_id=44; UPDATE tmdb_upcoming_movies SET assessed_at=NULL,french_releases='[]',reason_codes='{}' WHERE tmdb_id=45"); err != nil {
		t.Fatal(err)
	}
	if q := list("pending_assessment", "", 0); q.Total != 1 || q.Items[0].TMDBID != 45 || !q.Items[0].PubliclyVisible {
		t.Fatalf("pending %+v", q)
	}
	if _, err := store.SetUpcomingDecision(ctx, 45, UpcomingDecisionUpdate{Decision: "approved", ExpectedRevision: 1}, now); err != nil {
		t.Fatal(err)
	}
	if q := list("pending_assessment", "", 0); q.Total != 1 {
		t.Fatal("decision hid pending row")
	}
	if _, err := store.SetUpcomingDecision(ctx, 43, UpcomingDecisionUpdate{Decision: "approved", ExpectedRevision: 1}, now); err != nil {
		t.Fatal(err)
	}
	p.Releases[1].FrenchReleases[0].Type = 2
	p.Releases[1].ReasonCodes = []string{ReasonLimitedOnly}
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	if q := list("needs_review", "43", 0); q.Total != 0 {
		t.Fatal("changed reasons requeued approval")
	}
	if q := list("approved", "43", 0); q.Total != 1 || q.Items[0].Revision != 3 || len(q.Items[0].ReasonCodes) != 1 {
		t.Fatalf("approval lost %+v", q)
	}
	if q := list("approved", "45", 0); q.Total != 1 || q.Items[0].AssessmentStatus != "assessed" || q.Items[0].Revision != 3 {
		t.Fatalf("pending transition %+v", q)
	}
	retained, err := store.RetainedUpcomingIDs(ctx)
	if err != nil || !slices.Equal(retained, []int64{42, 43, 44, 45}) {
		t.Fatalf("retained %v %v", retained, err)
	}
	// A new store is restart-equivalent: no decision/evidence lives in process memory.
	restarted := NewPostgresStore(pool)
	q, err := restarted.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "excluded", Limit: 50}, now)
	if err != nil || q.Total != 1 {
		t.Fatalf("restart %+v %v", q, err)
	}
	if _, err := store.SetUpcomingDecision(ctx, 999, UpcomingDecisionUpdate{Decision: "excluded", ExpectedRevision: 1}, now); !errors.Is(err, ErrUpcomingReviewNotFound) {
		t.Fatal("unknown row created")
	}
}

func TestUpcomingReviewSyncConcurrentEditAndCancellationIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p := reviewPublication(now)
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{"excluded", "approved", "unreviewed"} {
		started, resume := make(chan struct{}), make(chan struct{})
		provider := &upcomingTestProvider{dates: map[int64]string{42: "2026-10-07", 43: "2026-10-07", 44: "2028-01-01"}, discover: func(ctx context.Context) error {
			close(started)
			select {
			case <-resume:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
		done := make(chan error, 1)
		go func() { done <- NewUpcomingService(store, provider, func() time.Time { return now }, nil).sync(ctx) }()
		<-started
		q, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Search: "42", Limit: 50}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: decision, ExpectedRevision: q.Items[0].Revision}, now); err != nil {
			t.Fatal(err)
		}
		close(resume)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		q, err = store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Limit: 50}, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range q.Items {
			if item.TMDBID == 42 && item.Decision != decision {
				t.Fatal("sync overwrote edit")
			}
			if item.TMDBID == 44 && (item.Active || item.FrenchReleaseDate == nil) {
				t.Fatal("out of window lost")
			}
			if item.TMDBID == 45 && (item.Active || item.AssessmentStatus != "assessed" || item.FrenchReleaseDate != nil) {
				t.Fatal("withdrawal not assessed")
			}
		}
		if !slices.Equal(provider.verified, []int64{42, 43, 44, 45}) {
			t.Fatalf("inactive omitted %v", provider.verified)
		}
	}
	var before, after string
	const state = `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(u) ORDER BY tmdb_id) FROM tmdb_upcoming_movies u),(SELECT to_jsonb(s) FROM tmdb_upcoming_state s),(SELECT to_jsonb(s) FROM movie_enrichment_state s))::text`
	if err := pool.QueryRow(ctx, state).Scan(&before); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.PublishUpcoming(canceled, p); err == nil {
		t.Fatal("canceled publication accepted")
	}
	bad := p
	bad.Releases = append([]UpcomingRelease{}, p.Releases...)
	bad.Releases[0].ReasonCodes = []string{"unknown"}
	if err := store.PublishUpcoming(ctx, bad); err == nil {
		t.Fatal("invalid evidence accepted")
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_review_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE CONSTRAINT TRIGGER review_commit_failure AFTER UPDATE ON tmdb_upcoming_state DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_review_commit()`); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishUpcoming(ctx, p); err == nil {
		t.Fatal("failed commit accepted")
	}
	if err := pool.QueryRow(ctx, state).Scan(&after); err != nil || before != after {
		t.Fatalf("partial assessment %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE CONSTRAINT TRIGGER decision_commit_failure AFTER UPDATE ON tmdb_upcoming_movies DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_review_commit()`); err != nil {
		t.Fatal(err)
	}
	q, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "all", Search: "42", Limit: 50}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "excluded", ExpectedRevision: q.Items[0].Revision}, now); err == nil {
		t.Fatal("failed decision commit accepted")
	}
	if err := pool.QueryRow(ctx, state).Scan(&after); err != nil || before != after {
		t.Fatalf("partial decision %v", err)
	}
}

func TestUpcomingReviewPublicReloadIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p := reviewPublication(now)
	p.Metadata[0].Genres = []string{"Unique"}
	p.Releases[0].FrenchReleaseDate = "2026-11-04"
	p.Releases[0].FrenchReleases[1].Date = "2026-11-04"
	if err := store.PublishUpcoming(ctx, p); err != nil {
		t.Fatal(err)
	}
	reader := schedulepg.NewStore(pool)
	load := func() (*schedule.Service, schedule.UpcomingMoviesResponse) {
		t.Helper()
		source, err := schedule.NewPostgresSource(ctx, reader)
		if err != nil {
			t.Fatal(err)
		}
		service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		list, err := service.UpcomingMovies(schedule.UpcomingMoviesQuery{})
		if err != nil {
			t.Fatal(err)
		}
		return service, list
	}
	_, before := load()
	if before.Total != 4 || len(before.Items) != 4 || before.TotalWeeks != 2 || before.TotalPages != 1 {
		t.Fatalf("flags hid film %+v", before)
	}
	item, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "excluded", ExpectedRevision: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	service, hidden := load()
	if hidden.Total != 3 || len(hidden.Items) != 3 || hidden.TotalWeeks != 1 || hidden.TotalPages != 1 || slices.ContainsFunc(hidden.Items, func(movie schedule.MovieCatalogItem) bool { return movie.Slug == item.Slug }) || hidden.CatalogRevision == before.CatalogRevision || !hidden.GeneratedAt.Equal(before.GeneratedAt) {
		t.Fatalf("exclusion reload %+v", hidden)
	}
	detail, err := service.MovieShowtimes(schedule.MovieShowtimesQuery{Slug: item.Slug, Date: "2026-09-13"})
	if err != nil || detail.ReleaseStatus != "upcoming" {
		t.Fatalf("detail lost %+v %v", detail, err)
	}
	inventory, err := service.Movies(schedule.MovieCatalogQuery{IncludeEnded: true})
	if err != nil || len(inventory.Items) != 4 {
		t.Fatalf("inventory lost %+v %v", inventory, err)
	}
	if _, err := store.SetUpcomingDecision(ctx, 42, UpcomingDecisionUpdate{Decision: "unreviewed", ExpectedRevision: item.Revision}, now); err != nil {
		t.Fatal(err)
	}
	_, reset := load()
	if reset.Total != 4 || reset.TotalWeeks != before.TotalWeeks || reset.TotalPages != before.TotalPages || reset.CatalogRevision == hidden.CatalogRevision || !reset.GeneratedAt.Equal(before.GeneratedAt) || !reflect.DeepEqual(reset.Items, before.Items) {
		t.Fatalf("reset %+v", reset)
	}
	queue, err := store.UpcomingReviews(ctx, UpcomingReviewQuery{Filter: "needs_review", Limit: 50}, now)
	if err != nil || queue.Total != 1 {
		t.Fatal("reset did not requeue")
	}
}
