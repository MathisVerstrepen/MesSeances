package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityReadingQueryAndAuthorization(t *testing.T) {
	for _, raw := range []string{"?", "?&", "?owner=other", "?event_id=", "?event_id=01", "?event_id=0", "?event_id=9223372036854775808", "?event_id=%FF", "?" + strings.Repeat("event_id=1&", 100) + "event_id=1"} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v1/account/activity/reading"+raw, nil)
		r.Header.Set("Cookie", accountCookieName+"="+strings.Repeat("A", 43))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid reading query status %d", w.Code)
		}
		assertAccountHeaders(t, w)
	}
	for _, tc := range []struct {
		cookie  bool
		pending bool
		want    int
	}{
		{false, false, 401}, {true, true, 403}, {true, false, 200},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, tc.pending)})
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/api/v1/account/activity/reading", nil)
		if tc.cookie {
			r.Header.Set("Cookie", accountCookieName+"="+strings.Repeat("A", 43))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("reading state status %d, want %d", w.Code, tc.want)
		}
		assertAccountHeaders(t, w)
	}
}

func TestActivityReadingHTTPBoundary(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"reading", `{"expected_username":"owner","manual_reading":"true"}`},
		{"read", `{"expected_username":"owner","event_id":"9007199254740993","consulted":"false"}`},
		{"read-all", `{"expected_username":"owner","last_event_id":"9007199254740993"}`},
	} {
		for _, csrf := range []bool{false, true} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
			r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/account/activity/"+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "https://messeances.fr")
			want := 403
			if csrf {
				r.Header.Set("X-Messeances-CSRF", "1")
				want = 401
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s CSRF=%t: %d want %d", tc.path, csrf, w.Code, want)
			}
			assertAccountHeaders(t, w)
		}
	}
	for _, tc := range []struct{ path, body string }{
		{"reading", `{}`},
		{"reading", `{"expected_username":"owner","manual_reading":true}`},
		{"reading", `{"expected_username":"owner","manual_reading":"yes"}`},
		{"reading", `{"expected_username":"owner","manual_reading":null}`},
		{"read", `{"expected_username":"owner","event_id":42,"consulted":"true"}`},
		{"read", `{"expected_username":"owner","event_id":"42"}`},
		{"read", `{"expected_username":"owner","event_id":"42","consulted":"true","consulted":"false"}`},
		{"read-all", `{"expected_username":"owner"}`},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/account/activity/"+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://messeances.fr")
		r.Header.Set("X-Messeances-CSRF", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid %s: %d want 400", tc.path, w.Code)
		}
	}
}
