package schedule

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrUpcomingUnavailable = errors.New("upcoming publication unavailable")

type UpcomingMoviesQuery struct {
	Page int
}

type UpcomingMoviesResponse struct {
	GeneratedAt     time.Time          `json:"generated_at"`
	CatalogRevision string             `json:"catalog_revision"`
	Timezone        string             `json:"timezone"`
	Window          Window             `json:"window"`
	Items           []MovieCatalogItem `json:"items"`
	Page            int                `json:"page"`
	Total           int                `json:"total"`
	TotalWeeks      int                `json:"total_weeks"`
	TotalPages      int                `json:"total_pages"`
}

const upcomingWeeksPerPage = 4

func (s *Service) UpcomingMovies(query UpcomingMoviesQuery) (UpcomingMoviesResponse, error) {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.Page < 1 {
		return UpcomingMoviesResponse{}, invalid("Pagination invalide.")
	}
	view, now := s.source.Snapshot(), s.now()
	if view == nil || view.data.UpcomingCompletedAt.IsZero() {
		return UpcomingMoviesResponse{}, ErrUpcomingUnavailable
	}
	result := UpcomingMoviesResponse{GeneratedAt: view.data.UpcomingCompletedAt, CatalogRevision: catalogRevisionAt(view, now, s.location), Timezone: Timezone, Window: UpcomingDisplayWindow(now), Items: []MovieCatalogItem{}, Page: query.Page}
	filtered := []PublicMovieRecord{}
	for _, movie := range view.data.PublicMovies {
		if movie.RedirectToID != 0 || !movie.UpcomingActive || movie.UpcomingExcluded || movie.FrenchReleaseDate < result.Window.From || movie.FrenchReleaseDate > result.Window.Through {
			continue
		}
		filtered = append(filtered, movie)
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		if left.FrenchReleaseDate != right.FrenchReleaseDate {
			return left.FrenchReleaseDate < right.FrenchReleaseDate
		}
		if comparison := compareNormalized(left.Title, right.Title); comparison != 0 {
			return comparison < 0
		}
		return left.ID < right.ID
	})
	result.Total = len(filtered)
	// Date ordering makes each nonempty Wednesday-Tuesday week contiguous.
	// Record offsets only after eligibility filtering, so gaps consume no slots.
	weekStarts := []int{}
	previousWeek := ""
	for i, movie := range filtered {
		week := upcomingReleaseWeek(movie.FrenchReleaseDate)
		if week != previousWeek {
			weekStarts = append(weekStarts, i)
			previousWeek = week
		}
	}
	result.TotalWeeks = len(weekStarts)
	if result.TotalWeeks > 0 {
		result.TotalPages = (result.TotalWeeks-1)/upcomingWeeksPerPage + 1
	}
	// Check before multiplying an arbitrary positive page to avoid overflow.
	if query.Page > result.TotalPages {
		return result, nil
	}
	firstWeek := (query.Page - 1) * upcomingWeeksPerPage
	end := result.Total
	if result.TotalWeeks-firstWeek > upcomingWeeksPerPage {
		end = weekStarts[firstWeek+upcomingWeeksPerPage]
	}
	for _, movie := range filtered[weekStarts[firstWeek]:end] {
		result.Items = append(result.Items, materializePublicMovie(movie))
	}
	return result, nil
}

func catalogRevisionAt(view *SnapshotView, now time.Time, location *time.Location) string {
	if view.data.UpcomingCompletedAt.IsZero() {
		return view.catalogRevision
	}
	return strings.Join([]string{view.catalogRevision, "paris:" + now.In(location).Format(time.DateOnly)}, ";")
}
