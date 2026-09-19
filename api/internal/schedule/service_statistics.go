package schedule

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const statisticsUnknown = "unknown"

type statisticsMovie struct {
	item      MovieCatalogItem
	genres    map[string]string
	showtimes int
	theaters  map[string]bool
}

type statisticsLocalCount struct {
	showtimes int
	movies    map[string]bool
	theaters  map[string]bool
}

type statisticsFilters struct {
	query            StatisticsQuery
	cities, theaters map[string]bool
}

// Statistics aggregates one retained immutable view, including already-started
// sessions on the selected service dates. Options always describe the full view.
func (s *Service) Statistics(ctx context.Context, query StatisticsQuery) (Statistics, error) {
	view, now := s.source.Snapshot(), s.now()
	if view == nil || view.catalogOnly {
		return Statistics{}, ErrNoCompleteSnapshot
	}
	query, window, err := s.statisticsQuery(query, now)
	if err != nil {
		return Statistics{}, err
	}
	if err := ctx.Err(); err != nil {
		return Statistics{}, err
	}
	filters := statisticsFilters{query: query, cities: statisticsSelectionSet(query.City), theaters: statisticsSelectionSet(query.Theater)}
	// Match the same alias-first identity used for retained showtimes below.
	film := query.Film
	if canonical, ok := view.movieAlias[film]; ok {
		film = canonical
	}
	result := newStatistics(view, now, window)
	options, theaterOptions, err := statisticsInventory(ctx, view)
	if err != nil {
		return Statistics{}, err
	}
	result.Options = options
	movies := make(map[string]*statisticsMovie)
	cities, theaters := make(map[string]*statisticsLocalCount), make(map[string]*statisticsLocalCount)
	languages, formats := make(map[string]bool), make(map[string]bool)
	genreLabels := make(map[string]string)
	versions, formatCounts, genres := make(map[string]int), make(map[string]int), make(map[string]int)
	seen := make(map[struct {
		provider Provider
		id       string
	}]bool)
	for index, showing := range view.data.Showtimes {
		if index%256 == 0 {
			if err := ctx.Err(); err != nil {
				return Statistics{}, err
			}
		}
		identity := struct {
			provider Provider
			id       string
		}{recordProvider(showing.Provider, showing.ID), showing.ID}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		slug := view.publicMovieSlug(showing.Movie)
		// Prefer aliases even when an old source ID still has a showtime index.
		if canonical, ok := view.movieAlias[slug]; ok {
			slug = canonical
		}
		movie, ok := movies[slug]
		if !ok {
			movie = statisticsResolveMovie(view, slug, showing.Movie)
			movies[slug] = movie
			for value, label := range movie.genres {
				statisticsGenreLabel(genreLabels, value, label)
			}
		}
		language, format := statisticsLanguage(showing.Language), statisticsFormat(showing.Format)
		languages[language], formats[format] = true, true
		if query.Film != "" && slug != film {
			continue
		}
		theater, ok := theaterOptions[showing.TheaterID]
		if !ok || !statisticsMatches(filters, window, showing, theater, movie, language, format) {
			continue
		}
		result.Totals.Showtimes++
		versions[language]++
		formatCounts[format]++
		if movie.showtimes == 0 {
			result.Totals.Movies++
			for value := range movie.genres {
				genres[value]++
			}
			result.Runtimes[statisticsRuntime(movie.item.RuntimeMinutes)].Count++
		}
		movie.showtimes++
		movie.theaters[theater.ID] = true
		statisticsAddLocal(cities, theater.CitySlug, slug, theater.ID)
		statisticsAddLocal(theaters, theater.ID, slug, theater.ID)
		date, err := time.ParseInLocation(dateLayout, showing.ServiceDate, s.location)
		if err != nil {
			return Statistics{}, err
		}
		weekday := (int(date.Weekday()) + 6) % 7
		hour := showing.StartTime.In(s.location).Hour()
		result.Heatmap[weekday*24+hour].ShowtimeCount++
	}
	result.Options.Languages = statisticsSortedKeys(languages)
	result.Options.Formats = statisticsSortedKeys(formats)
	for value, label := range genreLabels {
		result.Options.Genres = append(result.Options.Genres, StatisticsGenreOption{Value: value, Label: label})
	}
	sort.Slice(result.Options.Genres, func(i, j int) bool {
		a, b := result.Options.Genres[i], result.Options.Genres[j]
		if c := compareNormalized(a.Label, b.Label); c != 0 {
			return c < 0
		}
		return a.Value < b.Value
	})
	result.Versions = statisticsBuckets(versions, nil)
	result.Formats = statisticsBuckets(formatCounts, nil)
	result.Genres = statisticsBuckets(genres, genreLabels)
	statisticsFinishRanks(&result, movies, cities, theaters, theaterOptions)
	if err := ctx.Err(); err != nil {
		return Statistics{}, err
	}
	return result, nil
}

