package schedulepg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/trailer"
)

const trailerMP4 = "https://fr.vid.web.acsta.net/nmedia/33/synthetic_hd.mp4"

func trailerMatch(t *testing.T, pool *pgxpool.Pool, provider, id string, tmdbID int64, key string) {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	match := enrichment.Match{SourceProvider: provider, SourceMovieID: id, MetadataProvider: "tmdb", Status: enrichment.StatusMatched, MetadataMovieID: tmdbID, Score: 1, NormalizedSourceTitle: "synthetic", SourceRuntimeMinutes: 100, EvaluatedAt: now, RetryAfter: now.Add(30 * 24 * time.Hour), Candidates: []enrichment.Candidate{}}
	metadata := enrichment.Metadata{Provider: "tmdb", ProviderMovieID: tmdbID, Locale: "fr-FR", ProviderTitle: "Synthetic", LocalizedTitle: "Synthetic", RuntimeMinutes: 100, Genres: []string{}, TrailerVFYouTubeKey: key, FetchedAt: now, RefreshAfter: now.Add(30 * 24 * time.Hour)}
	if err := enrichment.NewPostgresStore(pool).Publish(t.Context(), match, metadata); err != nil {
		t.Fatal(err)
	}
}

func loadedTrailer(t *testing.T, store *Store, tmdbID int64) *trailer.Media {
	t.Helper()
	data, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range data.PublicMovies {
		if m.TMDBID == tmdbID {
			return m.Trailer
		}
	}
	t.Fatal("matched public movie absent")
	return nil
}

func TestProviderTrailerPublicationAndPrecedenceIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := cgrTestDataset()
	data.Showtimes[0].Movie.TrailerObserved = true
	data.Showtimes[0].Movie.SourceTrailer = trailer.MP4(trailerMP4, "cgr")
	historyPublish(t, store, data)
	historyCount(t, pool, `SELECT count(*) FROM movie_trailer_candidates WHERE available`, 1)
	// A successful TMDB metadata fetch is required before absence can be inferred.
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fallbacks, err := trailer.Fallbacks(t.Context(), tx)
	_ = tx.Rollback(t.Context())
	if err != nil || len(fallbacks) != 0 {
		t.Fatal("unmatched film received fallback", err)
	}
	trailerMatch(t, pool, "cgr", "1001", 42, "")
	if got := loadedTrailer(t, store, 42); got == nil || got.URL != trailerMP4 {
		t.Fatal("fallback absent", got)
	}
	// An unrelated publication preserves evidence and current identity resolution.
	historyPublish(t, store, testDataset())
	trailerMatch(t, pool, "ugc", "200", 42, "")
	if got := loadedTrailer(t, store, 42); got == nil || got.Source != "cgr" {
		t.Fatal("shared canonical fallback lost", got)
	}
	trailerMatch(t, pool, "cgr", "1001", 42, "AbC-12_xYz9")
	if loadedTrailer(t, store, 42) != nil {
		t.Fatal("TMDB trailer did not suppress fallback")
	}
	trailerMatch(t, pool, "cgr", "1001", 42, "")
	historyExec(t, pool, `INSERT INTO public_movie_metadata_overrides(public_movie_id,trailer_vf_youtube_key_overridden,trailer_vf_youtube_key) SELECT id,true,NULL FROM public_movies WHERE confirmed_tmdb_id=42`)
	if loadedTrailer(t, store, 42) != nil {
		t.Fatal("explicit null override did not hide fallback")
	}
	historyExec(t, pool, `DELETE FROM public_movie_metadata_overrides WHERE trailer_vf_youtube_key_overridden`)
	if loadedTrailer(t, store, 42) == nil {
		t.Fatal("restoring automatic trailer did not restore fallback")
	}
	// Move the source to a different verified movie: evidence follows the source.
	trailerMatch(t, pool, "cgr", "1001", 43, "")
	if got := loadedTrailer(t, store, 43); got == nil || got.Source != "cgr" {
		t.Fatal("corrected source lost evidence")
	}
	if loadedTrailer(t, store, 42) != nil {
		t.Fatal("corrected evidence leaked to old movie")
	}
	// A valid explicit absence withdraws the old candidate.
	data.Showtimes[0].Movie.SourceTrailer = nil
	historyPublish(t, store, data)
	if loadedTrailer(t, store, 43) != nil {
		t.Fatal("withdrawn trailer remained visible")
	}
}

