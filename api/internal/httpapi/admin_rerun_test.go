package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"messeances/api/internal/enrichment"
)

type adminRerunner struct {
	summary enrichment.RerunSummary
	err     error
	calls   int
}

func (r *adminRerunner) Rerun(context.Context) (enrichment.RerunSummary, error) {
	r.calls++
	return r.summary, r.err
}

func rerunAdminHandler(t *testing.T, rerunner TMDBRerunner) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, TMDBReruns: rerunner})
}

func TestAdminTMDBRerunAuthorizationOriginAndBody(t *testing.T) {
	rerunner := &adminRerunner{}
	handler := rerunAdminHandler(t, rerunner)
	unauthorized := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", "", "http://localhost:3000", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", "", "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	invalidBody := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", `{}`, "http://localhost:3000", cookie)
	assertAPIError(t, invalidBody, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	if rerunner.calls != 0 || unauthorized.Header().Get("Cache-Control") != "no-store" || wrongOrigin.Header().Get("Cache-Control") != "no-store" || invalidBody.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("calls=%d unauthorized headers=%v wrong-origin headers=%v invalid headers=%v", rerunner.calls, unauthorized.Header(), wrongOrigin.Header(), invalidBody.Header())
	}
}

func TestAdminTMDBRerunSummaryContract(t *testing.T) {
	for _, summary := range []enrichment.RerunSummary{
		{},
		{Processed: 12, Reused: 0, Matched: 5, ReviewRequired: 3, Unmatched: 3, Failed: 1},
	} {
		rerunner := &adminRerunner{summary: summary}
		handler := rerunAdminHandler(t, rerunner)
		cookie := loginAdmin(t, handler, "password")
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", "", "http://localhost:3000", cookie)
		want := `{"processed":` + strconv.Itoa(summary.Processed) + `,"reused":` + strconv.Itoa(summary.Reused) + `,"matched":` + strconv.Itoa(summary.Matched) + `,"review_required":` + strconv.Itoa(summary.ReviewRequired) + `,"unmatched":` + strconv.Itoa(summary.Unmatched) + `,"failed":` + strconv.Itoa(summary.Failed) + `}`
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want || response.Header().Get("Cache-Control") != "no-store" || rerunner.calls != 1 {
			t.Fatalf("summary=%+v status=%d body=%q headers=%v calls=%d", summary, response.Code, response.Body.String(), response.Header(), rerunner.calls)
		}
	}
}

func TestAdminTMDBRerunSafeErrorMappings(t *testing.T) {
	secret := "synthetic-provider-or-database-secret"
	tests := []struct {
		name       string
		rerunner   TMDBRerunner
		wantStatus int
		wantCode   string
		wantText   string
	}{
		{name: "unconfigured", rerunner: nil, wantStatus: http.StatusServiceUnavailable, wantCode: "tmdb_rerun_unavailable", wantText: "Service de relance TMDB indisponible."},
		{name: "in progress", rerunner: &adminRerunner{err: enrichment.ErrRerunInProgress}, wantStatus: http.StatusConflict, wantCode: "tmdb_rerun_in_progress", wantText: "Une relance TMDB est déjà en cours."},
		{name: "provider unavailable", rerunner: &adminRerunner{err: enrichment.ErrRerunUnavailable}, wantStatus: http.StatusServiceUnavailable, wantCode: "tmdb_rerun_unavailable", wantText: "Service de relance TMDB indisponible."},
		{name: "failure", rerunner: &adminRerunner{err: errors.New(secret)}, wantStatus: http.StatusBadGateway, wantCode: "tmdb_rerun_failed", wantText: "La relance TMDB a échoué."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := rerunAdminHandler(t, test.rerunner)
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", "", "http://localhost:3000", cookie)
			assertAPIError(t, response, test.wantStatus, test.wantCode, test.wantText)
			if strings.Contains(response.Body.String(), secret) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("body=%q headers=%v", response.Body.String(), response.Header())
			}
		})
	}
}
