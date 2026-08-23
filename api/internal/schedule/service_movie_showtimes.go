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
	canonicalSlug, found := view.resolveMovieSlug(query.Slug)
	if !found {
		return MovieSchedule{}, &NotFoundError{Message: "Film introuvable."}
	}
	movieIndex := view.movieBySlug[canonicalSlug]
	var movie MovieCatalogItem
	var backdrop *string
	if len(view.data.PublicMovies) > 0 {
		public := view.data.PublicMovies[movieIndex.publicMovie]
		movie = materializePublicMovie(public)
		if public.BackdropURL != "" {
			value := public.BackdropURL
			backdrop = &value
		}
	} else {
		representative := view.data.Showtimes[movieIndex.firstShowtime].Movie
		movie = materializeCatalogMovie(view, representative)
		_, backdrop = materializeMovieMedia(view, representative)
	}
	selected, err := s.selectTheaters(view, query.TheaterIDs, city, false)
	if err != nil {
		return MovieSchedule{}, err
	}
	grouped := make(map[string][]Showtime)
	selectedIDs := make(map[string]bool, len(selected))
	for _, position := range selected {
		selectedIDs[view.data.Theaters[position].ID] = true
	}
	availableDates := make([]string, 0, len(view.movieDates[canonicalSlug]))
	for _, date := range view.movieDates[canonicalSlug] {
		for _, showingPosition := range view.movieDate[movieDateKey{slug: canonicalSlug, date: date}] {
			if selectedIDs[view.data.Showtimes[showingPosition].TheaterID] {
				availableDates = append(availableDates, date)
				break
			}
		}
	}
	for _, showingPosition := range view.movieDate[movieDateKey{slug: canonicalSlug, date: query.Date}] {
		record := view.data.Showtimes[showingPosition]
		if !selectedIDs[record.TheaterID] {
			continue
		}
		grouped[record.TheaterID] = append(grouped[record.TheaterID], materializeRecord(view, record))
	}
	result := MovieSchedule{Movie: movie, BackdropURL: backdrop, CurrentlyScreened: movieCurrentlyScreened(view, canonicalSlug, s.now()), Date: query.Date, AvailableDates: availableDates, Theaters: []MovieTheaterShowtimes{}}
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
