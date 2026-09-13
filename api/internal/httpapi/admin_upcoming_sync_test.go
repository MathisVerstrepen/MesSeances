package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/syncschedule"
	"messeances/api/internal/tmdb"
)

const upcomingSyncPath = "/api/v1/admin/tmdb-upcoming-movies/sync"

type adminUpcomingController struct {
	status    *enrichment.UpcomingStatus
	err       error
	starts    int
	snapshots int
}

func (c *adminUpcomingController) Start() (enrichment.UpcomingStatus, error) {
	c.starts++
	if c.status == nil {
		return enrichment.UpcomingStatus{}, c.err
	}
	return *c.status, c.err
}

func (c *adminUpcomingController) Snapshot() *enrichment.UpcomingStatus {
	c.snapshots++
	return c.status
}

func upcomingSyncAdminHandler(t *testing.T, controller TMDBUpcomingSyncer) http.Handler {
	t.Helper()
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil), TMDBUpcoming: controller})
}

func TestAdminUpcomingSyncAuthorizationOriginAndBody(t *testing.T) {
	c := &adminUpcomingController{}
	handler := upcomingSyncAdminHandler(t, c)
	cookie := loginAdmin(t, handler, "password")
	for _, test := range []struct {
		name, method, origin, body string
		authorized                 bool
		status                     int
		code, message              string
	}{
		{"unauthorized GET", http.MethodGet, "", "", false, 401, "unauthorized", "Authentification requise."},
		{"unauthorized POST", http.MethodPost, "http://localhost:3000", "", false, 401, "unauthorized", "Authentification requise."},
		{"missing origin", http.MethodPost, "", "", true, 403, "origin_forbidden", "Origine non autorisée."},
		{"wrong origin", http.MethodPost, "https://evil.example", "", true, 403, "origin_forbidden", "Origine non autorisée."},
		{"object body", http.MethodPost, "http://localhost:3000", "{}", true, 400, "invalid_request", "Requête invalide."},
		{"oversized body", http.MethodPost, "http://localhost:3000", strings.Repeat("x", 4096), true, 400, "invalid_request", "Requête invalide."},
	} {
		t.Run(test.name, func(t *testing.T) {
			var session *http.Cookie
			if test.authorized {
				session = cookie
			}
			response := adminRequest(handler, test.method, upcomingSyncPath, test.body, test.origin, session)
			assertAPIError(t, response, test.status, test.code, test.message)
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
		})
	}
	if c.starts != 0 || c.snapshots != 0 {
		t.Fatalf("rejected requests reached controller: %+v", c)
	}
}

