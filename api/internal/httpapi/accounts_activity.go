package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"messeances/api/internal/accounts"
)

func parseAccountActivityQuery(raw string, forceQuery bool) (accounts.FollowedActivityQuery, error) {
	q := accounts.FollowedActivityQuery{}
	if forceQuery || len(raw) > 2048 || !utf8.ValidString(raw) {
		return q, accounts.ErrInvalidInput
	}
	if raw != "" {
		for _, field := range strings.Split(raw, "&") {
			if field == "" {
				return q, accounts.ErrInvalidInput
			}
		}
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return q, accounts.ErrInvalidInput
	}
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 || entries[0] == "" || !utf8.ValidString(entries[0]) {
			return q, accounts.ErrInvalidInput
		}
		if key == "limit" {
			if !activityLimit.MatchString(entries[0]) {
				return q, accounts.ErrInvalidInput
			}
			q.Limit, _ = strconv.Atoi(entries[0])
		} else {
			q.Cursor = entries[0]
		}
	}
	return accounts.NormalizeFollowedActivityQuery(q)
}

func (h *accountHTTP) followedActivity(w http.ResponseWriter, r *http.Request) {
	q, err := parseAccountActivityQuery(r.URL.RawQuery, r.URL.ForceQuery)
	if err != nil {
		accountError(w, err)
		return
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
	view, err := h.service.FollowedActivity(r.Context(), raw, q)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