func TestTrailerObservationRollbackAndInvalidOptionalValueIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := cinevilleTestDataset()
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.SourceTrailer = trailer.YouTube("AbC-12_xYz9", "cineville")
		data.Showtimes[i].Movie.TrailerObserved = true
	}
	historyPublish(t, store, data)
	historyCount(t, pool, `SELECT count(*) FROM movie_trailer_candidates WHERE available`, 1)
	// Unsafe optional media is ignored without rejecting valid showtimes.
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.SourceTrailer = &trailer.Media{Type: "mp4", Source: "cineville", URL: "https://evil.example/a.mp4"}
	}
	historyPublish(t, store, data)
	historyCount(t, pool, `SELECT count(*) FROM movie_trailer_candidates WHERE available`, 1)
	before, err := store.CurrentRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Force a failure inside candidate persistence, after schedule rows were staged.
	historyExec(t, pool, `CREATE FUNCTION reject_trailer() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic'; END $$; CREATE TRIGGER reject_trailer BEFORE INSERT ON movie_trailer_candidates FOR EACH ROW EXECUTE FUNCTION reject_trailer()`)
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.SourceTrailer = trailer.YouTube("VoT-98_rQp7", "cineville")
	}
	if _, err := store.Replace(t.Context(), []schedule.Dataset{data}); err == nil {
		t.Fatal("injected failure did not abort")
	}
	after, err := store.CurrentRevision(t.Context())
	if err != nil || after != before {
		t.Fatal("aborted observation published schedule", err)
	}
	historyCount(t, pool, `SELECT count(*) FROM movie_trailer_candidates WHERE available AND url='https://www.youtube.com/watch?v=AbC-12_xYz9'`, 1)
}

type trailerFetchFunc func(context.Context, string) (*trailer.Media, error)

func (f trailerFetchFunc) Fetch(ctx context.Context, id string) (*trailer.Media, error) {
	return f(ctx, id)
}

func TestUGCTrailerWorkerEligibilityCachingAndFailuresIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := testDataset()
	historyPublish(t, store, data)
	trailerMatch(t, pool, "ugc", "200", 42, "")
	now := data.GeneratedAt.Add(time.Minute)
	clock := func() time.Time { return now }
	worker := &trailer.Store{Pool: pool}
	calls := 0
	fetch := trailerFetchFunc(func(context.Context, string) (*trailer.Media, error) {
		calls++
		return nil, fmt.Errorf("synthetic failure")
	})
	before, err := store.CurrentRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	summary, err := trailer.RunCycle(t.Context(), worker, fetch, clock)
	if err != nil || summary.Failed != 1 {
		t.Fatal(summary, err)
	}
	after, err := store.CurrentRevision(t.Context())
	if err != nil || before != after {
		t.Fatal("failed enrichment changed schedule revision", err)
	}
	_, err = trailer.RunCycle(t.Context(), worker, fetch, clock)
	if err != nil || calls != 1 {
		t.Fatal("failure cache ignored", err)
	}
	now = now.Add(time.Hour)
	fetch = func(context.Context, string) (*trailer.Media, error) {
		calls++
		return trailer.MP4(trailerMP4, "ugc"), nil
	}
	summary, err = trailer.RunCycle(t.Context(), worker, fetch, clock)
	if err != nil || summary.Found != 1 {
		t.Fatal(summary, err)
	}
	if loadedTrailer(t, store, 42) == nil {
		t.Fatal("UGC lookup not published")
	}
	after, err = store.CurrentRevision(t.Context())
	if err != nil || after.ScheduleVersion != before.ScheduleVersion || after.EnrichmentVersion <= before.EnrichmentVersion {
		t.Fatal("lookup revision incorrect", err)
	}
	_, err = trailer.RunCycle(t.Context(), worker, fetch, clock)
	if err != nil || calls != 2 {
		t.Fatal("found cache ignored", err)
	}
	// Cached candidates age out without changing the schedule generation.
	now = now.Add(31 * 24 * time.Hour)
	_, err = trailer.RunCycle(t.Context(), worker, nil, clock)
	if err != nil {
		t.Fatal(err)
	}
	if loadedTrailer(t, store, 42) != nil {
		t.Fatal("expired trailer remained")
	}
	// No API replica can duplicate a cycle while another owns the session lease.
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), `SELECT pg_advisory_lock(6211428337968391)`); err != nil {
		t.Fatal(err)
	}
	summary, err = trailer.RunCycle(t.Context(), worker, fetch, clock)
	if err != nil || summary.Attempted != 0 {
		t.Fatal("worker ignored replica lease", err)
	}
	_, _ = conn.Exec(t.Context(), `SELECT pg_advisory_unlock(6211428337968391)`)
	conn.Release()
}