func TestAdminUpcomingSyncWireContract(t *testing.T) {
	started := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	code := enrichment.UpcomingFailure
	for _, test := range []struct {
		name, method string
		job          *enrichment.UpcomingStatus
		status       int
		body         string
	}{
		{"idle", http.MethodGet, nil, 200, `{"job":null}`},
		{"accepted", http.MethodPost, &enrichment.UpcomingStatus{State: enrichment.UpcomingRunning, StartedAt: started}, 202, `{"job":{"state":"running","started_at":"2026-09-13T12:00:00Z","finished_at":null}}`},
		{"running", http.MethodGet, &enrichment.UpcomingStatus{State: enrichment.UpcomingRunning, StartedAt: started}, 200, `{"job":{"state":"running","started_at":"2026-09-13T12:00:00Z","finished_at":null}}`},
		{"succeeded", http.MethodGet, &enrichment.UpcomingStatus{State: enrichment.UpcomingSucceeded, StartedAt: started, FinishedAt: &finished}, 200, `{"job":{"state":"succeeded","started_at":"2026-09-13T12:00:00Z","finished_at":"2026-09-13T12:01:00Z"}}`},
		{"failed", http.MethodGet, &enrichment.UpcomingStatus{State: enrichment.UpcomingFailed, StartedAt: started, FinishedAt: &finished, ErrorCode: &code}, 200, `{"job":{"state":"failed","started_at":"2026-09-13T12:00:00Z","finished_at":"2026-09-13T12:01:00Z","error_code":"sync_failed"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := &adminUpcomingController{status: test.job}
			handler := upcomingSyncAdminHandler(t, c)
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, test.method, upcomingSyncPath, "", "http://localhost:3000", cookie)
			if response.Code != test.status || strings.TrimSpace(response.Body.String()) != test.body || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s headers=%v", response.Code, response.Body.String(), response.Header())
			}
			if (test.method == http.MethodPost && (c.starts != 1 || c.snapshots != 0)) || (test.method == http.MethodGet && (c.starts != 0 || c.snapshots != 1)) {
				t.Fatalf("controller calls=%+v", c)
			}
		})
	}
}

func TestAdminUpcomingSyncSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name, method  string
		controller    TMDBUpcomingSyncer
		status        int
		code, message string
	}{
		{"unconfigured GET", http.MethodGet, nil, 503, "tmdb_upcoming_sync_unavailable", "Service de synchronisation TMDB Prochainement indisponible."},
		{"unconfigured POST", http.MethodPost, nil, 503, "tmdb_upcoming_sync_unavailable", "Service de synchronisation TMDB Prochainement indisponible."},
		{"closed", http.MethodPost, &adminUpcomingController{err: syncschedule.ErrTargetUnavailable}, 503, "tmdb_upcoming_sync_unavailable", "Service de synchronisation TMDB Prochainement indisponible."},
		{"contention", http.MethodPost, &adminUpcomingController{err: syncschedule.ErrInProgress}, 409, "tmdb_upcoming_sync_in_progress", "Une opération TMDB est déjà en cours."},
		{"unexpected", http.MethodPost, &adminUpcomingController{err: errors.New("synthetic-private-data")}, 502, "tmdb_upcoming_sync_failed", "La synchronisation TMDB Prochainement a échoué."},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := upcomingSyncAdminHandler(t, test.controller)
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, test.method, upcomingSyncPath, "", "http://localhost:3000", cookie)
			assertAPIError(t, response, test.status, test.code, test.message)
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "synthetic-private-data") {
				t.Fatal("unsafe error response")
			}
		})
	}
}

type httpUpcomingStore struct{ published atomic.Bool }

func (*httpUpcomingStore) ActiveUpcomingIDs(context.Context) ([]int64, error) { return nil, nil }
func (*httpUpcomingStore) Metadata(context.Context, string, int64, string) (enrichment.Metadata, bool, error) {
	return enrichment.Metadata{}, false, nil
}
func (s *httpUpcomingStore) PublishUpcoming(ctx context.Context, _ enrichment.UpcomingPublication) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.published.Store(true)
	return nil
}

type httpUpcomingProvider struct {
	started chan struct{}
	release chan struct{}
}

func (p *httpUpcomingProvider) DiscoverMovies(ctx context.Context, _, _ string, _ int) (tmdb.DiscoverPage, error) {
	close(p.started)
	select {
	case <-ctx.Done():
		return tmdb.DiscoverPage{}, ctx.Err()
	case <-p.release:
		return tmdb.DiscoverPage{Page: 1, IDs: []int64{}}, nil
	}
}
func (*httpUpcomingProvider) FrenchTheatricalReleaseDate(context.Context, int64) (string, error) {
	return "", errors.New("unexpected release call")
}
func (*httpUpcomingProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{}, errors.New("unexpected details call")
}

type httpUpcomingLease struct{ released atomic.Bool }

func (l *httpUpcomingLease) Acquire(context.Context) (enrichment.UpcomingLease, error) { return l, nil }
func (l *httpUpcomingLease) Release(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.released.Store(true)
	return nil
}

func TestAdminUpcomingSyncBackgroundJobSurvivesRequestCancellation(t *testing.T) {
	store := &httpUpcomingStore{}
	provider := &httpUpcomingProvider{started: make(chan struct{}), release: make(chan struct{})}
	lease := &httpUpcomingLease{}
	manager, err := enrichment.NewUpcomingManager(t.Context(), enrichment.NewUpcomingService(store, provider, nil, nil), lease)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	handler := upcomingSyncAdminHandler(t, manager)
	cookie := loginAdmin(t, handler, "password")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, upcomingSyncPath, nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("POST waited for provider")
	}
	if response.Code != http.StatusAccepted {
		t.Fatalf("POST=%d %s", response.Code, response.Body.String())
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("background run did not start")
	}
	running := adminRequest(handler, http.MethodGet, upcomingSyncPath, "", "", cookie)
	if running.Code != http.StatusOK || !strings.Contains(running.Body.String(), `"state":"running"`) {
		t.Fatalf("running=%s", running.Body.String())
	}
	conflict := adminRequest(handler, http.MethodPost, upcomingSyncPath, "", "http://localhost:3000", cookie)
	assertAPIError(t, conflict, 409, "tmdb_upcoming_sync_in_progress", "Une opération TMDB est déjà en cours.")
	cancel()
	close(provider.release)
	deadline := time.Now().Add(time.Second)
	for {
		status := adminRequest(handler, http.MethodGet, upcomingSyncPath, "", "", cookie)
		if strings.Contains(status.Body.String(), `"state":"succeeded"`) {
			if status.Code != http.StatusOK || !store.published.Load() || !lease.released.Load() {
				t.Fatal("reported success before publication and cleanup")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not succeed after request cancellation: %s", status.Body.String())
		}
		time.Sleep(time.Millisecond)
	}
}
