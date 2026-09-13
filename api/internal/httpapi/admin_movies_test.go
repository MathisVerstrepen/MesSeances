package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/tmdb"
)

type adminMovieStoreStub struct {
	query         enrichment.AdminMovieQuery
	patch         enrichment.AdminMoviePatch
	id            int64
	listErr       error
	updateErr     error
	showtimeCount int
	tmdbID        int64
	identityErr   error
}

func (store *adminMovieStoreStub) AdminMovieTMDBID(_ context.Context, id int64) (int64, error) {
	store.id = id
	return store.tmdbID, store.identityErr
}

func (store *adminMovieStoreStub) AdminMovies(_ context.Context, query enrichment.AdminMovieQuery) (enrichment.AdminMovieList, error) {
	store.query = query
	if store.listErr != nil {
		return enrichment.AdminMovieList{}, store.listErr
	}
	return enrichment.AdminMovieList{Items: []enrichment.AdminMovieItem{{ID: "7", UpdatedAt: "2026-08-30T12:00:00.123456Z", ShowtimeCount: store.showtimeCount}}, Total: 1, Limit: query.Limit, Offset: query.Offset}, nil
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
		Movies: enrichment.NewAdminMovieService(store, nil),
	})
}

type adminMoviePosterProviderStub struct {
	id  int64
	err error
}

func (provider *adminMoviePosterProviderStub) Posters(_ context.Context, id int64) ([]tmdb.Poster, error) {
	provider.id = id
	language := "ja"
	return []tmdb.Poster{
		{URL: "https://image.tmdb.org/t/p/w500/poster.jpg", Width: 1000, Height: 1500, Language: &language},
		{URL: "https://image.tmdb.org/t/p/w500/neutral.jpg", Width: 500, Height: 750},
	}, provider.err
}

