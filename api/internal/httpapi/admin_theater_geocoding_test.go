package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
)

type fakeTheaterGeocodingController struct {
	status       geocoding.RunStatus
	snapshot     *geocoding.RunStatus
	snapshotErr  error
	snapshotFunc func() *geocoding.RunStatus
	startErr     error
	starts       int
	snapshots    int
}

func (c *fakeTheaterGeocodingController) Start() (geocoding.RunStatus, error) {
	c.starts++
	if c.startErr != nil {
		return geocoding.RunStatus{}, c.startErr
	}
	return c.status, nil
}

func (c *fakeTheaterGeocodingController) Snapshot(context.Context) (*geocoding.RunStatus, error) {
	c.snapshots++
	if c.snapshotErr != nil {
		return nil, c.snapshotErr
	}
	if c.snapshotFunc != nil {
		return c.snapshotFunc(), nil
	}
	return c.snapshot, nil
}

func theaterGeocodingAdminHandler(t *testing.T, controller TheaterGeocodingController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, TheaterGeocoding: controller})
}

func TestAdminTheaterGeocodingStatusAuthenticationAvailabilityAndSharedSnapshot(t *testing.T) {
	controller := &fakeTheaterGeocodingController{}
	handler := theaterGeocodingAdminHandler(t, controller)
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	initial := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs", "", "", cookie)
	if initial.Code != http.StatusOK || strings.TrimSpace(initial.Body.String()) != `{"job":null}` || initial.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", initial.Code, initial.Header(), initial.Body.String())
	}

	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	shared := &geocoding.RunStatus{ID: "8", State: geocoding.RunStateRunning, StartedAt: started}
	controller.snapshotFunc = func() *geocoding.RunStatus { return shared }
	running := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs", "", "", cookie)
	if running.Code != http.StatusOK || !strings.Contains(running.Body.String(), `"id":"8"`) || !strings.Contains(running.Body.String(), `"finished_at":null,"summary":null,"error_code":null`) {
		t.Fatalf("running status=%d body=%s", running.Code, running.Body.String())
	}
	finished := started.Add(time.Minute)
	summary := geocoding.RunSummary{Selected: 4, Skipped: 9, Matched: 1, Ambiguous: 2, NotFound: 1, Written: 4}
	shared = &geocoding.RunStatus{ID: "8", State: geocoding.RunStateSucceeded, StartedAt: started, FinishedAt: &finished, Summary: &summary}
	terminal := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs", "", "", cookie)
	if terminal.Code != http.StatusOK || !strings.Contains(terminal.Body.String(), `"state":"succeeded"`) || !strings.Contains(terminal.Body.String(), `"selected":4`) || !strings.Contains(terminal.Body.String(), `"error_code":null`) || controller.snapshots != 3 {
		t.Fatalf("terminal status=%d snapshots=%d body=%s", terminal.Code, controller.snapshots, terminal.Body.String())
	}

	unavailableHandler := theaterGeocodingAdminHandler(t, nil)
	unavailableCookie := loginAdmin(t, unavailableHandler, "password")
	unavailable := adminRequest(unavailableHandler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs", "", "", unavailableCookie)
	assertAPIError(t, unavailable, http.StatusServiceUnavailable, "theater_geocoding_unavailable", "Service de géocodage indisponible.")
	unavailableStart := adminRequest(unavailableHandler, http.MethodPost, "/api/v1/admin/theater-locations/geocoding-runs", "", "http://localhost:3000", unavailableCookie)
	assertAPIError(t, unavailableStart, http.StatusServiceUnavailable, "theater_geocoding_unavailable", "Service de géocodage indisponible.")
}

func TestAdminStartTheaterGeocodingContract(t *testing.T) {
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	controller := &fakeTheaterGeocodingController{status: geocoding.RunStatus{ID: "12", State: geocoding.RunStateRunning, StartedAt: started}}
	handler := theaterGeocodingAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/geocoding-runs", "", "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	accepted := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/geocoding-runs", "", "http://localhost:3000", cookie)
	if accepted.Code != http.StatusAccepted || accepted.Header().Get("Cache-Control") != "no-store" || !strings.Contains(accepted.Body.String(), `"job":{"id":"12","state":"running"`) || controller.starts != 1 {
		t.Fatalf("accepted status=%d starts=%d body=%s", accepted.Code, controller.starts, accepted.Body.String())
	}
	for _, body := range []string{`{}`, " ", strings.Repeat("x", maxAdminBody+1)} {
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/geocoding-runs", body, "http://localhost:3000", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	}
	if controller.starts != 1 {
		t.Fatalf("invalid bodies started jobs=%d", controller.starts)
	}
	controller.startErr = geocoding.ErrRunInProgress
	overlap := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/geocoding-runs", "", "http://localhost:3000", cookie)
	assertAPIError(t, overlap, http.StatusConflict, "theater_geocoding_in_progress", "Un géocodage est déjà en cours.")
	controller.startErr = errors.New("database synthetic-secret")
	failed := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/geocoding-runs", "", "http://localhost:3000", cookie)
	assertAPIError(t, failed, http.StatusBadGateway, "theater_geocoding_failed", "Le géocodage n'a pas pu démarrer.")
	if strings.Contains(failed.Body.String(), "secret") {
		t.Fatalf("secret leaked body=%s", failed.Body.String())
	}
}

func TestAdminTheaterGeocodingStatusFailureIsGeneric(t *testing.T) {
	controller := &fakeTheaterGeocodingController{snapshotErr: errors.New("database synthetic-secret")}
	handler := theaterGeocodingAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs?cause=synthetic-secret", "", "", cookie)
	assertAPIError(t, response, http.StatusBadGateway, "theater_geocoding_failed", "L'état du géocodage n'a pas pu être chargé.")
	if strings.Contains(response.Body.String(), "synthetic") || strings.Contains(response.Body.String(), "cause") {
		t.Fatalf("secret leaked body=%s", response.Body.String())
	}
}

func TestAdminTheaterGeocodingFailedJobIsNormalStatus(t *testing.T) {
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	summary := geocoding.RunSummary{Selected: 3, Failed: 1, Written: 2}
	code := geocoding.RunFailureFailed
	controller := &fakeTheaterGeocodingController{snapshot: &geocoding.RunStatus{ID: "14", State: geocoding.RunStateFailed, StartedAt: started, FinishedAt: &finished, Summary: &summary, ErrorCode: &code}}
	handler := theaterGeocodingAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations/geocoding-runs", "", "", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"failed"`) || !strings.Contains(response.Body.String(), `"error_code":"run_failed"`) || !strings.Contains(response.Body.String(), `"failed":1`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
