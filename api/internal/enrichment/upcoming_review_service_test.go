package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"

	"messeances/api/internal/schedule"
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
	now := time.Date(2026, 9, 15, 21, 59, 59, 0, time.UTC)
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
	date := "2026-09-16"
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

type reviewSnapshotSource struct{ view *schedule.SnapshotView }

func (s reviewSnapshotSource) Snapshot() *schedule.SnapshotView { return s.view }

func TestUpcomingReviewDisplayVisibilityParity(t *testing.T) {
	for _, test := range []struct {
		name, date, decision string
		active               bool
		before, after        bool
	}{
		{"current week later release", "2026-09-15", "unreviewed", true, false, false},
		{"next Wednesday", "2026-09-16", "unreviewed", true, true, false},
		{"next Tuesday approved", "2026-09-22", "approved", true, true, false},
		{"following week", "2026-09-23", "approved", true, true, true},
		{"excluded", "2026-09-23", "excluded", true, false, false},
		{"inactive", "2026-09-23", "approved", false, false, false},
		{"withdrawn", "", "unreviewed", false, false, false},
		{"inclusive anniversary", "2027-09-15", "unreviewed", true, true, true},
		{"advancing anniversary", "2027-09-16", "unreviewed", true, false, true},
		{"beyond anniversary", "2027-09-17", "approved", true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 9, 15, 21, 59, 59, 0, time.UTC)
			item := AdminUpcomingMovie{TMDBID: 42, PublicMovieID: "42", Active: test.active, Decision: test.decision, Revision: 7}
			if test.date != "" {
				item.FrenchReleaseDate = &test.date
			}
			data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now, PublicMovies: []schedule.PublicMovieRecord{{ID: 42, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Film", FrenchReleaseDate: test.date, HasUpcomingRelease: true, UpcomingActive: test.active, UpcomingExcluded: test.decision == "excluded", UpdatedAt: now}}}
			source := reviewSnapshotSource{schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 1})}
			service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			for _, visible := range []bool{test.before, test.after} {
				completeUpcomingReview(&item, now)
				result, err := service.UpcomingMovies(schedule.UpcomingMoviesQuery{})
				if err != nil || item.PubliclyVisible != visible || (result.Total == 1) != visible {
					t.Fatalf("now=%s admin=%+v public=%+v err=%v", now, item, result, err)
				}
				inWindow := test.date != "" && test.date >= result.Window.From && test.date <= result.Window.Through
				if item.InWindow != inWindow || item.Decision != test.decision || item.Revision != 7 || item.Active != test.active || item.FrenchReleaseDate != nil && *item.FrenchReleaseDate != test.date {
					t.Fatalf("read-only fields or window disagree: %+v", item)
				}
				now = now.Add(time.Second)
			}
		})
	}
}
