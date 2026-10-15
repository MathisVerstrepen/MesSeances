package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
)

const watchedRoute = "/api/v1/account/watched"
const validWatchedMutation = `{"expected_username":"owner","expected_revision":"0","movie_slug":"film-1","watched":"true","expected_watchlist_revision":"0"}`
const validWatchedPreferences = `{"expected_username":"owner","expected_revision":"0","default_visibility":"hide","search_visibility":"inherit","planning_visibility":"inherit","films_visibility":"inherit","upcoming_visibility":"inherit","home_visibility":"inherit","cinema_city_visibility":"inherit","auto_remove_watchlist":"false"}`

func TestWatchedStrictTransport(t *testing.T) {
	for _, route := range []struct{ path, body string }{{watchedRoute, validWatchedMutation}, {watchedRoute + "/preferences", validWatchedPreferences}} {
		check := func(body string, want int) {
			t.Helper()
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", route.path, body))
			if w.Code != want || (want == 400 && !strings.Contains(w.Body.String(), `"code":"invalid_request"`)) {
				t.Fatalf("%s status=%d want=%d body=%s", route.path, w.Code, want, body)
			}
			assertAccountHeaders(t, w)
		}
		check(route.body, 503)
		check(route.body+strings.Repeat(" ", 8192-len(route.body)), 503)
		check(route.body+strings.Repeat(" ", 8193-len(route.body)), 400)
		var input map[string]string
		if err := json.Unmarshal([]byte(route.body), &input); err != nil {
			t.Fatal(err)
		}
		for field, original := range input {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(route.body), &raw); err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"null", "1", "true", "false", "[]", "{}"} {
				raw[field] = json.RawMessage(value)
				body, err := json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				check(string(body), 400)
			}
			delete(raw, field)
			body, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			check(string(body), 400)
			check(strings.Replace(route.body, field, strings.ToUpper(field), 1), 400)
			check(strings.TrimSuffix(route.body, "}")+`,"`+field+`":"`+original+`"}`, 400)
			check(strings.TrimSuffix(route.body, "}")+`,"\u00`+"65"+`xpected_username":"owner"}`, 400)
			invalid := []string{}
			switch {
			case strings.HasSuffix(field, "revision"):
				invalid = []string{"", "-1", "+1", "01", " 1", "1 ", "1.0", "1e1", "9007199254740992"}
			case strings.HasSuffix(field, "visibility"):
				invalid = []string{"", "SHOW", "hide ", " inherit", "auto"}
				if field == "default_visibility" {
					invalid = append(invalid, "inherit")
				}
			case field == "watched" || field == "auto_remove_watchlist":
				invalid = []string{"", "TRUE", "False", "true ", "1", "yes"}
			case field == "movie_slug":
				invalid = []string{"", "https://evil.example", "film/1", strings.Repeat("a", 129)}
			}
			for _, value := range invalid {
				copy := map[string]string{}
				for key, v := range input {
					copy[key] = v
				}
				copy[field] = value
				b, err := json.Marshal(copy)
				if err != nil {
					t.Fatal(err)
				}
				check(string(b), 400)
			}
		}
		for _, body := range []string{`[]`, `null`, `{}`, "{\xff}", route.body + `{}`, strings.TrimSuffix(route.body, "}") + `,"unknown":"x"}`, strings.Replace(route.body, `owner`, `\ud800`, 1)} {
			check(body, 400)
		}
		check(strings.Replace(route.body, `"expected_revision":"0"`, `"expected_revision":"9007199254740991"`, 1), 503)
		if route.path == watchedRoute {
			check(strings.Replace(route.body, `"true"`, `"false"`, 1), 503)
		} else {
			for _, value := range []string{"show", "hide", "inherit"} {
				check(strings.ReplaceAll(route.body, `"inherit"`, `"`+value+`"`), 503)
			}
		}
	}
}

