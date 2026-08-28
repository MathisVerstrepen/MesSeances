package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/tmdb"
)

type adminMetadataRefreshController struct {
	startStatus enrichment.MetadataRefreshStatus
	startErr    error
	snapshot    *enrichment.MetadataRefreshStatus
	startCalls  int
	snapCalls   int
}

func (c *adminMetadataRefreshController) Start() (enrichment.MetadataRefreshStatus, error) {
	c.startCalls++
	return c.startStatus, c.startErr
}

func (c *adminMetadataRefreshController) Snapshot() *enrichment.MetadataRefreshStatus {
	c.snapCalls++
	return c.snapshot
}

func metadataRefreshAdminHandler(t *testing.T, refresher TMDBMetadataRefresher) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, TMDBRefreshes: refresher})
}

func TestAdminTMDBMetadataRefreshAuthorizationOriginBodyAndNoStore(t *testing.T) {
	controller := &adminMetadataRefreshController{}
	handler := metadataRefreshAdminHandler(t, controller)
	path := "/api/v1/admin/tmdb-matches/refresh-metadata"

	unauthorizedGet := adminRequest(handler, http.MethodGet, path, "", "", nil)
	assertAPIError(t, unauthorizedGet, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	unauthorizedPost := adminRequest(handler, http.MethodPost, path, "", "http://localhost:3000", nil)
	assertAPIError(t, unauthorizedPost, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	authorizedGet := adminRequest(handler, http.MethodGet, path, "", "", cookie)
	if authorizedGet.Code != http.StatusOK || strings.TrimSpace(authorizedGet.Body.String()) != `{"job":null}` || controller.snapCalls != 1 {
		t.Fatalf("GET status=%d body=%q snapshot calls=%d", authorizedGet.Code, authorizedGet.Body.String(), controller.snapCalls)
	}
	wrongOrigin := adminRequest(handler, http.MethodPost, path, "", "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	invalidBody := adminRequest(handler, http.MethodPost, path, `{}`, "http://localhost:3000", cookie)
	assertAPIError(t, invalidBody, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	for name, response := range map[string]*httptest.ResponseRecorder{
		"unauthorized GET":  unauthorizedGet,
		"unauthorized POST": unauthorizedPost,
		"authorized GET":    authorizedGet,
		"wrong origin":      wrongOrigin,
		"invalid body":      invalidBody,
	} {
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s headers=%v", name, response.Header())
		}
	}
	if controller.startCalls != 0 {
		t.Fatalf("start calls=%d", controller.startCalls)
	}
}

func TestAdminTMDBMetadataRefreshDeterministicStatusContract(t *testing.T) {
	startedAt := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(3 * time.Minute)
	running := enrichment.MetadataRefreshStatus{State: enrichment.MetadataRefreshRunning, StartedAt: startedAt}
	summary := enrichment.MetadataRefreshSummary{Processed: 12, Updated: 5, Unchanged: 6, Failed: 1}
	succeeded := enrichment.MetadataRefreshStatus{State: enrichment.MetadataRefreshSucceeded, StartedAt: startedAt, FinishedAt: &finishedAt, Summary: &summary}
	controller := &adminMetadataRefreshController{startStatus: running, snapshot: &succeeded}
	handler := metadataRefreshAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	path := "/api/v1/admin/tmdb-matches/refresh-metadata"

	started := adminRequest(handler, http.MethodPost, path, "", "http://localhost:3000", cookie)
	wantStarted := `{"job":{"state":"running","started_at":"2026-08-28T12:00:00Z","finished_at":null,"summary":null}}`
	if started.Code != http.StatusAccepted || strings.TrimSpace(started.Body.String()) != wantStarted || controller.startCalls != 1 {
		t.Fatalf("POST status=%d body=%q calls=%d", started.Code, started.Body.String(), controller.startCalls)
	}
	completed := adminRequest(handler, http.MethodGet, path, "", "", cookie)
	wantCompleted := `{"job":{"state":"succeeded","started_at":"2026-08-28T12:00:00Z","finished_at":"2026-08-28T12:03:00Z","summary":{"processed":12,"updated":5,"unchanged":6,"failed":1}}}`
	if completed.Code != http.StatusOK || strings.TrimSpace(completed.Body.String()) != wantCompleted || completed.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET status=%d body=%q headers=%v", completed.Code, completed.Body.String(), completed.Header())
	}
}

func TestAdminTMDBMetadataRefreshPOSTReturnsBeforeProviderAndRequestCancellationDoesNotStopJob(t *testing.T) {
	store := &httpMetadataRefreshStore{}
	provider := &httpBlockingMetadataProvider{started: make(chan struct{}), release: make(chan struct{})}
	service := enrichment.NewMetadataRefreshService(store, provider, time.Now, nil)
	manager, err := enrichment.NewMetadataRefreshManager(context.Background(), service, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	handler := metadataRefreshAdminHandler(t, manager)
	cookie := loginAdmin(t, handler, "password")
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	request := httptest.NewRequestWithContext(requestCtx, http.MethodPost, "/api/v1/admin/tmdb-matches/refresh-metadata", nil)
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
		close(provider.release)
		t.Fatal("POST waited for blocked provider")
	}
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"state":"running"`) {
		t.Fatalf("POST status=%d body=%q", response.Code, response.Body.String())
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		close(provider.release)
		t.Fatal("background job did not reach provider")
	}
	running := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/refresh-metadata", "", "", cookie)
	if running.Code != http.StatusOK || !strings.Contains(running.Body.String(), `"state":"running"`) {
		close(provider.release)
		t.Fatalf("running GET status=%d body=%q", running.Code, running.Body.String())
	}
	cancelRequest()
	close(provider.release)

	deadline := time.Now().Add(time.Second)
	for {
		status := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/refresh-metadata", "", "", cookie)
		if strings.Contains(status.Body.String(), `"state":"succeeded"`) {
			wantCounters := `"summary":{"processed":1,"updated":1,"unchanged":0,"failed":0}`
			if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), wantCounters) || store.publishCalls != 1 {
				t.Fatalf("GET status=%d body=%q publish calls=%d", status.Code, status.Body.String(), store.publishCalls)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not succeed after request cancellation: %s", status.Body.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAdminTMDBMetadataRefreshSafeFailureAndStartErrors(t *testing.T) {
	secret := "synthetic-provider-or-database-secret"
	startedAt := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	code := enrichment.MetadataRefreshFailure
	failed := enrichment.MetadataRefreshStatus{State: enrichment.MetadataRefreshFailed, StartedAt: startedAt, FinishedAt: &finishedAt, ErrorCode: &code}
	tests := []struct {
		name       string
		refresher  TMDBMetadataRefresher
		wantStatus int
		wantCode   string
		wantText   string
	}{
		{name: "unconfigured", refresher: nil, wantStatus: http.StatusServiceUnavailable, wantCode: "tmdb_metadata_refresh_unavailable", wantText: "Service d'actualisation TMDB indisponible."},
		{name: "in progress", refresher: &adminMetadataRefreshController{startErr: enrichment.ErrMetadataRefreshInProgress}, wantStatus: http.StatusConflict, wantCode: "tmdb_metadata_refresh_in_progress", wantText: "Une opération TMDB est déjà en cours."},
		{name: "manager closed", refresher: &adminMetadataRefreshController{startErr: enrichment.ErrMetadataRefreshUnavailable}, wantStatus: http.StatusServiceUnavailable, wantCode: "tmdb_metadata_refresh_unavailable", wantText: "Service d'actualisation TMDB indisponible."},
		{name: "unexpected start failure", refresher: &adminMetadataRefreshController{startErr: errors.New(secret)}, wantStatus: http.StatusBadGateway, wantCode: "tmdb_metadata_refresh_failed", wantText: "L'actualisation TMDB a échoué."},
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

	controller := &adminMetadataRefreshController{snapshot: &failed}
	handler := metadataRefreshAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/tmdb-matches/refresh-metadata", "", "", cookie)
	want := `{"job":{"state":"failed","started_at":"2026-08-28T12:00:00Z","finished_at":"2026-08-28T12:01:00Z","summary":null,"error_code":"refresh_failed"}}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want || strings.Contains(response.Body.String(), secret) {
		t.Fatalf("failed status=%d body=%q", response.Code, response.Body.String())
	}
}

type httpMetadataRefreshStore struct {
	publishCalls int
}

func (s *httpMetadataRefreshStore) MatchedTMDBIDs(context.Context) ([]int64, error) {
	return []int64{42}, nil
}

func (s *httpMetadataRefreshStore) Metadata(context.Context, string, int64, string) (enrichment.Metadata, bool, error) {
	return enrichment.Metadata{}, false, nil
}

func (s *httpMetadataRefreshStore) RefreshMetadata(context.Context, []enrichment.Metadata) error {
	s.publishCalls++
	return nil
}

type httpBlockingMetadataProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *httpBlockingMetadataProvider) Details(ctx context.Context, id int64) (tmdb.Details, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
		return tmdb.Details{ID: id, OriginalTitle: "Original", Title: "Film", Runtime: 90}, nil
	case <-ctx.Done():
		return tmdb.Details{}, ctx.Err()
	}
}
