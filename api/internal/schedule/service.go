package schedule

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

const (
	defaultMovieCatalogPageSize = 24
	maxMovieCatalogPageSize     = 100
)

type ServiceOptions struct {
	DefaultCity string
	CityAliases map[string][]string
}
type Service struct {
	location *time.Location
	source   Source
	options  ServiceOptions
}

func NewService(source Source, options ServiceOptions) (*Service, error) {
	if source == nil {
		return nil, fmt.Errorf("schedule source is required")
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, fmt.Errorf("load schedule timezone: %w", err)
	}
	options.DefaultCity = strings.TrimSpace(options.DefaultCity)
	return &Service{location: location, source: source, options: options}, nil
}

func (s *Service) Timeline(query TimelineQuery) (Timeline, error) {
	date, err := s.parseDate(query.Date)
	if err != nil {
		return Timeline{}, err
	}
	if err := validateLanguage(query.Language); err != nil {
		return Timeline{}, err
	}
	view := s.source.Snapshot()
	selected, err := s.selectedTheaters(view, query.TheaterIDs)
	if err != nil {
		return Timeline{}, err
	}
	timeline := Timeline{Date: query.Date, Timezone: Timezone, WindowStartTime: localTime(date, 8, 0).UTC(), WindowEndTime: localTime(date.AddDate(0, 0, 1), 2, 0).UTC(), Theaters: make([]TimelineTheater, 0)}
	for _, theaterPosition := range selected {
		theater := view.data.Theaters[theaterPosition]
		result := TimelineTheater{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, AcceptedPasses: append([]string(nil), theater.AcceptedPasses...), Showtimes: []TimelineShowtime{}}
		for _, showingPosition := range view.theaterDate[theaterDateKey{theaterID: theater.ID, date: query.Date}] {
			record := view.data.Showtimes[showingPosition]
			if !matchesLanguage(record.Language, query.Language) {
				continue
			}
			showtime := materializeRecord(record)
			offset := int(showtime.StartTime.Sub(timeline.WindowStartTime) / time.Minute)
			poster, backdrop := materializeMovieMedia(record.Movie)
			result.Showtimes = append(result.Showtimes, TimelineShowtime{Showtime: showtime, StartOffsetMinutes: offset, DurationMinutes: record.Movie.RuntimeMinutes, PosterURL: poster, BackdropURL: backdrop})
		}
		sort.Slice(result.Showtimes, func(i, j int) bool { return result.Showtimes[i].StartTime.Before(result.Showtimes[j].StartTime) })
		timeline.Theaters = append(timeline.Theaters, result)
	}
	return timeline, nil
}

