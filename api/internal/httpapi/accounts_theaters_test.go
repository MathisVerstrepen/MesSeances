package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
)

const theaterPreferencePath = "/api/v1/account/theaters"
const emptyTheaterSelection = `{"expected_username":"owner","expected_revision":"0","theater_ids":""}`

func theaterPreferenceRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	r.Header.Set("Origin", "https://messeances.fr")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Messeances-CSRF", "1")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

func TestTheaterPreferenceStrictInput(t *testing.T) {
	maxIDs := make([]string, 4096)
	for i := range maxIDs {
		maxIDs[i] = fmt.Sprintf("%0128d", i)
	}
	encode := func(revision, ids string) string {
		b, err := json.Marshal(map[string]string{"expected_username": "owner", "expected_revision": revision, "theater_ids": ids})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"empty", emptyTheaterSelection, 503},
		{"maximum", encode("9007199254740991", strings.Join(maxIDs, ",")), 503},
		{"too many", encode("0", strings.Join(append(maxIDs, "extra"), ",")), 400},
		{"missing all", `{}`, 400},
		{"missing ids", `{"expected_username":"owner","expected_revision":"0"}`, 400},
		{"missing owner", `{"expected_revision":"0","theater_ids":""}`, 400},
		{"missing revision", `{"expected_username":"owner","theater_ids":""}`, 400},
		{"null", strings.Replace(emptyTheaterSelection, `"theater_ids":""`, `"theater_ids":null`, 1), 400},
		{"array", strings.Replace(emptyTheaterSelection, `"theater_ids":""`, `"theater_ids":[]`, 1), 400},
		{"number", strings.Replace(emptyTheaterSelection, `"expected_revision":"0"`, `"expected_revision":0`, 1), 400},
		{"duplicate", strings.Replace(emptyTheaterSelection, `"theater_ids":""`, `"theater_ids":"","theater_ids":"ugc-1"`, 1), 400},
		{"escaped duplicate", strings.Replace(emptyTheaterSelection, `"theater_ids":""`, `"theater_ids":"","\u0074heater_ids":"ugc-1"`, 1), 400},
		{"case alias", strings.Replace(emptyTheaterSelection, "theater_ids", "Theater_ids", 1), 400},
		{"extra", strings.TrimSuffix(emptyTheaterSelection, "}") + `,"extra":"x"}`, 400},
		{"trailing", emptyTheaterSelection + ` {}`, 400},
		{"invalid utf8", encode("0", "ugc-1")[:1] + "\xff", 400},
		{"surrogate", strings.Replace(emptyTheaterSelection, `"theater_ids":""`, `"theater_ids":"\ud800"`, 1), 400},
		{"body bound", emptyTheaterSelection + strings.Repeat(" ", 1<<20), 400},
		{"revision syntax", encode("01", ""), 400},
		{"revision overflow", encode("9007199254740992", ""), 400},
		{"duplicate id", encode("0", "ugc-1,ugc-1"), 400},
		{"empty token", encode("0", "ugc-1,"), 400},
		{"space", encode("0", "ugc-1, ugc-2"), 400},
		{"bad id", encode("0", "ugc/1"), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", theaterPreferencePath, tc.body))
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			assertAccountHeaders(t, w)
		})
	}
}

func TestTheaterPreferenceSecurityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, header, value string
		duplicate           bool
		status              int
	}{
		{"missing origin", "Origin", "", false, 403},
		{"foreign origin", "Origin", "https://evil.example", false, 403},
		{"duplicate origin", "Origin", "https://messeances.fr", true, 403},
		{"missing csrf", "X-Messeances-CSRF", "", false, 403},
		{"duplicate csrf", "X-Messeances-CSRF", "1", true, 403},
		{"cross site", "Sec-Fetch-Site", "cross-site", false, 403},
		{"same site", "Sec-Fetch-Site", "same-site", false, 403},
		{"duplicate metadata", "Sec-Fetch-Site", "same-origin", true, 403},
		{"form", "Content-Type", "application/x-www-form-urlencoded", false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			r := theaterPreferenceRequest(t, "POST", theaterPreferencePath, emptyTheaterSelection)
			if tc.duplicate {
				r.Header.Add(tc.header, tc.value)
			} else {
				r.Header.Set(tc.header, tc.value)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			assertAccountHeaders(t, w)
		})
	}
	for _, method := range []string{"GET", "POST"} {
		for _, suffix := range []string{"?", "?owner=other"} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, method, theaterPreferencePath+suffix, emptyTheaterSelection))
			if w.Code != 400 {
				t.Fatal("query accepted")
			}
			assertAccountHeaders(t, w)
		}
		for _, cookie := range []string{"bad", strings.Repeat("A", 43) + "; " + accountCookieName + "=" + strings.Repeat("A", 43)} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			r := theaterPreferenceRequest(t, method, theaterPreferencePath, emptyTheaterSelection)
			r.Header.Set("Cookie", accountCookieName+"="+cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal("invalid or duplicate cookie accepted")
			}
			assertAccountHeaders(t, w)
		}
	}
}

