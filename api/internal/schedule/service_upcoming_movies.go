package schedule

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrUpcomingUnavailable = errors.New("upcoming publication unavailable")

type UpcomingMoviesQuery struct {
	View  string
	Year  int
	Month int
	Page  int
}

type UpcomingMoviesResponse struct {
	GeneratedAt     time.Time          `json:"generated_at"`
	CatalogRevision string             `json:"catalog_revision"`
	Timezone        string             `json:"timezone"`
	Window          *Window            `json:"window"`
	View            string             `json:"view"`
	Year            *int               `json:"year"`
	Month           *int               `json:"month"`
	AvailableYears  []int              `json:"available_years"`
	AvailableMonths []int              `json:"available_months"`
	Items           []MovieCatalogItem `json:"items"`
	Page            int                `json:"page"`
	Total           int                `json:"total"`
	TotalWeeks      int                `json:"total_weeks"`
	TotalPages      int                `json:"total_pages"`
}

const upcomingWeeksPerPage = 4

func (s *Service) UpcomingMovies(query UpcomingMoviesQuery) (UpcomingMoviesResponse, error) {
	if query.View == "" {
		query.View = "upcoming"
	}
	if query.View != "upcoming" && query.View != "history" {
		return UpcomingMoviesResponse{}, invalid("Vue invalide.")
	}
	if query.Year < 0 || query.Year > 9999 || query.Month < 0 || query.Month > 12 || query.View == "upcoming" && (query.Year != 0 || query.Month != 0) {
		return UpcomingMoviesResponse{}, invalid("Période invalide.")
	}
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
	result := UpcomingMoviesResponse{GeneratedAt: view.data.UpcomingCompletedAt, CatalogRevision: catalogRevisionAt(view, now, s.location), Timezone: Timezone, View: query.View, AvailableYears: []int{}, AvailableMonths: []int{}, Items: []MovieCatalogItem{}, Page: query.Page}
	filtered := []PublicMovieRecord{}
	if query.View == "history" {
		filtered = retainedReleaseHistory(view, now.In(s.location).Format(time.DateOnly), query, &result)
	} else {
		window := UpcomingDisplayWindow(now)
		result.Window = &window
		for _, movie := range view.data.PublicMovies {
			if movie.RedirectToID != 0 || !movie.UpcomingActive || movie.UpcomingExcluded || movie.FrenchReleaseDate < window.From || movie.FrenchReleaseDate > window.Through {
				continue
			}
			filtered = append(filtered, movie)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		if left.FrenchReleaseDate != right.FrenchReleaseDate {
			if query.View == "history" {
				return left.FrenchReleaseDate > right.FrenchReleaseDate
			}
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

// retainedReleaseHistory reads current stored knowledge, including inactive rows.
// Dates are validated by the snapshot boundary; never substitute general dates.
func retainedReleaseHistory(view *SnapshotView, today string, query UpcomingMoviesQuery, result *UpcomingMoviesResponse) []PublicMovieRecord {
	eligible := []PublicMovieRecord{}
	years := map[int]bool{}
	for _, movie := range view.data.PublicMovies {
		if movie.RedirectToID != 0 || !movie.HasUpcomingRelease || movie.UpcomingExcluded || movie.FrenchReleaseDate == "" || movie.FrenchReleaseDate > today {
			continue
		}
		eligible = append(eligible, movie)
		year, _ := strconv.Atoi(movie.FrenchReleaseDate[:4])
		years[year] = true
	}
	for year := range years {
		result.AvailableYears = append(result.AvailableYears, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(result.AvailableYears)))
	year := query.Year
	if year == 0 && len(result.AvailableYears) > 0 {
		year = result.AvailableYears[0]
	}
	filtered := []PublicMovieRecord{}
	if year == 0 {
		return filtered
	}
	result.Year = &year
	if query.Month != 0 {
		result.Month = &query.Month
	}
	months := map[int]bool{}
	for _, movie := range eligible {
		releaseYear, _ := strconv.Atoi(movie.FrenchReleaseDate[:4])
		if releaseYear != year {
			continue
		}
		month, _ := strconv.Atoi(movie.FrenchReleaseDate[5:7])
		months[month] = true
		if query.Month == 0 || month == query.Month {
			filtered = append(filtered, movie)
		}
	}
	for month := range months {
		result.AvailableMonths = append(result.AvailableMonths, month)
	}
	sort.Ints(result.AvailableMonths)
	return filtered
}

func catalogRevisionAt(view *SnapshotView, now time.Time, location *time.Location) string {
	if view.data.UpcomingCompletedAt.IsZero() {
		return view.catalogRevision
	}
	return strings.Join([]string{view.catalogRevision, "paris:" + now.In(location).Format(time.DateOnly)}, ";")
}