func (s *Service) statisticsQuery(query StatisticsQuery, now time.Time) (StatisticsQuery, Window, error) {
	film, err := statisticsNormalizeFilm(query.Film)
	if err != nil {
		return query, Window{}, err
	}
	query.Film = film
	for _, selection := range []*[]string{&query.City, &query.Theater} {
		values, err := statisticsNormalizeSelection(*selection)
		if err != nil {
			return query, Window{}, err
		}
		*selection = values
	}
	for _, value := range []*string{&query.Chain, &query.Language, &query.Format, &query.Genre, &query.Pass} {
		if len(*value) > 200 {
			return query, Window{}, invalid("Les filtres statistiques sont invalides.")
		}
		*value = strings.TrimSpace(*value)
	}
	query.Genre, _ = statisticsGenre(query.Genre)
	if query.Chain != "" && !validProvider(Provider(query.Chain), false) ||
		query.Language != "" && query.Language != statisticsUnknown && statisticsLanguage(Language(query.Language)) == statisticsUnknown ||
		query.Format != "" && query.Format != statisticsUnknown && statisticsFormat(Format(query.Format)) == statisticsUnknown {
		return query, Window{}, invalid("Les filtres statistiques sont invalides.")
	}
	local := now.In(s.location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.location)
	if query.Date == "" {
		if query.DateTo != "" {
			return query, Window{}, invalid("La date de début est requise.")
		}
		return query, Window{From: today.Format(dateLayout), Through: today.AddDate(0, 0, 6).Format(dateLayout)}, nil
	}
	from, err := time.ParseInLocation(dateLayout, query.Date, s.location)
	if err != nil || from.Format(dateLayout) != query.Date || from.Before(today) {
		return query, Window{}, invalid("La date de début doit être une date valide à partir d’aujourd’hui.")
	}
	through := from
	if query.DateTo != "" {
		through, err = time.ParseInLocation(dateLayout, query.DateTo, s.location)
		if err != nil || through.Format(dateLayout) != query.DateTo {
			return query, Window{}, invalid("La date de fin est invalide.")
		}
	}
	if through.Before(from) || through.After(from.AddDate(0, 0, 30)) {
		return query, Window{}, invalid("La période doit contenir de 1 à 31 jours.")
	}
	return query, Window{From: from.Format(dateLayout), Through: through.Format(dateLayout)}, nil
}

func statisticsNormalizeFilm(value string) (string, error) {
	if len(value) > 200 || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') || value != "" && strings.TrimSpace(value) == "" {
		return "", invalid("Les filtres statistiques sont invalides.")
	}
	return strings.TrimSpace(value), nil
}

func statisticsNormalizeSelection(values []string) ([]string, error) {
	if len(values) > 50 {
		return nil, invalid("Les filtres statistiques sont invalides.")
	}
	selection := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if len(value) > 200 {
			return nil, invalid("Les filtres statistiques sont invalides.")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, invalid("Les filtres statistiques sont invalides.")
		}
		if !seen[value] {
			selection = append(selection, value)
			seen[value] = true
		}
	}
	return selection, nil
}

func statisticsSelectionSet(values []string) map[string]bool {
	selection := make(map[string]bool, len(values))
	for _, value := range values {
		selection[value] = true
	}
	return selection
}

