package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
)

type httpRerunStore struct{}

func (httpRerunStore) UnresolvedMovies(context.Context) ([]enrichment.Movie, error) {
	return []enrichment.Movie{{ProviderID: "1"}}, nil
}

type httpRerunMatcher struct{ release <-chan struct{} }

func (m httpRerunMatcher) ForceRun(ctx context.Context, _ []enrichment.Movie) (enrichment.Summary, error) {
	select {
	case <-m.release:
		return enrichment.Summary{Matched: 1}, nil
	case <-ctx.Done():
		return enrichment.Summary{}, ctx.Err()
	}
}

func TestAdminTMDBRerunSlowJobSurvivesRequestCancellation(t *testing.T) {
	release := make(chan struct{})
	manager, err := enrichment.NewRerunManager(t.Context(), enrichment.NewRerunService(httpRerunStore{}, httpRerunMatcher{release: release}, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	handler := rerunAdminHandler(t, manager)
	cookie := loginAdmin(t, handler, "password")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("start waited for matching to finish")
	}
	cancel()
	if response.Code != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", response.Code, response.Body.String())
	}
	status := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/rerun", "", "", cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"running"`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
	duplicate := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", "", "http://localhost:3000", cookie)
	assertAPIError(t, duplicate, http.StatusConflict, "tmdb_rerun_in_progress", "Une relance TMDB est déjà en cours.")
	close(release)
	deadline := time.After(time.Second)
	for manager.Snapshot().State == enrichment.RerunRunning {
		select {
		case <-deadline:
			t.Fatal("job did not complete")
		case <-time.After(time.Millisecond):
		}
	}
	if status := manager.Snapshot(); status.State != enrichment.RerunSucceeded || status.Summary.Matched != 1 {
		t.Fatalf("completion=%+v", status)
	}
}

type adminRerunner struct {
	status enrichment.RerunStatus
	job    *enrichment.RerunStatus
	err    error
	calls  int
}

func (r *adminRerunner) Start() (enrichment.RerunStatus, error) {
	r.calls++
	return r.status, r.err
}

func (r *adminRerunner) Snapshot() *enrichment.RerunStatus { return r.job }

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

func TestAdminTMDBRerunStartAndStatusContract(t *testing.T) {
	startedAt := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	rerunner := &adminRerunner{status: enrichment.RerunStatus{State: enrichment.RerunRunning, StartedAt: startedAt}}
	handler := rerunAdminHandler(t, rerunner)
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/rerun", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	idle := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/rerun", "", "", cookie)
	if idle.Code != http.StatusOK || strings.TrimSpace(idle.Body.String()) != `{"job":null}` {
		t.Fatalf("idle status=%d body=%s", idle.Code, idle.Body.String())
	}
	response := adminRequest(handler, http.MethodPost, "/api/v1/admin/tmdb-matches/rerun", "", "http://localhost:3000", cookie)
	var accepted rerunResponse
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusAccepted || accepted.Job == nil || accepted.Job.State != enrichment.RerunRunning || !accepted.Job.StartedAt.Equal(startedAt) || accepted.Job.Summary != nil || accepted.Job.FinishedAt != nil || rerunner.calls != 1 {
		t.Fatalf("accepted status=%d body=%s calls=%d", response.Code, response.Body.String(), rerunner.calls)
	}
	finishedAt := startedAt.Add(time.Minute)
	summary := enrichment.RerunSummary{Processed: 12, Matched: 5, ReviewRequired: 3, Unmatched: 3, Failed: 1}
	rerunner.job = &enrichment.RerunStatus{State: enrichment.RerunSucceeded, StartedAt: startedAt, FinishedAt: &finishedAt, Summary: &summary}
	status := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/rerun", "", "", cookie)
	var completed rerunResponse
	if err := json.Unmarshal(status.Body.Bytes(), &completed); err != nil {
		t.Fatal(err)
	}
	if status.Code != http.StatusOK || completed.Job == nil || completed.Job.Summary == nil || *completed.Job.Summary != summary || completed.Job.State != enrichment.RerunSucceeded || rerunner.calls != 1 {
		t.Fatalf("status=%d body=%s calls=%d", status.Code, status.Body.String(), rerunner.calls)
	}
	for _, result := range []string{idle.Header().Get("Cache-Control"), response.Header().Get("Cache-Control"), status.Header().Get("Cache-Control")} {
		if result != "no-store" {
			t.Fatalf("cache-control=%q", result)
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
