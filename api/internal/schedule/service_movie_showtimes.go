package schedule

import (
	"sort"
	"strings"
)

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
