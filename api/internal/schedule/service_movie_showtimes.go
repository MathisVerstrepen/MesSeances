package schedule

import (
	"slices"
	"sort"
	"strings"
	"time"
)

const movieShowtimesPageSize = 10

var movieShowtimesLanguages = []Language{LanguageVOSTFR, LanguageVF, LanguageVO, LanguageVFSME, LanguageVFSTF}
var movieShowtimesFormats = []Format{Format2D, Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX, FormatICE, FormatInfinityVision}

func (s *Service) MovieShowtimes(query MovieShowtimesQuery) (MovieSchedule, error) {
	return s.movieShowtimes(s.source.Snapshot(), s.now(), query)
}

// MovieShowtimesBundle freezes snapshot and clock for both scopes.
func (s *Service) MovieShowtimesBundle(query MovieShowtimesQuery) (MovieSchedule, MovieSchedule, error) {
	view, now := s.source.Snapshot(), s.now()
	query.Page = 1
	scoped, err := s.movieShowtimes(view, now, query)
	if err != nil {
		return MovieSchedule{}, MovieSchedule{}, err
	}
	query.City, query.TheaterIDs = "", nil
	query.Language, query.Format, query.Sort = LanguageAll, FormatAll, MovieShowtimesSortCatalog
	nationwide, err := s.movieShowtimes(view, now, query)
	return scoped, nationwide, err
}