func TestWatchedSecurityBoundaryAndDisabledRoutes(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{{"GET", watchedRoute, ""}, {"POST", watchedRoute, validWatchedMutation}, {"POST", watchedRoute + "/preferences", validWatchedPreferences}} {
		serve := func(options AccountOptions, r *http.Request) *httptest.ResponseRecorder {
			t.Helper()
			w := httptest.NewRecorder()
			NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: options}).ServeHTTP(w, r)
			assertAccountHeaders(t, w)
			for key, want := range map[string]string{"X-Robots-Tag": "noindex, nofollow", "X-Content-Type-Options": "nosniff", "Cross-Origin-Resource-Policy": "same-origin", "Access-Control-Allow-Origin": ""} {
				if w.Header().Get(key) != want {
					t.Fatal("private boundary lost", key)
				}
			}
			return w
		}
		for _, enabled := range []bool{false, true} {
			w := serve(AccountOptions{Enabled: enabled}, theaterPreferenceRequest(t, route.method, route.path, route.body))
			code := "accounts_disabled"
			if enabled {
				code = "accounts_unavailable"
			}
			if w.Code != 503 || !strings.Contains(w.Body.String(), code) {
				t.Fatal("disabled route missing")
			}
		}
		for _, method := range []string{"PUT", "OPTIONS", "DELETE"} {
			if w := serve(lifecycleHTTPOptions(t), theaterPreferenceRequest(t, method, route.path, route.body)); w.Code != 405 {
				t.Fatal("unsupported method accepted")
			}
		}
		for _, suffix := range []string{"?", "?username=other"} {
			w := serve(lifecycleHTTPOptions(t), theaterPreferenceRequest(t, route.method, route.path+suffix, route.body))
			if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_request") {
				t.Fatal("query accepted")
			}
		}
		for _, cookie := range []string{"bad", strings.Repeat("A", 43) + "; " + accountCookieName + "=" + strings.Repeat("A", 43)} {
			r := theaterPreferenceRequest(t, route.method, route.path, route.body)
			r.Header.Set("Cookie", accountCookieName+"="+cookie)
			if w := serve(lifecycleHTTPOptions(t), r); w.Code != 401 {
				t.Fatal("invalid cookie accepted")
			}
		}
		if route.method == "GET" {
			for _, body := range []string{" ", "{}", strings.Repeat("x", 8193)} {
				if w := serve(lifecycleHTTPOptions(t), theaterPreferenceRequest(t, "GET", route.path, body)); w.Code != 400 {
					t.Fatal("GET body accepted")
				}
			}
			continue
		}
		for _, header := range []string{"Origin", "X-Messeances-CSRF", "Sec-Fetch-Site"} {
			for _, value := range []string{"foreign", "duplicate", ""} {
				if value == "" && header == "Sec-Fetch-Site" {
					continue
				}
				r := theaterPreferenceRequest(t, route.method, route.path, route.body)
				switch value {
				case "duplicate":
					r.Header.Add(header, r.Header.Get(header))
				case "":
					r.Header.Del(header)
				default:
					r.Header.Set(header, value)
				}
				if w := serve(lifecycleHTTPOptions(t), r); w.Code != 403 {
					t.Fatal("origin/CSRF/fetch boundary bypassed")
				}
			}
		}
		for _, content := range []string{"", "text/plain", "application/json; charset=iso-8859-1"} {
			r := theaterPreferenceRequest(t, route.method, route.path, route.body)
			r.Header.Set("Content-Type", content)
			if w := serve(lifecycleHTTPOptions(t), r); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_request") {
				t.Fatal("invalid content type accepted")
			}
		}
	}
}

func TestWatchedSharedIPQuotaAndSafeErrors(t *testing.T) {
	registered := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
	for i := range 120 {
		path, body := watchedRoute, validWatchedMutation
		if i%2 == 1 {
			path, body = watchedRoute+"/preferences", validWatchedPreferences
		}
		w := httptest.NewRecorder()
		registered.ServeHTTP(w, theaterPreferenceRequest(t, "POST", path, body))
		if w.Code != 503 {
			t.Fatal("incorrect registered write quota", w.Code)
		}
	}
	for _, path := range []string{watchedRoute, watchedRoute + "/preferences", watchlistRoute} {
		w := httptest.NewRecorder()
		registered.ServeHTTP(w, theaterPreferenceRequest(t, "POST", path, `{}`))
		if w.Code != 429 || w.Header().Get("Retry-After") == "" {
			t.Fatal("registered writes have additive quota", path, w.Code)
		}
		assertAccountHeaders(t, w)
	}
	h, err := newAccountHTTP(lifecycleHTTPOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.watchlistWrites.now = func() time.Time { return now }
	for range 120 {
		if ok, _ := h.watchlistWrites.allow(unknownClientKey); !ok {
			t.Fatal("incorrect quota")
		}
	}
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{{watchedRoute, h.saveWatched}, {watchedRoute + "/preferences", h.saveWatchedPreferences}, {watchlistRoute, h.saveWatchlist}} {
		w := httptest.NewRecorder()
		accountBoundary(h.mutation(route.handler, h.watchlistWrites)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", route.path, `{}`))
		if w.Code != 429 || w.Header().Get("Retry-After") == "" {
			t.Fatal("writes do not share IP quota")
		}
		assertAccountHeaders(t, w)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{accounts.ErrWatchedChanged, 409, "watched_changed"}, {accounts.ErrWatchedLimit, 409, "watched_limit_reached"},
		{accounts.ErrWatchedUnavailable, 503, "watched_unavailable"}, {accounts.ErrWatchlistChanged, 409, "watchlist_changed"},
		{accounts.ErrWatchlistUnavailable, 503, "watchlist_unavailable"}, {accounts.ErrMovieNotFound, 404, "movie_not_found"},
		{accounts.ErrInvalidInput, 400, "invalid_request"}, {accounts.ErrUnauthorized, 401, "authentication_required"},
		{accounts.ErrPending, 403, "onboarding_required"}, {&accounts.RateLimitError{RetryAfter: 1}, 429, "rate_limited"},
		{errors.New("secret SQL body"), 503, "watched_unavailable"},
	} {
		w := httptest.NewRecorder()
		watchedHTTPError(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("unsafe error", w.Body.String())
		}
	}
}

func TestWatchedPublicResponsesAccountNeutral(t *testing.T) {
	h := testHandlerWithOptions(t, HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
	for _, path := range []string{
		"/api/v1/movies?date=2026-08-15", "/api/v1/movies?date=2026-08-15&q=film",
		"/api/v1/timeline?date=2026-08-15", "/api/v1/search/slot?date=2026-08-15&start_after=08:00&finish_before=02:00",
		"/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15",
	} {
		var first string
		for i, cookie := range []string{"", accountCookieName + "=" + strings.Repeat("A", 43), accountCookieName + "=" + strings.Repeat("B", 43)} {
			r := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
			r.Header.Set("Cookie", cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || strings.Contains(w.Body.String(), "watched") || strings.Contains(w.Body.String(), "visibility") {
				t.Fatalf("public personalization %s status=%d", path, w.Code)
			}
			if i == 0 {
				first = w.Body.String()
			} else if w.Body.String() != first {
				t.Fatal("cookie changed public response")
			}
		}
	}
}
