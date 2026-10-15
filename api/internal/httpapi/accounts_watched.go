package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"messeances/api/internal/accounts"
)

type watchedMutation struct {
	ExpectedUsername          *string `json:"expected_username"`
	ExpectedRevision          *string `json:"expected_revision"`
	MovieSlug                 *string `json:"movie_slug"`
	Watched                   *string `json:"watched"`
	ExpectedWatchlistRevision *string `json:"expected_watchlist_revision"`
}

type watchedPreferences struct {
	ExpectedUsername     *string `json:"expected_username"`
	ExpectedRevision     *string `json:"expected_revision"`
	DefaultVisibility    *string `json:"default_visibility"`
	SearchVisibility     *string `json:"search_visibility"`
	PlanningVisibility   *string `json:"planning_visibility"`
	FilmsVisibility      *string `json:"films_visibility"`
	UpcomingVisibility   *string `json:"upcoming_visibility"`
	HomeVisibility       *string `json:"home_visibility"`
	CinemaCityVisibility *string `json:"cinema_city_visibility"`
	AutoRemoveWatchlist  *string `json:"auto_remove_watchlist"`
}

func watchedPath(path string) bool {
	return path == "/api/v1/account/watched" || strings.HasPrefix(path, "/api/v1/account/watched/")
}

func watchedHTTPError(w http.ResponseWriter, err error) {
	status, code := 503, "watched_unavailable"
	var rate *accounts.RateLimitError
	switch {
	case errors.Is(err, accounts.ErrInvalidInput):
		status, code = 400, "invalid_request"
	case errors.Is(err, accounts.ErrWatchedChanged):
		status, code = 409, "watched_changed"
	case errors.Is(err, accounts.ErrWatchedLimit):
		status, code = 409, "watched_limit_reached"
	case errors.Is(err, accounts.ErrMovieNotFound):
		status, code = 404, "movie_not_found"
	case errors.Is(err, accounts.ErrWatchlistChanged), errors.Is(err, accounts.ErrWatchlistUnavailable):
		watchlistHTTPError(w, err)
		return
	case errors.Is(err, accounts.ErrUnauthorized), errors.Is(err, accounts.ErrPending), errors.As(err, &rate):
		accountError(w, err)
		return
	}
	writeError(w, status, code, "Les films vus sont indisponibles ou ont changé. Vérifiez les films vus et la watchlist avant de recommencer.")
}

func (h *accountHTTP) watched(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		watchedHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		if err != nil || len(body) != 0 {
			watchedHTTPError(w, accounts.ErrInvalidInput)
			return
		}
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchedHTTPError(w, err)
		return
	}
	view, err := h.service.Watched(r.Context(), raw)
	if err != nil {
		watchedHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) saveWatched(w http.ResponseWriter, r *http.Request) {
	var input watchedMutation
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.MovieSlug == nil || input.Watched == nil || input.ExpectedWatchlistRevision == nil || (*input.Watched != "true" && *input.Watched != "false") {
		watchedHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchedHTTPError(w, err)
		return
	}
	view, err := h.service.SaveWatched(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.MovieSlug, *input.ExpectedWatchlistRevision, *input.Watched == "true")
	if err != nil {
		watchedHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) saveWatchedPreferences(w http.ResponseWriter, r *http.Request) {
	var input watchedPreferences
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.DefaultVisibility == nil || input.SearchVisibility == nil || input.PlanningVisibility == nil || input.FilmsVisibility == nil || input.UpcomingVisibility == nil || input.HomeVisibility == nil || input.CinemaCityVisibility == nil || input.AutoRemoveWatchlist == nil || (*input.AutoRemoveWatchlist != "true" && *input.AutoRemoveWatchlist != "false") {
		watchedHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchedHTTPError(w, err)
		return
	}
	p := accounts.WatchedPreferences{DefaultVisibility: *input.DefaultVisibility, SearchVisibility: *input.SearchVisibility, PlanningVisibility: *input.PlanningVisibility, FilmsVisibility: *input.FilmsVisibility, UpcomingVisibility: *input.UpcomingVisibility, HomeVisibility: *input.HomeVisibility, CinemaCityVisibility: *input.CinemaCityVisibility, AutoRemoveWatchlist: *input.AutoRemoveWatchlist == "true"}
	view, err := h.service.SaveWatchedPreferences(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, p)
	if err != nil {
		watchedHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}
