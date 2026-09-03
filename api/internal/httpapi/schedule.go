package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/schedule"
)

func (api *API) requireSchedule(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if api.schedule == nil || !api.schedule.HasSnapshot() {
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusServiceUnavailable, "schedule_unavailable", "Les horaires ne sont pas encore disponibles.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (api *API) timeline(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	language := schedule.LanguageAll
	if query.Has("language") {
		language = schedule.Language(query.Get("language"))
	}

	result, err := api.schedule.Timeline(schedule.TimelineQuery{
		Date:       query.Get("date"),
		TheaterIDs: parseCSVQuery(query, "theaters"),
		Language:   language,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) theaters(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	writeJSON(w, http.StatusOK, api.schedule.Theaters(schedule.TheaterCatalogQuery{
		City:  query.Get("city"),
		Chain: schedule.Provider(query.Get("chain")),
	}))
}

func (api *API) theaterShowtimes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := api.schedule.TheaterShowtimes(schedule.TheaterShowtimesQuery{Slug: chi.URLParam(r, "slug"), Date: query.Get("date"), DateProvided: query.Has("date")})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) cities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.schedule.Cities())
}

func (api *API) city(w http.ResponseWriter, r *http.Request) {
	result, err := api.schedule.City(chi.URLParam(r, "slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) movies(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	includeEnded := false
	if query.Has("include_ended") {
		rawValue := query.Get("include_ended")
		if !strings.EqualFold(rawValue, "true") && !strings.EqualFold(rawValue, "false") {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre include_ended doit être true ou false.")
			return
		}
		includeEnded = strings.EqualFold(rawValue, "true")
	}
	var currentlyScreened *bool
	if query.Has("currently_screened") {
		rawValue := query.Get("currently_screened")
		if !strings.EqualFold(rawValue, "true") && !strings.EqualFold(rawValue, "false") {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre currently_screened doit être true ou false.")
			return
		}
		value := strings.EqualFold(rawValue, "true")
		currentlyScreened = &value
	}

	page, ok := parsePositiveInteger(w, query, "page", "Le paramètre page doit être un entier supérieur ou égal à 1.")
	if !ok {
		return
	}
	pageSize, ok := parsePositiveInteger(w, query, "page_size", "Le paramètre page_size doit être un entier compris entre 1 et 100.")
	if !ok {
		return
	}
	var duration *schedule.MovieCatalogDuration
	if query.Has("duration") {
		value := schedule.MovieCatalogDuration(query.Get("duration"))
		duration = &value
	}
	var date *string
	if query.Has("date") {
		value := query.Get("date")
		date = &value
	}
	var dateTo *string
	if query.Has("date_to") {
		value := query.Get("date_to")
		dateTo = &value
	}

	result, err := api.schedule.Movies(schedule.MovieCatalogQuery{
		CurrentlyScreened: currentlyScreened,
		IncludeEnded:      includeEnded,
		Search:            query.Get("search"),
		Sort:              schedule.MovieCatalogSort(query.Get("sort")),
		TheaterIDs:        parseCSVQuery(query, "theaters"),
		Genres:            parseCSVQuery(query, "genres"),
		Duration:          duration,
		Date:              date,
		DateTo:            dateTo,
		Page:              page,
		PageSize:          pageSize,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) movieShowtimes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := api.schedule.MovieShowtimes(schedule.MovieShowtimesQuery{
		Slug:       chi.URLParam(r, "slug"),
		Date:       query.Get("date"),
		City:       query.Get("city"),
		TheaterIDs: parseCSVQuery(query, "theaters"),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) searchSlot(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	language := schedule.LanguageAll
	if query.Has("language") {
		language = schedule.Language(query.Get("language"))
	}
	format := schedule.FormatAll
	if query.Has("format") {
		format = schedule.Format(query.Get("format"))
		if format == "" {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA, 4DX ou ICE.")
			return
		}
	}

	buffer := 15
	if query.Has("buffer_ads") {
		rawBuffer := query.Get("buffer_ads")
		parsed, err := strconv.Atoi(rawBuffer)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre buffer_ads doit être un entier compris entre 0 et 120.")
			return
		}
		buffer = parsed
	}
	includeAds := true
	if query.Has("include_ads") {
		rawValue := query.Get("include_ads")
		if !strings.EqualFold(rawValue, "true") && !strings.EqualFold(rawValue, "false") {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre include_ads doit être true ou false.")
			return
		}
		includeAds = strings.EqualFold(rawValue, "true")
	}

	result, err := api.schedule.SearchSlot(schedule.SlotQuery{
		City:         query.Get("city"),
		TheaterIDs:   parseCSVQuery(query, "theaters"),
		Date:         query.Get("date"),
		StartAfter:   query.Get("start_after"),
		FinishBefore: query.Get("finish_before"),
		BufferAds:    buffer,
		IncludeAds:   includeAds,
		Language:     language,
		Format:       format,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseCSVQuery(query mapQuery, key string) []string {
	if !query.Has(key) {
		return nil
	}
	parts := strings.Split(query.Get(key), ",")
	values := make([]string, len(parts))
	for i, part := range parts {
		values[i] = strings.TrimSpace(part)
	}
	return values
}

type mapQuery interface {
	Get(string) string
	Has(string) bool
}

func parsePositiveInteger(w http.ResponseWriter, query mapQuery, key, message string) (int, bool) {
	if !query.Has(key) {
		return 0, true
	}
	value, err := strconv.Atoi(query.Get(key))
	if err != nil || value < 1 {
		writeError(w, http.StatusBadRequest, "invalid_query", message)
		return 0, false
	}
	return value, true
}
