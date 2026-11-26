package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/tmdb"
)

const syntheticAdminToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type adminSearchProvider struct {
	adminProvider
	query      string
	searchErr  error
	detailsErr error
	detailsID  int64
	candidates []tmdb.Candidate
}

func (p *adminSearchProvider) Search(_ context.Context, query string) ([]tmdb.Candidate, error) {
	p.query = query
	return p.candidates, p.searchErr
}

func (p *adminSearchProvider) Details(ctx context.Context, id int64) (tmdb.Details, error) {
	if p.detailsErr != nil {
		return tmdb.Details{}, p.detailsErr
	}
	details, err := p.adminProvider.Details(ctx, id)
	if p.detailsID != 0 {
		details.ID = p.detailsID
	}
	details.ReleaseDate, details.OriginalLanguage, details.IMDBID = "2026-01-02", "fr", "tt1234567"
	return details, err
}

func tokenAdminHandler(t *testing.T, token string, logs *bytes.Buffer, search AdminTMDBSearcher) http.Handler {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC) }
	options := AdminOptions{Password: "synthetic-admin-password", SessionSecret: "test-session-secret", APIToken: token, TMDBSearch: search, Reviews: enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, now), Now: now}
	if logs != nil {
		options.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	return testHandlerWithAdmin(t, options)
}

func tokenRequest(handler http.Handler, method, target, body string, headers map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:1234"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestAdminTokenAuthorizesReadsAndMutationsWithoutOrigin(t *testing.T) {
	var logs bytes.Buffer
	handler := tokenAdminHandler(t, syntheticAdminToken, &logs, nil)

	session := tokenRequest(handler, http.MethodGet, "/api/v1/admin/session", "", bearer(syntheticAdminToken), nil)
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"authenticated":true`) {
		t.Fatalf("session status=%d body=%s", session.Code, session.Body.String())
	}
	list := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches", "", bearer(syntheticAdminToken), nil)
	if list.Code != http.StatusOK || list.Header().Get("Cache-Control") != "no-store" || len(list.Result().Cookies()) != 0 || !strings.Contains(list.Body.String(), `"source_movie_id":"200"`) {
		t.Fatalf("list status=%d body=%s cookies=%v", list.Code, list.Body.String(), list.Result().Cookies())
	}
	approve := tokenRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", `{"tmdb_id":42}`, map[string]string{"Authorization": "bearer " + syntheticAdminToken}, nil)
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), `"status":"matched"`) {
		t.Fatalf("approve status=%d body=%s", approve.Code, approve.Body.String())
	}
	if !strings.Contains(logs.String(), `"msg":"admin_token_mutation"`) || !strings.Contains(logs.String(), `"path":"/api/v1/admin/tmdb-matches/ugc/200/approve"`) || strings.Contains(logs.String(), syntheticAdminToken) {
		t.Fatalf("logs=%s", logs.String())
	}
	if strings.Count(logs.String(), "admin_token_mutation") != 1 {
		t.Fatalf("reads must not be logged as mutations: %s", logs.String())
	}
}

func TestAdminTokenRejectsInvalidCredentials(t *testing.T) {
	handler := tokenAdminHandler(t, syntheticAdminToken, nil, nil)
	cookie := loginAdmin(t, handler, "synthetic-admin-password")
	for _, test := range []struct {
		name   string
		header string
		cookie *http.Cookie
	}{
		{"wrong token", "Bearer " + strings.Repeat("f", 64), nil},
		{"wrong scheme", "Basic " + syntheticAdminToken, nil},
		{"missing token", "Bearer", nil},
		{"empty token", "Bearer ", nil},
		{"padded token", "Bearer  " + syntheticAdminToken, nil},
		{"password as token", "Bearer synthetic-admin-password", nil},
		{"invalid header with valid cookie", "Bearer " + strings.Repeat("f", 64), cookie},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := map[string]string{"Authorization": test.header}
			list := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches", "", headers, test.cookie)
			if list.Code != http.StatusUnauthorized || !strings.Contains(list.Body.String(), `"code":"unauthorized"`) {
				t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
			}
			session := tokenRequest(handler, http.MethodGet, "/api/v1/admin/session", "", headers, test.cookie)
			if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"authenticated":false`) {
				t.Fatalf("session status=%d body=%s", session.Code, session.Body.String())
			}
		})
	}

	duplicate := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/tmdb-matches", nil)
	duplicate.RemoteAddr = "192.0.2.10:1234"
	duplicate.Header.Add("Authorization", "Bearer "+syntheticAdminToken)
	duplicate.Header.Add("Authorization", "Bearer "+syntheticAdminToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, duplicate)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate header status=%d", response.Code)
	}
}

func TestAdminTokenDisabledWhenUnconfigured(t *testing.T) {
	handler := tokenAdminHandler(t, "", nil, nil)
	for _, header := range []string{"Bearer ", "Bearer " + syntheticAdminToken} {
		response := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches", "", map[string]string{"Authorization": header}, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("header=%q status=%d body=%s", header, response.Code, response.Body.String())
		}
	}
}

