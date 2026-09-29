package httpapi

import (
	"net/http"

	"messeances/api/internal/accounts"
)

type watchlistTagCreate struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	Name             *string `json:"name"`
	Color            *string `json:"color"`
}

type watchlistTagUpdate struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	TagID            *string `json:"tag_id"`
	Name             *string `json:"name"`
	Color            *string `json:"color"`
}

type watchlistTagDelete struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	TagID            *string `json:"tag_id"`
}

type watchlistTagAssign struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	MovieSlug        *string `json:"movie_slug"`
	TagID            *string `json:"tag_id"`
	Assigned         *string `json:"assigned"`
}

func (h *accountHTTP) createWatchlistTag(w http.ResponseWriter, r *http.Request) {
	var input watchlistTagCreate
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.Name == nil || input.Color == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.CreateWatchlistTag(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.Name, *input.Color)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) updateWatchlistTag(w http.ResponseWriter, r *http.Request) {
	var input watchlistTagUpdate
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.TagID == nil || input.Name == nil || input.Color == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.UpdateWatchlistTag(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.TagID, *input.Name, *input.Color)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) deleteWatchlistTag(w http.ResponseWriter, r *http.Request) {
	var input watchlistTagDelete
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.TagID == nil {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.DeleteWatchlistTag(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.TagID)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (h *accountHTTP) assignWatchlistTag(w http.ResponseWriter, r *http.Request) {
	var input watchlistTagAssign
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.MovieSlug == nil || input.TagID == nil || input.Assigned == nil || (*input.Assigned != "true" && *input.Assigned != "false") {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	view, err := h.service.AssignWatchlistTag(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.MovieSlug, *input.TagID, *input.Assigned == "true")
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	writeJSON(w, 200, view)
}
