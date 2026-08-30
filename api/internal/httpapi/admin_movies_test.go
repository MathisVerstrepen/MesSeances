package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"messeances/api/internal/enrichment"
)

type adminMovieStoreStub struct {
	query     enrichment.AdminMovieQuery
	patch     enrichment.AdminMoviePatch
	id        int64
	listErr   error
	updateErr error
}

func (store *adminMovieStoreStub) AdminMovies(_ context.Context, query enrichment.AdminMovieQuery) (enrichment.AdminMovieList, error) {
	store.query = query
	if store.listErr != nil {
		return enrichment.AdminMovieList{}, store.listErr
	}
	return enrichment.AdminMovieList{Items: []enrichment.AdminMovieItem{{ID: "7", UpdatedAt: "2026-08-30T12:00:00.123456Z"}}, Total: 1, Limit: query.Limit, Offset: query.Offset}, nil
}

func (store *adminMovieStoreStub) UpdateAdminMovie(_ context.Context, id int64, patch enrichment.AdminMoviePatch) (enrichment.AdminMovieItem, error) {
	store.id, store.patch = id, patch
	if store.updateErr != nil {
		return enrichment.AdminMovieItem{}, store.updateErr
	}
	return enrichment.AdminMovieItem{ID: "7", UpdatedAt: "2026-08-30T12:01:00Z"}, nil
}

func adminMovieHandler(t *testing.T, store *adminMovieStoreStub) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil)
	return testHandlerWithAdmin(t, AdminOptions{
		Password: "password", SessionSecret: "test-session-secret", Reviews: reviews,
		Movies: enrichment.NewAdminMovieService(store),
	})
}

func TestAdminMovieListAuthenticationAndStrictQuery(t *testing.T) {
	store := &adminMovieStoreStub{}
	handler := adminMovieHandler(t, store)
	if response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", response.Code, response.Body.String())
	}
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies?limit=25&offset=5&search=%20Film%20&runtime_min=80&runtime_max=150&release_date_from=2026-01-01&release_date_to=2026-12-31&genre=%20Drame%20&override_status=overridden&override_field=title&sort=updated_at&direction=desc", "", "", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if store.query.Limit != 25 || store.query.Offset != 5 || store.query.Search != "Film" || store.query.Genre != "Drame" || store.query.OverrideField != enrichment.AdminMovieFieldTitle || store.query.Sort != "updated_at" || store.query.Direction != "desc" {
		t.Fatalf("query=%+v", store.query)
	}
	var payload enrichment.AdminMovieList
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Items[0].ID != "7" || payload.Items[0].UpdatedAt != "2026-08-30T12:00:00.123456Z" {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}

	invalid := []string{
		"unknown=value", "limit=1&limit=2", "search=", "search=%20%20", "limit=101", "offset=-1",
		"runtime_min=120&runtime_max=90", "release_date_from=2026-02-30", "override_status=automatic&override_field=title",
		"sort=bogus", "direction=sideways", "search=%ZZ",
	}
	for _, query := range invalid {
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies?"+query, "", "", cookie)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query=%q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestAdminMoviePatchSecurityParsingAndErrors(t *testing.T) {
	store := &adminMovieStoreStub{}
	handler := adminMovieHandler(t, store)
	cookie := loginAdmin(t, handler, "password")
	body := `{"expected_updated_at":"2026-08-30T12:00:00.123456Z","overrides":{"title":"Film corrigé","genres":[],"overview":null},"restore":["poster_url"]}`
	if response := adminRequest(handler, http.MethodPatch, "/api/v1/admin/movies/7", body, "", cookie); response.Code != http.StatusForbidden {
		t.Fatalf("missing origin status=%d body=%s", response.Code, response.Body.String())
	}
	if response := adminRequest(handler, http.MethodPatch, "/api/v1/admin/movies/7", body, "https://evil.example", cookie); response.Code != http.StatusForbidden {
		t.Fatalf("wrong origin status=%d body=%s", response.Code, response.Body.String())
	}
	response := adminRequest(handler, http.MethodPatch, "/api/v1/admin/movies/7", body, "http://localhost:3000", cookie)
	if response.Code != http.StatusOK || store.id != 7 {
		t.Fatalf("status=%d id=%d body=%s", response.Code, store.id, response.Body.String())
	}
	if !store.patch.Overrides.Title.Present || *store.patch.Overrides.Title.Value != "Film corrigé" || !store.patch.Overrides.Genres.Present || store.patch.Overrides.Genres.Value == nil || len(*store.patch.Overrides.Genres.Value) != 0 || !store.patch.Overrides.Overview.Present || store.patch.Overrides.Overview.Value != nil || len(store.patch.Restore) != 1 {
		t.Fatalf("patch=%+v", store.patch)
	}

	invalid := map[string]string{
		"id":             "/api/v1/admin/movies/07",
		"unknown field":  "/api/v1/admin/movies/7",
		"overlap":        "/api/v1/admin/movies/7",
		"no operation":   "/api/v1/admin/movies/7",
		"nullable title": "/api/v1/admin/movies/7",
	}
	bodies := map[string]string{
		"id":             body,
		"unknown field":  `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"identity_anchor_provider":"ugc"}}`,
		"overlap":        `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"title":"Film"},"restore":["title"]}`,
		"no operation":   `{"expected_updated_at":"2026-08-30T12:00:00Z"}`,
		"nullable title": `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"title":null}}`,
	}
	for name, target := range invalid {
		response := adminRequest(handler, http.MethodPatch, target, bodies[name], "http://localhost:3000", cookie)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", name, response.Code, response.Body.String())
		}
	}

	for err, status := range map[error]int{
		enrichment.ErrAdminMovieConflict: http.StatusConflict,
		enrichment.ErrAdminMovieNotFound: http.StatusNotFound,
		errors.New("database failed"):    http.StatusInternalServerError,
	} {
		store.updateErr = err
		response := adminRequest(handler, http.MethodPatch, "/api/v1/admin/movies/7", body, "http://localhost:3000", cookie)
		if response.Code != status {
			t.Fatalf("err=%v status=%d body=%s", err, response.Code, response.Body.String())
		}
	}
}

func TestAdminMoviePatchBodyLimitAndCORS(t *testing.T) {
	store := &adminMovieStoreStub{}
	handler := adminMovieHandler(t, store)
	cookie := loginAdmin(t, handler, "password")
	largeButValid := `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"overview":"` + strings.Repeat("a", 5000) + `"}}`
	response := adminRequest(handler, http.MethodPatch, "/api/v1/admin/movies/7", largeButValid, "http://localhost:3000", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("5000-byte status=%d body=%s", response.Code, response.Body.String())
	}
	tooLarge := `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"overview":"` + strings.Repeat("a", int(maxAdminMovieBody)) + `"}}`
	response = adminRequest(handler, http.MethodPatch, "/api/v1/admin/movies/7", tooLarge, "http://localhost:3000", cookie)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized status=%d body=%s", response.Code, response.Body.String())
	}

	preflight := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/admin/movies/7", nil)
	preflight.Header.Set("Origin", "http://localhost:3000")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, preflight)
	if responseRecorder.Code != http.StatusOK || !strings.Contains(responseRecorder.Header().Get("Access-Control-Allow-Methods"), http.MethodPatch) {
		t.Fatalf("preflight status=%d methods=%q", responseRecorder.Code, responseRecorder.Header().Get("Access-Control-Allow-Methods"))
	}
}