func TestTrailerWriteWaitsForSchedulePublicationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := testDataset()
	historyPublish(t, store, data)
	trailerMatch(t, pool, "ugc", "200", 42, "")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started, resume := make(chan struct{}), make(chan struct{})
	fetch := trailerFetchFunc(func(ctx context.Context, _ string) (*trailer.Media, error) {
		close(started)
		select {
		case <-resume:
			return trailer.MP4(trailerMP4, "ugc"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := trailer.RunCycle(ctx, &trailer.Store{Pool: pool}, fetch, func() time.Time { return data.GeneratedAt })
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("worker did not start")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(6211428337968315)`); err != nil {
		t.Fatal(err)
	}
	close(resume)
	// Observe the worker's actual PostgreSQL lock wait, rather than assuming
	// scheduling order or sleeping for an arbitrary period.
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks lease ON lease.pid=w.pid WHERE w.locktype='advisory' AND NOT w.granted AND w.classid::bigint*4294967296+w.objid::bigint=6211428337968315 AND lease.locktype='advisory' AND lease.granted AND lease.classid::bigint*4294967296+lease.objid::bigint=6211428337968391)`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("worker bypassed publication lock: %v", err)
		case <-ctx.Done():
			t.Fatal("publication wait not observed")
		case <-tick.C:
		}
	}
	historyCount(t, pool, `SELECT count(*) FROM movie_trailer_candidates`, 0)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not finish after publication")
	}
	if loadedTrailer(t, store, 42) == nil {
		t.Fatal("serialized lookup lost its result")
	}
}

func TestTrailerLookupRechecksAdminDecisionIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := testDataset()
	historyPublish(t, store, data)
	trailerMatch(t, pool, "ugc", "200", 42, "")
	fetch := trailerFetchFunc(func(ctx context.Context, _ string) (*trailer.Media, error) {
		_, err := pool.Exec(ctx, `INSERT INTO public_movie_metadata_overrides(public_movie_id,trailer_vo_youtube_key_overridden) SELECT id,true FROM public_movies WHERE confirmed_tmdb_id=42`)
		return trailer.MP4(trailerMP4, "ugc"), err
	})
	summary, err := trailer.RunCycle(t.Context(), &trailer.Store{Pool: pool}, fetch, func() time.Time { return data.GeneratedAt })
	if err != nil || summary.Attempted != 1 {
		t.Fatal(summary, err)
	}
	historyCount(t, pool, `SELECT count(*) FROM movie_trailer_candidates`, 0)
	if loadedTrailer(t, store, 42) != nil {
		t.Fatal("in-flight lookup bypassed new admin decision")
	}
}
