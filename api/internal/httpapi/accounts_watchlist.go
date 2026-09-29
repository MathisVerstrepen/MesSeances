package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"messeances/api/internal/accounts"
	"messeances/api/internal/enrichment"
)

type watchlistMutation struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	MovieSlug        *string `json:"movie_slug"`
	Saved            *string `json:"saved"`
}
type watchlistSearch struct {
	ExpectedUsername *string `json:"expected_username"`
	Query            *string `json:"query"`
}
type watchlistImport struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	TMDBID           *string `json:"tmdb_id"`
}

type watchlistSort struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	SortOrder        *string `json:"sort_order"`
}

type watchlistPreferences struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	ViewMode         *string `json:"view_mode"`
	FilterTagID      *string `json:"filter_tag_id"`
}

func watchlistPath(path string) bool {
	return path == "/api/v1/account/watchlist" || strings.HasPrefix(path, "/api/v1/account/watchlist/")
}

func accountInvalidInput(w http.ResponseWriter, r *http.Request) {
	if watchlistPath(r.URL.Path) {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	accountError(w, accounts.ErrInvalidInput)
}

func watchlistHTTPError(w http.ResponseWriter, err error) {
	status, code := 503, "watchlist_unavailable"
	var rate *accounts.RateLimitError
	switch {
	case errors.Is(err, accounts.ErrInvalidInput):
		status, code = 400, "invalid_request"
	case errors.Is(err, accounts.ErrWatchlistChanged):
		status, code = 409, "watchlist_changed"
	case errors.Is(err, accounts.ErrWatchlistLimit):
		status, code = 409, "watchlist_limit_reached"
	case errors.Is(err, accounts.ErrWatchlistTagNameTaken):
		status, code = 409, "watchlist_tag_name_taken"
	case errors.Is(err, accounts.ErrWatchlistTagLimit):
		status, code = 409, "watchlist_tag_limit_reached"
	case errors.Is(err, accounts.ErrWatchlistTagNotFound):
		status, code = 404, "watchlist_tag_not_found"
	case errors.Is(err, accounts.ErrWatchlistMovieNotSaved):
		status, code = 404, "watchlist_movie_not_saved"
	case errors.Is(err, accounts.ErrMovieNotFound):
		status, code = 404, "movie_not_found"
	case errors.Is(err, enrichment.ErrMovieNotImportable):
		status, code = 400, "movie_not_importable"
	case errors.Is(err, accounts.ErrWatchlistExternalUnavailable):
		code = "watchlist_external_unavailable"
	case errors.Is(err, accounts.ErrUnauthorized), errors.Is(err, accounts.ErrPending), errors.As(err, &rate):
		accountError(w, err)
		return
	}
	writeError(w, status, code, "La watchlist est indisponible ou a changé. Vérifiez son état avant de recommencer.")
}

func (h *accountHTTP) watchlist(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.Watchlist(r.Context(), raw)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) saveWatchlist(w http.ResponseWriter, r *http.Request) {
	var input watchlistMutation
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.MovieSlug == nil || input.Saved == nil || (*input.Saved != "true" && *input.Saved != "false") {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.SaveWatchlist(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.MovieSlug, *input.Saved == "true")
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) searchWatchlist(w http.ResponseWriter, r *http.Request) {
	var input watchlistSearch
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.Query == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.SearchWatchlist(r.Context(), raw, *input.ExpectedUsername, *input.Query)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) saveWatchlistSort(w http.ResponseWriter, r *http.Request) {
	var input watchlistSort
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.SortOrder == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.SaveWatchlistSort(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.SortOrder)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) importWatchlist(w http.ResponseWriter, r *http.Request) {
	var input watchlistImport
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.TMDBID == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.ImportWatchlist(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.TMDBID)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) saveWatchlistPreferences(w http.ResponseWriter, r *http.Request) {
	var input watchlistPreferences
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.ViewMode == nil || input.FilterTagID == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.SaveWatchlistPreferences(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.ViewMode, *input.FilterTagID)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}
