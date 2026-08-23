package schedule

import (
	"sort"
	"time"
)

func (s *Service) TheaterShowtimes(query TheaterShowtimesQuery) (TheaterShowtimes, error) {
	if query.DateProvided || query.Date != "" {
		if _, err := s.parseDate(query.Date); err != nil {
			return TheaterShowtimes{}, err
		}
	}
	view := s.source.Snapshot()
	position, found := view.theaterBySlug[query.Slug]
	if !found {
		return TheaterShowtimes{}, &NotFoundError{Message: "Cinéma introuvable."}
	}
	theater := view.data.Theaters[position]
	result := TheaterShowtimes{GeneratedAt: view.data.GeneratedAt, Timezone: Timezone, Theater: materializeTheater(view, position), Showtimes: []TimelineShowtime{}}
	date := query.Date
	if date == "" && len(theater.AvailableDates) > 0 {
		date = theater.AvailableDates[0]
		for _, candidate := range theater.AvailableDates[1:] {
			if candidate < date {
				date = candidate
			}
		}
	}
	if date == "" {
		return result, nil
	}
	result.Date = &date
	parsedDate, _ := time.ParseInLocation(dateLayout, date, s.location)
	windowStart := localTime(parsedDate, 8, 0).UTC()
	for _, showingPosition := range view.theaterDate[theaterDateKey{theaterID: theater.ID, date: date}] {
		record := view.data.Showtimes[showingPosition]
		showtime := materializeRecord(view, record)
		poster, backdrop := materializeMovieMedia(view, record.Movie)
		result.Showtimes = append(result.Showtimes, TimelineShowtime{Showtime: showtime, StartOffsetMinutes: int(showtime.StartTime.Sub(windowStart) / time.Minute), DurationMinutes: int(record.EndTime.Sub(record.StartTime) / time.Minute), PosterURL: poster, BackdropURL: backdrop})
	}
	sort.Slice(result.Showtimes, func(i, j int) bool {
		if !result.Showtimes[i].StartTime.Equal(result.Showtimes[j].StartTime) {
			return result.Showtimes[i].StartTime.Before(result.Showtimes[j].StartTime)
		}
		return result.Showtimes[i].ID < result.Showtimes[j].ID
	})
	return result, nil
}