func newStatistics(view *SnapshotView, now time.Time, window Window) Statistics {
	result := Statistics{
		GeneratedAt: view.data.GeneratedAt, Timezone: Timezone, Range: window,
		Coverage:  StatisticsCoverage{SnapshotWindow: view.data.Window, Completeness: statisticsUnknown, Stale: !view.ReadyAt(now)},
		TopMovies: StatisticsTopMovies{ByShowtimes: []StatisticsMovieRank{}, ByTheaters: []StatisticsMovieRank{}},
		Heatmap:   make([]StatisticsHeatmapCell, 168),
		Runtimes:  []StatisticsCountBucket{{Value: "short", Label: "Moins de 1h30"}, {Value: "medium", Label: "De 1h30 à 2h"}, {Value: "long", Label: "Plus de 2h"}, {Value: statisticsUnknown, Label: "Non renseignée"}},
		Local:     StatisticsLocal{Cities: []StatisticsCityRank{}, Theaters: []StatisticsTheaterRank{}},
	}
	intersection := Window{From: max(window.From, view.data.Window.From), Through: min(window.Through, view.data.Window.Through)}
	if intersection.From <= intersection.Through {
		result.Coverage.Intersection = &intersection
	}
	for i := range result.Heatmap {
		result.Heatmap[i] = StatisticsHeatmapCell{Weekday: i/24 + 1, Hour: i % 24}
	}
	return result
}

func statisticsInventory(ctx context.Context, view *SnapshotView) (StatisticsOptions, map[string]StatisticsTheaterOption, error) {
	options := StatisticsOptions{Cities: []City{}, Theaters: []StatisticsTheaterOption{}, Chains: []Provider{}, Genres: []StatisticsGenreOption{}}
	theaters := make(map[string]StatisticsTheaterOption)
	chains, passes := make(map[Provider]bool), make(map[string]bool)
	for _, city := range view.cityBuckets {
		options.Cities = append(options.Cities, City{Name: city.city, Slug: city.slug})
	}
	for i, theater := range view.data.Theaters {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return options, nil, err
			}
		}
		city := view.cityBuckets[view.theaterCity[i]]
		theaterPasses := make(map[string]bool)
		for _, pass := range theater.AcceptedPasses {
			theaterPasses[pass], passes[pass] = true, true
		}
		item := StatisticsTheaterOption{ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: city.city, CitySlug: city.slug, Chain: recordProvider(theater.Provider, theater.ID), Passes: statisticsSortedKeys(theaterPasses)}
		theaters[item.ID] = item
		options.Theaters = append(options.Theaters, item)
		chains[item.Chain] = true
	}
	for chain := range chains {
		options.Chains = append(options.Chains, chain)
	}
	slices.Sort(options.Chains)
	options.Passes = statisticsSortedKeys(passes)
	sort.Slice(options.Cities, func(i, j int) bool {
		a, b := options.Cities[i], options.Cities[j]
		if c := compareNormalized(a.Name, b.Name); c != 0 {
			return c < 0
		}
		return a.Slug < b.Slug
	})
	sort.Slice(options.Theaters, func(i, j int) bool {
		a, b := options.Theaters[i], options.Theaters[j]
		if c := compareNormalized(a.Name, b.Name); c != 0 {
			return c < 0
		}
		return a.ID < b.ID
	})
	return options, theaters, nil
}

func statisticsResolveMovie(view *SnapshotView, slug string, record MovieRecord) *statisticsMovie {
	var item MovieCatalogItem
	if index, ok := view.movieBySlug[slug]; ok {
		if index.publicMovie >= 0 && index.publicMovie < len(view.data.PublicMovies) && publicMovieIDSlug(view.data.PublicMovies[index.publicMovie].ID) == slug {
			item = materializePublicMovie(view.data.PublicMovies[index.publicMovie])
		} else if index.firstShowtime >= 0 {
			item = materializeCatalogMovie(view, view.data.Showtimes[index.firstShowtime].Movie)
		}
	} else {
		item = materializeCatalogMovie(view, record)
	}
	item.Slug = slug
	movie := &statisticsMovie{item: item, genres: make(map[string]string), theaters: make(map[string]bool)}
	for _, genre := range item.Genres {
		for _, parent := range statisticsGenreParents(genre) {
			value, label := statisticsGenre(parent)
			if label != "" {
				statisticsGenreLabel(movie.genres, value, label)
			}
		}
	}
	if len(movie.genres) == 0 {
		movie.genres[statisticsUnknown] = "Non renseigné"
	}
	return movie
}

