package httpapi

import (
	"io"
	"net/http"
	"net/url"

	"messeances/api/internal/accounts"
)

const digestUnsubscribePath = "/api/v1/notifications/unsubscribe"

type accountDigest struct {
	ExpectedUsername *string `json:"expected_username"`
	Frequency        *string `json:"frequency"`
}

func (h *accountHTTP) notificationDigest(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.NotificationDigest(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) saveNotificationDigest(w http.ResponseWriter, r *http.Request) {
	var input accountDigest
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.Frequency == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.SaveNotificationDigest(r.Context(), raw, *input.ExpectedUsername, *input.Frequency)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// unsubscribeDigest implements RFC 8058 one-click unsubscription. Mail
// providers POST without cookies or Origin, so the signed token in the query is
// the only authority, and it can only turn the digest off. GET is never
// accepted, so link scanners cannot unsubscribe.
func (h *accountHTTP) unsubscribeDigest(w http.ResponseWriter, r *http.Request) {
	if allowed, retry := h.unsubscribes.allow(requestIdentityFromContext(r.Context()).publicKey); !allowed {
		accountError(w, &accounts.RateLimitError{RetryAfter: retry})
		return
	}
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 4096))
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) != 1 || len(values["token"]) != 1 || len(values["token"][0]) > 64 {
		accountError(w, accounts.ErrInvalidLink)
		return
	}
	if err := h.service.UnsubscribeDigest(r.Context(), values["token"][0]); err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"unsubscribed": true})
}
