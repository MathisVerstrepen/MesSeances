package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

type fakeSyncScheduleController struct {
	list       []syncschedule.Schedule
	listErr    error
	saveErr    error
	nextErr    error
	saved      *syncschedule.Schedule
	now        time.Time
	saveCalls  int
	provider   synccontrol.Target
	enabled    bool
	definition syncschedule.Definition
}

func (c *fakeSyncScheduleController) List(context.Context) ([]syncschedule.Schedule, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.list, nil
}

func (c *fakeSyncScheduleController) Save(_ context.Context, provider synccontrol.Target, enabled bool, definition syncschedule.Definition) (syncschedule.Schedule, error) {
	c.saveCalls++
	c.provider, c.enabled, c.definition = provider, enabled, definition
	if c.saveErr != nil {
		return syncschedule.Schedule{}, c.saveErr
	}
	if _, err := syncschedule.NextFive(definition, c.now); err != nil {
		return syncschedule.Schedule{}, syncschedule.ErrInvalidSchedule
	}
	if c.saved != nil {
		return *c.saved, nil
	}
	return syncschedule.Schedule{Provider: provider, Revision: 1, Enabled: enabled, Definition: definition, UpdatedAt: c.now}, nil
}

func (c *fakeSyncScheduleController) NextRuns(definition syncschedule.Definition) ([]time.Time, error) {
	if c.nextErr != nil {
		return nil, c.nextErr
	}
	return syncschedule.NextFive(definition, c.now)
}

func syncScheduleAdminHandler(t *testing.T, controller SyncScheduleController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, SyncSchedules: controller})
}

func TestAdminSyncSchedulesAuthenticationAvailabilityEmptyAndNoStore(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	handler := syncScheduleAdminHandler(t, &fakeSyncScheduleController{now: now})
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/sync-schedules", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	empty := adminRequest(handler, http.MethodGet, "/api/v1/admin/sync-schedules", "", "", cookie)
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"timezone":"Europe/Paris","schedules":[]}` || empty.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("empty status=%d headers=%v body=%s", empty.Code, empty.Header(), empty.Body.String())
	}

	unavailableHandler := syncScheduleAdminHandler(t, nil)
	unavailableCookie := loginAdmin(t, unavailableHandler, "password")
	for _, request := range []struct {
		method string
		path   string
		body   string
		origin string
	}{
		{method: http.MethodGet, path: "/api/v1/admin/sync-schedules"},
		{method: http.MethodPost, path: "/api/v1/admin/sync-schedules/ugc", body: `{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}}`, origin: "http://localhost:3000"},
	} {
		response := adminRequest(unavailableHandler, request.method, request.path, request.body, request.origin, unavailableCookie)
		assertAPIError(t, response, http.StatusServiceUnavailable, "sync_schedule_unavailable", "Planification des synchronisations indisponible.")
	}
	session := adminRequest(unavailableHandler, http.MethodGet, "/api/v1/admin/session", "", "", unavailableCookie)
	if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"authenticated":true`) {
		t.Fatalf("session status=%d body=%s", session.Code, session.Body.String())
	}
}

