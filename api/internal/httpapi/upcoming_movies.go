package httpapi

import (
	"errors"
	"net/http"

	"messeances/api/internal/schedule"
)

func (api *API) requireCatalog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !api.schedule.HasCatalog() {
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusServiceUnavailable, "schedule_unavailable", "Les horaires ne sont pas encore disponibles.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (api *API) upcomingMovies(w http.ResponseWriter, r *http.Request) {
	// ParseQuery detects malformed escaping that URL.Query would silently discard.
	query, err := parseUpcomingQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", "Paramètres invalides.")
		return
	}
	page, ok := parsePositiveInteger(w, query, "page", "Pagination invalide.")
	if !ok {
		return
	}
	pageSize, ok := parsePositiveInteger(w, query, "page_size", "Pagination invalide.")
	if !ok {
		return
	}
	result, err := api.schedule.UpcomingMovies(schedule.UpcomingMoviesQuery{Month: query.Get("month"), Genres: parseCSVQuery(query, "genres"), Page: page, PageSize: pageSize})
	if errors.Is(err, schedule.ErrUpcomingUnavailable) {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, http.StatusServiceUnavailable, "upcoming_unavailable", "Les prochaines sorties ne sont pas encore disponibles.")
		return
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
