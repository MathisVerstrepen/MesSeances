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

type catalogMovieVariantKey struct {
	slug  string
	title string
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
	result := MovieCatalog{GeneratedAt: view.data.GeneratedAt, Items: []MovieCatalogItem{}, Page: page, PageSize: pageSize}
	var selectedTheaters []int
	if query.TheaterIDs != nil {
		var err error
		selectedTheaters, err = s.selectTheaters(view, query.TheaterIDs, "", false)
		if err != nil {
			return MovieCatalog{}, err
		}
	}
	if query.CurrentlyScreened != nil && !*query.CurrentlyScreened {
		return result, nil
	}
	search := normalized(strings.TrimSpace(query.Search))
	grouped := groupCatalogMovies(view, selectedTheaters, query.TheaterIDs != nil, search, s.now())
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

func groupCatalogMovies(view *SnapshotView, selectedTheaters []int, theaterFilterProvided bool, search string, now time.Time) []catalogGroupedMovie {
	if !theaterFilterProvided {
		remainingCounts := make(map[catalogMovieVariantKey]int)
		for _, record := range view.data.Showtimes {
			if now.After(record.StartTime.Add(movieCatalogWarningWindow)) {
				continue
			}
			key := catalogMovieVariantKey{slug: publicMovieSlug(record.Movie), title: normalized(record.Movie.Title)}
			remainingCounts[key]++
		}
		grouped := make([]catalogGroupedMovie, 0, len(view.movieOrder))
		for _, slug := range view.movieOrder {
			movie := view.movieBySlug[slug]
			current := catalogGroupedMovie{}
			itemSet := false
			for _, variant := range movie.variants {
				if search != "" && !strings.Contains(variant.title, search) {
					continue
				}
				if !itemSet {
					current.item = materializeCatalogMovie(view.data.Showtimes[variant.firstShowtime].Movie)
					itemSet = true
				}
				current.showtimeCount += remainingCounts[catalogMovieVariantKey{slug: slug, title: variant.title}]
			}
			if current.showtimeCount > 0 {
				grouped = append(grouped, current)
			}
		}
		return grouped
	}

	bySlug := make(map[string]*catalogGroupedMovie)
	order := make([]string, 0)
	for _, theaterPosition := range selectedTheaters {
		for _, showtimePosition := range view.theaterShowtimes[theaterPosition] {
			record := view.data.Showtimes[showtimePosition]
			if now.After(record.StartTime.Add(movieCatalogWarningWindow)) {
				continue
			}
			if search != "" && !strings.Contains(normalized(record.Movie.Title), search) {
				continue
			}
			slug := publicMovieSlug(record.Movie)
			current, exists := bySlug[slug]
			if !exists {
				current = &catalogGroupedMovie{item: materializeCatalogMovie(record.Movie)}
				bySlug[slug] = current
				order = append(order, slug)
			}
			current.showtimeCount++
		}
	}
	grouped := make([]catalogGroupedMovie, 0, len(order))
	for _, slug := range order {
		grouped = append(grouped, *bySlug[slug])
	}
	return grouped
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
