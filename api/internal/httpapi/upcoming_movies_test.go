package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestUpcomingCatalogOnlyWireContract(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Film à venir", HasUpcomingRelease: true, UpcomingActive: true, FrenchReleaseDate: "2026-10-07", ReleaseDate: "2000-01-01", Genres: []string{"Drame"}, UpdatedAt: now}}}
	if err := schedule.ValidateCatalogOnlyDataset(data); err != nil {
		t.Fatal(err)
	}
	source := &mutableFixtureSource{view: schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 1})}
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithOptions(service, "http://localhost:3000", HandlerOptions{})
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		return w
	}
	list := request("/api/v1/movies/upcoming")
	if list.Code != 200 {
		t.Fatalf("upcoming: %d %s", list.Code, list.Body)
	}
	var result schedule.UpcomingMoviesResponse
	if err := json.Unmarshal(list.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Page != 1 || result.PageSize != 24 || result.Total != 1 || result.Items[0].FrenchReleaseDate == nil || *result.Items[0].FrenchReleaseDate != "2026-10-07" || result.Items[0].ReleaseDate == nil || *result.Items[0].ReleaseDate != "2000-01-01" {
		t.Fatalf("list=%+v", result)
	}
	t.Logf("UPCOMING_WIRE %s", strings.TrimSpace(list.Body.String()))
	detail := request("/api/v1/movies/film-1/showtimes?date=2026-09-13&city=Paris")
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"release_status":"upcoming"`) || !strings.Contains(detail.Body.String(), `"theaters":[]`) || !strings.Contains(detail.Body.String(), `"available_dates":[]`) {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body)
	}
	t.Logf("DETAIL_WIRE %s", strings.TrimSpace(detail.Body.String()))
	for _, path := range []string{"/api/v1/movies/upcoming?month=2026-9", "/api/v1/movies/upcoming?month=2026-13", "/api/v1/movies/upcoming?page=0", "/api/v1/movies/upcoming?page_size=101", "/api/v1/movies/upcoming?page=1&page=2", "/api/v1/movies/upcoming?theaters=ugc-1", "/api/v1/movies/upcoming?genres=", "/api/v1/movies/upcoming?genres=Drame,,Action", "/api/v1/movies/upcoming?month=%ZZ", "/api/v1/movies/film-1/showtimes"} {
		w := request(path)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_query"`) {
			t.Errorf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := request("/api/v1/movies/film-999/showtimes?date=2026-09-13"); w.Code != 404 {
		t.Fatalf("unknown movie=%d", w.Code)
	}
	if w := request("/api/v1/movies"); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("default catalog=%d %s", w.Code, w.Body)
	}
	if w := request("/api/v1/movies?include_ended=true"); w.Code != 200 || !strings.Contains(w.Body.String(), `"slug":"film-1"`) {
		t.Fatalf("inventory=%d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/readyz", "/api/v1/timeline?date=2026-09-13"} {
		if w := request(path); w.Code != 503 {
			t.Fatalf("schedule availability %s=%d %s", path, w.Code, w.Body)
		}
	}
	data.UpcomingCompletedAt = time.Time{}
	source.view = schedule.NewSnapshotView(data)
	if w := request("/api/v1/movies/upcoming"); w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"upcoming_unavailable"`) {
		t.Fatalf("unpublished=%d %s", w.Code, w.Body)
	}
	data.UpcomingCompletedAt = now
	data.PublicMovies = nil
	source.view = schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 2})
	if w := request("/api/v1/movies/upcoming"); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"available_genres":[]`) || !strings.Contains(w.Body.String(), `"available_months":[]`) {
		t.Fatalf("empty=%d %s", w.Code, w.Body)
	}
}
