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
	if result.Window != (schedule.Window{From: "2026-09-16", Through: "2027-09-13"}) {
		t.Fatalf("display window=%+v", result.Window)
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

func TestUpcomingDisplayWindowHTTPMidnight(t *testing.T) {
	now := time.Date(2026, 9, 15, 21, 59, 59, 0, time.UTC)
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now}
	for i, date := range []string{"2026-09-15", "2026-09-17", "2026-09-23"} {
		id := int64(i + 1)
		data.PublicMovies = append(data.PublicMovies, schedule.PublicMovieRecord{ID: id, IdentityAnchorTMDBID: id, TMDBID: id, Title: "Film", HasUpcomingRelease: true, UpcomingActive: true, FrenchReleaseDate: date, UpdatedAt: now})
	}
	source := &mutableFixtureSource{view: schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 1})}
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithOptions(service, "http://localhost:3000", HandlerOptions{})
	var previousRevision string
	for _, test := range []struct {
		from, through, slug string
		total               int
	}{
		{"2026-09-16", "2027-09-15", "film-2", 2},
		{"2026-09-23", "2027-09-16", "film-3", 1},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/movies/upcoming?page_size=1", nil))
		var result schedule.UpcomingMoviesResponse
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Window != (schedule.Window{From: test.from, Through: test.through}) || result.Total != test.total || len(result.Items) != 1 || result.Items[0].Slug != test.slug || !result.GeneratedAt.Equal(data.UpcomingCompletedAt) || result.CatalogRevision == previousRevision {
			t.Fatalf("display response=%+v", result)
		}
		previousRevision = result.CatalogRevision
		now = now.Add(time.Second)
	}
	// The hidden Thursday release remains upcoming on its unchanged detail URL.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/movies/film-2/showtimes?date=2026-09-16", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"release_status":"upcoming"`) || !strings.Contains(w.Body.String(), `"french_release_date":"2026-09-17"`) {
		t.Fatalf("hidden detail=%d %s", w.Code, w.Body)
	}
}