func TestAdminTokenKeepsBrowserProtections(t *testing.T) {
	handler := tokenAdminHandler(t, syntheticAdminToken, nil, nil)
	login := tokenRequest(handler, http.MethodPost, "/api/v1/admin/login", `{"password":"synthetic-admin-password"}`, bearer(syntheticAdminToken), nil)
	if login.Code != http.StatusForbidden || len(login.Result().Cookies()) != 0 {
		t.Fatalf("login without origin status=%d cookies=%v", login.Code, login.Result().Cookies())
	}
	cookie := loginAdmin(t, handler, "synthetic-admin-password")
	crossSite := tokenRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", `{"tmdb_id":42}`, map[string]string{"Origin": "https://evil.example"}, cookie)
	if crossSite.Code != http.StatusForbidden || !strings.Contains(crossSite.Body.String(), `"code":"origin_forbidden"`) {
		t.Fatalf("cookie mutation without origin status=%d body=%s", crossSite.Code, crossSite.Body.String())
	}
	preflight := tokenRequest(handler, http.MethodOptions, "/api/v1/admin/tmdb-matches/ugc/200/approve", "", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": http.MethodPost, "Access-Control-Request-Headers": "authorization"}, nil)
	if strings.Contains(strings.ToLower(preflight.Header().Get("Access-Control-Allow-Headers")), "authorization") {
		t.Fatalf("CORS must not allow browser Authorization headers: %v", preflight.Header())
	}
}

func TestAdminTMDBSearch(t *testing.T) {
	provider := &adminSearchProvider{candidates: []tmdb.Candidate{{ID: 42, Title: "Film A", OriginalTitle: "Movie A", ReleaseDate: "2026-01-02", PosterURL: "https://image.tmdb.org/t/p/w500/42.jpg"}, {ID: 43, Title: "Film B"}}}
	handler := tokenAdminHandler(t, syntheticAdminToken, nil, provider)

	if response := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search?query=film", "", nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", response.Code)
	}
	response := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search?query=%20Film%20A%20", "", bearer(syntheticAdminToken), nil)
	body := response.Body.String()
	if response.Code != http.StatusOK || provider.query != "Film A" || !strings.Contains(body, `{"id":42,"title":"Film A","original_title":"Movie A","release_date":"2026-01-02","poster_url":"https://image.tmdb.org/t/p/w500/42.jpg","detail_url":"https://www.themoviedb.org/movie/42?language=fr-FR"}`) || !strings.Contains(body, `{"id":43,"title":"Film B","detail_url":"https://www.themoviedb.org/movie/43?language=fr-FR"}`) {
		t.Fatalf("search status=%d query=%q body=%s", response.Code, provider.query, body)
	}
	provider.candidates = nil
	if empty := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search?query=none", "", bearer(syntheticAdminToken), nil); empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"items":[]`) {
		t.Fatalf("empty status=%d body=%s", empty.Code, empty.Body.String())
	}
	for _, target := range []string{"/api/v1/admin/tmdb-search", "/api/v1/admin/tmdb-search?query=", "/api/v1/admin/tmdb-search?query=%20", "/api/v1/admin/tmdb-search?query=a&query=b", "/api/v1/admin/tmdb-search?query=%FF", "/api/v1/admin/tmdb-search?query=" + strings.Repeat("a", 201)} {
		if invalid := tokenRequest(handler, http.MethodGet, target, "", bearer(syntheticAdminToken), nil); invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"invalid_query"`) {
			t.Fatalf("target=%s status=%d body=%s", target, invalid.Code, invalid.Body.String())
		}
	}
	provider.searchErr = errors.New("synthetic provider body")
	if failed := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search?query=film", "", bearer(syntheticAdminToken), nil); failed.Code != http.StatusBadGateway || strings.Contains(failed.Body.String(), "synthetic") {
		t.Fatalf("failed status=%d body=%s", failed.Code, failed.Body.String())
	}
}

func TestAdminTMDBMovieDetails(t *testing.T) {
	provider := &adminSearchProvider{}
	handler := tokenAdminHandler(t, syntheticAdminToken, nil, provider)
	response := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search/42", "", bearer(syntheticAdminToken), nil)
	if response.Code != http.StatusOK || response.Body.String() != `{"id":42,"title":"Film A","original_title":"Film A","original_language":"fr","release_date":"2026-01-02","runtime_minutes":100,"genres":[],"imdb_id":"tt1234567","detail_url":"https://www.themoviedb.org/movie/42?language=fr-FR"}`+"\n" {
		t.Fatalf("details status=%d body=%q", response.Code, response.Body.String())
	}
	for _, id := range []string{"0", "-1", "042", "abc", "9223372036854775808"} {
		if invalid := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search/"+id, "", bearer(syntheticAdminToken), nil); invalid.Code != http.StatusBadRequest {
			t.Fatalf("id=%s status=%d body=%s", id, invalid.Code, invalid.Body.String())
		}
	}
	provider.detailsID = 43
	if mismatched := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search/42", "", bearer(syntheticAdminToken), nil); mismatched.Code != http.StatusBadGateway {
		t.Fatalf("mismatched status=%d", mismatched.Code)
	}
	provider.detailsErr = tmdb.ErrNotFound
	if missing := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search/42", "", bearer(syntheticAdminToken), nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
	provider.detailsErr = errors.New("synthetic provider body")
	if failed := tokenRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-search/42", "", bearer(syntheticAdminToken), nil); failed.Code != http.StatusBadGateway || strings.Contains(failed.Body.String(), "synthetic") {
		t.Fatalf("failed status=%d body=%s", failed.Code, failed.Body.String())
	}
}

func TestAdminTMDBSearchUnavailableWithoutProvider(t *testing.T) {
	handler := tokenAdminHandler(t, syntheticAdminToken, nil, nil)
	for _, target := range []string{"/api/v1/admin/tmdb-search?query=film", "/api/v1/admin/tmdb-search/42"} {
		if response := tokenRequest(handler, http.MethodGet, target, "", bearer(syntheticAdminToken), nil); response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"tmdb_unavailable"`) {
			t.Fatalf("target=%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}