// statisticsGenreParents replaces compound statistics genres with their parents.
// Keep the SQL expansion in schedulepg.historyCanonicalCTE in sync.
func statisticsGenreParents(genre string) []string {
	switch normalized(genre) {
	case "comédie dramatique":
		return []string{"Comédie", "Drame"}
	case "comédie romantique":
		return []string{"Comédie", "Romance"}
	case "comédie d'action":
		return []string{"Comédie", "Action"}
	default:
		return []string{genre}
	}
}

// statisticsGenre canonicalizes statistics aliases and parents, never stored metadata.
// Keep the SQL mapping in schedulepg.historyCanonicalCTE in sync.
func statisticsGenre(genre string) (value, label string) {
	label = strings.TrimSpace(genre)
	switch normalized(label) {
	case "familial", "famille", "famille/enfants":
		label = "Famille"
	case "opéra", "opera":
		label = "Opéra"
	case "science-fiction", "science fiction":
		label = "Science-fiction"
	case "histoire", "historique":
		label = "Histoire"
	case "horreur", "horreur / épouvante":
		label = "Horreur"
	case "romance", "amour":
		label = "Romance"
	case "animation", "dessin animé":
		label = "Animation"
	case "comédie":
		label = "Comédie"
	case "drame":
		label = "Drame"
	case "action":
		label = "Action"
	}
	return normalized(label), label
}

func statisticsGenreLabel(labels map[string]string, value, label string) {
	if current, ok := labels[value]; !ok || label < current {
		labels[value] = label
	}
}

func statisticsMatches(filters statisticsFilters, window Window, showing ShowtimeRecord, theater StatisticsTheaterOption, movie *statisticsMovie, language, format string) bool {
	query := filters.query
	_, genre := movie.genres[query.Genre]
	return showing.ServiceDate >= window.From && showing.ServiceDate <= window.Through &&
		(len(filters.cities) == 0 || filters.cities[theater.CitySlug]) && (len(filters.theaters) == 0 || filters.theaters[theater.ID]) &&
		(query.Chain == "" || query.Chain == string(theater.Chain)) && (query.Language == "" || query.Language == language) &&
		(query.Format == "" || query.Format == format) && (query.Genre == "" || genre) &&
		(query.Pass == "" || slices.Contains(theater.Passes, query.Pass))
}

func statisticsLanguage(language Language) string {
	switch language {
	case LanguageVF, LanguageVOSTFR, LanguageVO, LanguageVFSME, LanguageVFSTF:
		return string(language)
	}
	return statisticsUnknown
}

func statisticsFormat(format Format) string {
	switch format {
	case Format2D, Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX, FormatICE, FormatInfinityVision:
		return string(format)
	}
	return statisticsUnknown
}

func statisticsRuntime(minutes int) int {
	if _, valid := RuntimeDuration(minutes); !valid {
		return 3
	}
	if minutes < 90 {
		return 0
	}
	if minutes <= 120 {
		return 1
	}
	return 2
}

func statisticsSortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func statisticsBuckets(counts map[string]int, labels map[string]string) []StatisticsCountBucket {
	buckets := make([]StatisticsCountBucket, 0, len(counts))
	for value, count := range counts {
		label := value
		if text, ok := labels[value]; ok {
			label = text
		}
		if value == statisticsUnknown {
			label = "Non renseigné"
		}
		buckets = append(buckets, StatisticsCountBucket{Value: value, Label: label, Count: count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		a, b := buckets[i], buckets[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if c := compareNormalized(a.Label, b.Label); c != 0 {
			return c < 0
		}
		return a.Value < b.Value
	})
	return buckets
}

func statisticsAddLocal(counts map[string]*statisticsLocalCount, key, movie, theater string) {
	count, ok := counts[key]
	if !ok {
		count = &statisticsLocalCount{movies: make(map[string]bool), theaters: make(map[string]bool)}
		counts[key] = count
	}
	count.showtimes++
	count.movies[movie], count.theaters[theater] = true, true
}

func statisticsFinishRanks(result *Statistics, movies map[string]*statisticsMovie, cities, theaters map[string]*statisticsLocalCount, inventory map[string]StatisticsTheaterOption) {
	for slug, movie := range movies {
		if movie.showtimes > 0 {
			result.TopMovies.ByShowtimes = append(result.TopMovies.ByShowtimes, StatisticsMovieRank{Slug: slug, Title: movie.item.Title, ShowtimeCount: movie.showtimes, TheaterCount: len(movie.theaters)})
		}
	}
	result.TopMovies.ByTheaters = append(result.TopMovies.ByTheaters, result.TopMovies.ByShowtimes...)
	statisticsSortMovies(result.TopMovies.ByShowtimes, false)
	statisticsSortMovies(result.TopMovies.ByTheaters, true)
	result.TopMovies.ByShowtimes = result.TopMovies.ByShowtimes[:min(10, len(result.TopMovies.ByShowtimes))]
	result.TopMovies.ByTheaters = result.TopMovies.ByTheaters[:min(10, len(result.TopMovies.ByTheaters))]
	result.Concentration.TopMovieCount = len(result.TopMovies.ByShowtimes)
	for _, movie := range result.TopMovies.ByShowtimes {
		result.Concentration.TopShowtimeCount += movie.ShowtimeCount
	}
	result.Concentration.OtherShowtimeCount = result.Totals.Showtimes - result.Concentration.TopShowtimeCount
	for _, city := range result.Options.Cities {
		if count, ok := cities[city.Slug]; ok {
			result.Local.Cities = append(result.Local.Cities, StatisticsCityRank{Slug: city.Slug, Name: city.Name, ShowtimeCount: count.showtimes, MovieCount: len(count.movies), TheaterCount: len(count.theaters)})
		}
	}
	for id, count := range theaters {
		theater := inventory[id]
		result.Local.Theaters = append(result.Local.Theaters, StatisticsTheaterRank{ID: id, Slug: theater.Slug, Name: theater.Name, City: theater.City, CitySlug: theater.CitySlug, Chain: theater.Chain, ShowtimeCount: count.showtimes, MovieCount: len(count.movies)})
	}
	sort.Slice(result.Local.Cities, func(i, j int) bool {
		a, b := result.Local.Cities[i], result.Local.Cities[j]
		return statisticsLocalLess(a.ShowtimeCount, b.ShowtimeCount, a.MovieCount, b.MovieCount, a.Name, b.Name, a.Slug, b.Slug)
	})
	sort.Slice(result.Local.Theaters, func(i, j int) bool {
		a, b := result.Local.Theaters[i], result.Local.Theaters[j]
		return statisticsLocalLess(a.ShowtimeCount, b.ShowtimeCount, a.MovieCount, b.MovieCount, a.Name, b.Name, a.ID, b.ID)
	})
	result.Totals.Cities, result.Totals.Theaters = len(cities), len(theaters)
}

func statisticsSortMovies(movies []StatisticsMovieRank, byTheaters bool) {
	sort.Slice(movies, func(i, j int) bool {
		a, b := movies[i], movies[j]
		firstA, firstB, secondA, secondB := a.ShowtimeCount, b.ShowtimeCount, a.TheaterCount, b.TheaterCount
		if byTheaters {
			firstA, firstB, secondA, secondB = secondA, secondB, firstA, firstB
		}
		return statisticsLocalLess(firstA, firstB, secondA, secondB, a.Title, b.Title, a.Slug, b.Slug)
	})
}

func statisticsLocalLess(firstA, firstB, secondA, secondB int, nameA, nameB, idA, idB string) bool {
	if firstA != firstB {
		return firstA > firstB
	}
	if secondA != secondB {
		return secondA > secondB
	}
	if c := compareNormalized(nameA, nameB); c != 0 {
		return c < 0
	}
	return idA < idB
}
