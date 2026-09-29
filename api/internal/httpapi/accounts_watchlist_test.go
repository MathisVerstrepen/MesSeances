package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
)

const watchlistRoute = "/api/v1/account/watchlist"
const validWatchlistMutation = `{"expected_username":"owner","expected_revision":"0","movie_slug":"film-1","saved":"true"}`
const validWatchlistSearch = `{"expected_username":"owner","query":"Film"}`
const validWatchlistImport = `{"expected_username":"owner","expected_revision":"0","tmdb_id":"42"}`
const validWatchlistSort = `{"expected_username":"owner","expected_revision":"0","sort_order":"title_asc"}`
const validWatchlistPreferences = `{"expected_username":"owner","expected_revision":"0","view_mode":"tags","filter_tag_id":""}`

func TestWatchlistPreferencesStrictTransport(t *testing.T) {
	check := func(body string, status int) {
		t.Helper()
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", watchlistRoute+"/preferences", body))
		if w.Code != status || (status == 400 && !strings.Contains(w.Body.String(), `"code":"invalid_request"`)) {
			t.Fatalf("preferences transport status=%d want=%d", w.Code, status)
		}
		assertAccountHeaders(t, w)
	}
	for _, mode := range []string{"list", "tags"} {
		for _, filter := range []string{"", "1", "9223372036854775807"} {
			body := strings.Replace(validWatchlistPreferences, `"tags"`, `"`+mode+`"`, 1)
			check(strings.Replace(body, `"filter_tag_id":""`, `"filter_tag_id":"`+filter+`"`, 1), 503)
		}
	}
	for _, field := range []string{"expected_username", "expected_revision", "view_mode", "filter_tag_id"} {
		var input map[string]json.RawMessage
		if err := json.Unmarshal([]byte(validWatchlistPreferences), &input); err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"null", "0", "true", "[]", "{}"} {
			input[field] = json.RawMessage(value)
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			check(string(body), 400)
		}
		delete(input, field)
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		check(string(body), 400)
		check(strings.Replace(validWatchlistPreferences, field, strings.ToUpper(field), 1), 400)
		check(strings.TrimSuffix(validWatchlistPreferences, "}")+`,"`+field+`":""}`, 400)
	}
	for _, mode := range []string{"", "LIST", "Tags", "list ", " tags", "grouped"} {
		check(strings.Replace(validWatchlistPreferences, `"tags"`, `"`+mode+`"`, 1), 400)
	}
	for _, filter := range []string{"0", "-1", "+1", "01", " 1", "1 ", "1.0", "1e1", "9223372036854775808", "null"} {
		check(strings.Replace(validWatchlistPreferences, `"filter_tag_id":""`, `"filter_tag_id":"`+filter+`"`, 1), 400)
	}
	for _, body := range []string{
		strings.TrimSuffix(validWatchlistPreferences, "}") + `,"\u0076iew_mode":"list"}`,
		strings.TrimSuffix(validWatchlistPreferences, "}") + `,"\u0066ilter_tag_id":"1"}`,
		strings.TrimSuffix(validWatchlistPreferences, "}") + `,"account_id":"1"}`,
		strings.Replace(validWatchlistPreferences, `"0"`, `"9007199254740992"`, 1),
		strings.Replace(validWatchlistPreferences, `"0"`, `"01"`, 1),
		strings.Replace(validWatchlistPreferences, `tags`, `\ud800`, 1),
		validWatchlistPreferences + `{}`, validWatchlistPreferences + strings.Repeat(" ", 8192), "{\xff}", `[]`, `null`,
	} {
		check(body, 400)
	}
}

