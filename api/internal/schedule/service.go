package schedule

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

type Service struct {
	location *time.Location
}

func NewService() (*Service, error) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, fmt.Errorf("load schedule timezone: %w", err)
	}

	return &Service{location: location}, nil
}

func (s *Service) Timeline(query TimelineQuery) (Timeline, error) {
	date, err := s.parseDate(query.Date)
	if err != nil {
		return Timeline{}, err
	}
	if err := validateLanguage(query.Language); err != nil {
		return Timeline{}, err
	}

	selected, err := selectedTheaters(query.TheaterIDs)
	if err != nil {
		return Timeline{}, err
	}

	timeline := Timeline{
		Date:            query.Date,
		Timezone:        Timezone,
		WindowStartTime: localTime(date, 8, 0).UTC(),
		WindowEndTime:   localTime(date.AddDate(0, 0, 1), 2, 0).UTC(),
		Theaters:        make([]TimelineTheater, 0, len(theaterFixtures)),
	}

	for _, theater := range theaterFixtures {
		if !selected[theater.id] {
			continue
		}

		result := TimelineTheater{
			ID:             theater.id,
			Slug:           theater.slug,
			Name:           theater.name,
			City:           theater.city,
			AcceptedPasses: append([]string(nil), theater.acceptedPasses...),
			Showtimes:      make([]TimelineShowtime, 0),
		}
		for _, fixture := range showtimeFixtures {
			if fixture.theaterID != theater.id || !matchesLanguage(fixture.language, query.Language) {
				continue
			}
			showtime, offset := materializeShowtime(date, fixture)
			result.Showtimes = append(result.Showtimes, TimelineShowtime{
				Showtime:           showtime,
				StartOffsetMinutes: offset,
				DurationMinutes:    fixture.runtimeMinutes,
			})
		}
		sort.Slice(result.Showtimes, func(i, j int) bool {
			return result.Showtimes[i].StartTime.Before(result.Showtimes[j].StartTime)
		})
		timeline.Theaters = append(timeline.Theaters, result)
	}

	return timeline, nil
}

func (s *Service) SearchSlot(query SlotQuery) ([]SlotResult, error) {
	city := strings.TrimSpace(query.City)
	if city == "" {
		return nil, invalid("Le paramètre city est requis.")
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

	results := make([]SlotResult, 0)
	for _, theater := range theaterFixtures {
		if !strings.EqualFold(theater.city, city) {
			continue
		}
		for _, fixture := range showtimeFixtures {
			if fixture.theaterID != theater.id || !matchesLanguage(fixture.language, query.Language) {
				continue
			}
			showtime, _ := materializeShowtime(date, fixture)
			effectiveEnd := showtime.EndTime.Add(time.Duration(query.BufferAds) * time.Minute)
			if showtime.StartTime.Before(start) || effectiveEnd.After(finish) {
				continue
			}
			results = append(results, SlotResult{
				Showtime: showtime,
				Theater: TheaterSummary{
					ID:   theater.id,
					Name: theater.name,
					City: theater.city,
				},
				EffectiveEndTime:   effectiveEnd,
				BufferAdsMinutes:   query.BufferAds,
				SlackBeforeMinutes: int(showtime.StartTime.Sub(start) / time.Minute),
				SlackAfterMinutes:  int(finish.Sub(effectiveEnd) / time.Minute),
			})
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

func selectedTheaters(ids []string) (map[string]bool, error) {
	selected := make(map[string]bool, len(theaterFixtures))
	if len(ids) == 0 {
		for _, theater := range theaterFixtures {
			selected[theater.id] = true
		}
		return selected, nil
	}

	known := make(map[string]bool, len(theaterFixtures))
	for _, theater := range theaterFixtures {
		known[theater.id] = true
	}
	for _, id := range ids {
		if id == "" || !known[id] {
			return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
		}
		selected[id] = true
	}
	return selected, nil
}

func validateLanguage(language string) error {
	if language != LanguageAll && language != LanguageVOSTFR && language != LanguageVF {
		return invalid("Le paramètre language doit être ALL, VOSTFR ou VF.")
	}
	return nil
}

func matchesLanguage(sessionLanguage, requested string) bool {
	return requested == LanguageAll || requested == sessionLanguage
}

func materializeShowtime(date time.Time, fixture showtimeFixture) (Showtime, int) {
	hour, _ := strconv.Atoi(fixture.startClock[:2])
	minute, _ := strconv.Atoi(fixture.startClock[3:])
	startDate := date
	offset := (hour-8)*60 + minute
	if hour < 8 {
		startDate = date.AddDate(0, 0, 1)
		offset = (hour+16)*60 + minute
	}
	start := localTime(startDate, hour, minute).UTC()
	end := start.Add(time.Duration(fixture.runtimeMinutes) * time.Minute)

	return Showtime{
		ID: fixture.id,
		Movie: Movie{
			Slug:           fixture.movieSlug,
			Title:          fixture.movieTitle,
			RuntimeMinutes: fixture.runtimeMinutes,
		},
		StartTime:  start,
		EndTime:    end,
		Language:   fixture.language,
		Format:     fixture.format,
		Room:       fixture.room,
		BookingURL: nil,
	}, offset
}

func localTime(date time.Time, hour, minute int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location())
}

func invalid(message string) error {
	return &ValidationError{Message: message}
}
