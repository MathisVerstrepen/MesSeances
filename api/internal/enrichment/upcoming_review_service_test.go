package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"
)

type reviewClockStore struct {
	calls int
	now   time.Time
}

func (s *reviewClockStore) UpcomingReviews(_ context.Context, _ UpcomingReviewQuery, now time.Time) (UpcomingReviewList, error) {
	s.calls++
	s.now = now
	return UpcomingReviewList{Items: []AdminUpcomingMovie{}}, nil
}
func (s *reviewClockStore) SetUpcomingDecision(_ context.Context, _ int64, _ UpcomingDecisionUpdate, now time.Time) (AdminUpcomingMovie, error) {
	s.calls++
	s.now = now
	return AdminUpcomingMovie{}, nil
}

func TestUpcomingReviewClockAndValidation(t *testing.T) {
	now := time.Date(2026, 9, 13, 21, 59, 59, 0, time.UTC)
	calls := 0
	store := &reviewClockStore{}
	s := NewUpcomingReviewService(store, func() time.Time { calls++; return now })
	if _, err := s.List(t.Context(), UpcomingReviewQuery{Filter: "all", Limit: 50}); err != nil || calls != 1 || store.now != now {
		t.Fatal("list did not capture exactly one clock")
	}
	if _, err := s.Update(t.Context(), 42, UpcomingDecisionUpdate{Decision: "approved", ExpectedRevision: 1}); err != nil || calls != 2 || store.now != now {
		t.Fatal("edit did not capture exactly one clock")
	}
	if _, err := s.List(t.Context(), UpcomingReviewQuery{Filter: "all", Limit: 101}); !errors.Is(err, ErrUpcomingReviewInvalid) || store.calls != 2 {
		t.Fatal("invalid list reached store")
	}
	if _, err := s.Update(t.Context(), 0, UpcomingDecisionUpdate{Decision: "approved", ExpectedRevision: 1}); !errors.Is(err, ErrUpcomingReviewInvalid) || store.calls != 2 {
		t.Fatal("invalid edit reached store")
	}
	date := "2026-09-14"
	item := AdminUpcomingMovie{PublicMovieID: "42", FrenchReleaseDate: &date, Active: true, Decision: "unreviewed"}
	completeUpcomingReview(&item, now)
	if !item.PubliclyVisible || item.AssessmentStatus != "pending" || item.FrenchReleases == nil || item.ReasonCodes == nil || item.Slug != "film-42" {
		t.Fatalf("pending %+v", item)
	}
	completeUpcomingReview(&item, now.Add(time.Second))
	if item.InWindow || item.PubliclyVisible {
		t.Fatal("Paris midnight window did not age")
	}
	item.Decision = "approved"
	completeUpcomingReview(&item, now.Add(time.Second))
	if item.PubliclyVisible {
		t.Fatal("approval forced out-of-window row visible")
	}
	item.Active = false
	completeUpcomingReview(&item, now)
	if item.PubliclyVisible || !item.InWindow {
		t.Fatal("approval forced inactive visible")
	}
	item.Active = true
	item.Decision = "excluded"
	completeUpcomingReview(&item, now)
	if item.PubliclyVisible || !item.InWindow {
		t.Fatal("exclusion modified window eligibility")
	}
}
