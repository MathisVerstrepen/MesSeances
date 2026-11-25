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
	"messeances/api/internal/syncsource"
)

type fakeSyncSourceController struct {
	sources     []syncsource.AdminSource
	err         error
	lastBulk    *schedule.Provider
	lastEnabled bool
}

func (f *fakeSyncSourceController) List(context.Context) ([]syncsource.AdminSource, error) {
	return f.sources, f.err
}

func (f *fakeSyncSourceController) SetEnabled(_ context.Context, provider schedule.Provider, key string, enabled bool) (syncsource.AdminSource, error) {
	for i, source := range f.sources {
		if source.Provider == provider && source.Key == key {
			f.sources[i].Enabled = enabled
			return f.sources[i], nil
		}
	}
	return syncsource.AdminSource{}, syncsource.ErrNotFound
}

func (f *fakeSyncSourceController) SetAllEnabled(_ context.Context, provider schedule.Provider, enabled bool) (int64, error) {
	f.lastBulk, f.lastEnabled = &provider, enabled
	n := int64(0)
	for i, source := range f.sources {
		if (provider == "" || source.Provider == provider) && source.Enabled != enabled {
			f.sources[i].Enabled = enabled
			n++
		}
	}
	return n, f.err
}

func syncSourceAdminHandler(t *testing.T, controller SyncSourceController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, SyncSources: controller})
}

func TestAdminSyncSourcesListToggleAndBulk(t *testing.T) {
	now := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	controller := &fakeSyncSourceController{sources: []syncsource.AdminSource{
		{Source: syncsource.Source{Provider: schedule.ProviderTicketingcine, Key: "EMS0963", Name: "Amphi (Vienne)", Website: "https://www.amphi-vienne.fr/", CNCID: "308102"}, UpdatedAt: now},
		{Source: syncsource.Source{Provider: schedule.ProviderScare, Key: "1854", Name: "Café des Images"}, UpdatedAt: now},
	}}
	h := syncSourceAdminHandler(t, controller)
	origin := "http://localhost:3000"
	assertAPIError(t, adminRequest(h, http.MethodGet, "/api/v1/admin/sync-sources", "", "", nil), http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, h, "password")
	r := adminRequest(h, http.MethodGet, "/api/v1/admin/sync-sources", "", "", cookie)
	if body := r.Body.String(); r.Code != http.StatusOK || r.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, `"key":"EMS0963"`) || !strings.Contains(body, `"cnc_id":"308102"`) || !strings.Contains(body, `"enabled":false`) || strings.Contains(body, "params") {
		t.Fatalf("list status=%d body=%s", r.Code, body)
	}

	path := "/api/v1/admin/sync-sources/ticketingcine/EMS0963"
	assertAPIError(t, adminRequest(h, http.MethodPatch, path, `{"enabled":true}`, "https://evil.example", cookie), http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	if r := adminRequest(h, http.MethodPatch, path, `{"enabled":true}`, origin, cookie); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"enabled":true`) || !controller.sources[0].Enabled {
		t.Fatalf("toggle status=%d body=%s", r.Code, r.Body.String())
	}
	for _, bad := range []struct{ path, body string }{
		{path, `{}`},
		{path, `{"enabled":"yes"}`},
		{path, `{"enabled":true,"extra":1}`},
		{"/api/v1/admin/sync-sources/unknown/EMS0963", `{"enabled":true}`},
		{"/api/v1/admin/sync-sources/ticketingcine/bad%20key", `{"enabled":true}`},
	} {
		assertAPIError(t, adminRequest(h, http.MethodPatch, bad.path, bad.body, origin, cookie), http.StatusBadRequest, "invalid_sync_source", "Requête de cinéma source invalide.")
	}
	assertAPIError(t, adminRequest(h, http.MethodPatch, "/api/v1/admin/sync-sources/ticketingcine/EMS0000", `{"enabled":true}`, origin, cookie), http.StatusNotFound, "sync_source_not_found", "Cinéma source introuvable.")

	bulk := "/api/v1/admin/sync-sources/enabled"
	if r := adminRequest(h, http.MethodPost, bulk, `{"provider":"scare","enabled":true}`, origin, cookie); r.Code != http.StatusOK || r.Body.String() != "{\"updated\":1}\n" || *controller.lastBulk != schedule.ProviderScare {
		t.Fatalf("provider bulk status=%d body=%q", r.Code, r.Body.String())
	}
	if r := adminRequest(h, http.MethodPost, bulk, `{"enabled":false}`, origin, cookie); r.Code != http.StatusOK || r.Body.String() != "{\"updated\":2}\n" || *controller.lastBulk != "" || controller.lastEnabled {
		t.Fatalf("all bulk status=%d body=%q", r.Code, r.Body.String())
	}
	for _, body := range []string{`{"provider":"scare"}`, `{"provider":"combined","enabled":true}`, `{"provider":"independants","enabled":true}`} {
		assertAPIError(t, adminRequest(h, http.MethodPost, bulk, body, origin, cookie), http.StatusBadRequest, "invalid_sync_source", "Requête de cinéma source invalide.")
	}
	controller.err = errors.New("database-secret")
	r = adminRequest(h, http.MethodGet, "/api/v1/admin/sync-sources", "", "", cookie)
	if r.Code != http.StatusBadGateway || strings.Contains(r.Body.String(), "secret") {
		t.Fatalf("failure status=%d body=%s", r.Code, r.Body.String())
	}
}

func TestAdminSyncSourcesUnavailableWithoutStore(t *testing.T) {
	h := syncSourceAdminHandler(t, nil)
	cookie := loginAdmin(t, h, "password")
	assertAPIError(t, adminRequest(h, http.MethodGet, "/api/v1/admin/sync-sources", "", "", cookie), http.StatusServiceUnavailable, "sync_source_unavailable", "Gestion des cinémas sources indisponible.")
}