func (s *Service) movieShowtimes(view *SnapshotView, now time.Time, query MovieShowtimesQuery) (MovieSchedule, error) {
	if _, err := s.parseDate(query.Date); err != nil {
		return MovieSchedule{}, err
	}
	if query.Page < 0 {
		return MovieSchedule{}, invalid("Le paramètre page doit être un entier supérieur ou égal à 1.")
	}
	if query.Language == "" {
		query.Language = LanguageAll
	}
	if query.Language != LanguageAll && query.Language != LanguageOriginal && query.Language != LanguageVOF && !slices.Contains(movieShowtimesLanguages, query.Language) {
		return MovieSchedule{}, invalid("Le paramètre language doit être ALL, ORIGINAL, VOF, VOSTFR, VF, VO, VF_SME ou VFSTF.")
	}
	if err := validateSlotFormat(query.Format); err != nil {
		return MovieSchedule{}, err
	}
	if query.Sort == "" {
		query.Sort = MovieShowtimesSortCatalog
	}
	if query.Sort != MovieShowtimesSortCatalog && query.Sort != MovieShowtimesSortNext {
		return MovieSchedule{}, invalid("Le paramètre sort doit être catalog ou next.")
	}
	city := strings.TrimSpace(query.City)
	if city != "" && len(query.TheaterIDs) > 0 {
		return MovieSchedule{}, invalid("Les paramètres city et theaters sont mutuellement exclusifs.")
	}
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
	} else if movieIndex.firstShowtime >= 0 {
		representative := view.data.Showtimes[movieIndex.firstShowtime].Movie
		movie = materializeCatalogMovie(view, representative)
		_, backdrop = materializeMovieMedia(view, representative)
	}
	selected, err := s.selectTheaters(view, query.TheaterIDs, city, false)
	if err != nil {
		return MovieSchedule{}, err
	}
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
	// Scan raw records for full-date facets and matching cinemas, without materializing showtimes.
	languages, formats := make(map[Language]bool), make(map[Format]bool)
	earliest := make(map[string]time.Time)
	datePositions := view.movieDate[movieDateKey{slug: canonicalSlug, date: query.Date}]
	for _, showingPosition := range datePositions {
		record := view.data.Showtimes[showingPosition]
		if !selectedIDs[record.TheaterID] {
			continue
		}
		languages[record.Language], formats[record.Format] = true, true
		if !matchesMovieShowtime(view, record, query) {
			continue
		}
		if start, ok := earliest[record.TheaterID]; !ok || record.StartTime.Before(start) {
			earliest[record.TheaterID] = record.StartTime
		}
	}
	candidates := make([]int, 0, len(earliest))
	for _, position := range view.theaterCatalog {
		if _, ok := earliest[view.data.Theaters[position].ID]; ok {
			candidates = append(candidates, position)
		}
	}
	if query.Sort == MovieShowtimesSortNext {
		sort.Slice(candidates, func(i, j int) bool {
			left, right := candidates[i], candidates[j]
			leftStart, rightStart := earliest[view.data.Theaters[left].ID], earliest[view.data.Theaters[right].ID]
			if !leftStart.Equal(rightStart) {
				return leftStart.Before(rightStart)
			}
			return view.theaterRank[left] < view.theaterRank[right]
		})
	}
	result := MovieSchedule{Movie: movie, BackdropURL: backdrop, CurrentlyScreened: movieCurrentlyScreened(view, canonicalSlug, now), Date: query.Date, AvailableDates: availableDates, CatalogRevision: view.catalogRevision, AvailableLanguages: []Language{}, AvailableFormats: []Format{}, Theaters: []MovieTheaterShowtimes{}, ReleaseStatus: "ended"}
	for _, language := range movieShowtimesLanguages {
		if languages[language] {
			result.AvailableLanguages = append(result.AvailableLanguages, language)
		}
	}
	for _, format := range movieShowtimesFormats {
		if formats[format] {
			result.AvailableFormats = append(result.AvailableFormats, format)
		}
	}
	if query.Page > 0 || city == "" && len(query.TheaterIDs) == 0 || len(selected) == len(view.theaterCatalog) {
		page := query.Page
		if page == 0 {
			page = 1
		}
		total := len(candidates)
		result.Pagination = &MovieShowtimesPagination{Page: page, PageSize: movieShowtimesPageSize, Total: total}
		// Guard range before offset multiplication, including machine-int-sized pages.
		if total == 0 || page-1 > (total-1)/movieShowtimesPageSize {
			candidates = nil
		} else {
			start := (page - 1) * movieShowtimesPageSize
			end := start + min(movieShowtimesPageSize, total-start)
			candidates = candidates[start:end]
			result.Pagination.HasMore = end < total
		}
	}
	if movie.FrenchReleaseDate != nil && *movie.FrenchReleaseDate > now.In(s.location).Format(time.DateOnly) {
		result.ReleaseStatus = "upcoming"
	} else if result.CurrentlyScreened {
		result.ReleaseStatus = "showing"
	} else if len(view.data.PublicMovies) > 0 && view.data.PublicMovies[movieIndex.publicMovie].HasUpcomingRelease && movie.FrenchReleaseDate == nil {
		result.ReleaseStatus = "unavailable"
	}
	// Fix page membership before allocating or materializing any returned showtimes.
	pageIDs := make(map[string]bool, len(candidates))
	for _, position := range candidates {
		pageIDs[view.data.Theaters[position].ID] = true
	}
	grouped := make(map[string][]Showtime, len(candidates))
	for _, position := range datePositions {
		record := view.data.Showtimes[position]
		if pageIDs[record.TheaterID] && matchesMovieShowtime(view, record, query) {
			grouped[record.TheaterID] = append(grouped[record.TheaterID], materializeRecord(view, record))
		}
	}
	for _, theaterPosition := range candidates {
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
		result.Theaters = append(result.Theaters, MovieTheaterShowtimes{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, CitySlug: view.cityBuckets[view.theaterCity[theaterPosition]].slug, Showtimes: showtimes})
	}
	return result, nil
}

func matchesMovieShowtime(view *SnapshotView, record ShowtimeRecord, query MovieShowtimesQuery) bool {
	if !matchesFormat(record.Format, query.Format) {
		return false
	}
	if query.Language == LanguageOriginal || query.Language == LanguageVOF {
		return matchesLanguage(record.Language, query.Language, movieOriginalLanguage(view, record.Movie))
	}
	return query.Language == LanguageAll || query.Language == record.Language
}
