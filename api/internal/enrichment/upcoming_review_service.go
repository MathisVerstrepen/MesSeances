package enrichment

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"messeances/api/internal/schedule"
	"messeances/api/internal/tmdb"
)

const MaxReviewInteger int64 = 9007199254740991

var (
	ErrUpcomingReviewInvalid  = errors.New("invalid upcoming review")
	ErrUpcomingReviewNotFound = errors.New("upcoming review not found")
	ErrUpcomingReviewConflict = errors.New("upcoming review conflict")
)

type UpcomingReviewQuery struct {
	Filter string
	Search string
	Limit  int
	Offset int
}

type UpcomingDecisionUpdate struct {
	Decision         string `json:"decision"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type AdminUpcomingMovie struct {
	TMDBID            int64                   `json:"tmdb_id"`
	PublicMovieID     string                  `json:"public_movie_id"`
	Slug              string                  `json:"slug"`
	Title             string                  `json:"title"`
	PosterURL         *string                 `json:"poster_url"`
	FrenchReleaseDate *string                 `json:"french_release_date"`
	Active            bool                    `json:"active"`
	InWindow          bool                    `json:"in_window"`
	PubliclyVisible   bool                    `json:"publicly_visible"`
	AssessmentStatus  string                  `json:"assessment_status"`
	AssessedAt        *time.Time              `json:"assessed_at"`
	FrenchReleases    []tmdb.FrenchReleaseRow `json:"french_releases"`
	ReasonCodes       []string                `json:"reason_codes"`
	Decision          string                  `json:"decision"`
	Revision          int64                   `json:"revision"`
}

type UpcomingReviewList struct {
	Items  []AdminUpcomingMovie `json:"items"`
	Total  int64                `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

type UpcomingReviewStore interface {
	UpcomingReviews(context.Context, UpcomingReviewQuery, time.Time) (UpcomingReviewList, error)
	SetUpcomingDecision(context.Context, int64, UpcomingDecisionUpdate, time.Time) (AdminUpcomingMovie, error)
}

type UpcomingReviewService struct {
	store UpcomingReviewStore
	now   func() time.Time
}

func NewUpcomingReviewService(store UpcomingReviewStore, now func() time.Time) *UpcomingReviewService {
	if now == nil {
		now = time.Now
	}
	return &UpcomingReviewService{store: store, now: now}
}

func ValidUpcomingReviewQuery(q UpcomingReviewQuery) bool {
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > math.MaxInt32 || !utf8.ValidString(q.Search) || strings.ContainsRune(q.Search, 0) || utf8.RuneCountInString(q.Search) > 1024 || strings.TrimSpace(q.Search) != q.Search {
		return false
	}
	switch q.Filter {
	case "needs_review", "pending_assessment", "approved", "excluded", "all":
		return true
	default:
		return false
	}
}

func ValidUpcomingDecision(id int64, input UpcomingDecisionUpdate) bool {
	return id > 0 && id <= MaxReviewInteger && input.ExpectedRevision > 0 && input.ExpectedRevision <= MaxReviewInteger && (input.Decision == "unreviewed" || input.Decision == "approved" || input.Decision == "excluded")
}

func (s *UpcomingReviewService) List(ctx context.Context, q UpcomingReviewQuery) (UpcomingReviewList, error) {
	if !ValidUpcomingReviewQuery(q) {
		return UpcomingReviewList{}, ErrUpcomingReviewInvalid
	}
	return s.store.UpcomingReviews(ctx, q, s.now())
}

func (s *UpcomingReviewService) Update(ctx context.Context, id int64, input UpcomingDecisionUpdate) (AdminUpcomingMovie, error) {
	if !ValidUpcomingDecision(id, input) {
		return AdminUpcomingMovie{}, ErrUpcomingReviewInvalid
	}
	return s.store.SetUpcomingDecision(ctx, id, input, s.now())
}

func completeUpcomingReview(item *AdminUpcomingMovie, now time.Time) {
	item.Slug = "film-" + item.PublicMovieID
	item.AssessmentStatus = "pending"
	if item.AssessedAt != nil {
		at := item.AssessedAt.UTC()
		item.AssessedAt = &at
		item.AssessmentStatus = "assessed"
	}
	if item.FrenchReleases == nil {
		item.FrenchReleases = []tmdb.FrenchReleaseRow{}
	}
	if item.ReasonCodes == nil {
		item.ReasonCodes = []string{}
	}
	window := schedule.UpcomingDisplayWindow(now)
	item.InWindow = item.FrenchReleaseDate != nil && *item.FrenchReleaseDate >= window.From && *item.FrenchReleaseDate <= window.Through
	item.PubliclyVisible = item.Active && item.InWindow && item.Decision != "excluded"
}
