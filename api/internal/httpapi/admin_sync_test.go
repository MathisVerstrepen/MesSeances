package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
)

type fakeSyncController struct {
	status       synccontrol.Status
	snapshot     synccontrol.Snapshot
	snapshotErr  error
	snapshotFunc func() synccontrol.Snapshot
	startErr     error
	started      []synccontrol.Target
	events       []string
}

func (c *fakeSyncController) Start(target synccontrol.Target) (synccontrol.Status, error) {
	c.events = append(c.events, "start")
	c.started = append(c.started, target)
	if c.startErr != nil {
		return synccontrol.Status{}, c.startErr
	}
	return c.status, nil
}

func (c *fakeSyncController) Snapshot(context.Context) (synccontrol.Snapshot, error) {
	c.events = append(c.events, "snapshot")
	if c.snapshotErr != nil {
		return synccontrol.Snapshot{}, c.snapshotErr
	}
	if c.snapshotFunc != nil {
		return c.snapshotFunc(), nil
	}
	return c.snapshot, nil
}

func syncAdminHandler(t *testing.T, controller SyncController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, Syncs: controller})
}

func pendingSyncAdminHandler(t *testing.T, controller SyncController) http.Handler {
	t.Helper()
	service, err := schedule.NewService(fixtureSource{}, schedule.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return NewHandlerWithAdmin(service, "http://localhost:3000", AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, Syncs: controller})
}

