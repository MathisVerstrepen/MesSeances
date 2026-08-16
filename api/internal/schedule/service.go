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
	data := s.source.Snapshot()
	selected, err := s.selectedTheaters(data, query.TheaterIDs)
	if err != nil {
		return Timeline{}, err
	}
	timeline := Timeline{Date: query.Date, Timezone: Timezone, WindowStartTime: localTime(date, 8, 0).UTC(), WindowEndTime: localTime(date.AddDate(0, 0, 1), 2, 0).UTC(), Theaters: make([]TimelineTheater, 0)}
	for _, theater := range data.Theaters {
		if !selected[theater.ID] {
			continue
		}
		result := TimelineTheater{ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, AcceptedPasses: append([]string(nil), theater.AcceptedPasses...), Showtimes: []TimelineShowtime{}}
		for _, record := range data.Showtimes {
			if record.TheaterID != theater.ID || record.ServiceDate != query.Date || !matchesLanguage(record.Language, query.Language) {
				continue
			}
			showtime := materializeRecord(record)
			offset := int(showtime.StartTime.Sub(timeline.WindowStartTime) / time.Minute)
			result.Showtimes = append(result.Showtimes, TimelineShowtime{Showtime: showtime, StartOffsetMinutes: offset, DurationMinutes: record.Movie.RuntimeMinutes})
		}
		sort.Slice(result.Showtimes, func(i, j int) bool { return result.Showtimes[i].StartTime.Before(result.Showtimes[j].StartTime) })
		timeline.Theaters = append(timeline.Theaters, result)
	}
	return timeline, nil
}

