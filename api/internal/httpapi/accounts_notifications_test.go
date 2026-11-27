package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

const notificationRulePath = "/api/v1/account/notification-rules"
const validNotificationRule = `{"expected_username":"owner","kind":"cinema_activity","theater_scope":"custom","theater_ids":"ugc-1,ugc-2","include_returns":"true","enabled":"true"}`

func TestNotificationRuleStrictInput(t *testing.T) {
	withID := strings.Replace(validNotificationRule, `{`, `{"id":"7",`, 1)
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"create", notificationRulePath, validNotificationRule, 503},
		{"followed", notificationRulePath, strings.Replace(strings.Replace(validNotificationRule, "custom", "followed", 1), "ugc-1,ugc-2", "", 1), 503},
		{"update", notificationRulePath + "/update", withID, 503},
		{"delete", notificationRulePath + "/delete", `{"expected_username":"owner","id":"7"}`, 503},
		{"create with id", notificationRulePath, withID, 400},
		{"update without id", notificationRulePath + "/update", validNotificationRule, 400},
		{"update bad id", notificationRulePath + "/update", strings.Replace(withID, `"7"`, `"07"`, 1), 400},
		{"delete bad id", notificationRulePath + "/delete", `{"expected_username":"owner","id":"0"}`, 400},
		{"delete missing id", notificationRulePath + "/delete", `{"expected_username":"owner"}`, 400},
		{"unknown kind", notificationRulePath, strings.Replace(validNotificationRule, "cinema_activity", "push", 1), 400},
		{"unknown scope", notificationRulePath, strings.Replace(validNotificationRule, `"custom"`, `"all"`, 1), 400},
		{"custom empty", notificationRulePath, strings.Replace(validNotificationRule, "ugc-1,ugc-2", "", 1), 400},
		{"followed ids", notificationRulePath, strings.Replace(validNotificationRule, "custom", "followed", 1), 400},
		{"duplicate ids", notificationRulePath, strings.Replace(validNotificationRule, "ugc-1,ugc-2", "ugc-1,ugc-1", 1), 400},
		{"invalid id", notificationRulePath, strings.Replace(validNotificationRule, "ugc-1,ugc-2", "ugc/1", 1), 400},
		{"watchlist returns", notificationRulePath, strings.Replace(validNotificationRule, "cinema_activity", "watchlist_screening", 1), 400},
		{"boolean casing", notificationRulePath, strings.Replace(validNotificationRule, `"enabled":"true"`, `"enabled":"True"`, 1), 400},
		{"json boolean", notificationRulePath, strings.Replace(validNotificationRule, `"enabled":"true"`, `"enabled":true`, 1), 400},
		{"missing enabled", notificationRulePath, strings.Replace(validNotificationRule, `,"enabled":"true"`, "", 1), 400},
		{"missing username", notificationRulePath, strings.Replace(validNotificationRule, `"expected_username":"owner",`, "", 1), 400},
		{"extra", notificationRulePath, strings.TrimSuffix(validNotificationRule, "}") + `,"push":"true"}`, 400},
		{"read", "/api/v1/account/notifications/read", `{"expected_username":"owner","ids":"1,2"}`, 503},
		{"read empty", "/api/v1/account/notifications/read", `{"expected_username":"owner","ids":""}`, 400},
		{"read invalid", "/api/v1/account/notifications/read", `{"expected_username":"owner","ids":"1,x"}`, 400},
		{"read too many", "/api/v1/account/notifications/read", `{"expected_username":"owner","ids":"` + strings.Repeat("1,", 100) + `1"}`, 400},
		{"read all", "/api/v1/account/notifications/read-all", `{"expected_username":"owner","through_id":"9"}`, 503},
		{"read all zero", "/api/v1/account/notifications/read-all", `{"expected_username":"owner","through_id":"0"}`, 400},
		{"read all missing", "/api/v1/account/notifications/read-all", `{"expected_username":"owner"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", tc.path, tc.body))
			if w.Code != tc.status {
				t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			assertAccountHeaders(t, w)
		})
	}
}

func TestNotificationMutationsRequireBrowserOrigin(t *testing.T) {
	for _, path := range []string{notificationRulePath, notificationRulePath + "/update", notificationRulePath + "/delete", "/api/v1/account/notifications/read", "/api/v1/account/notifications/read-all"} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
		r := theaterPreferenceRequest(t, "POST", path, validNotificationRule)
		r.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s cross-origin status %d", path, w.Code)
		}
	}
}

func TestNotificationReadQueries(t *testing.T) {
	for _, tc := range []struct {
		target string
		status int
	}{
		{"/api/v1/account/notifications", 503},
		{"/api/v1/account/notifications?limit=50&before=12", 503},
		{"/api/v1/account/notifications?limit=51", 400},
		{"/api/v1/account/notifications?limit=-1", 400},
		{"/api/v1/account/notifications?before=0", 400},
		{"/api/v1/account/notifications?before=01", 400},
		{"/api/v1/account/notifications?before=1&before=2", 400},
		{"/api/v1/account/notifications?cursor=1", 400},
		{"/api/v1/account/notifications?limit=1&", 400},
		{"/api/v1/account/notifications?", 400},
		{"/api/v1/account/notifications/status", 503},
		{"/api/v1/account/notifications/status?x=1", 400},
		{"/api/v1/account/notification-rules", 503},
		{"/api/v1/account/notification-rules?x=1", 400},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", tc.target, nil))
		if w.Code != tc.status {
			t.Fatalf("%s status %d want %d", tc.target, w.Code, tc.status)
		}
		assertAccountHeaders(t, w)
	}
}

func TestNotificationRoutesWhenAccountsDisabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: AccountOptions{Enabled: enabled, Origin: "https://messeances.fr"}})
		for _, tc := range []struct{ method, path string }{
			{"GET", "/api/v1/account/notifications"}, {"GET", "/api/v1/account/notifications/status"},
			{"POST", "/api/v1/account/notifications/read"}, {"POST", "/api/v1/account/notifications/read-all"},
			{"GET", notificationRulePath}, {"POST", notificationRulePath},
			{"POST", notificationRulePath + "/update"}, {"POST", notificationRulePath + "/delete"},
		} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, tc.method, tc.path, "{}"))
			if w.Code != 503 {
				t.Fatalf("enabled=%t %s %s status %d", enabled, tc.method, tc.path, w.Code)
			}
		}
	}
}
