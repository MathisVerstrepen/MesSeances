package enrichment

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

type rerunStore struct {
	movies  []Movie
	err     error
	queries atomic.Int32
}

func (s *rerunStore) UnresolvedMovies(context.Context) ([]Movie, error) {
	s.queries.Add(1)
	return append([]Movie(nil), s.movies...), s.err
}

type rerunMatcher struct {
	summary Summary
	err     error
}

func (m rerunMatcher) ForceRun(context.Context, []Movie) (Summary, error) {
	return m.summary, m.err
}

type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (p *blockingProvider) Search(context.Context, string) ([]tmdb.Candidate, error) {
	if p.calls.Add(1) == 1 {
		close(p.started)
	}
	<-p.release
	return nil, nil
}

func (*blockingProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{}, errors.New("unexpected details call")
}

func TestRerunServiceSummaryAndErrors(t *testing.T) {
	movies := []Movie{{ProviderID: "1"}, {ProviderID: "2"}, {ProviderID: "3"}, {ProviderID: "4"}, {ProviderID: "5"}}
	store := &rerunStore{movies: movies}
	service := NewRerunService(store, rerunMatcher{summary: Summary{Reused: 1, Matched: 1, ReviewRequired: 1, Unmatched: 1, Failed: 1}}, nil)
	summary, err := service.Rerun(context.Background())
	if err != nil || summary != (RerunSummary{Processed: 5, Reused: 1, Matched: 1, ReviewRequired: 1, Unmatched: 1, Failed: 1}) {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}

	if _, err := (*RerunService)(nil).Rerun(context.Background()); !errors.Is(err, ErrRerunUnavailable) {
		t.Fatalf("nil service error=%v", err)
	}
	service = NewRerunService(store, rerunMatcher{err: tmdb.ErrStop}, nil)
	if _, err := service.Rerun(context.Background()); !errors.Is(err, ErrRerunUnavailable) {
		t.Fatalf("provider stop error=%v", err)
	}
	store.err = errors.New("database secret")
	service = NewRerunService(store, rerunMatcher{}, nil)
	if _, err := service.Rerun(context.Background()); err == nil || errors.Is(err, ErrRerunUnavailable) {
		t.Fatalf("store error=%v", err)
	}
}

func TestRerunServiceSingleFlightReleasesAfterEveryRun(t *testing.T) {
	store := &rerunStore{movies: []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}}}
	provider := &blockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	service := NewRerunService(store, NewMatcher(newMemoryStore(), provider, func() time.Time { return matcherNow }), nil)
	first := make(chan error, 1)
	go func() {
		_, err := service.Rerun(context.Background())
		first <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("first rerun did not reach provider")
	}
	if _, err := service.Rerun(context.Background()); !errors.Is(err, ErrRerunInProgress) {
		t.Fatalf("second rerun error=%v", err)
	}
	if store.queries.Load() != 1 || provider.calls.Load() != 1 {
		t.Fatalf("queries=%d provider calls=%d", store.queries.Load(), provider.calls.Load())
	}
	close(provider.release)
	if err := <-first; err != nil {
		t.Fatalf("first rerun error=%v", err)
	}
	if _, err := service.Rerun(context.Background()); err != nil {
		t.Fatalf("later rerun error=%v", err)
	}
	if store.queries.Load() != 2 || provider.calls.Load() != 2 {
		t.Fatalf("later queries=%d provider calls=%d", store.queries.Load(), provider.calls.Load())
	}
}