func (s *Service) Theaters(query TheaterCatalogQuery) []Theater {
	chain := strings.TrimSpace(query.Chain)
	if chain != "" && !strings.EqualFold(chain, ProviderUGC) {
		return []Theater{}
	}

	city := strings.TrimSpace(query.City)
	data := s.source.Snapshot()
	result := make([]Theater, 0, len(data.Theaters))
	for _, theater := range data.Theaters {
		if city != "" && !s.cityMatches(theater.City, city) {
			continue
		}
		result = append(result, Theater{
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
	sort.Slice(result, func(i, j int) bool {
		if compareNormalized(result[i].City, result[j].City) != 0 {
			return compareNormalized(result[i].City, result[j].City) < 0
		}
		if compareNormalized(result[i].Name, result[j].Name) != 0 {
			return compareNormalized(result[i].Name, result[j].Name) < 0
		}
		return result[i].ID < result[j].ID
	})
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
	unique := make(map[string]MovieCatalogItem)
	for _, record := range s.source.Snapshot().Showtimes {
		if search != "" && !strings.Contains(normalized(record.Movie.Title), search) {
			continue
		}
		if _, exists := unique[record.Movie.Slug]; !exists {
			unique[record.Movie.Slug] = materializeCatalogMovie(record.Movie)
		}
	}
	items := make([]MovieCatalogItem, 0, len(unique))
	for _, movie := range unique {
		items = append(items, movie)
	}
	sort.Slice(items, func(i, j int) bool {
		if compareNormalized(items[i].Title, items[j].Title) != 0 {
			return compareNormalized(items[i].Title, items[j].Title) < 0
		}
		return items[i].Slug < items[j].Slug
	})
	result.Total = len(items)
	if result.Total == 0 || page > (result.Total-1)/pageSize+1 {
		return result, nil
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, result.Total)
	result.Items = append(result.Items, items[start:end]...)
	return result, nil
}

func (s *Service) MovieShowtimes(query MovieShowtimesQuery) (MovieSchedule, error) {
	if _, err := s.parseDate(query.Date); err != nil {
		return MovieSchedule{}, err
	}
	city := strings.TrimSpace(query.City)
	if city != "" && len(query.TheaterIDs) > 0 {
		return MovieSchedule{}, invalid("Les paramètres city et theaters sont mutuellement exclusifs.")
	}

	data := s.source.Snapshot()
	var movie MovieCatalogItem
	found := false
	for _, record := range data.Showtimes {
		if record.Movie.Slug == query.Slug {
			movie = materializeCatalogMovie(record.Movie)
			found = true
			break
		}
	}
	if !found {
		return MovieSchedule{}, &NotFoundError{Message: "Film introuvable."}
	}

	selected, err := s.selectTheaters(data, query.TheaterIDs, city, false)
	if err != nil {
		return MovieSchedule{}, err
	}
	grouped := make(map[string][]Showtime)
	for _, record := range data.Showtimes {
		if record.Movie.Slug != query.Slug || record.ServiceDate != query.Date || !selected[record.TheaterID] {
			continue
		}
		grouped[record.TheaterID] = append(grouped[record.TheaterID], materializeRecord(record))
	}

	theaters := append([]TheaterRecord(nil), data.Theaters...)
	sort.Slice(theaters, func(i, j int) bool {
		if compareNormalized(theaters[i].City, theaters[j].City) != 0 {
			return compareNormalized(theaters[i].City, theaters[j].City) < 0
		}
		if compareNormalized(theaters[i].Name, theaters[j].Name) != 0 {
			return compareNormalized(theaters[i].Name, theaters[j].Name) < 0
		}
		return theaters[i].ID < theaters[j].ID
	})
	result := MovieSchedule{Movie: movie, Date: query.Date, Theaters: []MovieTheaterShowtimes{}}
	for _, theater := range theaters {
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
		result.Theaters = append(result.Theaters, MovieTheaterShowtimes{ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, Showtimes: showtimes})
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
	data := s.source.Snapshot()
	selected, err := s.selectTheaters(data, query.TheaterIDs, city, false)
	if err != nil {
		return nil, err
	}
	results := []SlotResult{}
	for _, theater := range data.Theaters {
		if !selected[theater.ID] {
			continue
		}
		for _, record := range data.Showtimes {
			if record.TheaterID != theater.ID || record.ServiceDate != query.Date || !matchesLanguage(record.Language, query.Language) {
				continue
			}
			showtime := materializeRecord(record)
			effectiveEnd := showtime.EndTime.Add(time.Duration(query.BufferAds) * time.Minute)
			if showtime.StartTime.Before(start) || effectiveEnd.After(finish) {
				continue
			}
			results = append(results, SlotResult{Showtime: showtime, Theater: TheaterSummary{ID: theater.ID, Name: theater.Name, City: theater.City}, EffectiveEndTime: effectiveEnd, BufferAdsMinutes: query.BufferAds, SlackBeforeMinutes: int(showtime.StartTime.Sub(start) / time.Minute), SlackAfterMinutes: int(finish.Sub(effectiveEnd) / time.Minute)})
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

func (s *Service) selectedTheaters(data Dataset, ids []string) (map[string]bool, error) {
	return s.selectTheaters(data, ids, "", true)
}

func (s *Service) selectTheaters(data Dataset, ids []string, city string, useDefault bool) (map[string]bool, error) {
	selected := map[string]bool{}
	known := map[string]TheaterRecord{}
	for _, theater := range data.Theaters {
		known[theater.ID] = theater
	}
	if len(ids) > 0 {
		for _, id := range ids {
			if id == "" {
				return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
			}
			if _, ok := known[id]; !ok {
				return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
			}
			selected[id] = true
		}
		return selected, nil
	}
	requestedCity := strings.TrimSpace(city)
	if useDefault {
		requestedCity = s.options.DefaultCity
		for _, theater := range data.Theaters {
			if s.cityMatches(theater.City, requestedCity) {
				selected[theater.ID] = true
			}
		}
		return selected, nil
	}
	if requestedCity == "" {
		for _, theater := range data.Theaters {
			selected[theater.ID] = true
		}
		return selected, nil
	}
	for _, theater := range data.Theaters {
		if s.cityMatches(theater.City, requestedCity) {
			selected[theater.ID] = true
		}
	}
	return selected, nil
}

func (s *Service) cityMatches(actual, requested string) bool {
	if strings.EqualFold(actual, requested) {
		return true
	}
	for alias, cities := range s.options.CityAliases {
		if strings.EqualFold(alias, requested) {
			for _, city := range cities {
				if strings.EqualFold(actual, city) {
					return true
				}
			}
		}
	}
	return false
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
func materializeRecord(record ShowtimeRecord) Showtime {
	booking := record.BookingURL
	return Showtime{ID: record.ID, Movie: Movie{Slug: record.Movie.Slug, Title: record.Movie.Title, RuntimeMinutes: record.Movie.RuntimeMinutes}, StartTime: record.StartTime.UTC(), EndTime: record.EndTime.UTC(), Language: record.Language, Format: record.Format, Room: record.Room, BookingURL: &booking}
}
func materializeCatalogMovie(record MovieRecord) MovieCatalogItem {
	var poster *string
	if record.PosterURL != "" {
		value := record.PosterURL
		poster = &value
	}
	return MovieCatalogItem{Slug: record.Slug, Title: record.Title, RuntimeMinutes: record.RuntimeMinutes, PosterURL: poster}
}
func normalized(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func compareNormalized(a, b string) int {
	return strings.Compare(normalized(a), normalized(b))
}
func localTime(date time.Time, hour, minute int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location())
}
func invalid(message string) error { return &ValidationError{Message: message} }
