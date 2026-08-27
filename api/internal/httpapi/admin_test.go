package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/tmdb"
)

type adminReviewStore struct{}

func (adminReviewStore) PendingMatches(context.Context, enrichment.PendingMatchFilter, int, int) ([]enrichment.PendingMatch, error) {
	return []enrichment.PendingMatch{{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200", SourceTitle: "Film A", SourceRuntimeMinutes: 100, SourcePosterURL: "https://static.ugc.fr/posters/200.jpg", SourceDetailURL: "https://evil.example/source", Status: enrichment.StatusUnmatched, Candidates: []enrichment.Candidate{{ID: 42, Title: "Film A", PosterURL: "https://image.tmdb.org/t/p/w500/42.jpg", DetailURL: "https://evil.example/candidate"}}}}, nil
}
func (adminReviewStore) ReviewCandidate(context.Context, string, string, int64) (enrichment.Candidate, int, error) {
	return enrichment.Candidate{ID: 42, Title: "Film A", Score: 1}, 100, nil
}
func (adminReviewStore) ApproveReview(context.Context, string, string, int64, enrichment.Metadata, int, time.Time) error {
	return nil
}
func (adminReviewStore) RejectReview(context.Context, string, string, time.Time) error { return nil }

type adminProvider struct{}

type adminReviewFilterStore struct {
	adminReviewStore
	filter enrichment.PendingMatchFilter
	limit  int
	offset int
	calls  int
}

func (s *adminReviewFilterStore) PendingMatches(_ context.Context, filter enrichment.PendingMatchFilter, limit, offset int) ([]enrichment.PendingMatch, error) {
	s.filter, s.limit, s.offset = filter, limit, offset
	s.calls++
	status := enrichment.StatusUnmatched
	if filter == enrichment.PendingMatchFilterRejected {
		status = enrichment.StatusRejected
	}
	return []enrichment.PendingMatch{{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200", SourceTitle: "Film A", Status: status}}, nil
}

func (adminProvider) Details(_ context.Context, movieID int64) (tmdb.Details, error) {
	return tmdb.Details{ID: movieID, Title: "Film A", OriginalTitle: "Film A", Runtime: 100, Genres: []string{}}, nil
}

type adminReviewErrorStore struct {
	adminReviewStore
	preflightErr error
	approvalErr  error
}

func (s adminReviewErrorStore) ReviewCandidate(context.Context, string, string, int64) (enrichment.Candidate, int, error) {
	return enrichment.Candidate{ID: 42, Score: 1}, 100, s.preflightErr
}

func (s adminReviewErrorStore) ApproveReview(context.Context, string, string, int64, enrichment.Metadata, int, time.Time) error {
	return s.approvalErr
}

type adminMismatchedProvider struct{}

func (adminMismatchedProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{ID: 43, Title: "Wrong", Runtime: 100, Genres: []string{}}, nil
}

func configuredAdminHandler(t *testing.T, password string, now func() time.Time) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, now)
	return testHandlerWithAdmin(t, AdminOptions{Password: password, SessionSecret: "test-session-secret", Reviews: reviews, Now: now})
}

func adminRequest(handler http.Handler, method, target, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.10:1234"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func loginAdmin(t *testing.T, handler http.Handler, password string) *http.Cookie {
	t.Helper()
	response := adminRequest(handler, http.MethodPost, "/api/v1/admin/login", `{"password":"`+password+`"}`, "http://localhost:3000", nil)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("login status=%d body=%s cookies=%v", response.Code, response.Body.String(), response.Result().Cookies())
	}
	return response.Result().Cookies()[0]
}

func TestAdminSessionSecurityAndRotation(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	handler := configuredAdminHandler(t, "synthetic-admin-password", clock)
	cookie := loginAdmin(t, handler, "synthetic-admin-password")
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 12*60*60 || cookie.Path != "/api/v1/admin" || cookie.Value == "" || strings.Contains(cookie.Value, "synthetic") {
		t.Fatalf("cookie=%+v", cookie)
	}
	list := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches", "", "", cookie)
	if list.Code != http.StatusOK || list.Header().Get("Cache-Control") != "no-store" || !strings.Contains(list.Body.String(), `"source_poster_url":"https://static.ugc.fr/posters/200.jpg"`) || !strings.Contains(list.Body.String(), `"source_detail_url":"https://www.ugc.fr/film.html?id=200"`) || !strings.Contains(list.Body.String(), `"status":"unmatched"`) || !strings.Contains(list.Body.String(), `"poster_url":"https://image.tmdb.org/t/p/w500/42.jpg"`) || !strings.Contains(list.Body.String(), `"detail_url":"https://www.themoviedb.org/movie/42?language=fr-FR"`) || strings.Contains(list.Body.String(), "evil.example") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	rotated := configuredAdminHandler(t, "rotated-password", clock)
	status := adminRequest(rotated, http.MethodGet, "/api/v1/admin/session", "", "", cookie)
	if status.Code != http.StatusOK || strings.TrimSpace(status.Body.String()) != `{"authenticated":true}` {
		t.Fatalf("rotated status=%d body=%s", status.Code, status.Body.String())
	}
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, clock)
	secretRotated := testHandlerWithAdmin(t, AdminOptions{Password: "rotated-password", SessionSecret: "different-session-secret", Reviews: reviews, Now: clock})
	status = adminRequest(secretRotated, http.MethodGet, "/api/v1/admin/session", "", "", cookie)
	if strings.TrimSpace(status.Body.String()) != `{"authenticated":false}` {
		t.Fatalf("secret-rotated body=%s", status.Body.String())
	}
	oldExpiry := now.Add(time.Hour)
	payload := "v1." + strconv.FormatInt(oldExpiry.Unix(), 10)
	oldKey := sha256.Sum256([]byte("messeances-admin-session-v1\x00synthetic-admin-password"))
	mac := hmac.New(sha256.New, oldKey[:])
	_, _ = mac.Write([]byte(payload))
	oldCookie := &http.Cookie{Name: adminCookieName, Value: payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}
	status = adminRequest(handler, http.MethodGet, "/api/v1/admin/session", "", "", oldCookie)
	if strings.TrimSpace(status.Body.String()) != `{"authenticated":false}` {
		t.Fatalf("legacy cookie body=%s", status.Body.String())
	}
	now = now.Add(12 * time.Hour)
	status = adminRequest(handler, http.MethodGet, "/api/v1/admin/session", "", "", cookie)
	if !strings.Contains(status.Body.String(), `"authenticated":false`) {
		t.Fatalf("expired body=%s", status.Body.String())
	}
}