func TestTheaterPreferenceUnavailableRoutes(t *testing.T) {
	for _, options := range []AccountOptions{{}, {Enabled: true}, {Enabled: true, Service: lifecycleHTTPOptions(t).Service, Origin: "http://evil.example"}, lifecycleHTTPOptions(t)} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: options})
		for _, method := range []string{"GET", "POST", "PUT", "OPTIONS"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, method, theaterPreferencePath, emptyTheaterSelection))
			want := 503
			if method == "PUT" || method == "OPTIONS" {
				want = 405
			}
			if w.Code != want {
				t.Fatalf("%s status=%d want=%d", method, w.Code, want)
			}
			assertAccountHeaders(t, w)
		}
	}
}

func TestTheaterPreferenceLimiterAndConflict(t *testing.T) {
	h, err := newAccountHTTP(lifecycleHTTPOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.theaters.now = func() time.Time { return now }
	for range 120 {
		if ok, _ := h.theaters.allow(unknownClientKey); !ok {
			t.Fatal("preference capacity too small")
		}
	}
	if ok, retry := h.theaters.allow(unknownClientKey); ok || retry <= 0 {
		t.Fatal("preference limit not applied")
	}
	w := httptest.NewRecorder()
	accountBoundary(h.mutation(h.saveTheaterPreferences, h.theaters)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", theaterPreferencePath, emptyTheaterSelection))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("mutation ignored preference limiter")
	}
	assertAccountHeaders(t, w)
	now = now.Add(500 * time.Millisecond)
	if ok, _ := h.theaters.allow(unknownClientKey); !ok {
		t.Fatal("preference quota did not refill at 120/minute")
	}
	for _, limiter := range []*tokenBucketLimiter{h.login, h.send, h.step} {
		if ok, _ := limiter.allow(unknownClientKey); !ok {
			t.Fatal("preference clicks depleted identity quota")
		}
	}
	w = httptest.NewRecorder()
	accountError(w, accounts.ErrTheaterSelectionChanged)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"theater_selection_changed"`) || strings.Contains(w.Body.String(), "theater_ids") || strings.Contains(w.Body.String(), "username") {
		t.Fatal("unsafe conflict envelope")
	}
}

func TestTheaterPreferenceRegisteredRoutesIntegration(t *testing.T) {
	p := newBrowserProbe(t)
	input := map[string]string{"expected_username": "theater_http", "expected_revision": "0", "theater_ids": "unknown-z,ugc-1"}
	p.request("GET", theaterPreferencePath, nil, 401, nil)
	p.request("POST", theaterPreferencePath, input, 401, nil)
	p.google(map[string]string{"mode": "login"}, "verified", "/finaliser")
	p.request("GET", theaterPreferencePath, nil, 403, nil)
	p.request("POST", theaterPreferencePath, input, 403, nil)
	p.request("POST", "/api/v1/account/username", accountUsername{Username: "theater_http"}, 200, nil)
	var view accounts.TheaterPreferencesView
	p.request("GET", theaterPreferencePath, nil, 200, &view)
	if view.Username != "theater_http" || view.Revision != "0" || view.TheaterIDs == nil || len(view.TheaterIDs) != 0 {
		t.Fatal("unset JSON contract")
	}
	p.request("POST", theaterPreferencePath, input, 200, &view)
	if view.Revision != "1" || !slices.Equal(view.TheaterIDs, []string{"ugc-1", "unknown-z"}) {
		t.Fatal("save JSON contract")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := *p.client
	client.Jar = jar
	other := &browserProbe{t: t, h: p.h, client: &client}
	other.google(map[string]string{"mode": "login"}, "verified", "/compte")
	other.request("GET", theaterPreferencePath, nil, 200, &view)
	if view.Revision != "1" || !slices.Equal(view.TheaterIDs, []string{"ugc-1", "unknown-z"}) {
		t.Fatal("second browser not synchronized")
	}
	other.request("POST", theaterPreferencePath, input, 409, nil)
	input["expected_revision"] = "1"
	input["expected_username"] = "different_owner"
	other.request("POST", theaterPreferencePath, input, 401, nil)
	input["expected_username"] = "theater_http"
	input["theater_ids"] = ""
	other.request("POST", theaterPreferencePath, input, 200, &view)
	p.request("GET", theaterPreferencePath, nil, 200, &view)
	if view.Revision != "2" || view.TheaterIDs == nil || len(view.TheaterIDs) != 0 {
		t.Fatal("empty JSON contract or session convergence")
	}
	p.request("POST", "/api/v1/auth/logout", struct{}{}, 204, nil)
	p.request("GET", theaterPreferencePath, nil, 401, nil)
}
