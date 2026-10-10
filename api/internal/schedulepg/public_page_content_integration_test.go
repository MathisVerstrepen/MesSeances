package schedulepg

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
)

func observePublicPages(t *testing.T, observer *SitemapObserver) schedule.SitemapData {
	t.Helper()
	result, err := observer.ObserveSitemapData(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.MovieTotal != len(result.Movies) {
		t.Fatal("incomplete film inventory")
	}
	for _, movie := range result.Movies {
		if _, ok := result.LastmodByPath["/film/"+movie.Slug]; !ok {
			t.Fatal("missing film map key")
		}
	}
	for _, date := range result.LastmodByPath {
		if date != nil && date.After(result.AsOf) {
			t.Fatal("future date")
		}
	}
	return result
}

func publicPagesLedger(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(t.Context(), `SELECT jsonb_build_array((SELECT observed_at FROM public_page_content_state),(SELECT jsonb_agg(to_jsonb(p) ORDER BY path) FROM public_page_content p))::text`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func publicPagesMetadata(now time.Time) (enrichment.Match, enrichment.Metadata) {
	metadata := enrichment.Metadata{Provider: enrichment.ProviderTMDB, ProviderMovieID: 42, Locale: enrichment.LocaleFrench, ProviderTitle: "Film A", LocalizedTitle: "Film A", Overview: "Original synopsis", RuntimeMinutes: 100, Genres: []string{}, FetchedAt: now, RefreshAfter: now.Add(24 * time.Hour)}
	match := enrichment.Match{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200", MetadataProvider: enrichment.ProviderTMDB, Status: enrichment.StatusMatched, MetadataMovieID: 42, Score: 1, NormalizedSourceTitle: "film a", SourceRuntimeMinutes: 100, Candidates: []enrichment.Candidate{}, EvaluatedAt: now, RetryAfter: now.Add(24 * time.Hour)}
	return match, metadata
}

func TestPublicPageContentPublicationPersistenceIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := testDataset()
	historyPublish(t, store, data)
	now := time.Date(2026, 8, 15, 8, 0, 0, 123456789, time.UTC)
	match, metadata := publicPagesMetadata(now)
	enrichmentStore := enrichment.NewPostgresStore(pool)
	if err := enrichmentStore.Publish(t.Context(), match, metadata); err != nil {
		t.Fatal(err)
	}
	observer := &SitemapObserver{Store: store, Now: func() time.Time { return now }}
	baseline := observePublicPages(t, observer)
	for _, date := range baseline.LastmodByPath {
		if date != nil {
			t.Fatal("initial dates must be unknown")
		}
	}
	var film string
	for _, movie := range baseline.Movies {
		if movie.TMDBID != nil && *movie.TMDBID == 42 {
			film = "/film/" + movie.Slug
		}
	}
	if film == "" {
		t.Fatal("matched identity absent")
	}
	for i := 0; i < 3; i++ {
		data.GeneratedAt = data.GeneratedAt.Add(time.Minute)
		historyPublish(t, store, data)
		metadata.FetchedAt = metadata.FetchedAt.Add(time.Minute)
		metadata.RefreshAfter = metadata.RefreshAfter.Add(time.Minute)
		if err := enrichmentStore.RefreshMetadata(t.Context(), []enrichment.Metadata{metadata}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
		result := observePublicPages(t, observer)
		if !reflect.DeepEqual(result.LastmodByPath, baseline.LastmodByPath) || result.Revision == baseline.Revision {
			t.Fatal("equal publication altered dates or omitted exact revision")
		}
	}
	// Replace prunes older generations; the independent ledger survives a repository restart.
	historyCount(t, pool, `SELECT count(DISTINCT generation_id) FROM theaters`, 2)
	observer.Store = NewStore(pool)
	restart := observePublicPages(t, observer)
	if !reflect.DeepEqual(restart.LastmodByPath, baseline.LastmodByPath) {
		t.Fatal("restart lost baseline")
	}
	metadata.Overview = "Changed synopsis"
	if err := enrichmentStore.RefreshMetadata(t.Context(), []enrichment.Metadata{metadata}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	changed := observePublicPages(t, observer)
	for path, date := range changed.LastmodByPath {
		if path == film {
			if date == nil || !date.Equal(changed.AsOf) {
				t.Fatal("changed metadata date missing")
			}
		} else if date != nil {
			t.Fatalf("unrendered synopsis changed unrelated %s", path)
		}
	}
	// Removing a newest session retains evidence despite an older session remaining.
	data.Showtimes = append(data.Showtimes[:1], data.Showtimes[2:]...)
	historyPublish(t, store, data)
	now = now.Add(time.Minute)
	removed := observePublicPages(t, observer)
	if removed.LastmodByPath[film] == nil || !removed.LastmodByPath[film].Equal(removed.AsOf) {
		t.Fatal("session removal lost")
	}
	if removed.LastmodByPath["/cinema/ugc-99"] != nil || removed.LastmodByPath["/ville/lyon/cinemas"] != nil {
		t.Fatal("unrelated local page advanced")
	}
	// Final cinema cancellation and disappearance must not recreate its URL from storage.
	data.Theaters = data.Theaters[:2]
	data.Showtimes = data.Showtimes[:len(data.Showtimes)-1]
	historyPublish(t, store, data)
	now = now.Add(time.Minute)
	disappeared := observePublicPages(t, observer)
	if _, ok := disappeared.LastmodByPath["/cinema/ugc-99"]; ok {
		t.Fatal("tombstone recreated URL")
	}
	historyCount(t, pool, `SELECT count(*) FROM public_page_content WHERE NOT present AND path IN('/cinema/ugc-99','/ville/lyon/cinemas')`, 2)
	data = testDataset()
	historyPublish(t, store, data)
	now = now.Add(time.Minute)
	reappeared := observePublicPages(t, observer)
	for _, path := range []string{"/cinema/ugc-99", "/ville/lyon/cinemas"} {
		if date := reappeared.LastmodByPath[path]; date == nil || !date.Equal(reappeared.AsOf) {
			t.Fatal("reappearance date missing", path)
		}
	}
	now = now.Add(time.Minute)
	equal := observePublicPages(t, observer)
	if !reflect.DeepEqual(equal.LastmodByPath, reappeared.LastmodByPath) {
		t.Fatal("equal observation reset changed dates")
	}
}

func TestPublicPageContentRollbackClockAndImagesIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	historyPublish(t, store, testDataset())
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	observer := &SitemapObserver{Store: store, PublicImages: true, Now: func() time.Time { return now }}
	baseline := observePublicPages(t, observer)
	historyExec(t, pool, `INSERT INTO theater_images(provider,provider_theater_id,image_revision,file_key,width,height,size_bytes) VALUES('ugc','25',1,$1,800,600,1000)`, strings.Repeat("a", 32)+".webp")
	now = now.Add(time.Minute)
	photo := observePublicPages(t, observer)
	for path, date := range photo.LastmodByPath {
		if (date != nil) != (path == "/cinema/ugc-25") {
			t.Fatal("photo isolation", path)
		}
	}
	historyExec(t, pool, `UPDATE theater_images SET image_revision=2,file_key=$1 WHERE provider='ugc' AND provider_theater_id='25'`, strings.Repeat("b", 32)+".webp")
	now = now.Add(time.Minute)
	replacement := observePublicPages(t, observer)
	if replacement.LastmodByPath["/cinema/ugc-25"] == nil || !replacement.LastmodByPath["/cinema/ugc-25"].Equal(replacement.AsOf) {
		t.Fatal("photo replacement missed")
	}
	historyExec(t, pool, `UPDATE theater_images SET image_revision=3,file_key=NULL,width=NULL,height=NULL,size_bytes=NULL WHERE provider='ugc' AND provider_theater_id='25'`)
	now = now.Add(time.Minute)
	removal := observePublicPages(t, observer)
	if !removal.LastmodByPath["/cinema/ugc-25"].Equal(removal.AsOf) {
		t.Fatal("photo removal missed")
	}
	if baseline.LastmodByPath["/cinema/ugc-25"] != nil {
		t.Fatal("baseline mutated")
	}
	ledger := publicPagesLedger(t, pool)
	now = now.Add(-time.Hour)
	if _, err := observer.ObserveSitemapData(t.Context()); err == nil {
		t.Fatal("clock regression accepted")
	}
	if publicPagesLedger(t, pool) != ledger {
		t.Fatal("regression modified ledger")
	}
	now = removal.AsOf.Add(time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	observer.Now = func() time.Time { cancel(); return now }
	if _, err := observer.ObserveSitemapData(ctx); err == nil {
		t.Fatal("cancellation accepted")
	}
	if publicPagesLedger(t, pool) != ledger {
		t.Fatal("cancellation committed ledger")
	}
	observer.Now = func() time.Time { return now }
	historyExec(t, pool, `UPDATE public_movies SET overview='New metadata' WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200')`)
	// Force failure after queued comparison writes; singleton and preceding writes must roll back.
	historyExec(t, pool, `ALTER TABLE public_page_content ADD CONSTRAINT observation_test_failure CHECK (changed_at IS NULL OR path <> '/cinema/ugc-26') NOT VALID`)
	historyExec(t, pool, `UPDATE theater_images SET image_revision=1,file_key=$1,width=800,height=600,size_bytes=1000 WHERE provider='ugc' AND provider_theater_id='25'`, strings.Repeat("c", 32)+".webp")
	// The currently absent ugc-26 photo receives a new stable public image URL.
	historyExec(t, pool, `INSERT INTO theater_images(provider,provider_theater_id,image_revision,file_key,width,height,size_bytes) VALUES('ugc','26',1,$1,800,600,1000)`, strings.Repeat("d", 32)+".webp")
	if _, err := observer.ObserveSitemapData(t.Context()); err == nil {
		t.Fatal("failed batch accepted")
	}
	if publicPagesLedger(t, pool) != ledger {
		t.Fatal("partial batch committed")
	}
	historyExec(t, pool, `ALTER TABLE public_page_content DROP CONSTRAINT observation_test_failure`)
	result := observePublicPages(t, observer)
	if result.LastmodByPath["/cinema/ugc-26"] == nil {
		t.Fatal("retry failed")
	}
	ledger = publicPagesLedger(t, pool)
	historyExec(t, pool, `DELETE FROM schedule_snapshot`)
	if _, err := observer.ObserveSitemapData(t.Context()); err == nil {
		t.Fatal("unavailable observation accepted")
	}
	if publicPagesLedger(t, pool) != ledger {
		t.Fatal("unavailable tombstoned ledger")
	}
}

type publicPageLockTrace struct{ started chan uint32 }

func (tr *publicPageLockTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "public_page_content_state WHERE singleton FOR UPDATE") {
		select {
		case tr.started <- conn.PgConn().PID():
		default:
		}
	}
	return ctx
}
func (*publicPageLockTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPublicPageContentSerializationOverlapIntegration(t *testing.T) {
	trace := &publicPageLockTrace{started: make(chan uint32, 10)}
	pool := newHistoryPool(t, trace)
	store := NewStore(pool)
	historyPublish(t, store, testDataset())
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	observePublicPages(t, &SitemapObserver{Store: store, Now: func() time.Time { return now }})
	<-trace.started
	locked := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	first := &SitemapObserver{Store: store, Now: func() time.Time { close(locked); <-release; return now.Add(time.Minute) }}
	go func() { _, err := first.ObserveSitemapData(t.Context()); firstDone <- err }()
	<-locked
	<-trace.started
	// Publication does not lock the derived observation ledger. First snapshot stays old.
	historyExec(t, pool, `UPDATE public_movies SET overview='Concurrent new metadata' WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200')`)
	type outcome struct {
		data schedule.SitemapData
		err  error
	}
	secondDone := make(chan outcome, 1)
	secondCalls := 0
	second := &SitemapObserver{Store: store, Now: func() time.Time { secondCalls++; return now.Add(2 * time.Minute) }}
	go func() { data, err := second.ObserveSitemapData(t.Context()); secondDone <- outcome{data, err} }()
	pid := <-trace.started
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(t.Context(), `SELECT cardinality(pg_blocking_pids($1)) > 0`, int64(pid)).Scan(&waiting); err != nil {
			close(release)
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("second observer never overlapped")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	result := <-secondDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	if secondCalls != 1 {
		t.Fatalf("clock frozen before successful lock: %d", secondCalls)
	}
	var changed int
	for _, date := range result.data.LastmodByPath {
		if date != nil {
			changed++
			if !date.Equal(result.data.AsOf) {
				t.Fatal("stale timestamp overwrite")
			}
		}
	}
	if changed != 1 {
		t.Fatalf("coherent second snapshot changes=%d", changed)
	}
	final := observePublicPages(t, &SitemapObserver{Store: NewStore(pool), Now: func() time.Time { return now.Add(3 * time.Minute) }})
	if !reflect.DeepEqual(final.LastmodByPath, result.data.LastmodByPath) {
		t.Fatal("stale/future ledger overwrite")
	}
}

func TestPublicPageContentCatalogueOnlyAndConstraintsIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	_, metadata := publicPagesMetadata(now)
	enrichmentStore := enrichment.NewPostgresStore(pool)
	if err := enrichmentStore.PublishUpcoming(t.Context(), enrichment.UpcomingPublication{CompletedAt: now, Window: schedule.UpcomingWindow(now), Metadata: []enrichment.Metadata{metadata}, Releases: []enrichment.UpcomingRelease{}}); err != nil {
		t.Fatal(err)
	}
	observer := &SitemapObserver{Store: NewStore(pool), Now: func() time.Time { return now }}
	result := observePublicPages(t, observer)
	if result.Cities != nil || !result.UpcomingAvailable || !strings.HasPrefix(result.Revision, "schedule:0;") {
		t.Fatalf("catalogue-only=%+v", result)
	}
	for _, path := range []string{"/", "/films", "/film/a?x=1", "/film/a#x", "/ville/a", "/cinema/"} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO public_page_content(path,fingerprint,present) VALUES($1,$2,true)`, path, make([]byte, 32)); err == nil {
			t.Fatal("non-detail path accepted", path)
		}
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO public_page_content(path,fingerprint,present) VALUES('/film/fake',$1,true)`, make([]byte, 31)); err == nil {
		t.Fatal("invalid digest size accepted")
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO public_page_content_state(singleton) VALUES(false)`); err == nil {
		t.Fatal("second singleton accepted")
	}
	historyCount(t, pool, `SELECT count(*) FROM public_page_content_state`, 1)
}