func TestWatchlistStrictTransport(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		status           int
	}{
		{"valid", watchlistRoute, validWatchlistMutation, 503},
		{"search", watchlistRoute + "/search", validWatchlistSearch, 503},
		{"import", watchlistRoute + "/import", validWatchlistImport, 503},
		{"sort", watchlistRoute + "/sort", validWatchlistSort, 503},
		{"sort missing owner", watchlistRoute + "/sort", `{"expected_revision":"0","sort_order":"title_asc"}`, 400},
		{"sort missing revision", watchlistRoute + "/sort", `{"expected_username":"owner","sort_order":"title_asc"}`, 400},
		{"sort missing order", watchlistRoute + "/sort", `{"expected_username":"owner","expected_revision":"0"}`, 400},
		{"sort null", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `"title_asc"`, `null`, 1), 400},
		{"sort array", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `"title_asc"`, `[]`, 1), 400},
		{"sort boolean", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `"title_asc"`, `true`, 1), 400},
		{"sort number", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `"title_asc"`, `1`, 1), 400},
		{"sort invalid enum", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `title_asc`, `newest`, 1), 400},
		{"sort padded enum", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `title_asc`, `title_asc `, 1), 400},
		{"sort case enum", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `title_asc`, `TITLE_ASC`, 1), 400},
		{"sort empty enum", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `title_asc`, ``, 1), 400},
		{"sort overflow revision", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `"0"`, `"9007199254740992"`, 1), 400},
		{"sort padded revision", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `"0"`, `"01"`, 1), 400},
		{"sort case field", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `sort_order`, `Sort_order`, 1), 400},
		{"sort unknown", watchlistRoute + "/sort", strings.TrimSuffix(validWatchlistSort, "}") + `,"account_id":"1"}`, 400},
		{"sort duplicate", watchlistRoute + "/sort", strings.TrimSuffix(validWatchlistSort, "}") + `,"sort_order":"title_desc"}`, 400},
		{"sort escaped duplicate", watchlistRoute + "/sort", strings.TrimSuffix(validWatchlistSort, "}") + `,"\u0073ort_order":"title_desc"}`, 400},
		{"sort trailing", watchlistRoute + "/sort", validWatchlistSort + `{}`, 400},
		{"sort invalid utf8", watchlistRoute + "/sort", "{\xff}", 400},
		{"sort surrogate", watchlistRoute + "/sort", strings.Replace(validWatchlistSort, `title_asc`, `\ud800`, 1), 400},
		{"sort body limit", watchlistRoute + "/sort", validWatchlistSort + strings.Repeat(" ", 8192), 400},
		{"missing", watchlistRoute, `{}`, 400},
		{"boolean", watchlistRoute, strings.Replace(validWatchlistMutation, `"true"`, `true`, 1), 400},
		{"wrong boolean", watchlistRoute, strings.Replace(validWatchlistMutation, `"true"`, `"yes"`, 1), 400},
		{"null", watchlistRoute, strings.Replace(validWatchlistMutation, `"film-1"`, `null`, 1), 400},
		{"array", watchlistRoute, strings.Replace(validWatchlistMutation, `"film-1"`, `[]`, 1), 400},
		{"numeric revision", watchlistRoute, strings.Replace(validWatchlistMutation, `"0"`, `0`, 1), 400},
		{"overflow revision", watchlistRoute, strings.Replace(validWatchlistMutation, `"0"`, `"9007199254740992"`, 1), 400},
		{"padded revision", watchlistRoute, strings.Replace(validWatchlistMutation, `"0"`, `"01"`, 1), 400},
		{"case field", watchlistRoute, strings.Replace(validWatchlistMutation, `movie_slug`, `Movie_slug`, 1), 400},
		{"unknown", watchlistRoute, strings.TrimSuffix(validWatchlistMutation, "}") + `,"account_id":"1"}`, 400},
		{"duplicate", watchlistRoute, strings.TrimSuffix(validWatchlistMutation, "}") + `,"movie_slug":"film-2"}`, 400},
		{"escaped duplicate", watchlistRoute, strings.TrimSuffix(validWatchlistMutation, "}") + `,"\u006dovie_slug":"film-2"}`, 400},
		{"trailing", watchlistRoute, validWatchlistMutation + `{}`, 400},
		{"invalid utf8", watchlistRoute, "{\xff}", 400},
		{"surrogate", watchlistRoute, strings.Replace(validWatchlistMutation, `film-1`, `\ud800`, 1), 400},
		{"body limit", watchlistRoute, validWatchlistMutation + strings.Repeat(" ", 8192), 400},
		{"slug url", watchlistRoute, strings.Replace(validWatchlistMutation, `film-1`, `https://evil.example`, 1), 400},
		{"search missing", watchlistRoute + "/search", `{"query":"Film"}`, 400},
		{"search bound", watchlistRoute + "/search", strings.Replace(validWatchlistSearch, "Film", strings.Repeat("é", 201), 1), 400},
		{"search minimum", watchlistRoute + "/search", strings.Replace(validWatchlistSearch, "Film", " x ", 1), 400},
		{"import missing", watchlistRoute + "/import", `{}`, 400},
		{"import numeric", watchlistRoute + "/import", strings.Replace(validWatchlistImport, `"42"`, `42`, 1), 400},
		{"import url", watchlistRoute + "/import", strings.Replace(validWatchlistImport, `42`, `https://evil.example`, 1), 400},
		{"import overflow", watchlistRoute + "/import", strings.Replace(validWatchlistImport, `42`, `9223372036854775808`, 1), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", tc.path, tc.body))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status == 400 && !strings.Contains(w.Body.String(), `"code":"invalid_request"`) {
				t.Fatal("wrong watchlist validation code")
			}
			assertAccountHeaders(t, w)
		})
	}
}