func (s *Service) Theaters(query TheaterCatalogQuery) []Theater {
	chain := strings.TrimSpace(query.Chain)
	if chain != "" && !strings.EqualFold(chain, ProviderUGC) && !strings.EqualFold(chain, ProviderKinepolis) {
		return []Theater{}
	}

	city := strings.TrimSpace(query.City)
	view := s.source.Snapshot()
	positions := view.theaterCatalog
	if city != "" {
		positions = view.catalogPositionsForCities(s.cityLookupValues(city))
	}
	result := make([]Theater, 0, len(positions))
	for _, position := range positions {
		theater := view.data.Theaters[position]
		provider := recordProvider(theater.Provider, theater.ID)
		if chain != "" && !strings.EqualFold(chain, provider) {
			continue
		}
		result = append(result, Theater{
			Provider:       provider,
			ID:             theater.ID,
			Slug:           theater.Slug,
			Name:           theater.Name,
			Address:        theater.Address,
			City:           theater.City,
			PostalCode:     theater.PostalCode,
			AvailableDates: append([]string(nil), theater.AvailableDates...),
			AcceptedPasses: append([]string(nil), theater.AcceptedPasses...),
		})
	}
	return result
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

	result := MovieCatalog{Items: []MovieCatalogItem{}, Page: page, PageSize: pageSize}
	if query.CurrentlyScreened != nil && !*query.CurrentlyScreened {
		return result, nil
	}

	search := normalized(strings.TrimSpace(query.Search))
	type groupedMovie struct {
		item          MovieCatalogItem
		showtimeCount int
	}
	view := s.source.Snapshot()
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

func (s *Service) MovieShowtimes(query MovieShowtimesQuery) (MovieSchedule, error) {
	if _, err := s.parseDate(query.Date); err != nil {
		return MovieSchedule{}, err
	}
	city := strings.TrimSpace(query.City)
	if city != "" && len(query.TheaterIDs) > 0 {
		return MovieSchedule{}, invalid("Les paramètres city et theaters sont mutuellement exclusifs.")
	}

	view := s.source.Snapshot()
	movieIndex, found := view.movieBySlug[query.Slug]
	if !found {
		return MovieSchedule{}, &NotFoundError{Message: "Film introuvable."}
	}
	representative := view.data.Showtimes[movieIndex.firstShowtime].Movie
	movie := materializeCatalogMovie(representative)
	_, backdrop := materializeMovieMedia(representative)

	selected, err := s.selectTheaters(view, query.TheaterIDs, city, false)
	if err != nil {
		return MovieSchedule{}, err
	}
	grouped := make(map[string][]Showtime)
	selectedIDs := make(map[string]bool, len(selected))
	for _, position := range selected {
		selectedIDs[view.data.Theaters[position].ID] = true
	}
	for _, showingPosition := range view.movieDate[movieDateKey{slug: query.Slug, date: query.Date}] {
		record := view.data.Showtimes[showingPosition]
		if !selectedIDs[record.TheaterID] {
			continue
		}
		grouped[record.TheaterID] = append(grouped[record.TheaterID], materializeRecord(record))
	}

	result := MovieSchedule{Movie: movie, BackdropURL: backdrop, Date: query.Date, Theaters: []MovieTheaterShowtimes{}}
	for _, theaterPosition := range view.theaterCatalog {
		theater := view.data.Theaters[theaterPosition]
		showtimes := grouped[theater.ID]
		if len(showtimes) == 0 {
			continue
		}
		sort.Slice(showtimes, func(i, j int) bool {
			if !showtimes[i].StartTime.Equal(showtimes[j].StartTime) {
				return showtimes[i].StartTime.Before(showtimes[j].StartTime)
			}
			return showtimes[i].ID < showtimes[j].ID
		})
		result.Theaters = append(result.Theaters, MovieTheaterShowtimes{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, Showtimes: showtimes})
	}
	return result, nil
}

func (s *Service) SearchSlot(query SlotQuery) ([]SlotResult, error) {
	city := strings.TrimSpace(query.City)
	hasTheaters := len(query.TheaterIDs) > 0
	if city != "" && hasTheaters {
		return nil, invalid("Les paramètres city et theaters sont mutuellement exclusifs.")
	}
	if city == "" && !hasTheaters {
		return nil, invalid("Le paramètre city ou theaters est requis.")
	}
	date, err := s.parseDate(query.Date)
	if err != nil {
		return nil, err
	}
	start, err := s.parseServiceClock(date, query.StartAfter, "start_after")
	if err != nil {
		return nil, err
	}
	finish, err := s.parseServiceClock(date, query.FinishBefore, "finish_before")
	if err != nil {
		return nil, err
	}
	if !finish.After(start) {
		return nil, invalid("Le paramètre finish_before doit être postérieur à start_after.")
	}
	if query.BufferAds < 0 || query.BufferAds > 120 {
		return nil, invalid("Le paramètre buffer_ads doit être un entier compris entre 0 et 120.")
	}
	if err := validateLanguage(query.Language); err != nil {
		return nil, err
	}
	if err := validateSlotFormat(query.Format); err != nil {
		return nil, err
	}
	view := s.source.Snapshot()
	selected, err := s.selectTheaters(view, query.TheaterIDs, city, false)
	if err != nil {
		return nil, err
	}
	results := []SlotResult{}
	for _, theaterPosition := range selected {
		theater := view.data.Theaters[theaterPosition]
		for _, showingPosition := range view.theaterDate[theaterDateKey{theaterID: theater.ID, date: query.Date}] {
			record := view.data.Showtimes[showingPosition]
			if !matchesLanguage(record.Language, query.Language) || !matchesFormat(record.Format, query.Format) {
				continue
			}
			showtime := materializeRecord(record)
			effectiveStart := showtime.StartTime
			if !query.IncludeAds {
				effectiveStart = effectiveStart.Add(time.Duration(query.BufferAds) * time.Minute)
			}
			effectiveEnd := showtime.EndTime.Add(time.Duration(query.BufferAds) * time.Minute)
			if effectiveStart.Before(start) || effectiveEnd.After(finish) {
				continue
			}
			poster, backdrop := materializeMovieMedia(record.Movie)
			results = append(results, SlotResult{Showtime: showtime, Theater: TheaterSummary{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Name: theater.Name, City: theater.City}, EffectiveStartTime: effectiveStart, EffectiveEndTime: effectiveEnd, BufferAdsMinutes: query.BufferAds, SlackBeforeMinutes: int(effectiveStart.Sub(start) / time.Minute), SlackAfterMinutes: int(finish.Sub(effectiveEnd) / time.Minute), PosterURL: poster, BackdropURL: backdrop})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if !results[i].Showtime.StartTime.Equal(results[j].Showtime.StartTime) {
			return results[i].Showtime.StartTime.Before(results[j].Showtime.StartTime)
		}
		if results[i].Theater.Name != results[j].Theater.Name {
			return results[i].Theater.Name < results[j].Theater.Name
		}
		return results[i].Showtime.ID < results[j].Showtime.ID
	})
	return results, nil
}

func (s *Service) selectedTheaters(view *SnapshotView, ids []string) ([]int, error) {
	return s.selectTheaters(view, ids, "", true)
}

func (s *Service) selectTheaters(view *SnapshotView, ids []string, city string, useDefault bool) ([]int, error) {
	if len(ids) > 0 {
		selected := make([]int, 0, len(ids))
		seen := make(map[int]bool, len(ids))
		for _, id := range ids {
			if id == "" {
				return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
			}
			position, ok := view.theaterByID[id]
			if !ok {
				return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
			}
			if !seen[position] {
				seen[position] = true
				selected = append(selected, position)
			}
		}
		sort.Ints(selected)
		return selected, nil
	}
	requestedCity := strings.TrimSpace(city)
	if useDefault {
		return view.positionsForCities(s.cityLookupValues(s.options.DefaultCity)), nil
	}
	if requestedCity == "" {
		return append([]int(nil), view.theaterPositions...), nil
	}
	return view.positionsForCities(s.cityLookupValues(requestedCity)), nil
}

func (s *Service) cityLookupValues(requested string) []string {
	values := []string{requested}
	for alias, cities := range s.options.CityAliases {
		if strings.EqualFold(alias, requested) {
			for _, city := range cities {
				duplicate := false
				for _, value := range values {
					if strings.EqualFold(value, city) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					values = append(values, city)
				}
			}
		}
	}
	return values
}

func (s *Service) parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, invalid("Le paramètre date est requis.")
	}
	parsed, err := time.ParseInLocation(dateLayout, value, s.location)
	if err != nil || parsed.Format(dateLayout) != value {
		return time.Time{}, invalid("Le paramètre date doit respecter le format YYYY-MM-DD.")
	}
	return parsed, nil
}
func (s *Service) parseServiceClock(date time.Time, value, parameter string) (time.Time, error) {
	if value == "" {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s est requis.", parameter))
	}
	if len(value) != 5 || value[2] != ':' {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s doit respecter le format HH:MM.", parameter))
	}
	hour, hourErr := strconv.Atoi(value[:2])
	minute, minuteErr := strconv.Atoi(value[3:])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s doit respecter le format HH:MM.", parameter))
	}
	if hour > 2 && hour < 8 || hour == 2 && minute > 0 {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s doit appartenir à la journée cinéma (08:00–02:00).", parameter))
	}
	if hour < 8 {
		date = date.AddDate(0, 0, 1)
	}
	return localTime(date, hour, minute).UTC(), nil
}
func validateLanguage(language string) error {
	if language != LanguageAll && language != LanguageVOSTFR && language != LanguageVF {
		return invalid("Le paramètre language doit être ALL, VOSTFR ou VF.")
	}
	return nil
}
func matchesLanguage(session, requested string) bool {
	return requested == LanguageAll || requested == session || requested == LanguageVF && session == LanguageVFSME
}
func validateSlotFormat(format string) error {
	if format != "" && format != FormatAll && !validFormat(format) {
		return invalid("Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA ou 4DX.")
	}
	return nil
}
func matchesFormat(session, requested string) bool {
	return requested == "" || requested == FormatAll || requested == session
}
func materializeRecord(record ShowtimeRecord) Showtime {
	booking := record.BookingURL
	provider := recordProvider(record.Provider, record.ID)
	return Showtime{Provider: provider, ID: record.ID, Movie: Movie{Provider: provider, Slug: publicMovieSlug(record.Movie), Title: record.Movie.Title, RuntimeMinutes: record.Movie.RuntimeMinutes}, StartTime: record.StartTime.UTC(), EndTime: record.EndTime.UTC(), Language: record.Language, Format: record.Format, Room: record.Room, BookingURL: &booking}
}
func materializeCatalogMovie(record MovieRecord) MovieCatalogItem {
	poster, _ := materializeMovieMedia(record)
	item := MovieCatalogItem{Provider: recordProvider(record.Provider, record.Slug), Slug: publicMovieSlug(record), Title: record.Title, RuntimeMinutes: record.RuntimeMinutes, PosterURL: poster, Genres: append([]string{}, record.Genres...)}
	if record.Overview != "" {
		value := record.Overview
		item.Overview = &value
	}
	if record.ReleaseDate != "" {
		value := record.ReleaseDate
		item.ReleaseDate = &value
	}
	if record.Enrichment != nil && record.Enrichment.TMDBID > 0 {
		id := record.Enrichment.TMDBID
		item.TMDBID = &id
		if record.Enrichment.Overview != "" {
			value := record.Enrichment.Overview
			item.Overview = &value
		}
		if record.Enrichment.ReleaseDate != "" {
			value := record.Enrichment.ReleaseDate
			item.ReleaseDate = &value
		}
		if len(record.Enrichment.Genres) > 0 {
			item.Genres = append([]string{}, record.Enrichment.Genres...)
		}
	}
	return item
}
func materializeMovieMedia(record MovieRecord) (*string, *string) {
	var poster *string
	if record.PosterURL != "" {
		value := record.PosterURL
		poster = &value
	}
	var backdrop *string
	if record.Enrichment != nil {
		if record.Enrichment.TMDBID > 0 && record.Enrichment.PosterURL != "" {
			value := record.Enrichment.PosterURL
			poster = &value
		}
		if validTMDBBackdropURL(record.Enrichment.BackdropURL) {
			value := record.Enrichment.BackdropURL
			backdrop = &value
		}
	}
	return poster, backdrop
}
func publicMovieSlug(record MovieRecord) string {
	if record.LocalMovieID > 0 {
		return "local-film-" + strconv.FormatInt(record.LocalMovieID, 10)
	}
	if record.Enrichment != nil && record.Enrichment.TMDBID > 0 {
		return "tmdb-film-" + strconv.FormatInt(record.Enrichment.TMDBID, 10)
	}
	return record.Slug
}
func normalized(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func compareNormalized(a, b string) int {
	return strings.Compare(normalized(a), normalized(b))
}
func localTime(date time.Time, hour, minute int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location())
}
func recordProvider(explicit, identity string) string {
	if explicit != "" {
		return explicit
	}
	if strings.HasPrefix(identity, ProviderKinepolis+"-") {
		return ProviderKinepolis
	}
	return ProviderUGC
}
func invalid(message string) error { return &ValidationError{Message: message} }
