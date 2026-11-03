package httpapi

import (
	"net/http"

	"messeances/api/internal/accounts"
)

func (h *accountHTTP) visitActivity(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedUsername *string `json:"expected_username"`
		Scope            *string `json:"scope"`
		LastEventID      *string `json:"last_event_id"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.Scope == nil || input.LastEventID == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.VisitActivity(r.Context(), raw, *input.ExpectedUsername, *input.Scope, *input.LastEventID)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
