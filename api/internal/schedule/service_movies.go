package schedule

import (
	"sort"
	"strings"
	"time"
)

const movieCatalogWarningWindow = 20 * time.Minute

type catalogGroupedMovie struct {
	item          MovieCatalogItem
	showtimeCount int
}

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
	result := MovieCatalog{GeneratedAt: view.data.GeneratedAt, CatalogRevision: view.catalogRevision, Items: []MovieCatalogItem{}, Page: page, PageSize: pageSize}
	if query.IncludeEnded && (query.TheaterIDs != nil || query.CurrentlyScreened != nil && *query.CurrentlyScreened) {
		return MovieCatalog{}, invalid("Le paramètre include_ended est incompatible avec currently_screened=true ou theaters.")
	}
	var selectedTheaters []int
	if query.TheaterIDs != nil {
		var err error
		selectedTheaters, err = s.selectTheaters(view, query.TheaterIDs, "", false)
		if err != nil {
			return MovieCatalog{}, err
		}
	}
	if query.CurrentlyScreened != nil && !*query.CurrentlyScreened && !query.IncludeEnded {
		return result, nil
	}
	search := normalized(strings.TrimSpace(query.Search))
	grouped := groupCatalogMovies(view, selectedTheaters, query.TheaterIDs != nil, search, s.now(), query.IncludeEnded)
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
		item := movie.item
		if movie.showtimeCount > 0 {
			item.ShowtimeCount = movie.showtimeCount
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func groupCatalogMovies(view *SnapshotView, selectedTheaters []int, theaterFilterProvided bool, search string, now time.Time, includeEnded bool) []catalogGroupedMovie {
	counts := make(map[string]int)
	variantCounts := make(map[string]int)
	add := func(record ShowtimeRecord) {
		slug := view.publicMovieSlug(record.Movie)
		counts[slug]++
		variantCounts[slug+"\x00"+normalized(record.Movie.Title)]++
	}
	if theaterFilterProvided {
		for _, theaterPosition := range selectedTheaters {
			for _, position := range view.theaterShowtimes[theaterPosition] {
				record := view.data.Showtimes[position]
				if isCurrentMovieShowtime(record, now) {
					add(record)
				}
			}
		}
	} else {
		for _, record := range view.data.Showtimes {
			if isCurrentMovieShowtime(record, now) {
				add(record)
			}
		}
	}
	order := view.movieOrder
	if includeEnded {
		order = view.allMovieOrder
	}
	grouped := make([]catalogGroupedMovie, 0, len(order))
	for _, slug := range order {
		count := counts[slug]
		if !includeEnded && count == 0 {
			continue
		}
		index := view.movieBySlug[slug]
		var item MovieCatalogItem
		if len(view.data.PublicMovies) > 0 {
			item = materializePublicMovie(view.data.PublicMovies[index.publicMovie])
		} else {
			position := index.firstShowtime
			if search != "" {
				matched := false
				for _, variant := range index.variants {
					if strings.Contains(variant.title, search) {
						position = variant.firstShowtime
						count = variantCounts[slug+"\x00"+variant.title]
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			item = materializeCatalogMovie(view, view.data.Showtimes[position].Movie)
		}
		if search != "" && !strings.Contains(normalized(item.Title), search) {
			continue
		}
		grouped = append(grouped, catalogGroupedMovie{item: item, showtimeCount: count})
	}
	return grouped
}

func isCurrentMovieShowtime(record ShowtimeRecord, now time.Time) bool {
	return !now.After(record.StartTime.Add(movieCatalogWarningWindow))
}

func movieCurrentlyScreened(view *SnapshotView, slug string, now time.Time) bool {
	for _, date := range view.movieDates[slug] {
		for _, position := range view.movieDate[movieDateKey{slug: slug, date: date}] {
			if isCurrentMovieShowtime(view.data.Showtimes[position], now) {
				return true
			}
		}
	}
	return false
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
