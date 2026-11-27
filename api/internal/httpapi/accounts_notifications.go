package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"messeances/api/internal/accounts"
)

type accountNotificationRule struct {
	ExpectedUsername *string `json:"expected_username"`
	ID               *string `json:"id"`
	Kind             *string `json:"kind"`
	TheaterScope     *string `json:"theater_scope"`
	TheaterIDs       *string `json:"theater_ids"`
	IncludeReturns   *string `json:"include_returns"`
	Enabled          *string `json:"enabled"`
	// Optional for clients predating push; absent means disabled.
	Push  *string `json:"push"`
	Email *string `json:"email"`
}

type accountNotificationRuleDelete struct {
	ExpectedUsername *string `json:"expected_username"`
	ID               *string `json:"id"`
}

type accountNotificationRead struct {
	ExpectedUsername *string `json:"expected_username"`
	IDs              *string `json:"ids"`
}

type accountNotificationReadAll struct {
	ExpectedUsername *string `json:"expected_username"`
	ThroughID        *string `json:"through_id"`
}

func accountBool(raw *string) (bool, bool) {
	if raw == nil || (*raw != "true" && *raw != "false") {
		return false, false
	}
	return *raw == "true", true
}

func accountList(raw string) []string {
	if raw == "" {
		return []string{}
	}
	return strings.Split(raw, ",")
}

func parseNotificationRule(input accountNotificationRule) (accounts.NotificationRuleInput, bool) {
	includeReturns, okReturns := accountBool(input.IncludeReturns)
	enabled, okEnabled := accountBool(input.Enabled)
	push, email := false, false
	if input.Push != nil {
		var ok bool
		if push, ok = accountBool(input.Push); !ok {
			return accounts.NotificationRuleInput{}, false
		}
	}
	if input.Email != nil {
		var ok bool
		if email, ok = accountBool(input.Email); !ok {
			return accounts.NotificationRuleInput{}, false
		}
	}
	if input.ExpectedUsername == nil || input.Kind == nil || input.TheaterScope == nil || input.TheaterIDs == nil || !okReturns || !okEnabled {
		return accounts.NotificationRuleInput{}, false
	}
	return accounts.NotificationRuleInput{Kind: *input.Kind, TheaterScope: *input.TheaterScope, TheaterIDs: accountList(*input.TheaterIDs), IncludeReturns: includeReturns, Enabled: enabled, Push: push, Email: email}, true
}

func parseAccountNotificationsQuery(raw string, forceQuery bool) (int, string, error) {
	if forceQuery || len(raw) > 512 || !utf8.ValidString(raw) {
		return 0, "", accounts.ErrInvalidInput
	}
	if raw != "" {
		for _, field := range strings.Split(raw, "&") {
			if field == "" {
				return 0, "", accounts.ErrInvalidInput
			}
		}
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return 0, "", accounts.ErrInvalidInput
	}
	limit, before := 0, ""
	for key, entries := range values {
		if (key != "limit" && key != "before") || len(entries) != 1 || entries[0] == "" {
			return 0, "", accounts.ErrInvalidInput
		}
		if key == "limit" {
			if !activityLimit.MatchString(entries[0]) {
				return 0, "", accounts.ErrInvalidInput
			}
			limit, _ = strconv.Atoi(entries[0])
		} else {
			before = entries[0]
		}
	}
	limit, _, err = accounts.NormalizeNotificationsQuery(limit, before)
	return limit, before, err
}

func (h *accountHTTP) notifications(w http.ResponseWriter, r *http.Request) {
	limit, before, err := parseAccountNotificationsQuery(r.URL.RawQuery, r.URL.ForceQuery)
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
	view, err := h.service.Notifications(r.Context(), raw, limit, before)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) notificationStatus(w http.ResponseWriter, r *http.Request) {
	if allowed, retry := h.activityReads.allow(requestIdentityFromContext(r.Context()).publicKey); !allowed {
		accountError(w, &accounts.RateLimitError{RetryAfter: retry})
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.NotificationStatus(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var input accountNotificationRead
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.IDs == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.MarkNotificationsRead(r.Context(), raw, *input.ExpectedUsername, accountList(*input.IDs))
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var input accountNotificationReadAll
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ThroughID == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.MarkAllNotificationsRead(r.Context(), raw, *input.ExpectedUsername, *input.ThroughID)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) notificationRules(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.NotificationRules(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) createNotificationRule(w http.ResponseWriter, r *http.Request) {
	var input accountNotificationRule
	if !accountJSON(w, r, &input) {
		return
	}
	rule, ok := parseNotificationRule(input)
	if !ok || input.ID != nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.CreateNotificationRule(r.Context(), raw, *input.ExpectedUsername, rule)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) updateNotificationRule(w http.ResponseWriter, r *http.Request) {
	var input accountNotificationRule
	if !accountJSON(w, r, &input) {
		return
	}
	rule, ok := parseNotificationRule(input)
	if !ok || input.ID == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.UpdateNotificationRule(r.Context(), raw, *input.ExpectedUsername, *input.ID, rule)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) deleteNotificationRule(w http.ResponseWriter, r *http.Request) {
	var input accountNotificationRuleDelete
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ID == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.DeleteNotificationRule(r.Context(), raw, *input.ExpectedUsername, *input.ID)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type accountNotificationTest struct {
	ExpectedUsername *string `json:"expected_username"`
	Channel          *string `json:"channel"`
}

func (h *accountHTTP) sendTestNotification(w http.ResponseWriter, r *http.Request) {
	var input accountNotificationTest
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.Channel == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.SendTestNotification(r.Context(), raw, *input.ExpectedUsername, *input.Channel)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