func TestAdminSyncSchedulesStableOrderAndPreviews(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	controller := &fakeSyncScheduleController{now: now, list: []syncschedule.Schedule{
		{Provider: synccontrol.TargetKinepolis, Revision: 4, Enabled: false, Definition: syncschedule.Definition{Kind: syncschedule.KindCron, Expression: "15 8 * * 1"}, UpdatedAt: updated},
		{Provider: synccontrol.TargetUGC, Revision: 2, Enabled: true, Definition: syncschedule.Definition{Kind: syncschedule.KindDaily, Time: "12:30"}, UpdatedAt: updated},
	}}
	handler := syncScheduleAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/sync-schedules", "", "", cookie)
	body := response.Body.String()
	ugc := strings.Index(body, `"provider":"ugc"`)
	kinepolis := strings.Index(body, `"provider":"kinepolis"`)
	if response.Code != http.StatusOK || ugc < 0 || kinepolis < 0 || ugc >= kinepolis || !strings.Contains(body, `"revision":2`) || !strings.Contains(body, `"revision":4`) || strings.Count(body, `"next_runs":[`) != 2 || strings.Count(body, "T") < 10 {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
}

func TestAdminSaveSyncScheduleSecurityStrictJSONAndValidation(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	controller := &fakeSyncScheduleController{now: now}
	handler := syncScheduleAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")

	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", `{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}}`, "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")

	invalidProvider := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/all", `{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}}`, "http://localhost:3000", cookie)
	assertAPIError(t, invalidProvider, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")

	invalidBodies := []string{
		`{}`,
		`{"enabled":null,"schedule":{"kind":"daily","time":"09:30"}}`,
		`{"enabled":true}`,
		`{"enabled":true,"schedule":{"kind":"daily"}}`,
		`{"enabled":true,"schedule":{"kind":"daily","time":"09:30","weekdays":null}}`,
		`{"enabled":true,"schedule":{"kind":"weekly","time":"09:30"}}`,
		`{"enabled":true,"schedule":{"kind":"weekly","time":"09:30","weekdays":[]}}`,
		`{"enabled":true,"schedule":{"kind":"cron","expression":"0 0 * *"}}`,
		`{"enabled":true,"schedule":{"kind":"cron","expression":"0 0 * * *","time":null}}`,
		`{"enabled":true,"schedule":{"kind":"daily","time":"09:30","unknown":true}}`,
		`{"enabled":true,"schedule":{"kind":"daily","time":"09:30"},"unknown":true}`,
		`{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}} {}`,
		`{"enabled":true,"schedule":{"kind":"cron","expression":"` + strings.Repeat("x", maxAdminBody) + `"}}`,
	}
	for _, body := range invalidBodies {
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/ugc", body, "http://localhost:3000", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sync-schedules/ugc", strings.NewReader(`{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}}`))
	request.Header.Set("Origin", "http://localhost:3000")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
}

func TestAdminSaveSyncScheduleReturnsCommittedCanonicalItem(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	updated := now.Add(time.Minute)
	saved := syncschedule.Schedule{Provider: synccontrol.TargetKinepolis, Revision: 8, Enabled: true, Definition: syncschedule.Definition{Kind: syncschedule.KindWeekly, Time: "07:45", Weekdays: []string{"mon", "fri"}}, UpdatedAt: updated}
	controller := &fakeSyncScheduleController{now: now, saved: &saved}
	handler := syncScheduleAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodPost, "/api/v1/admin/sync-schedules/kinepolis", `{"enabled":true,"schedule":{"kind":"weekly","time":"07:45","weekdays":["fri","mon","fri"]}}`, "http://localhost:3000", cookie)
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || controller.saveCalls != 1 || controller.provider != synccontrol.TargetKinepolis || !controller.enabled || !strings.Contains(body, `"provider":"kinepolis"`) || !strings.Contains(body, `"revision":8`) || !strings.Contains(body, `"weekdays":["mon","fri"]`) || !strings.Contains(body, `"next_runs":[`) {
		t.Fatalf("status=%d calls=%d provider=%s enabled=%t definition=%+v body=%s", response.Code, controller.saveCalls, controller.provider, controller.enabled, controller.definition, body)
	}
}

func TestAdminSyncScheduleFailuresAreRedacted(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	secret := "database synthetic-secret"
	tests := []struct {
		name       string
		controller *fakeSyncScheduleController
		method     string
		path       string
		body       string
		origin     string
	}{
		{name: "list", controller: &fakeSyncScheduleController{now: now, listErr: errors.New(secret)}, method: http.MethodGet, path: "/api/v1/admin/sync-schedules"},
		{name: "preview", controller: &fakeSyncScheduleController{now: now, list: []syncschedule.Schedule{{Provider: synccontrol.TargetUGC, Revision: 1, Enabled: true, Definition: syncschedule.Definition{Kind: syncschedule.KindDaily, Time: "09:30"}, UpdatedAt: now}}, nextErr: errors.New(secret)}, method: http.MethodGet, path: "/api/v1/admin/sync-schedules"},
		{name: "save", controller: &fakeSyncScheduleController{now: now, saveErr: errors.New(secret)}, method: http.MethodPost, path: "/api/v1/admin/sync-schedules/ugc", body: `{"enabled":true,"schedule":{"kind":"daily","time":"09:30"}}`, origin: "http://localhost:3000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := syncScheduleAdminHandler(t, test.controller)
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, test.method, test.path, test.body, test.origin, cookie)
			assertAPIError(t, response, http.StatusBadGateway, "sync_schedule_failed", "La planification des synchronisations n'a pas pu être traitée.")
			if strings.Contains(response.Body.String(), "synthetic") {
				t.Fatalf("leaked body=%s", response.Body.String())
			}
		})
	}
}
