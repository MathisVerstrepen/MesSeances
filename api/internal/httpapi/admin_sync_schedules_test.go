package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/syncschedule"
)

type fakeSyncScheduleController struct {
	list           []syncschedule.Schedule
	available      []syncschedule.Target
	now            time.Time
	err            error
	lastOp         string
	lastID         int64
	lastTarget     syncschedule.Target
	lastEnabled    bool
	lastDefinition syncschedule.Definition
}

func (c *fakeSyncScheduleController) List(context.Context) ([]syncschedule.Schedule, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.list, nil
}
func (c *fakeSyncScheduleController) AvailableTargets() []syncschedule.Target { return c.available }
func (c *fakeSyncScheduleController) Create(_ context.Context, target syncschedule.Target, enabled bool, definition syncschedule.Definition) (syncschedule.Schedule, error) {
	return c.mutate("create", target, 0, enabled, definition)
}
func (c *fakeSyncScheduleController) Update(_ context.Context, target syncschedule.Target, id int64, enabled bool, definition syncschedule.Definition) (syncschedule.Schedule, error) {
	return c.mutate("update", target, id, enabled, definition)
}
func (c *fakeSyncScheduleController) Delete(_ context.Context, target syncschedule.Target, id int64) error {
	c.lastOp, c.lastTarget, c.lastID = "delete", target, id
	return c.err
}
func (c *fakeSyncScheduleController) mutate(op string, target syncschedule.Target, id int64, enabled bool, definition syncschedule.Definition) (syncschedule.Schedule, error) {
	c.lastOp, c.lastTarget, c.lastID, c.lastEnabled, c.lastDefinition = op, target, id, enabled, definition
	if c.err != nil {
		return syncschedule.Schedule{}, c.err
	}
	if id == 0 {
		id = 41
	}
	return syncschedule.Schedule{ID: id, Target: target, Revision: 1, Enabled: enabled, Definition: definition, UpdatedAt: c.now}, nil
}
func (c *fakeSyncScheduleController) NextRuns(definition syncschedule.Definition) ([]time.Time, error) {
	if c.err != nil {
		return nil, c.err
	}
	return syncschedule.NextFive(definition, c.now)
}

func syncScheduleAdminHandler(t *testing.T, controller SyncScheduleController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, SyncSchedules: controller})
}

func TestAdminSyncSchedulesListAuthenticationOrderAvailabilityAndNoStore(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	controller := &fakeSyncScheduleController{now: now, available: []syncschedule.Target{syncschedule.TargetUGC, syncschedule.TargetMetadataRefresh}, list: []syncschedule.Schedule{
		{ID: 12, Target: syncschedule.TargetMetadataRefresh, Revision: 1, Enabled: false, Definition: syncschedule.Definition{Kind: syncschedule.KindDaily, Time: "21:00"}, UpdatedAt: now},
		{ID: 11, Target: syncschedule.TargetUGC, Revision: 2, Enabled: true, Definition: syncschedule.Definition{Kind: syncschedule.KindDaily, Time: "19:00"}, UpdatedAt: now},
		{ID: 2, Target: syncschedule.TargetUGC, Revision: 4, Enabled: true, Definition: syncschedule.Definition{Kind: syncschedule.KindDaily, Time: "08:00"}, UpdatedAt: now},
	}}
	handler := syncScheduleAdminHandler(t, controller)
	assertAPIError(t, adminRequest(handler, http.MethodGet, "/api/v1/admin/sync-schedules", "", "", nil), http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/sync-schedules", "", "", cookie)
	body := response.Body.String()
	first, second, metadata := strings.Index(body, `"id":"2"`), strings.Index(body, `"id":"11"`), strings.Index(body, `"id":"12"`)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || first < 0 || second <= first || metadata <= second || !strings.Contains(body, `"available_targets":["ugc","tmdb_metadata_refresh"]`) || strings.Count(body, `"next_runs":[`) != 3 {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), body)
	}
}