func TestAdminPendingMatchesStatusFilter(t *testing.T) {
	store := &adminReviewFilterStore{}
	reviews := enrichment.NewReviewService(store, nil, nil)
	handler := testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews})
	cookie := loginAdmin(t, handler, "password")

	tests := []struct {
		name       string
		target     string
		wantFilter enrichment.PendingMatchFilter
		wantStatus string
	}{
		{name: "omitted defaults unresolved", target: "/api/v1/admin/tmdb-matches?limit=20&offset=40", wantFilter: enrichment.PendingMatchFilterUnresolved, wantStatus: enrichment.StatusUnmatched},
		{name: "explicit unresolved", target: "/api/v1/admin/tmdb-matches?status=unresolved&limit=20&offset=40", wantFilter: enrichment.PendingMatchFilterUnresolved, wantStatus: enrichment.StatusUnmatched},
		{name: "rejected", target: "/api/v1/admin/tmdb-matches?status=rejected&limit=20&offset=40", wantFilter: enrichment.PendingMatchFilterRejected, wantStatus: enrichment.StatusRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := adminRequest(handler, http.MethodGet, test.target, "", "", cookie)
			if response.Code != http.StatusOK || store.filter != test.wantFilter || store.limit != 20 || store.offset != 40 || !strings.Contains(response.Body.String(), `"status":"`+test.wantStatus+`"`) {
				t.Fatalf("status=%d filter=%q pagination=%d/%d body=%s", response.Code, store.filter, store.limit, store.offset, response.Body.String())
			}
		})
	}

	for _, target := range []string{
		"/api/v1/admin/tmdb-matches?status=matched",
		"/api/v1/admin/tmdb-matches?status=",
		"/api/v1/admin/tmdb-matches?status=unresolved&status=rejected",
	} {
		calls := store.calls
		response := adminRequest(handler, http.MethodGet, target, "", "", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_query", "Filtre de statut invalide.")
		if store.calls != calls {
			t.Fatalf("invalid filter reached store: target=%s calls=%d", target, store.calls)
		}
	}
}

func TestAdminOriginAuthorizationAndCORS(t *testing.T) {
	handler := configuredAdminHandler(t, "password", time.Now)
	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/login", `{"password":"password"}`, "https://evil.example", nil)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	reject := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/reject", "", "https://evil.example", cookie)
	assertAPIError(t, reject, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	preflight := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/api/v1/admin/login", nil)
	preflight.Header.Set("Origin", "http://localhost:3000")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("CORS headers=%v", response.Header())
	}
}

