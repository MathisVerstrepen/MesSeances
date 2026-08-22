package schedule

import (
	"sort"
	"strings"
)

func (s *Service) Movies(query MovieCatalogQuery) (MovieCatalog, error) {
	page := query.Page
	if page == 0 {
		page = 1
	}
	if page < 1 {
		return MovieCatalog{}, invalid("Le paramètre page doit être un entier supérieur ou égal à 1.")
	}
	pageSize := query.PageSize
	if pageSize == 0 {
		pageSize = defaultMovieCatalogPageSize
	}
	if pageSize < 1 || pageSize > maxMovieCatalogPageSize {
		return MovieCatalog{}, invalid("Le paramètre page_size doit être un entier compris entre 1 et 100.")
	}
	view := s.source.Snapshot()
	result := MovieCatalog{GeneratedAt: view.data.GeneratedAt, Items: []MovieCatalogItem{}, Page: page, PageSize: pageSize}
	if query.CurrentlyScreened != nil && !*query.CurrentlyScreened {
		return result, nil
	}
	search := normalized(strings.TrimSpace(query.Search))
	type groupedMovie struct {
		item          MovieCatalogItem
		showtimeCount int
	}
	grouped := make([]groupedMovie, 0, len(view.movieOrder))
	for _, slug := range view.movieOrder {
		movie := view.movieBySlug[slug]
		groupedMovie := groupedMovie{}
		for _, variant := range movie.variants {
			if search != "" && !strings.Contains(variant.title, search) {
				continue
			}
			if groupedMovie.showtimeCount == 0 {
				groupedMovie.item = materializeCatalogMovie(view.data.Showtimes[variant.firstShowtime].Movie)
			}
			groupedMovie.showtimeCount += variant.count
		}
		if groupedMovie.showtimeCount > 0 {
			grouped = append(grouped, groupedMovie)
		}
	}
	sortMode := normalizeMovieCatalogSort(query.Sort)
	sort.Slice(grouped, func(i, j int) bool {
		left, right := grouped[i], grouped[j]
		switch sortMode {
		case MovieCatalogSortTitleAsc:
			return compareMovieCatalogTitle(left.item, right.item, false)
		case MovieCatalogSortTitleDesc:
			return compareMovieCatalogTitle(left.item, right.item, true)
		case MovieCatalogSortReleaseDateDesc:
			if (left.item.ReleaseDate != nil) != (right.item.ReleaseDate != nil) {
				return left.item.ReleaseDate != nil
			}
			if left.item.ReleaseDate != nil && *left.item.ReleaseDate != *right.item.ReleaseDate {
				return *left.item.ReleaseDate > *right.item.ReleaseDate
			}
		case MovieCatalogSortRuntimeAsc:
			if left.item.RuntimeMinutes != right.item.RuntimeMinutes {
				return left.item.RuntimeMinutes < right.item.RuntimeMinutes
			}
		case MovieCatalogSortRuntimeDesc:
			if left.item.RuntimeMinutes != right.item.RuntimeMinutes {
				return left.item.RuntimeMinutes > right.item.RuntimeMinutes
			}
		case MovieCatalogSortShowtimesDesc:
			if left.showtimeCount != right.showtimeCount {
				return left.showtimeCount > right.showtimeCount
			}
		}
		return compareMovieCatalogTitle(left.item, right.item, false)
	})
	result.Total = len(grouped)
	if result.Total == 0 || page > (result.Total-1)/pageSize+1 {
		return result, nil
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, result.Total)
	for _, movie := range grouped[start:end] {
		result.Items = append(result.Items, movie.item)
	}
	return result, nil
}

func normalizeMovieCatalogSort(value MovieCatalogSort) MovieCatalogSort {
	switch value {
	case MovieCatalogSortTitleAsc, MovieCatalogSortTitleDesc, MovieCatalogSortReleaseDateDesc, MovieCatalogSortRuntimeAsc, MovieCatalogSortRuntimeDesc, MovieCatalogSortShowtimesDesc:
		return value
	default:
		return MovieCatalogSortShowtimesDesc
	}
}

func compareMovieCatalogTitle(left, right MovieCatalogItem, descending bool) bool {
	comparison := compareNormalized(left.Title, right.Title)
	if comparison != 0 {
		if descending {
			return comparison > 0
		}
		return comparison < 0
	}
	return left.Slug < right.Slug
}