func TestAdminSyncScheduleCRUDStrictBodiesOriginAndErrors(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	controller := &fakeSyncScheduleController{now: now, available: []syncschedule.Target{syncschedule.TargetUGC}}
	handler := syncScheduleAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	origin := "http://localhost:3000"
	body := `{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}}`

	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", body, "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	invalid := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/all", body, origin, cookie)
	assertAPIError(t, invalid, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
	invalid = adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", `{"enabled":true,"schedule":{"kind":"daily","time":"09:30"},"extra":1}`, origin, cookie)
	assertAPIError(t, invalid, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")

	created := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", body, origin, cookie)
	if created.Code != http.StatusCreated || controller.lastOp != "create" || !strings.Contains(created.Body.String(), `"id":"41"`) || !strings.Contains(created.Body.String(), `"target":"ugc"`) {
		t.Fatalf("created status=%d call=%s body=%s", created.Code, controller.lastOp, created.Body.String())
	}
	updated := adminRequest(handler, http.MethodPut, "/api/v1/admin/sync-schedules/ugc/41", body, origin, cookie)
	if updated.Code != http.StatusOK || controller.lastOp != "update" || controller.lastID != 41 {
		t.Fatalf("update status=%d op=%s id=%d", updated.Code, controller.lastOp, controller.lastID)
	}
	badID := adminRequest(handler, http.MethodPut, "/api/v1/admin/sync-schedules/ugc/041", body, origin, cookie)
	assertAPIError(t, badID, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
	badDelete := adminRequest(handler, http.MethodDelete, "/api/v1/admin/sync-schedules/ugc/41", `{}`, origin, cookie)
	assertAPIError(t, badDelete, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
	deleted := adminRequest(handler, http.MethodDelete, "/api/v1/admin/sync-schedules/ugc/41", "", origin, cookie)
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 || controller.lastOp != "delete" {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}

	controller.err = syncschedule.ErrScheduleMissing
	notFound := adminRequest(handler, http.MethodPut, "/api/v1/admin/sync-schedules/ugc/41", body, origin, cookie)
	assertAPIError(t, notFound, http.StatusNotFound, "sync_schedule_not_found", "Planification de synchronisation introuvable.")
	controller.err = syncschedule.ErrTargetUnavailable
	unavailable := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", body, origin, cookie)
	assertAPIError(t, unavailable, http.StatusServiceUnavailable, "sync_schedule_target_unavailable", "Cette synchronisation n'est pas disponible.")
	controller.err = errors.New("database synthetic-secret")
	failure := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", body, origin, cookie)
	assertAPIError(t, failure, http.StatusBadGateway, "sync_schedule_failed", "La planification des synchronisations n'a pas pu être traitée.")
	if strings.Contains(failure.Body.String(), "synthetic") {
		t.Fatalf("leaked body=%s", failure.Body.String())
	}
}

func TestAdminSyncSchedulesUnavailableController(t *testing.T) {
	handler := syncScheduleAdminHandler(t, nil)
	cookie := loginAdmin(t, handler, "password")
	for _, request := range []struct{ method, path, body, origin string }{
		{http.MethodGet, "/api/v1/admin/sync-schedules", "", ""},
		{http.MethodPost, "/api/v1/admin/sync-schedules/ugc", `{"enabled":false,"schedule":{"kind":"daily","time":"09:30"}}`, "http://localhost:3000"},
		{http.MethodPut, "/api/v1/admin/sync-schedules/ugc/1", `{"enabled":false,"schedule":{"kind":"daily","time":"09:30"}}`, "http://localhost:3000"},
		{http.MethodDelete, "/api/v1/admin/sync-schedules/ugc/1", "", "http://localhost:3000"},
	} {
		assertAPIError(t, adminRequest(handler, request.method, request.path, request.body, request.origin, cookie), http.StatusServiceUnavailable, "sync_schedule_unavailable", "Planification des synchronisations indisponible.")
	}
}
