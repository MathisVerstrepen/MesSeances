package enrichment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

type metadataRefreshStore struct {
	ids          []int64
	idsErr       error
	metadata     map[int64]Metadata
	readErr      error
	publishErr   error
	publishCalls int
	published    []Metadata
}

func (s *metadataRefreshStore) MatchedTMDBIDs(context.Context) ([]int64, error) {
	return append([]int64(nil), s.ids...), s.idsErr
}

func (s *metadataRefreshStore) Metadata(_ context.Context, _ string, id int64, _ string) (Metadata, bool, error) {
	if s.readErr != nil {
		return Metadata{}, false, s.readErr
	}
	metadata, found := s.metadata[id]
	return metadata, found, nil
}

func (s *metadataRefreshStore) RefreshMetadata(_ context.Context, metadata []Metadata) error {
	s.publishCalls++
	if s.publishErr != nil {
		return s.publishErr
	}
	s.published = append(s.published, metadata...)
	return nil
}

type metadataDetailsResult struct {
	details tmdb.Details
	err     error
}

type metadataRefreshProvider struct {
	results map[int64]metadataDetailsResult
	calls   []int64
}

func (p *metadataRefreshProvider) Details(_ context.Context, id int64) (tmdb.Details, error) {
	p.calls = append(p.calls, id)
	result := p.results[id]
	return result.details, result.err
}

func TestMetadataRefreshFetchesDistinctIDsAndPublishesAllCurrentFields(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	unchanged := metadataFromDetails(tmdb.Details{ID: 20, IMDBID: "tt1234567", OriginalTitle: "Original 20", Title: "Film 20", Overview: "Résumé", ReleaseDate: "2026-08-01", PosterURL: "https://image.tmdb.org/t/p/w500/poster.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/backdrop.jpg", TrailerVFYouTubeKey: "abcdefghijk", TrailerVOYouTubeKey: "lmnopqrstuv", Runtime: 101, Genres: []string{"Drame"}}, 0, now.Add(-time.Hour))
	unchanged.RefreshAfter = now.Add(29 * 24 * time.Hour)
	store := &metadataRefreshStore{
		ids:      []int64{20, 10, 20, 0, -1},
		metadata: map[int64]Metadata{20: unchanged},
	}
	provider := &metadataRefreshProvider{results: map[int64]metadataDetailsResult{
		10: {details: tmdb.Details{ID: 10, IMDBID: "tt7654321", OriginalTitle: "Original 10", Title: "Film 10", Overview: "Nouveau résumé", ReleaseDate: "2026-08-02", PosterURL: "https://image.tmdb.org/t/p/w500/new.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/new.jpg", TrailerVFYouTubeKey: "12345678901", TrailerVOYouTubeKey: "10987654321", Runtime: 95, Genres: []string{"Action", "Comédie"}}},
		20: {details: tmdb.Details{ID: 20, IMDBID: "tt1234567", OriginalTitle: "Original 20", Title: "Film 20", Overview: "Résumé", ReleaseDate: "2026-08-01", PosterURL: "https://image.tmdb.org/t/p/w500/poster.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/backdrop.jpg", TrailerVFYouTubeKey: "abcdefghijk", TrailerVOYouTubeKey: "lmnopqrstuv", Runtime: 101, Genres: []string{"Drame"}}},
	}}
	service := NewMetadataRefreshService(store, provider, func() time.Time { return now }, nil)

	summary, err := service.Refresh(context.Background())
	if err != nil || summary != (MetadataRefreshSummary{Processed: 2, Updated: 1, Unchanged: 1}) {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	if len(provider.calls) != 2 || provider.calls[0] != 10 || provider.calls[1] != 20 {
		t.Fatalf("provider calls=%v", provider.calls)
	}
	if store.publishCalls != 1 || len(store.published) != 2 {
		t.Fatalf("publish calls=%d published=%+v", store.publishCalls, store.published)
	}
	got := store.published[0]
	if got.ProviderMovieID != 10 || got.IMDBID != "tt7654321" || got.ProviderTitle != "Original 10" || got.LocalizedTitle != "Film 10" || got.Overview != "Nouveau résumé" || got.ReleaseDate != "2026-08-02" || got.PosterURL != "https://image.tmdb.org/t/p/w500/new.jpg" || got.BackdropURL != "https://image.tmdb.org/t/p/w780/new.jpg" || got.TrailerVFYouTubeKey != "12345678901" || got.TrailerVOYouTubeKey != "10987654321" || got.RuntimeMinutes != 95 || len(got.Genres) != 2 || !got.FetchedAt.Equal(now) || !got.RefreshAfter.Equal(now.Add(metadataTTL)) {
		t.Fatalf("refreshed metadata=%+v", got)
	}
	if !store.published[1].FetchedAt.Equal(now) || !store.published[1].RefreshAfter.Equal(now.Add(metadataTTL)) {
		t.Fatalf("unchanged metadata freshness=%+v", store.published[1])
	}
}

func TestSameMetadataContentComparesBothTrailerVariants(t *testing.T) {
	base := Metadata{Provider: ProviderTMDB, ProviderMovieID: 1, IMDBID: "tt1234567", Locale: LocaleFrench, ProviderTitle: "Original", LocalizedTitle: "Film", TrailerVFYouTubeKey: "FRoff123456", TrailerVOYouTubeKey: "ENoff123456", RuntimeMinutes: 90, Genres: []string{}}
	imdbChanged := base
	imdbChanged.IMDBID = "tt7654321"
	if sameMetadataContent(base, imdbChanged) {
		t.Fatal("IMDb-only change classified as unchanged")
	}
	for _, variant := range []string{"vf", "vo"} {
		changed := base
		if variant == "vf" {
			changed.TrailerVFYouTubeKey = "FRoff654321"
		} else {
			changed.TrailerVOYouTubeKey = "ENoff654321"
		}
		if sameMetadataContent(base, changed) {
			t.Fatalf("%s trailer change classified as unchanged", variant)
		}
	}
}

func TestMetadataRefreshContinuesAfterIndividualDetailFailures(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store := &metadataRefreshStore{ids: []int64{1, 2, 3, 4}, metadata: map[int64]Metadata{}}
	provider := &metadataRefreshProvider{results: map[int64]metadataDetailsResult{
		1: {err: errors.New("retryable detail failure")},
		2: {details: tmdb.Details{ID: 999, OriginalTitle: "Wrong", Title: "Wrong", Runtime: 90}},
		3: {details: tmdb.Details{ID: 3, OriginalTitle: "", Title: "Invalid", Runtime: 90}},
		4: {details: tmdb.Details{ID: 4, OriginalTitle: "Original", Title: "Valid", Runtime: 90}},
	}}
	service := NewMetadataRefreshService(store, provider, func() time.Time { return now }, nil)

	summary, err := service.Refresh(context.Background())
	if err != nil || summary != (MetadataRefreshSummary{Processed: 4, Updated: 1, Failed: 3}) {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	if len(provider.calls) != 4 || len(store.published) != 1 || store.published[0].ProviderMovieID != 4 {
		t.Fatalf("provider calls=%v published=%+v", provider.calls, store.published)
	}
}

func TestMetadataRefreshFatalErrorsFailClosedAndReleaseGate(t *testing.T) {
	store := &metadataRefreshStore{ids: []int64{1, 2}, metadata: map[int64]Metadata{}}
	provider := &metadataRefreshProvider{results: map[int64]metadataDetailsResult{
		1: {err: tmdb.ErrStop},
		2: {details: tmdb.Details{ID: 2, OriginalTitle: "Original", Title: "Film", Runtime: 90}},
	}}
	service := NewMetadataRefreshService(store, provider, time.Now, nil)
	if _, err := service.Refresh(context.Background()); !errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("stop error=%v", err)
	}
	if len(provider.calls) != 1 || store.publishCalls != 0 || len(store.published) != 0 {
		t.Fatalf("provider calls=%v publish calls=%d published=%+v", provider.calls, store.publishCalls, store.published)
	}
	provider.results[1] = metadataDetailsResult{details: tmdb.Details{ID: 1, OriginalTitle: "Original", Title: "Film", Runtime: 90}}
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("later refresh error=%v", err)
	}
	if len(provider.calls) != 3 {
		t.Fatalf("later provider calls=%v", provider.calls)
	}

	store.idsErr = errors.New("database secret")
	if _, err := service.Refresh(context.Background()); err == nil || errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("store error=%v", err)
	}
}

type sharedGateProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *sharedGateProvider) Search(context.Context, string) ([]tmdb.Candidate, error) {
	p.block()
	return nil, nil
}

func (p *sharedGateProvider) Details(_ context.Context, id int64) (tmdb.Details, error) {
	p.block()
	return tmdb.Details{ID: id, OriginalTitle: "Original", Title: "Film", Runtime: 90}, nil
}

func (p *sharedGateProvider) block() {
	p.once.Do(func() { close(p.started) })
	<-p.release
}

func TestMetadataRefreshAndUnresolvedRerunShareSingleFlightGate(t *testing.T) {
	gate := NewTMDBRunGate()
	provider := &sharedGateProvider{started: make(chan struct{}), release: make(chan struct{})}
	runStore := &rerunStore{movies: []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}}}
	run := NewRerunService(runStore, NewMatcher(newMemoryStore(), provider, func() time.Time { return matcherNow }), gate)
	refreshStore := &metadataRefreshStore{ids: []int64{20}, metadata: map[int64]Metadata{}}
	refresh := NewMetadataRefreshService(refreshStore, provider, func() time.Time { return matcherNow }, gate)

	rerunDone := make(chan error, 1)
	go func() {
		_, err := run.Rerun(context.Background())
		rerunDone <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("rerun did not reach provider")
	}
	if _, err := refresh.Refresh(context.Background()); !errors.Is(err, ErrMetadataRefreshInProgress) {
		t.Fatalf("refresh during rerun error=%v", err)
	}
	close(provider.release)
	if err := <-rerunDone; err != nil {
		t.Fatalf("rerun error=%v", err)
	}

	provider = &sharedGateProvider{started: make(chan struct{}), release: make(chan struct{})}
	refresh = NewMetadataRefreshService(refreshStore, provider, func() time.Time { return matcherNow }, gate)
	refreshDone := make(chan error, 1)
	go func() {
		_, err := refresh.Refresh(context.Background())
		refreshDone <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach provider")
	}
	if _, err := run.Rerun(context.Background()); !errors.Is(err, ErrRerunInProgress) {
		t.Fatalf("rerun during refresh error=%v", err)
	}
	close(provider.release)
	if err := <-refreshDone; err != nil {
		t.Fatalf("refresh error=%v", err)
	}
}

func TestMetadataRefreshUnavailableWithoutDependencies(t *testing.T) {
	if _, err := (*MetadataRefreshService)(nil).Refresh(context.Background()); !errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("nil service error=%v", err)
	}
	service := NewMetadataRefreshService(nil, &metadataRefreshProvider{}, time.Now, nil)
	if _, err := service.Refresh(context.Background()); !errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("nil store error=%v", err)
	}
}
