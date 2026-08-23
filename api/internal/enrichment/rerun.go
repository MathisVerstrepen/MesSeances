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
	running atomic.Bool
}

func NewRerunService(store RerunStore, matcher ForceMatcher) *RerunService {
	return &RerunService{store: store, matcher: matcher}
}

func (s *RerunService) Rerun(ctx context.Context) (RerunSummary, error) {
	if s == nil || s.store == nil || s.matcher == nil {
		return RerunSummary{}, ErrRerunUnavailable
	}
	if !s.running.CompareAndSwap(false, true) {
		return RerunSummary{}, ErrRerunInProgress
	}
	defer s.running.Store(false)

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
