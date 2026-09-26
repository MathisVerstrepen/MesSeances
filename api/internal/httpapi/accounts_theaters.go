package httpapi

import (
	"net/http"
	"strings"

	"messeances/api/internal/accounts"
)

// Pointers distinguish missing required fields from an explicit empty selection.
// accountJSONLimit rejects null and every non-string JSON value before decoding.
type accountTheaterSelection struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	TheaterIDs       *string `json:"theater_ids"`
}

func (h *accountHTTP) theaterPreferences(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.TheaterPreferences(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) saveTheaterPreferences(w http.ResponseWriter, r *http.Request) {
	var input accountTheaterSelection
	if !accountJSONLimit(w, r, &input, 1<<20) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.TheaterIDs == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	ids := []string{}
	if *input.TheaterIDs != "" {
		ids = strings.Split(*input.TheaterIDs, ",")
	}
	view, err := h.service.SaveTheaterPreferences(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, ids)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
