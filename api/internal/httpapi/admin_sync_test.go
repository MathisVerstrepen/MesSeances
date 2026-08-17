package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"movieflow/api/internal/enrichment"
	"movieflow/api/internal/synccontrol"
)

type fakeSyncController struct {
	status   synccontrol.Status
	startErr error
	started  []synccontrol.Target
}

func (c *fakeSyncController) Start(target synccontrol.Target) (synccontrol.Status, error) {
	c.started = append(c.started, target)
	if c.startErr != nil {
		return synccontrol.Status{}, c.startErr
	}
	return c.status, nil
}

func (c *fakeSyncController) Status() synccontrol.Status { return c.status }

func syncAdminHandler(t *testing.T, controller SyncController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", Reviews: reviews, Syncs: controller})
}

func TestAdminSyncStatusAuthenticationAvailabilityAndNoStore(t *testing.T) {
	controller := &fakeSyncController{}
	handler := syncAdminHandler(t, controller)
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/syncs", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	initial := adminRequest(handler, http.MethodGet, "/api/v1/admin/syncs", "", "", cookie)
	if initial.Code != http.StatusOK || strings.TrimSpace(initial.Body.String()) != `{"job":null}` || initial.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("initial status=%d headers=%v body=%s", initial.Code, initial.Header(), initial.Body.String())
	}
	unavailableHandler := syncAdminHandler(t, nil)
	unavailableCookie := loginAdmin(t, unavailableHandler, "password")
	unavailable := adminRequest(unavailableHandler, http.MethodGet, "/api/v1/admin/syncs", "", "", unavailableCookie)
	assertAPIError(t, unavailable, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
	session := adminRequest(unavailableHandler, http.MethodGet, "/api/v1/admin/session", "", "", unavailableCookie)
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"authenticated":true`) {
		t.Fatalf("session status=%d body=%s", session.Code, session.Body.String())
	}
}

func TestAdminStartSyncContract(t *testing.T) {
	started := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	controller := &fakeSyncController{status: synccontrol.Status{ID: "1", Target: synccontrol.TargetAll, State: synccontrol.StateRunning, StartedAt: started, From: "2026-08-17", Through: "2026-08-24", Providers: map[string]synccontrol.ProviderStatus{"ugc": {State: synccontrol.ProviderPending}, "kinepolis": {State: synccontrol.ProviderPending}}}}
	handler := syncAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/all", "", "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	accepted := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/all", "", "http://localhost:3000", cookie)
	if accepted.Code != http.StatusAccepted || accepted.Header().Get("Cache-Control") != "no-store" || !strings.Contains(accepted.Body.String(), `"target":"all"`) || strings.Contains(accepted.Body.String(), "proxy") {
		t.Fatalf("accepted status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	if len(controller.started) != 1 || controller.started[0] != synccontrol.TargetAll {
		t.Fatalf("started=%v", controller.started)
	}
	body := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/ugc", `{}`, "http://localhost:3000", cookie)
	assertAPIError(t, body, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	controller.startErr = synccontrol.ErrInvalidTarget
	invalid := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/bad", "", "http://localhost:3000", cookie)
	assertAPIError(t, invalid, http.StatusBadRequest, "invalid_sync_target", "Cible de synchronisation invalide.")
	controller.startErr = synccontrol.ErrInProgress
	overlap := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/ugc", "", "http://localhost:3000", cookie)
	assertAPIError(t, overlap, http.StatusConflict, "sync_in_progress", "Une synchronisation est déjà en cours.")
	controller.startErr = errors.New("internal secret")
	failed := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/kinepolis", "", "http://localhost:3000", cookie)
	assertAPIError(t, failed, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
	if strings.Contains(failed.Body.String(), "secret") {
		t.Fatalf("leaked body=%s", failed.Body.String())
	}
}
