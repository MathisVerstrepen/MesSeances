package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityNotificationHTTPBoundary(t *testing.T) {
	const path = "/api/v1/account/activity/notification"
	for _, suffix := range []string{"?", "?owner=another", "?limit=1"} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", path+suffix, nil))
		if w.Code != 400 {
			t.Fatal("query accepted", w.Code)
		}
		assertAccountHeaders(t, w)
	}
	for _, tc := range []struct {
		cookie  string
		pending bool
		want    int
	}{
		{"", false, 401},
		{"admin=anything", false, 401},
		{accountCookieName + "=bad", false, 401},
		{accountCookieName + "=" + strings.Repeat("A", 43), true, 403},
		{accountCookieName + "=" + strings.Repeat("A", 43), false, 200},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, tc.pending)})
		r := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
		r.Header.Set("Cookie", tc.cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("status %d, want %d", w.Code, tc.want)
		}
		assertAccountHeaders(t, w)
		if tc.want == 200 {
			var value map[string]any
			if json.Unmarshal(w.Body.Bytes(), &value) != nil || len(value) != 3 || value["username"] != "owner" || value["follows_revision"] != "0" || value["has_new_activity"] != false {
				t.Fatal("notification contract", w.Body.String())
			}
		}
	}
}
