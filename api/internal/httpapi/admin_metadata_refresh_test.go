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

type adminMetadataRefresher struct {
	summary enrichment.MetadataRefreshSummary
	err     error
	calls   int
}

func (r *adminMetadataRefresher) Refresh(context.Context) (enrichment.MetadataRefreshSummary, error) {
	r.calls++
	return r.summary, r.err
}

func metadataRefreshAdminHandler(t *testing.T, refresher TMDBMetadataRefresher) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, TMDBRefreshes: refresher})
}

func TestAdminTMDBMetadataRefreshAuthorizationOriginAndBody(t *testing.T) {
	refresher := &adminMetadataRefresher{}
	handler := metadataRefreshAdminHandler(t, refresher)
	path := "/api/v1/admin/tmdb-matches/refresh-metadata"
	unauthorized := adminRequest(handler, http.MethodPost, path, "", "http://localhost:3000", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	wrongOrigin := adminRequest(handler, http.MethodPost, path, "", "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	invalidBody := adminRequest(handler, http.MethodPost, path, `{}`, "http://localhost:3000", cookie)
	assertAPIError(t, invalidBody, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	if refresher.calls != 0 || unauthorized.Header().Get("Cache-Control") != "no-store" || wrongOrigin.Header().Get("Cache-Control") != "no-store" || invalidBody.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("calls=%d unauthorized headers=%v wrong-origin headers=%v invalid headers=%v", refresher.calls, unauthorized.Header(), wrongOrigin.Header(), invalidBody.Header())
	}
}

func TestAdminTMDBMetadataRefreshSummaryContract(t *testing.T) {
	for _, summary := range []enrichment.MetadataRefreshSummary{
		{},
		{Processed: 12, Updated: 5, Unchanged: 6, Failed: 1},
	} {
		refresher := &adminMetadataRefresher{summary: summary}
		handler := metadataRefreshAdminHandler(t, refresher)
		cookie := loginAdmin(t, handler, "password")
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/refresh-metadata", "", "http://localhost:3000", cookie)
		want := `{"processed":` + strconv.Itoa(summary.Processed) + `,"updated":` + strconv.Itoa(summary.Updated) + `,"unchanged":` + strconv.Itoa(summary.Unchanged) + `,"failed":` + strconv.Itoa(summary.Failed) + `}`
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want || response.Header().Get("Cache-Control") != "no-store" || refresher.calls != 1 {
			t.Fatalf("summary=%+v status=%d body=%q headers=%v calls=%d", summary, response.Code, response.Body.String(), response.Header(), refresher.calls)
		}
	}
}

func TestAdminTMDBMetadataRefreshSafeErrorMappings(t *testing.T) {
	secret := "synthetic-provider-or-database-secret"
	tests := []struct {
		name       string
		refresher  TMDBMetadataRefresher
		wantStatus int
		wantCode   string
		wantText   string
	}{
		{name: "unconfigured", refresher: nil, wantStatus: http.StatusServiceUnavailable, wantCode: "tmdb_metadata_refresh_unavailable", wantText: "Service d'actualisation TMDB indisponible."},
		{name: "in progress", refresher: &adminMetadataRefresher{err: enrichment.ErrMetadataRefreshInProgress}, wantStatus: http.StatusConflict, wantCode: "tmdb_metadata_refresh_in_progress", wantText: "Une opération TMDB est déjà en cours."},
		{name: "provider unavailable", refresher: &adminMetadataRefresher{err: enrichment.ErrMetadataRefreshUnavailable}, wantStatus: http.StatusServiceUnavailable, wantCode: "tmdb_metadata_refresh_unavailable", wantText: "Service d'actualisation TMDB indisponible."},
		{name: "failure", refresher: &adminMetadataRefresher{err: errors.New(secret)}, wantStatus: http.StatusBadGateway, wantCode: "tmdb_metadata_refresh_failed", wantText: "L'actualisation TMDB a échoué."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := metadataRefreshAdminHandler(t, test.refresher)
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/refresh-metadata", "", "http://localhost:3000", cookie)
			assertAPIError(t, response, test.wantStatus, test.wantCode, test.wantText)
			if strings.Contains(response.Body.String(), secret) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("body=%q headers=%v", response.Body.String(), response.Header())
			}
		})
	}
}