func TestAdminApproveRejectAndLogoutSuccess(t *testing.T) {
	handler := configuredAdminHandler(t, "password", time.Now)
	cookie := loginAdmin(t, handler, "password")
	approve := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", `{"tmdb_id":42}`, "http://localhost:3000", cookie)
	if approve.Code != http.StatusOK || strings.TrimSpace(approve.Body.String()) != `{"status":"matched"}` {
		t.Fatalf("approve status=%d body=%s", approve.Code, approve.Body.String())
	}
	manual := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", `{"tmdb_id":999}`, "http://localhost:3000", cookie)
	if manual.Code != http.StatusOK || strings.TrimSpace(manual.Body.String()) != `{"status":"matched"}` {
		t.Fatalf("manual approve status=%d body=%s", manual.Code, manual.Body.String())
	}
	reject := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/reject", "", "http://localhost:3000", cookie)
	if reject.Code != http.StatusOK || strings.TrimSpace(reject.Body.String()) != `{"status":"rejected"}` {
		t.Fatalf("reject status=%d body=%s", reject.Code, reject.Body.String())
	}
	logout := adminRequest(handler, http.MethodPost, "/api/v1/admin/logout", "", "http://localhost:3000", cookie)
	if logout.Code != http.StatusOK || strings.TrimSpace(logout.Body.String()) != `{"authenticated":false}` || len(logout.Result().Cookies()) != 1 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout status=%d body=%s cookies=%v", logout.Code, logout.Body.String(), logout.Result().Cookies())
	}
}

func TestAdminApproveFailsClosedWithoutTMDBProvider(t *testing.T) {
	reviews := enrichment.NewReviewService(adminReviewStore{}, nil, time.Now)
	handler := testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews})
	cookie := loginAdmin(t, handler, "password")
	approve := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", `{"tmdb_id":42}`, "http://localhost:3000", cookie)
	assertAPIError(t, approve, http.StatusServiceUnavailable, "review_unavailable", "Service de validation indisponible.")
}

func TestAdminApproveRejectsInvalidBodies(t *testing.T) {
	handler := configuredAdminHandler(t, "password", time.Now)
	cookie := loginAdmin(t, handler, "password")
	tests := []string{
		`{"tmdb_id":0}`,
		`{"tmdb_id":-1}`,
		`{"tmdb_id":"42"}`,
		`{"tmdb_id":42,"unknown":true}`,
		`{"tmdb_id":42} {"tmdb_id":43}`,
		`{"tmdb_id":`,
		`{"tmdb_id":42,"padding":"` + strings.Repeat("x", maxAdminBody) + `"}`,
	}
	for _, body := range tests {
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", body, "http://localhost:3000", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	}
}

func TestAdminApproveErrorMappingsRemainUnchanged(t *testing.T) {
	tests := []struct {
		name     string
		store    enrichment.ReviewStore
		provider interface {
			Details(context.Context, int64) (tmdb.Details, error)
		}
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "missing", store: adminReviewErrorStore{preflightErr: enrichment.ErrReviewNotFound}, provider: adminProvider{}, wantStatus: http.StatusNotFound, wantCode: "not_found", wantMessage: "Correspondance introuvable."},
		{name: "conflict", store: adminReviewErrorStore{preflightErr: enrichment.ErrReviewConflict}, provider: adminProvider{}, wantStatus: http.StatusConflict, wantCode: "review_conflict", wantMessage: "Cette correspondance ne peut plus être modifiée."},
		{name: "preflight failure", store: adminReviewErrorStore{preflightErr: errors.New("database failed")}, provider: adminProvider{}, wantStatus: http.StatusBadGateway, wantCode: "review_failed", wantMessage: "La correspondance n'a pas pu être modifiée."},
		{name: "approval failure", store: adminReviewErrorStore{approvalErr: errors.New("commit failed")}, provider: adminProvider{}, wantStatus: http.StatusBadGateway, wantCode: "review_failed", wantMessage: "La correspondance n'a pas pu être modifiée."},
		{name: "provider ID mismatch", store: adminReviewStore{}, provider: adminMismatchedProvider{}, wantStatus: http.StatusBadGateway, wantCode: "review_failed", wantMessage: "La correspondance n'a pas pu être modifiée."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reviews := enrichment.NewReviewService(test.store, test.provider, time.Now)
			handler := testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews})
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/ugc/200/approve", `{"tmdb_id":42}`, "http://localhost:3000", cookie)
			assertAPIError(t, response, test.wantStatus, test.wantCode, test.wantMessage)
		})
	}
}

func TestAdminGenericFailuresBodyLimitAndMissingConfig(t *testing.T) {
	handler := configuredAdminHandler(t, "password", time.Now)
	oversized := adminRequest(handler, http.MethodPost, "/api/v1/admin/login", `{"password":"`+strings.Repeat("x", maxAdminBody)+`"}`, "http://localhost:3000", nil)
	assertAPIError(t, oversized, http.StatusUnauthorized, "authentication_failed", "Authentification impossible.")
	for index := 0; index < 6; index++ {
		failed := adminRequest(handler, http.MethodPost, "/api/v1/admin/login", `{"password":"wrong"}`, "http://localhost:3000", nil)
		assertAPIError(t, failed, http.StatusUnauthorized, "authentication_failed", "Authentification impossible.")
	}
	unconfigured := testHandlerWithAdmin(t, AdminOptions{})
	unavailable := adminRequest(unconfigured, http.MethodGet, "/api/v1/admin/session", "", "", nil)
	assertAPIError(t, unavailable, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
}