func TestAdminSyncStatusAuthenticationAvailabilityAndNoStore(t *testing.T) {
	controller := &fakeSyncController{}
	handler := pendingSyncAdminHandler(t, controller)
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/syncs", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	initial := adminRequest(handler, http.MethodGet, "/api/v1/admin/syncs", "", "", cookie)
	if initial.Code != http.StatusOK || strings.TrimSpace(initial.Body.String()) != `{"job":null,"runs":[]}` || initial.Header().Get("Cache-Control") != "no-store" {
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
	controller := &fakeSyncController{status: synccontrol.Status{ID: "1", Target: synccontrol.TargetAll, State: synccontrol.StateRunning, Trigger: synccontrol.TriggerManual, StartedAt: started, From: "2026-08-17", Through: "2026-08-17", Providers: map[string]synccontrol.ProviderStatus{"ugc": {State: synccontrol.ProviderPending, Log: []string{"sensitive-start-log"}}, "kinepolis": {State: synccontrol.ProviderPending}, "pathe": {State: synccontrol.ProviderPending}, "cgr": {State: synccontrol.ProviderPending}}}}
	handler := pendingSyncAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/all", "", "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	accepted := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/all", "", "http://localhost:3000", cookie)
	if accepted.Code != http.StatusAccepted || accepted.Header().Get("Cache-Control") != "no-store" || !strings.Contains(accepted.Body.String(), `"target":"all"`) || strings.Contains(accepted.Body.String(), "proxy") || strings.Contains(accepted.Body.String(), "sensitive-start-log") {
		t.Fatalf("accepted status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	if len(controller.started) != 1 || controller.started[0] != synccontrol.TargetAll {
		t.Fatalf("started=%v", controller.started)
	}
	if strings.Join(controller.events, ",") != "snapshot,start" {
		t.Fatalf("events=%v", controller.events)
	}
	pathe := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/pathe", "", "http://localhost:3000", cookie)
	if pathe.Code != http.StatusAccepted || len(controller.started) != 2 || controller.started[1] != synccontrol.TargetPathe {
		t.Fatalf("Pathé status=%d started=%v body=%s", pathe.Code, controller.started, pathe.Body.String())
	}
	cgr := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/cgr", "", "http://localhost:3000", cookie)
	if cgr.Code != http.StatusAccepted || len(controller.started) != 3 || controller.started[2] != synccontrol.TargetCGR {
		t.Fatalf("CGR status=%d started=%v body=%s", cgr.Code, controller.started, cgr.Body.String())
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
	controller.startErr = synccontrol.ErrOccurrenceClaimed
	claim := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/ugc", "", "http://localhost:3000", cookie)
	assertAPIError(t, claim, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
	if strings.Contains(claim.Body.String(), "occurrence") || strings.Contains(claim.Body.String(), "claim") {
		t.Fatalf("public claim conflict body=%s", claim.Body.String())
	}
}

func TestAdminSyncStatusExposesTypedTerminalContractWithoutCause(t *testing.T) {
	secret := "synthetic-secret"
	finished := time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC)
	scheduledFor := time.Date(2026, 8, 17, 11, 30, 0, 0, time.UTC)
	canonicalLog := `ts=2026-08-17T13:00:00Z level=error provider=ugc event=provider_failed stage=provider_fetch operation=showings category=http_status http_status=403 attempt=1/4 requests=26 message="Le fournisseur a renvoyé un statut HTTP inattendu."`
	job := synccontrol.Status{
		ID: "4", Target: synccontrol.TargetAll, State: synccontrol.StateRunning, Trigger: synccontrol.TriggerManual,
		StartedAt: finished.Add(-time.Minute), From: "2026-08-17", Through: "2026-08-24",
		Providers: map[string]synccontrol.ProviderStatus{
			"ugc":       {State: synccontrol.ProviderSucceeded, Outcome: &synccontrol.ProviderOutcome{Sync: synccontrol.SyncOutcome{Version: 9, Cinemas: 3, Movies: 8, NewMovies: 2, Requests: 20, Showtimes: 12, NewShowtimes: 4, Through: "2026-12-24"}, Enrichment: synccontrol.EnrichmentOutcome{Status: "complete", Counts: &synccontrol.EnrichmentCounts{Matched: 2}}}},
			"kinepolis": {State: synccontrol.ProviderRunning},
			"pathe":     {State: synccontrol.ProviderNotRequested},
		},
	}
	controller := &fakeSyncController{snapshot: synccontrol.Snapshot{Job: &job, Runs: []synccontrol.Status{{ID: "3", Target: synccontrol.TargetUGC, State: synccontrol.StateFailed, Trigger: synccontrol.TriggerScheduled, Occurrence: &synccontrol.Occurrence{ScheduleID: 17, Provider: synccontrol.TargetUGC, Revision: 7, ScheduledFor: scheduledFor, Attempt: 1}, StartedAt: finished.Add(-2 * time.Hour), FinishedAt: &finished, From: "2026-08-17", Through: "2026-08-24", Providers: map[string]synccontrol.ProviderStatus{"ugc": {State: synccontrol.ProviderFailed, ErrorCode: synccontrol.FailureProviderSync, Log: []string{canonicalLog, secret}}}}}}}
	handler := syncAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/syncs?cause="+secret, "", "", cookie)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"version":9`) || !strings.Contains(body, `"movies":8`) || !strings.Contains(body, `"new_movies":2`) || !strings.Contains(body, `"requests":20`) || !strings.Contains(body, `"new_showtimes":4`) || !strings.Contains(body, `"window_through":"2026-12-24"`) || !strings.Contains(body, `"pathe":{"state":"not_requested"}`) || !strings.Contains(body, `"runs":[{"id":"3"`) || !strings.Contains(body, `"trigger":"manual"`) || !strings.Contains(body, `"trigger":"scheduled"`) || !strings.Contains(body, `"occurrence":{"schedule_id":"17","schedule_revision":7,"scheduled_for":"2026-08-17T11:30:00Z","attempt":1}`) || !strings.Contains(body, `"status":"complete"`) || !strings.Contains(body, `"error_code":"provider_sync_failed"`) || !strings.Contains(body, `"log":["ts=2026-08-17T13:00:00Z level=error provider=ugc`) || !strings.Contains(body, `event=log_truncated`) || strings.Contains(body, `"provider"`) || strings.Contains(body, secret) || strings.Contains(body, "cause") {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
}

func TestAdminSyncSnapshotFailureIsRedactedAndPreventsManualStart(t *testing.T) {
	controller := &fakeSyncController{snapshotErr: errors.New("database synthetic-secret")}
	handler := syncAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")

	get := adminRequest(handler, http.MethodGet, "/api/v1/admin/syncs", "", "", cookie)
	assertAPIError(t, get, http.StatusBadGateway, "sync_failed", "L'état des synchronisations n'a pas pu être chargé.")
	post := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/ugc", "", "http://localhost:3000", cookie)
	assertAPIError(t, post, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
	if len(controller.started) != 0 || strings.Contains(get.Body.String()+post.Body.String(), "synthetic") {
		t.Fatalf("started=%v get=%s post=%s", controller.started, get.Body.String(), post.Body.String())
	}
}

func TestAdminStartSyncRejectsInvalidTargetBeforeSnapshotFailure(t *testing.T) {
	controller := &fakeSyncController{snapshotErr: errors.New("database synthetic-secret")}
	handler := syncAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")

	response := adminRequest(handler, http.MethodPost, "/api/v1/admin/syncs/bad", "", "http://localhost:3000", cookie)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_sync_target", "Cible de synchronisation invalide.")
	if len(controller.events) != 0 || len(controller.started) != 0 || strings.Contains(response.Body.String(), "synthetic") {
		t.Fatalf("events=%v started=%v body=%s", controller.events, controller.started, response.Body.String())
	}
}

func TestAdminSyncStatusReadsSharedAuthoritativeSnapshotOnEveryRequest(t *testing.T) {
	started := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	active := synccontrol.Status{ID: "8", Target: synccontrol.TargetUGC, State: synccontrol.StateRunning, Trigger: synccontrol.TriggerManual, StartedAt: started, From: "2026-08-17", Through: "2026-08-17", Providers: map[string]synccontrol.ProviderStatus{}}
	shared := synccontrol.Snapshot{Job: &active, Runs: []synccontrol.Status{}}
	controllerA := &fakeSyncController{snapshotFunc: func() synccontrol.Snapshot { return shared }}
	controllerB := &fakeSyncController{snapshotFunc: func() synccontrol.Snapshot { return shared }}
	handlerA := syncAdminHandler(t, controllerA)
	handlerB := syncAdminHandler(t, controllerB)
	cookieA := loginAdmin(t, handlerA, "password")
	cookieB := loginAdmin(t, handlerB, "password")

	first := adminRequest(handlerB, http.MethodGet, "/api/v1/admin/syncs", "", "", cookieB)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"id":"8"`) {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	finished := started.Add(time.Minute)
	active.State = synccontrol.StateSucceeded
	active.FinishedAt = &finished
	shared = synccontrol.Snapshot{Runs: []synccontrol.Status{active}}
	second := adminRequest(handlerA, http.MethodGet, "/api/v1/admin/syncs", "", "", cookieA)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"job":null`) || !strings.Contains(second.Body.String(), `"runs":[{"id":"8"`) {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	if len(controllerA.events) != 1 || len(controllerB.events) != 1 {
		t.Fatalf("controllerA=%v controllerB=%v", controllerA.events, controllerB.events)
	}
}
