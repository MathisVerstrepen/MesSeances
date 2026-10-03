package httpapi

import (
	"net/http"

	"messeances/api/internal/accounts"
)

type accountTheaterFollow struct {
	ExpectedUsername *string `json:"expected_username"`
	ExpectedRevision *string `json:"expected_revision"`
	TheaterID        *string `json:"theater_id"`
	Followed         *string `json:"followed"`
}

func (h *accountHTTP) theaterFollows(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.TheaterFollows(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) saveTheaterFollow(w http.ResponseWriter, r *http.Request) {
	var input accountTheaterFollow
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ExpectedRevision == nil || input.TheaterID == nil || input.Followed == nil || (*input.Followed != "true" && *input.Followed != "false") {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.SaveTheaterFollow(r.Context(), raw, *input.ExpectedUsername, *input.ExpectedRevision, *input.TheaterID, *input.Followed == "true")
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