func TestAdminMoviePostersAuthenticationAndResponse(t *testing.T) {
	store := &adminMovieStoreStub{tmdbID: 42}
	provider := &adminMoviePosterProviderStub{}
	handler := testHandlerWithAdmin(t, AdminOptions{
		Password: "password", SessionSecret: "test-session-secret",
		Reviews: enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil),
		Movies:  enrichment.NewAdminMovieService(store, provider),
	})
	if response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies/7/posters", "", "", nil); response.Code != http.StatusUnauthorized || provider.id != 0 || store.id != 0 {
		t.Fatalf("unauthenticated status=%d body=%s", response.Code, response.Body.String())
	}
	cookie := loginAdmin(t, handler, "password")
	// No Origin required for a read. Query-supplied TMDB identities cannot replace the stored match.
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies/7/posters?tmdb_id=99", "", "", cookie)
	if response.Code != http.StatusOK || provider.id != 42 || store.id != 7 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d providerID=%d storeID=%d headers=%v", response.Code, provider.id, store.id, response.Header())
	}
	want := `{"posters":[{"url":"https://image.tmdb.org/t/p/w500/poster.jpg","width":1000,"height":1500,"language":"ja"},{"url":"https://image.tmdb.org/t/p/w500/neutral.jpg","width":500,"height":750,"language":null}]}`
	if strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("body=%s", response.Body.String())
	}
	for _, id := range []string{"0", "-1", "01", "abc", "9223372036854775808"} {
		store.id, provider.id = 0, 0
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies/"+id+"/posters", "", "", cookie)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_admin_movie_id"`) || store.id != 0 || provider.id != 0 {
			t.Fatalf("id=%s status=%d body=%s", id, response.Code, response.Body.String())
		}
	}
}

func TestAdminMoviePostersEmptyAndErrors(t *testing.T) {
	for _, test := range []struct {
		name        string
		tmdbID      int64
		storeErr    error
		providerErr error
		noProvider  bool
		noService   bool
		status      int
		code        string
	}{
		{name: "no match", status: 200},
		{name: "no match without provider", noProvider: true, status: 200},
		{name: "nonexistent movie", storeErr: enrichment.ErrAdminMovieNotFound, status: 404, code: "admin_movie_not_found"},
		{name: "database failure", storeErr: errors.New("secret database error"), status: 500, code: "admin_movie_posters_failed"},
		{name: "unconfigured TMDB", tmdbID: 42, noProvider: true, status: 503, code: "admin_movie_posters_unavailable"},
		{name: "upstream failure", tmdbID: 42, providerErr: errors.New("secret upstream body"), status: 502, code: "admin_movie_posters_failed"},
		{name: "missing service", noService: true, status: 503, code: "admin_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &adminMovieStoreStub{tmdbID: test.tmdbID, identityErr: test.storeErr}
			provider := &adminMoviePosterProviderStub{err: test.providerErr}
			service := enrichment.NewAdminMovieService(store, provider)
			if test.noProvider {
				service = enrichment.NewAdminMovieService(store, nil)
			}
			if test.noService {
				service = nil
			}
			handler := testHandlerWithAdmin(t, AdminOptions{
				Password: "password", SessionSecret: "test-session-secret",
				Reviews: enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, nil), Movies: service,
			})
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies/7/posters", "", "", cookie)
			if response.Code != test.status || strings.Contains(response.Body.String(), "secret") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.code != "" && !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("body=%s want code=%s", response.Body.String(), test.code)
			}
			if test.status == 200 && strings.TrimSpace(response.Body.String()) != `{"posters":[]}` {
				t.Fatalf("empty result=%s", response.Body.String())
			}
		})
	}
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
		"sort=showtime_count&sort=id", "sort=showtime_count%20DESC", "sort=showtime_count&direction=DESC",
		"showtime_count=1", "override_field=showtime_count",
	}
	for _, query := range invalid {
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies?"+query, "", "", cookie)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query=%q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestAdminMovieListShowtimeCountAndSorting(t *testing.T) {
	store := &adminMovieStoreStub{}
	handler := adminMovieHandler(t, store)
	cookie := loginAdmin(t, handler, "password")
	for _, count := range []int{0, 7} {
		store.showtimeCount = count
		for _, direction := range []string{"asc", "desc"} {
			response := adminRequest(handler, http.MethodGet, "/api/v1/admin/movies?sort=showtime_count&direction="+direction+"&limit=25&offset=5", "", "", cookie)
			if response.Code != http.StatusOK || store.query.Sort != "showtime_count" || store.query.Direction != direction || store.query.Limit != 25 || store.query.Offset != 5 {
				t.Fatalf("status=%d query=%+v body=%s", response.Code, store.query, response.Body.String())
			}
			var payload struct {
				Items []map[string]json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.Items) != 1 {
				t.Fatalf("payload=%+v err=%v", payload, err)
			}
			if got := string(payload.Items[0]["showtime_count"]); got != strconv.Itoa(count) {
				t.Fatalf("showtime_count=%q want integer %d", got, count)
			}
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
		"id":              "/api/v1/admin/movies/07",
		"unknown field":   "/api/v1/admin/movies/7",
		"overlap":         "/api/v1/admin/movies/7",
		"no operation":    "/api/v1/admin/movies/7",
		"nullable title":  "/api/v1/admin/movies/7",
		"count override":  "/api/v1/admin/movies/7",
		"count restore":   "/api/v1/admin/movies/7",
		"count top level": "/api/v1/admin/movies/7",
	}
	bodies := map[string]string{
		"id":              body,
		"unknown field":   `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"identity_anchor_provider":"ugc"}}`,
		"overlap":         `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"title":"Film"},"restore":["title"]}`,
		"no operation":    `{"expected_updated_at":"2026-08-30T12:00:00Z"}`,
		"nullable title":  `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"title":null}}`,
		"count override":  `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"showtime_count":7}}`,
		"count restore":   `{"expected_updated_at":"2026-08-30T12:00:00Z","restore":["showtime_count"]}`,
		"count top level": `{"expected_updated_at":"2026-08-30T12:00:00Z","overrides":{"title":"Film"},"showtime_count":7}`,
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
