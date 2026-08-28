package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"messeances/api/internal/tmdb"
)

var (
	ErrRerunInProgress  = errors.New("TMDB rerun already in progress")
	ErrRerunUnavailable = errors.New("TMDB rerun unavailable")
)

type RerunSummary struct {
	Processed      int `json:"processed"`
	Reused         int `json:"reused"`
	Matched        int `json:"matched"`
	ReviewRequired int `json:"review_required"`
	Unmatched      int `json:"unmatched"`
	Failed         int `json:"failed"`
}

type RerunStore interface {
	UnresolvedMovies(context.Context) ([]Movie, error)
}

type ForceMatcher interface {
	ForceRun(context.Context, []Movie) (Summary, error)
}

type RerunService struct {
	store   RerunStore
	matcher ForceMatcher
	gate    *TMDBRunGate
}

type TMDBRunGate struct {
	running atomic.Bool
}

func NewTMDBRunGate() *TMDBRunGate {
	return &TMDBRunGate{}
}

func (g *TMDBRunGate) tryAcquire() bool {
	return g != nil && g.running.CompareAndSwap(false, true)
}

func (g *TMDBRunGate) release() {
	if g != nil {
		g.running.Store(false)
	}
}

func NewRerunService(store RerunStore, matcher ForceMatcher, gate *TMDBRunGate) *RerunService {
	if gate == nil {
		gate = NewTMDBRunGate()
	}
	return &RerunService{store: store, matcher: matcher, gate: gate}
}

func (s *RerunService) Rerun(ctx context.Context) (RerunSummary, error) {
	if s == nil || s.store == nil || s.matcher == nil || s.gate == nil {
		return RerunSummary{}, ErrRerunUnavailable
	}
	if !s.gate.tryAcquire() {
		return RerunSummary{}, ErrRerunInProgress
	}
	defer s.gate.release()

	movies, err := s.store.UnresolvedMovies(ctx)
	if err != nil {
		return RerunSummary{}, fmt.Errorf("read unresolved movies failed: %w", err)
	}
	summary, err := s.matcher.ForceRun(ctx, movies)
	if errors.Is(err, tmdb.ErrStop) {
		return RerunSummary{}, ErrRerunUnavailable
	}
	if err != nil {
		return RerunSummary{}, fmt.Errorf("rerun unresolved movies failed: %w", err)
	}
	return RerunSummary{
		Processed: len(movies), Reused: summary.Reused, Matched: summary.Matched,
		ReviewRequired: summary.ReviewRequired, Unmatched: summary.Unmatched, Failed: summary.Failed,
	}, nil
}
