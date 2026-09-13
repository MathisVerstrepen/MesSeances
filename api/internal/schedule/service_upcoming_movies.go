package schedule

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrUpcomingUnavailable = errors.New("upcoming publication unavailable")

type UpcomingMoviesQuery struct {
	Month    string
	Genres   []string
	Page     int
	PageSize int
}

type UpcomingMoviesResponse struct {
	GeneratedAt     time.Time          `json:"generated_at"`
	CatalogRevision string             `json:"catalog_revision"`
	Timezone        string             `json:"timezone"`
	Window          Window             `json:"window"`
	Items           []MovieCatalogItem `json:"items"`
	AvailableGenres []string           `json:"available_genres"`
	AvailableMonths []string           `json:"available_months"`
	Page            int                `json:"page"`
	PageSize        int                `json:"page_size"`
	Total           int                `json:"total"`
}

func (s *Service) UpcomingMovies(query UpcomingMoviesQuery) (UpcomingMoviesResponse, error) {
	if query.Month != "" {
		if date, err := time.Parse("2006-01", query.Month); err != nil || date.Format("2006-01") != query.Month {
			return UpcomingMoviesResponse{}, invalid("Le paramètre month doit respecter le format YYYY-MM.")
		}
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = defaultMovieCatalogPageSize
	}
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > maxMovieCatalogPageSize {
		return UpcomingMoviesResponse{}, invalid("Pagination invalide.")
	}
	if len(query.Genres) > 32 {
		return UpcomingMoviesResponse{}, invalid("Le paramètre genres est invalide.")
	}
	for _, genre := range query.Genres {
		if len(genre) > 256 {
			return UpcomingMoviesResponse{}, invalid("Le paramètre genres est invalide.")
		}
	}
	genres, err := validateMovieCatalogGenres(query.Genres)
	if err != nil {
		return UpcomingMoviesResponse{}, err
	}
	view, now := s.source.Snapshot(), s.now()
	if view == nil || view.data.UpcomingCompletedAt.IsZero() {
		return UpcomingMoviesResponse{}, ErrUpcomingUnavailable
	}
	result := UpcomingMoviesResponse{GeneratedAt: view.data.UpcomingCompletedAt, CatalogRevision: catalogRevisionAt(view, now, s.location), Timezone: Timezone, Window: UpcomingWindow(now), Items: []MovieCatalogItem{}, AvailableGenres: []string{}, AvailableMonths: []string{}, Page: query.Page, PageSize: query.PageSize}
	eligible := []catalogGroupedMovie{}
	months := map[string]bool{}
	filtered := []PublicMovieRecord{}
	for _, movie := range view.data.PublicMovies {
		if movie.RedirectToID != 0 || !movie.UpcomingActive || movie.FrenchReleaseDate < result.Window.From || movie.FrenchReleaseDate > result.Window.Through {
			continue
		}
		item := materializePublicMovie(movie)
		eligible = append(eligible, catalogGroupedMovie{item: item})
		month := movie.FrenchReleaseDate[:7]
		months[month] = true
		if query.Month != "" && query.Month != month || len(genres) > 0 && !movieMatchesCatalogGenres(item, genres) {
			continue
		}
		filtered = append(filtered, movie)
	}
	result.AvailableGenres = availableMovieCatalogGenres(eligible)
	for month := range months {
		result.AvailableMonths = append(result.AvailableMonths, month)
	}
	sort.Strings(result.AvailableMonths)
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
	if result.Total == 0 || query.Page > (result.Total-1)/query.PageSize+1 {
		return result, nil
	}
	start := (query.Page - 1) * query.PageSize
	for _, movie := range filtered[start:min(start+query.PageSize, result.Total)] {
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
