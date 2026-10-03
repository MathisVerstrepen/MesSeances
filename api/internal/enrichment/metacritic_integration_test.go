package enrichment

import (
	"messeances/api/internal/schedule"
	"testing"
	"time"

	"messeances/api/internal/schedulepg"
	"messeances/api/internal/tmdb"
)

func TestMetacriticFreshPublishedRefreshIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx := t.Context()
	store := NewPostgresStore(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('ugc','10','Film',90); INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'ugc','10',id,'ugc-film-10',title,runtime_minutes FROM public_movies`); err != nil {
		t.Fatal(err)
	}
	now := matcherNow
	details := tmdb.Details{ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: 90}
	if err := store.Publish(ctx, upcomingMatch(now, 42), metadataFromDetails(details, 0, now)); err != nil {
		t.Fatal(err)
	}
	// A synthetic completed empty upcoming publication makes the catalog loadable
	// without fabricating a live schedule or invoking any real acquisition.
	if err := store.PublishUpcoming(ctx, UpcomingPublication{CompletedAt: now, Window: schedule.Window{From: "2026-08-01", Through: "2026-09-01"}}); err != nil {
		t.Fatal(err)
	}
	provider := &metadataRefreshProvider{results: map[int64]metadataDetailsResult{}}
	service := NewMetadataRefreshService(store, provider, func() time.Time { return now }, nil)
	for _, tc := range []struct {
		observed, want   string
		checked, updated bool
	}{
		{"movie/a", "movie/a", true, true}, {"", "movie/a", false, false}, {"movie/b", "movie/b", true, true}, {"", "", true, true},
	} {
		var beforeVersion int64
		var beforeTime time.Time
		if err := pool.QueryRow(ctx, `SELECT (SELECT version FROM movie_enrichment_state),updated_at FROM public_movies WHERE confirmed_tmdb_id=42`).Scan(&beforeVersion, &beforeTime); err != nil {
			t.Fatal(err)
		}
		cached, found, err := store.Metadata(ctx, ProviderTMDB, 42, LocaleFrench)
		if err != nil || !found || !now.Before(cached.RefreshAfter) {
			t.Fatal("not fresh", err)
		}
		details.MetacriticID, details.MetacriticChecked = tc.observed, tc.checked
		provider.results[42] = metadataDetailsResult{details: details}
		now = now.Add(time.Hour)
		summary, err := service.Refresh(ctx)
		want := MetadataRefreshSummary{Processed: 1, Unchanged: 1}
		if tc.updated {
			want.Updated, want.Unchanged = 1, 0
		}
		if err != nil || summary != want {
			t.Fatalf("summary=%+v err=%v", summary, err)
		}
		cached, found, err = store.Metadata(ctx, ProviderTMDB, 42, LocaleFrench)
		if err != nil || !found || cached.MetacriticID != tc.want || cached.MetacriticChecked || !cached.FetchedAt.Equal(now) {
			t.Fatalf("cache=%+v err=%v", cached, err)
		}
		data, revision, err := schedulepg.NewStore(pool).Load(ctx)
		if err != nil || revision.EnrichmentVersion != beforeVersion+1 || len(data.PublicMovies) != 1 || data.PublicMovies[0].MetacriticID != tc.want {
			t.Fatalf("data=%+v revision=%+v err=%v", data.PublicMovies, revision, err)
		}
		updated := data.PublicMovies[0].UpdatedAt
		if tc.updated && !updated.After(beforeTime) || !tc.updated && !updated.Equal(beforeTime) {
			t.Fatal("ID-only timestamp contract", beforeTime, updated)
		}
	}
	if len(provider.calls) != 4 {
		t.Fatal("fresh film skipped", provider.calls)
	}
	// Late reconciliation failure must roll back successful observation, timestamps and version.
	if _, err := pool.Exec(ctx, `UPDATE movie_slug_aliases SET alias_kind='tmdb',source_provider=NULL,source_movie_id=NULL WHERE slug='ugc-film-10'`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(c)) FROM movie_metadata_cache c),(SELECT jsonb_agg(to_jsonb(m)) FROM public_movies m),(SELECT version FROM movie_enrichment_state))::text`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snapshot()
	details.MetacriticID = "movie/rollback"
	provider.results[42] = metadataDetailsResult{details: details}
	if _, err := service.Refresh(ctx); err == nil {
		t.Fatal("bad reconciliation succeeded")
	}
	if snapshot() != before {
		t.Fatal("failed refresh committed")
	}
}

func TestMetacriticCommonWritersIntegration(t *testing.T) {
	pool := upcomingIntegrationPool(t)
	ctx := t.Context()
	store := NewPostgresStore(pool)
	details := tmdb.Details{ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: 90, MetacriticID: "movie/a", MetacriticChecked: true}
	checked := metadataFromDetails(details, 0, matcherNow)
	if err := store.RefreshMetadata(ctx, []Metadata{checked}); err != nil {
		t.Fatal(err)
	}
	details.MetacriticID = ""
	details.MetacriticChecked = false
	details.Overview = "TMDB changed"
	unchecked := metadataFromDetails(details, 0, matcherNow)
	assertCache := func(want string) {
		t.Helper()
		cached, found, err := store.Metadata(ctx, ProviderTMDB, 42, LocaleFrench)
		if err != nil || !found || cached.MetacriticID != want || cached.MetacriticChecked || cached.Overview != details.Overview {
			t.Fatalf("cache=%+v err=%v", cached, err)
		}
	}
	// Shared cache write used by matching/manual publication.
	if err := store.Publish(ctx, upcomingMatch(matcherNow, 42), unchecked); err != nil {
		t.Fatal(err)
	}
	assertCache("movie/a")
	if _, err := pool.Exec(ctx, `INSERT INTO schedule_snapshot(version,schema_version,provider,scope,generated_at,timezone,window_from,window_through) VALUES(1,1,'ugc','all_cinemas',now(),'Europe/Paris','2026-08-01','2026-09-01'); INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','11','ugc-film-11','Film',90)`); err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveReview(ctx, SourceUGC, "11", 42, unchecked, 0, matcherNow); err != nil {
		t.Fatal(err)
	}
	assertCache("movie/a")
	// Upcoming publication uses same flag-based SQL preservation.
	if err := store.PublishUpcoming(ctx, UpcomingPublication{CompletedAt: matcherNow, Window: schedule.Window{From: "2026-08-01", Through: "2026-09-01"}, Metadata: []Metadata{unchecked}}); err != nil {
		t.Fatal(err)
	}
	assertCache("movie/a")
	// Account catalog import performs no secondary I/O inside its transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	adult := false
	details.Adult = &adult
	if _, err := ImportCatalogMovie(ctx, tx, details, matcherNow); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCache("movie/a")
	unchecked.MetacriticChecked = true
	if err := store.RefreshMetadata(ctx, []Metadata{unchecked}); err != nil {
		t.Fatal(err)
	}
	assertCache("")
	// Unchecked insert must not bind even a valid supplied ID.
	unchecked.ProviderMovieID = 99
	unchecked.MetacriticID = "movie/ignored"
	unchecked.MetacriticChecked = false
	if err := store.RefreshMetadata(ctx, []Metadata{unchecked}); err != nil {
		t.Fatal(err)
	}
	cached, _, err := store.Metadata(ctx, ProviderTMDB, 99, LocaleFrench)
	if err != nil || cached.MetacriticID != "" {
		t.Fatal("unchecked insert published ID", err)
	}
}