func TestWatchlistSecurityBoundaryAndDisabledRoutes(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{{"GET", watchlistRoute, ""}, {"POST", watchlistRoute, validWatchlistMutation}, {"POST", watchlistRoute + "/sort", validWatchlistSort}, {"POST", watchlistRoute + "/search", validWatchlistSearch}, {"POST", watchlistRoute + "/import", validWatchlistImport},
		{"POST", watchlistRoute + "/preferences", validWatchlistPreferences}, {"POST", watchlistRoute + "/tags", validWatchlistTagCreate}, {"POST", watchlistRoute + "/tags/update", validWatchlistTagUpdate}, {"POST", watchlistRoute + "/tags/delete", validWatchlistTagDelete}, {"POST", watchlistRoute + "/tags/assign", validWatchlistTagAssign}} {
		for _, suffix := range []string{"?", "?username=other"} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, route.method, route.path+suffix, route.body))
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_request"`) {
				t.Fatal("watchlist query accepted")
			}
			assertAccountHeaders(t, w)
		}
		for _, cookie := range []string{"bad", strings.Repeat("A", 43) + "; " + accountCookieName + "=" + strings.Repeat("A", 43)} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			r := theaterPreferenceRequest(t, route.method, route.path, route.body)
			r.Header.Set("Cookie", accountCookieName+"="+cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal("bad cookie accepted")
			}
			assertAccountHeaders(t, w)
		}
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, theaterPreferenceRequest(t, route.method, route.path, route.body))
		if w.Code != 503 {
			t.Fatal("disabled accounts route not safely unavailable")
		}
		assertAccountHeaders(t, w)
		unavailable := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: AccountOptions{Enabled: true}})
		w = httptest.NewRecorder()
		unavailable.ServeHTTP(w, theaterPreferenceRequest(t, route.method, route.path, route.body))
		if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"accounts_unavailable"`) {
			t.Fatal("enabled unavailable accounts route not registered")
		}
		assertAccountHeaders(t, w)
		for _, method := range []string{"PUT", "OPTIONS"} {
			w = httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, method, route.path, route.body))
			if w.Code != 405 {
				t.Fatal("unsupported method accepted")
			}
			assertAccountHeaders(t, w)
		}
		if route.method == "GET" {
			continue
		}
		for _, contentType := range []string{"", "text/plain", "application/json; charset=iso-8859-1"} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			r := theaterPreferenceRequest(t, route.method, route.path, route.body)
			r.Header.Set("Content-Type", contentType)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 400 {
				t.Fatal("invalid content type accepted")
			}
			assertAccountHeaders(t, w)
		}
		for _, header := range []string{"Origin", "X-Messeances-CSRF", "Sec-Fetch-Site"} {
			for _, duplicate := range []bool{false, true} {
				h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
				r := theaterPreferenceRequest(t, route.method, route.path, route.body)
				if duplicate {
					r.Header.Add(header, r.Header.Get(header))
				} else {
					r.Header.Set(header, "foreign")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 403 {
					t.Fatal("CSRF boundary bypassed")
				}
				assertAccountHeaders(t, w)
			}
			if header != "Sec-Fetch-Site" {
				h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
				r := theaterPreferenceRequest(t, route.method, route.path, route.body)
				r.Header.Del(header)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 403 {
					t.Fatal("missing required CSRF header accepted")
				}
				assertAccountHeaders(t, w)
			}
		}
	}
}

func TestWatchlistIndependentIPQuotasAndSafeErrors(t *testing.T) {
	h, err := newAccountHTTP(lifecycleHTTPOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, tc := range []struct {
		limiter    *tokenBucketLimiter
		limit      int
		refill     time.Duration
		handler    http.HandlerFunc
		path, body string
	}{
		{h.watchlistWrites, 120, time.Second / 2, h.saveWatchlist, watchlistRoute, validWatchlistMutation},
		{h.watchlistSearches, 60, time.Second, h.searchWatchlist, watchlistRoute + "/search", validWatchlistSearch},
		{h.watchlistImports, 10, 90 * time.Second, h.importWatchlist, watchlistRoute + "/import", validWatchlistImport},
	} {
		tc.limiter.now = func() time.Time { return now }
		for range tc.limit {
			if ok, _ := tc.limiter.allow(unknownClientKey); !ok {
				t.Fatal("quota too small")
			}
		}
		w := httptest.NewRecorder()
		accountBoundary(h.mutation(tc.handler, tc.limiter)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", tc.path, tc.body))
		if w.Code != 429 || w.Header().Get("Retry-After") == "" {
			t.Fatal("route quota not applied")
		}
		assertAccountHeaders(t, w)
		if tc.path == watchlistRoute {
			w := httptest.NewRecorder()
			accountBoundary(h.mutation(h.saveWatchlistSort, h.watchlistWrites)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", watchlistRoute+"/sort", validWatchlistSort))
			if w.Code != 429 {
				t.Fatal("sort did not share membership IP quota")
			}
			assertAccountHeaders(t, w)
			for _, tag := range []struct {
				handler    http.HandlerFunc
				path, body string
			}{
				{h.saveWatchlistPreferences, "/preferences", validWatchlistPreferences},
				{h.createWatchlistTag, "/tags", validWatchlistTagCreate},
				{h.updateWatchlistTag, "/tags/update", validWatchlistTagUpdate},
				{h.deleteWatchlistTag, "/tags/delete", validWatchlistTagDelete},
				{h.assignWatchlistTag, "/tags/assign", validWatchlistTagAssign},
			} {
				w := httptest.NewRecorder()
				accountBoundary(h.mutation(tag.handler, h.watchlistWrites)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", watchlistRoute+tag.path, tag.body))
				if w.Code != 429 {
					t.Fatal("tag did not share membership IP quota")
				}
				assertAccountHeaders(t, w)
			}
		}
		now = now.Add(tc.refill)
		if ok, _ := tc.limiter.allow(unknownClientKey); !ok {
			t.Fatal("incorrect refill")
		}
	}
	for _, limiter := range []*tokenBucketLimiter{h.login, h.send, h.step, h.theaters} {
		if ok, _ := limiter.allow(unknownClientKey); !ok {
			t.Fatal("watchlist depleted other account quota")
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{accounts.ErrWatchlistTagNameTaken, 409, "watchlist_tag_name_taken"},
		{accounts.ErrWatchlistTagLimit, 409, "watchlist_tag_limit_reached"},
		{accounts.ErrWatchlistTagNotFound, 404, "watchlist_tag_not_found"},
		{accounts.ErrWatchlistMovieNotSaved, 404, "watchlist_movie_not_saved"},
		{accounts.ErrWatchlistChanged, 409, "watchlist_changed"}, {accounts.ErrWatchlistLimit, 409, "watchlist_limit_reached"}, {accounts.ErrWatchlistExternalUnavailable, 503, "watchlist_external_unavailable"}, {accounts.ErrWatchlistUnavailable, 503, "watchlist_unavailable"}, {accounts.ErrMovieNotFound, 404, "movie_not_found"}, {enrichment.ErrMovieNotImportable, 400, "movie_not_importable"}, {errors.New("provider secret body"), 503, "watchlist_unavailable"},
	} {
		w := httptest.NewRecorder()
		watchlistHTTPError(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "username") {
			t.Fatalf("unsafe error %s", w.Body.String())
		}
	}
}

func TestWatchlistRegisteredRoutesIntegration(t *testing.T) {
	p := newBrowserProbe(t)
	var catalog struct {
		Items []schedule.MovieCatalogItem `json:"items"`
	}
	p.request("GET", "/api/v1/movies?include_ended=true", nil, 200, &catalog)
	if len(catalog.Items) == 0 || !strings.HasPrefix(catalog.Items[0].Slug, "film-") {
		t.Fatal("browser fixture has no canonical public movie")
	}
	movie := catalog.Items[0]
	var persistedMovies, snapshots, showtimes int
	if err := p.h.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM public_movies),(SELECT count(*) FROM schedule_snapshot),(SELECT count(*) FROM showtimes)`).Scan(&persistedMovies, &snapshots, &showtimes); err != nil || persistedMovies != len(catalog.Items) || snapshots != 0 || showtimes != 0 {
		t.Fatal("browser fixture must persist catalog identities without provider showtimes")
	}
	input := map[string]string{"expected_username": "watchlist_http", "expected_revision": "0", "movie_slug": movie.Slug, "saved": "true"}
	sortInput := map[string]string{"expected_username": "watchlist_http", "expected_revision": "0", "sort_order": "added_desc"}
	prefsInput := map[string]string{"expected_username": "watchlist_http", "expected_revision": "0", "view_mode": "list", "filter_tag_id": ""}
	p.request("GET", watchlistRoute, nil, 401, nil)
	p.request("POST", watchlistRoute, input, 401, nil)
	p.request("POST", watchlistRoute+"/sort", sortInput, 401, nil)
	p.request("POST", watchlistRoute+"/preferences", prefsInput, 401, nil)
	watchlistTagRouteDenied(t, p, 401)
	p.google(map[string]string{"mode": "login"}, "verified", "/finaliser")
	p.request("GET", watchlistRoute, nil, 403, nil)
	p.request("POST", watchlistRoute, input, 403, nil)
	p.request("POST", watchlistRoute+"/sort", sortInput, 403, nil)
	p.request("POST", watchlistRoute+"/preferences", prefsInput, 403, nil)
	watchlistTagRouteDenied(t, p, 403)
	p.request("POST", "/api/v1/account/username", accountUsername{Username: "watchlist_http"}, 200, nil)
	var view accounts.WatchlistView
	p.request("GET", watchlistRoute, nil, 200, &view)
	if view.Username != "watchlist_http" || view.Revision != "0" || view.SortOrder != "added_desc" || view.ViewMode != "list" || view.FilterTagID != nil || view.Items == nil || view.Tags == nil || len(view.Items) != 0 || view.ExternalSearchAvailable {
		t.Fatal("initial wire mismatch")
	}
	var wire map[string]json.RawMessage
	p.request("POST", watchlistRoute+"/preferences", prefsInput, 200, &wire)
	if string(wire["revision"]) != `"0"` || string(wire["view_mode"]) != `"list"` || string(wire["filter_tag_id"]) != `null` {
		t.Fatal("default preference no-op initialized state or omitted fields")
	}
	p.request("POST", watchlistRoute+"/sort", sortInput, 200, &view)
	if view.Revision != "0" || view.SortOrder != "added_desc" {
		t.Fatal("default sort initialized resource")
	}
	p.request("POST", watchlistRoute, input, 200, &view)
	if view.Revision != "1" || view.SortOrder != "added_desc" || len(view.Items) != 1 || view.Items[0].Slug != movie.Slug || view.Items[0].Title != movie.Title || view.Items[0].AddedAt.IsZero() {
		t.Fatal("saved wire mismatch")
	}
	p.request("POST", watchlistRoute, input, 409, nil)
	var search accounts.WatchlistSearchView
	p.request("POST", watchlistRoute+"/search", map[string]string{"expected_username": "watchlist_http", "query": movie.Title}, 200, &search)
	if len(search.Catalog) != 1 || search.Catalog[0].Slug != movie.Slug || search.Catalog[0].Title != movie.Title || search.External == nil || search.ExternalStatus != "disabled" {
		t.Fatal("local-only search wire mismatch")
	}
	var detail schedule.MovieSchedule
	p.request("GET", "/api/v1/movies/"+view.Items[0].Slug+"/showtimes?date="+time.Now().UTC().Format(time.DateOnly), nil, 200, &detail)
	if detail.Movie.Slug != movie.Slug || detail.Movie.Title != movie.Title || len(detail.Theaters) == 0 {
		t.Fatal("saved fixture movie does not resolve to its public showtimes")
	}
	p.request("POST", watchlistRoute+"/import", map[string]string{"expected_username": "watchlist_http", "expected_revision": "1", "tmdb_id": "42"}, 503, nil)
	input["expected_revision"] = "1"
	input["expected_username"] = "different_owner"
	p.request("POST", watchlistRoute, input, 401, nil)
	input["expected_username"] = "watchlist_http"
	input["saved"] = "false"
	p.request("POST", watchlistRoute, input, 200, &view)
	if view.Revision != "2" || view.Items == nil || len(view.Items) != 0 {
		t.Fatal("remove wire mismatch")
	}
	for i, order := range []string{"title_asc", "title_desc", "release_asc", "release_desc", "added_asc", "added_desc"} {
		sortInput["expected_revision"] = strconv.Itoa(i + 2)
		sortInput["sort_order"] = order
		p.request("POST", watchlistRoute+"/sort", sortInput, 200, &view)
		if view.Revision != strconv.Itoa(i+3) || view.SortOrder != order || view.Items == nil || len(view.Items) != 0 {
			t.Fatal("sort wire mismatch")
		}
		p.request("POST", watchlistRoute+"/sort", sortInput, 409, nil)
		sortInput["expected_revision"] = view.Revision
		p.request("POST", watchlistRoute+"/sort", sortInput, 200, &view)
		p.request("GET", watchlistRoute, nil, 200, &view)
		if view.Revision != strconv.Itoa(i+3) || view.SortOrder != order {
			t.Fatal("sort persistence/no-op failed")
		}
	}
	sortInput["expected_username"] = "different_owner"
	p.request("POST", watchlistRoute+"/sort", sortInput, 401, nil)
	watchlistTagRegisteredCRUD(t, p, view, movie.Slug)
	watchlistPreferencesRegisteredBounds(t, p)
	p.request("POST", "/api/v1/auth/logout", struct{}{}, 204, nil)
	p.request("GET", watchlistRoute, nil, 401, nil)
	p.request("POST", watchlistRoute+"/sort", sortInput, 401, nil)
	p.request("POST", watchlistRoute+"/preferences", prefsInput, 401, nil)
	watchlistTagRouteDenied(t, p, 401)
}

