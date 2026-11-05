package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityVisitHTTPBoundary(t *testing.T) {
	const path = "/api/v1/account/activity/visit"
	const body = `{"expected_username":"owner","scope":"followed","last_event_id":"10"}`
	for _, tc := range []struct {
		body string
		csrf bool
		want int
	}{
		{body, false, 403},
		{body, true, 401},
		{`{}`, true, 400},
		{`{"expected_username":"owner","scope":"followed"}`, true, 400},
		{`{"expected_username":"owner","scope":"followed","last_event_id":10}`, true, 400},
		{`{"expected_username":"owner","scope":"followed","last_event_id":null}`, true, 400},
		{`{"expected_username":"owner","scope":"followed","scope":"theater:ugc-1","last_event_id":"10"}`, true, 400},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		r := httptest.NewRequestWithContext(t.Context(), "POST", path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://messeances.fr")
		if tc.csrf {
			r.Header.Set("X-Messeances-CSRF", "1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("visit status %d, want %d", w.Code, tc.want)
		}
		assertAccountHeaders(t, w)
	}
}
