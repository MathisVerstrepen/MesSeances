package schedule

import (
	"sort"
	"time"
)

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
			showtime := materializeRecord(view, record)
			offset := int(showtime.StartTime.Sub(timeline.WindowStartTime) / time.Minute)
			poster, backdrop := materializeMovieMedia(view, record.Movie)
			result.Showtimes = append(result.Showtimes, TimelineShowtime{Showtime: showtime, StartOffsetMinutes: offset, DurationMinutes: int(record.EndTime.Sub(record.StartTime) / time.Minute), PosterURL: poster, BackdropURL: backdrop})
		}
		sort.Slice(result.Showtimes, func(i, j int) bool { return result.Showtimes[i].StartTime.Before(result.Showtimes[j].StartTime) })
		timeline.Theaters = append(timeline.Theaters, result)
	}
	return timeline, nil
}