func watchlistPreferencesRegisteredBounds(t *testing.T, p *browserProbe) {
	t.Helper()
	var view accounts.WatchlistView
	p.request("GET", watchlistRoute, nil, 200, &view)
	if len(view.Tags) != 1 {
		t.Fatal("missing remaining tag fixture")
	}
	id := view.Tags[0].ID
	for _, pair := range [][2]string{{"list", id}, {"list", ""}, {"tags", ""}, {"tags", id}} {
		before := view
		input := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "view_mode": pair[0], "filter_tag_id": pair[1]}
		p.request("POST", watchlistRoute+"/preferences", input, 200, &view)
		previous, err := strconv.Atoi(before.Revision)
		if err != nil || view.Revision != strconv.Itoa(previous+1) || !reflect.DeepEqual(view.Items, before.Items) || !reflect.DeepEqual(view.Tags, before.Tags) || view.SortOrder != before.SortOrder || view.ViewMode != pair[0] || (pair[1] == "" && view.FilterTagID != nil) || (pair[1] != "" && (view.FilterTagID == nil || *view.FilterTagID != pair[1])) {
			t.Fatal("registered preference pair write not atomic/preserving")
		}
		p.request("POST", watchlistRoute+"/preferences", input, 409, nil)
		input["expected_revision"] = view.Revision
		var unchanged accounts.WatchlistView
		p.request("POST", watchlistRoute+"/preferences", input, 200, &unchanged)
		if !reflect.DeepEqual(unchanged, view) {
			t.Fatal("registered pair no-op changed snapshot")
		}
	}
	// Foreign and absent well-formed IDs have the same safe response, after CAS.
	var foreignID string
	if err := p.h.pool.QueryRow(t.Context(), `WITH owner AS (INSERT INTO accounts(email,created_at,pending_kind) VALUES('foreign-filter@example.com',now(),'email') RETURNING id) INSERT INTO account_watchlist_tags(account_id,name,name_key) SELECT id,'Private','private' FROM owner RETURNING id::text`).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	input := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "view_mode": "list", "filter_tag_id": foreignID}
	var foreign, missing map[string]any
	p.request("POST", watchlistRoute+"/preferences", input, 404, &foreign)
	input["filter_tag_id"] = "9223372036854775807"
	p.request("POST", watchlistRoute+"/preferences", input, 404, &missing)
	if !reflect.DeepEqual(foreign, missing) {
		t.Fatal("foreign tag disclosed")
	}
	input["expected_revision"] = "0"
	p.request("POST", watchlistRoute+"/preferences", input, 409, nil)
	var unchanged accounts.WatchlistView
	p.request("GET", watchlistRoute, nil, 200, &unchanged)
	if !reflect.DeepEqual(unchanged, view) {
		t.Fatal("lookup failure partially changed pair")
	}
	// Exercise no-op and transactional rollback at the wire revision limit.
	if _, err := p.h.pool.Exec(t.Context(), `UPDATE account_watchlist_state SET revision=9007199254740991 WHERE account_id=(SELECT account_id FROM account_username_claims WHERE username='watchlist_http')`); err != nil {
		t.Fatal(err)
	}
	input["expected_revision"], input["view_mode"], input["filter_tag_id"] = "9007199254740991", "tags", id
	view = accounts.WatchlistView{}
	p.request("POST", watchlistRoute+"/preferences", input, 200, &view)
	if view.Revision != "9007199254740991" || view.ViewMode != "tags" || view.FilterTagID == nil || *view.FilterTagID != id {
		t.Fatal("registered max no-op")
	}
	input["view_mode"], input["filter_tag_id"] = "list", ""
	p.request("POST", watchlistRoute+"/preferences", input, 503, nil)
	p.request("POST", watchlistRoute+"/tags/delete", map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "tag_id": id}, 503, nil)
	p.request("GET", watchlistRoute, nil, 200, &unchanged)
	if !reflect.DeepEqual(unchanged, view) {
		t.Fatal("registered max rollback failed")
	}
}
