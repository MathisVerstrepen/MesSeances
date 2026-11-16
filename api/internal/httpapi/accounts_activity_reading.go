package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"messeances/api/internal/accounts"
)

func (h *accountHTTP) activityReading(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.RawQuery) > 4096 || r.URL.ForceQuery || !utf8.ValidString(r.URL.RawQuery) {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	if r.URL.RawQuery != "" {
		for _, field := range strings.Split(r.URL.RawQuery, "&") {
			if field == "" {
				accountError(w, accounts.ErrInvalidInput)
				return
			}
		}
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q["event_id"]) > 100 {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	for key := range q {
		if key != "event_id" {
			accountError(w, accounts.ErrInvalidInput)
			return
		}
	}
	if allowed, retry := h.activityReads.allow(requestIdentityFromContext(r.Context()).publicKey); !allowed {
		accountError(w, &accounts.RateLimitError{RetryAfter: retry})
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.ActivityReading(r.Context(), raw, q["event_id"])
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) saveActivityReading(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedUsername *string `json:"expected_username"`
		ManualReading    *string `json:"manual_reading"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ManualReading == nil || (*input.ManualReading != "true" && *input.ManualReading != "false") {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.SaveActivityReading(r.Context(), raw, *input.ExpectedUsername, *input.ManualReading == "true")
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) saveActivityRead(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedUsername *string `json:"expected_username"`
		EventID          *string `json:"event_id"`
		Consulted        *string `json:"consulted"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.EventID == nil || input.Consulted == nil || (*input.Consulted != "true" && *input.Consulted != "false") {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.SaveActivityRead(r.Context(), raw, *input.ExpectedUsername, *input.EventID, *input.Consulted == "true")
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) markAllActivityConsulted(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedUsername *string `json:"expected_username"`
		LastEventID      *string `json:"last_event_id"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.LastEventID == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.MarkAllActivityConsulted(r.Context(), raw, *input.ExpectedUsername, *input.LastEventID)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
