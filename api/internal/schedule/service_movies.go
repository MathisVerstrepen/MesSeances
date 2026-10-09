package schedule

import (
	"sort"
	"strings"
	"time"

	"messeances/api/internal/moviesearch"
)

const movieCatalogWarningWindow = 20 * time.Minute

type catalogGroupedMovie struct {
	item                   MovieCatalogItem
	showtimeCount          int
	remainingShowtimeCount int
	next7DaysShowtimeCount int
}

type catalogShowtimeCounts struct {
	current   int
	remaining int
	next7Days int
}

type catalogDateWindow struct {
	from    string
	through string
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
	if view == nil || query.ScreeningSummary && view.catalogOnly {
		return MovieCatalog{}, ErrNoCompleteSnapshot
	}
	result := MovieCatalog{GeneratedAt: view.data.GeneratedAt, CatalogRevision: view.catalogRevision, Items: []MovieCatalogItem{}, AvailableGenres: []string{}, Page: page, PageSize: pageSize}
	selectedGenres, err := validateMovieCatalogGenres(query.Genres)
	if err != nil {
		return MovieCatalog{}, err
	}
	if err := validateMovieCatalogDuration(query.Duration); err != nil {
		return MovieCatalog{}, err
	}
	now := s.now()
	result.CatalogRevision = catalogRevisionAt(view, now, s.location)
	if query.ScreeningSummary && (query.Date != nil || query.DateTo != nil) {
		return MovieCatalog{}, invalid("Le paramètre screening_summary est incompatible avec date ou date_to.")
	}
	var screeningEnd *time.Time
	if query.ScreeningSummary {
		window, end := s.movieScreeningWindow(now)
		result.ScreeningWindow = &window
		screeningEnd = &end
	}
	if query.IncludeEnded && (query.Date != nil || query.DateTo != nil) {
		return MovieCatalog{}, invalid("Le paramètre include_ended est incompatible avec date ou date_to.")
	}
	dateWindow, err := s.resolveMovieCatalogDateWindow(query.Date, query.DateTo, now)
	if err != nil {
		return MovieCatalog{}, err
	}
	if query.IncludeEnded && (query.TheaterIDs != nil || query.CurrentlyScreened != nil && *query.CurrentlyScreened) {
		return MovieCatalog{}, invalid("Le paramètre include_ended est incompatible avec currently_screened=true ou theaters.")
	}
	var selectedTheaters []int
	if query.TheaterIDs != nil {
		selectedTheaters, err = s.selectTheaters(view, query.TheaterIDs, "", false)
		if err != nil {
			return MovieCatalog{}, err
		}
	}
	if query.CurrentlyScreened != nil && !*query.CurrentlyScreened && !query.IncludeEnded {
		return result, nil
	}
	search := moviesearch.Compile(query.Search)
	grouped := groupCatalogMovies(view, selectedTheaters, query.TheaterIDs != nil, search, now, query.IncludeEnded, dateWindow, screeningEnd)
	result.AvailableGenres = availableMovieCatalogGenres(grouped)
	grouped = filterCatalogMovies(grouped, selectedGenres, query.Duration)
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
		if query.ScreeningSummary {
			item.RemainingShowtimeCount = &movie.remainingShowtimeCount
			item.Next7DaysShowtimeCount = &movie.next7DaysShowtimeCount
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) movieScreeningWindow(now time.Time) (MovieScreeningWindow, time.Time) {
	local := now.In(s.location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.location)
	daysUntilTuesday := (int(time.Tuesday) - int(today.Weekday()) + 7) % 7
	through := today.AddDate(0, 0, daysUntilTuesday)
	return MovieScreeningWindow{AsOf: now, Timezone: Timezone, From: today.Format(dateLayout), Through: through.Format(dateLayout), DayCount: daysUntilTuesday + 1}, through.AddDate(0, 0, 1)
}

func groupCatalogMovies(view *SnapshotView, selectedTheaters []int, theaterFilterProvided bool, search moviesearch.Query, now time.Time, includeEnded bool, dateWindow *catalogDateWindow, screeningEnd *time.Time) []catalogGroupedMovie {
	counts := make(map[string]catalogShowtimeCounts)
	variantCounts := make(map[string]catalogShowtimeCounts)
	next7DaysEnd := now.Add(7 * 24 * time.Hour)
	add := func(record ShowtimeRecord) {
		if dateWindow != nil && (record.ServiceDate < dateWindow.from || record.ServiceDate > dateWindow.through) {
			return
		}
		slug := view.publicMovieSlug(record.Movie)
		count := counts[slug]
		variantKey := slug + "\x00" + normalized(record.Movie.Title)
		variantCount := variantCounts[variantKey]
		count.current++
		variantCount.current++
		if screeningEnd != nil && !record.StartTime.Before(now) {
			if record.StartTime.Before(*screeningEnd) {
				count.remaining++
				variantCount.remaining++
			}
			if record.StartTime.Before(next7DaysEnd) {
				count.next7Days++
				variantCount.next7Days++
			}
		}
		counts[slug] = count
		variantCounts[variantKey] = variantCount
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
		if !includeEnded && count.current == 0 {
			continue
		}
		index := view.movieBySlug[slug]
		var item MovieCatalogItem
		if len(view.data.PublicMovies) > 0 {
			item = materializePublicMovie(view.data.PublicMovies[index.publicMovie])
			original := ""
			if item.OriginalTitle != nil {
				original = *item.OriginalTitle
			}
			if !search.Matches(item.Title, original) {
				continue
			}
		} else {
			position := index.firstShowtime
			if !search.Blank() {
				matched := false
				for _, variant := range index.variants {
					candidate := materializeCatalogMovie(view, view.data.Showtimes[variant.firstShowtime].Movie)
					original := ""
					if candidate.OriginalTitle != nil {
						original = *candidate.OriginalTitle
					}
					if search.Matches(candidate.Title, original) {
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
		grouped = append(grouped, catalogGroupedMovie{item: item, showtimeCount: count.current, remainingShowtimeCount: count.remaining, next7DaysShowtimeCount: count.next7Days})
	}
	return grouped
}

func validateMovieCatalogGenres(genres []string) ([]string, error) {
	if genres == nil {
		return nil, nil
	}
	result := make([]string, 0, len(genres))
	for _, genre := range genres {
		genre = strings.TrimSpace(genre)
		if genre == "" {
			return nil, invalid("Le paramètre genres contient une valeur vide.")
		}
		duplicate := false
		for _, selected := range result {
			if strings.EqualFold(selected, genre) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, genre)
		}
	}
	return result, nil
}

func validateMovieCatalogDuration(duration *MovieCatalogDuration) error {
	if duration == nil {
		return nil
	}
	switch *duration {
	case MovieCatalogDurationShort, MovieCatalogDurationMedium, MovieCatalogDurationLong:
		return nil
	default:
		return invalid("Le paramètre duration doit être short, medium ou long.")
	}
}

func (s *Service) resolveMovieCatalogDateWindow(date, dateTo *string, now time.Time) (*catalogDateWindow, error) {
	if date == nil {
		if dateTo != nil {
			return nil, invalid("Le paramètre date_to nécessite une date personnalisée au format YYYY-MM-DD.")
		}
		return nil, nil
	}
	if dateTo != nil {
		customStart, err := time.ParseInLocation(dateLayout, *date, s.location)
		if err != nil || customStart.Format(dateLayout) != *date {
			return nil, invalid("Le paramètre date_to nécessite une date personnalisée au format YYYY-MM-DD.")
		}
	}
	today := now.In(s.location)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, s.location)
	var from, through time.Time
	switch *date {
	case "today":
		from, through = today, today
	case "tomorrow":
		from = today.AddDate(0, 0, 1)
		through = from
	case "weekend":
		switch today.Weekday() {
		case time.Saturday:
			from, through = today, today.AddDate(0, 0, 1)
		case time.Sunday:
			from, through = today, today
		default:
			daysUntilSaturday := (int(time.Saturday) - int(today.Weekday()) + 7) % 7
			from = today.AddDate(0, 0, daysUntilSaturday)
			through = from.AddDate(0, 0, 1)
		}
	default:
		parsed, err := time.ParseInLocation(dateLayout, *date, s.location)
		if err != nil || parsed.Format(dateLayout) != *date {
			return nil, invalid("Le paramètre date doit être today, tomorrow, weekend ou respecter le format YYYY-MM-DD.")
		}
		from, through = parsed, parsed
	}
	if dateTo != nil {
		parsed, err := time.ParseInLocation(dateLayout, *dateTo, s.location)
		if err != nil || parsed.Format(dateLayout) != *dateTo {
			return nil, invalid("Le paramètre date_to doit respecter le format YYYY-MM-DD.")
		}
		through = parsed
	}
	if from.Before(today) || through.Before(today) {
		return nil, invalid("Les dates de séance ne peuvent pas être antérieures à aujourd’hui.")
	}
	if through.Before(from) {
		return nil, invalid("Le paramètre date_to doit être supérieur ou égal au paramètre date.")
	}
	return &catalogDateWindow{from: from.Format(dateLayout), through: through.Format(dateLayout)}, nil
}

func availableMovieCatalogGenres(movies []catalogGroupedMovie) []string {
	genres := make([]string, 0)
	for _, movie := range movies {
		for _, genre := range movie.item.Genres {
			duplicate := false
			for _, available := range genres {
				if strings.EqualFold(available, genre) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				genres = append(genres, genre)
			}
		}
	}
	sort.Slice(genres, func(i, j int) bool {
		if comparison := compareNormalized(genres[i], genres[j]); comparison != 0 {
			return comparison < 0
		}
		return genres[i] < genres[j]
	})
	return genres
}

func filterCatalogMovies(movies []catalogGroupedMovie, genres []string, duration *MovieCatalogDuration) []catalogGroupedMovie {
	if len(genres) == 0 && duration == nil {
		return movies
	}
	filtered := make([]catalogGroupedMovie, 0, len(movies))
	for _, movie := range movies {
		if len(genres) > 0 && !movieMatchesCatalogGenres(movie.item, genres) {
			continue
		}
		if duration != nil && !movieMatchesCatalogDuration(movie.item.RuntimeMinutes, *duration) {
			continue
		}
		filtered = append(filtered, movie)
	}
	return filtered
}

func movieMatchesCatalogGenres(movie MovieCatalogItem, selected []string) bool {
	for _, genre := range movie.Genres {
		for _, requested := range selected {
			if strings.EqualFold(genre, requested) {
				return true
			}
		}
	}
	return false
}

func movieMatchesCatalogDuration(runtime int, duration MovieCatalogDuration) bool {
	switch duration {
	case MovieCatalogDurationShort:
		return runtime > 0 && runtime < 90
	case MovieCatalogDurationMedium:
		return runtime >= 90 && runtime <= 120
	case MovieCatalogDurationLong:
		return runtime > 120
	default:
		return false
	}
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
