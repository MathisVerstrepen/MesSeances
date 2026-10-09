package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
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
	limiterNow := now
	handler := NewHandlerWithOptions(service, "http://localhost:3000", HandlerOptions{RateLimitClock: func() time.Time { return limiterNow }})
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		// Refill between contract cases without changing the catalog clock.
		limiterNow = limiterNow.Add(time.Second)
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
	if result.Page != 1 || result.Total != 1 || result.TotalWeeks != 1 || result.TotalPages != 1 || len(result.Items) != 1 || result.Items[0].FrenchReleaseDate == nil || *result.Items[0].FrenchReleaseDate != "2026-10-07" || result.Items[0].ReleaseDate == nil || *result.Items[0].ReleaseDate != "2000-01-01" {
		t.Fatalf("list=%+v", result)
	}
	if result.Window == nil || *result.Window != (schedule.Window{From: "2026-09-16", Through: "2027-09-13"}) {
		t.Fatalf("display window=%+v", result.Window)
	}
	t.Logf("UPCOMING_WIRE %s", strings.TrimSpace(list.Body.String()))
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(list.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"generated_at", "catalog_revision", "timezone", "window", "items", "page", "total", "total_weeks", "total_pages", "view", "year", "month", "available_years", "available_months"}
	if len(wire) != len(wantKeys) {
		t.Fatalf("unexpected response fields: %s", list.Body)
	}
	for _, key := range wantKeys {
		if _, ok := wire[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
	if string(wire["view"]) != `"upcoming"` || string(wire["year"]) != "null" || string(wire["month"]) != "null" || string(wire["available_years"]) != "[]" || string(wire["available_months"]) != "[]" {
		t.Fatalf("upcoming defaults=%s", list.Body)
	}
	for _, page := range []int{2, int(^uint(0) >> 1)} {
		w := request("/api/v1/movies/upcoming?page=" + strconv.Itoa(page))
		var out schedule.UpcomingMoviesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || out.Items == nil || len(out.Items) != 0 || out.Page != page || out.Total != 1 || out.TotalWeeks != 1 || out.TotalPages != 1 {
			t.Fatalf("out of range: %d %s", w.Code, w.Body)
		}
	}
	detail := request("/api/v1/movies/film-1/showtimes?date=2026-09-13&city=Paris")
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"release_status":"upcoming"`) || !strings.Contains(detail.Body.String(), `"theaters":[]`) || !strings.Contains(detail.Body.String(), `"available_dates":[]`) {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body)
	}
	t.Logf("DETAIL_WIRE %s", strings.TrimSpace(detail.Body.String()))
	for _, query := range []string{"month=2026-10", "genres=Drame", "page_size=24", "month=2026-9", "month=2026-13", "page=0", "page=-1", "page=1.5", "page=one", "page=", "page", "page=%20", "page=%ZZ", "page=1;ignored=2", "page=999999999999999999999999999999", "page_size=101", "page=1&page=2", "page=1&page=1", "page=1&%70age=2", "theaters=ugc-1", "unknown=1", "genres=", "genres=Drame,,Action", "month=%ZZ"} {
		path := "/api/v1/movies/upcoming?" + query
		w := request(path)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_query"`) {
			t.Errorf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := request("/api/v1/movies/film-1/showtimes"); w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_query"`) {
		t.Fatalf("detail date missing=%d %s", w.Code, w.Body)
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
	if w := request("/api/v1/movies/upcoming"); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"total_weeks":0`) || !strings.Contains(w.Body.String(), `"total_pages":0`) {
		t.Fatalf("empty=%d %s", w.Code, w.Body)
	}
}

func TestUpcomingOriginalLanguageWireContract(t *testing.T) {
	for _, original := range []string{"fr", "en", ""} {
		now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
		data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Upcoming", HasUpcomingRelease: true, UpcomingActive: true, FrenchReleaseDate: "2026-10-07", OriginalLanguage: original, UpdatedAt: now}}}
		if err := schedule.ValidateCatalogOnlyDataset(data); err != nil {
			t.Fatal(err)
		}
		service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 1})}, schedule.ServiceOptions{Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		handler := NewHandler(service, "http://localhost:3000")
		for _, route := range []string{"/api/v1/movies/upcoming", "/api/v1/movies/film-1/showtimes?date=2026-09-13"} {
			response := performRequest(t, handler, route)
			if response.Code != 200 {
				t.Fatalf("%s: %d %s", route, response.Code, response.Body)
			}
			assertOriginalLanguageWire(t, response.Body.Bytes(), original, 1)
		}
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
		handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/movies/upcoming?page=1", nil))
		var result schedule.UpcomingMoviesResponse
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Window == nil || *result.Window != (schedule.Window{From: test.from, Through: test.through}) || result.Total != test.total || result.TotalWeeks != test.total || result.TotalPages != 1 || len(result.Items) != test.total || result.Items[0].Slug != test.slug || !result.GeneratedAt.Equal(data.UpcomingCompletedAt) || result.CatalogRevision == previousRevision {
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

func TestUpcomingHTTPCompleteWeeksWithoutFilmCap(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now}
	wantPages := [][]string{{}, {}}
	for week := range 5 {
		count := 1
		if week == 0 {
			count = 126
		}
		for i := range count {
			id := int64(len(data.PublicMovies) + 1)
			date := time.Date(2026, 9, 16+week*14+i%7, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
			data.PublicMovies = append(data.PublicMovies, schedule.PublicMovieRecord{ID: id, IdentityAnchorTMDBID: id, TMDBID: id, Title: "Film", FrenchReleaseDate: date, HasUpcomingRelease: true, UpcomingActive: true, UpdatedAt: now})
			wantPages[week/4] = append(wantPages[week/4], "film-"+strconv.FormatInt(id, 10))
		}
	}
	if err := schedule.ValidateCatalogOnlyDataset(data); err != nil {
		t.Fatal(err)
	}
	source := &mutableFixtureSource{view: schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 1})}
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithOptions(service, "http://localhost:3000", HandlerOptions{})
	for page, want := range wantPages {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/movies/upcoming?page="+strconv.Itoa(page+1), nil))
		var result schedule.UpcomingMoviesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || result.Page != page+1 || result.Total != 130 || result.TotalWeeks != 5 || result.TotalPages != 2 {
			t.Fatalf("page %d=%d %s", page+1, w.Code, w.Body)
		}
		got := []string{}
		for _, movie := range result.Items {
			got = append(got, movie.Slug)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("page %d: got=%v want=%v", page+1, got, want)
		}
	}
}

func TestUpcomingHistoryHTTPContract(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now}
	wantPages := [][]string{{}, {}}
	for week := range 5 {
		count := 1
		if week == 0 {
			count = 126
		}
		for range count {
			id := int64(len(data.PublicMovies) + 1)
			date := time.Date(2026, 10, 28-week*7, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
			data.PublicMovies = append(data.PublicMovies, schedule.PublicMovieRecord{ID: id, IdentityAnchorTMDBID: id, TMDBID: id, Title: "Film", HasUpcomingRelease: true, FrenchReleaseDate: date, UpdatedAt: now})
			wantPages[week/4] = append(wantPages[week/4], "film-"+strconv.FormatInt(id, 10))
		}
	}
	for i, date := range []string{"2025-12-31", "2024-11-01", "2027-01-01", ""} {
		id := int64(1000 + i)
		movie := schedule.PublicMovieRecord{ID: id, IdentityAnchorTMDBID: id, TMDBID: id, Title: "Other", HasUpcomingRelease: true, FrenchReleaseDate: date, ReleaseDate: "2000-01-01", UpdatedAt: now}
		if i == 1 {
			movie.UpcomingExcluded = true
		}
		data.PublicMovies = append(data.PublicMovies, movie)
	}
	if err := schedule.ValidateCatalogOnlyDataset(data); err != nil {
		t.Fatal(err)
	}
	source := &mutableFixtureSource{view: schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 1})}
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	limiterNow := now
	handler := NewHandlerWithOptions(service, "http://localhost:3000", HandlerOptions{RateLimitClock: func() time.Time { return limiterNow }})
	request := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		limiterNow = limiterNow.Add(time.Second)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/movies/upcoming?"+query, nil))
		return w
	}
	for _, query := range []string{
		"view=history", "view=history&year=2026&month=10", "view=history&month=10",
		"view=history&year=0002026&month=010&page=01", "view=history&year=%2B2026&month=%2B10&page=%2B1",
	} {
		w := request(query)
		var result schedule.UpcomingMoviesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		wantWeeks, wantTotal, wantTotalPages := 5, 130, 2
		if strings.Contains(query, "month=") {
			wantWeeks, wantTotal, wantTotalPages = 4, 129, 1
		}
		if w.Code != 200 || result.View != "history" || result.Window != nil || result.Year == nil || *result.Year != 2026 || !slices.Equal(result.AvailableYears, []int{2026, 2025}) || !slices.Equal(result.AvailableMonths, []int{9, 10}) || result.Total != wantTotal || result.TotalWeeks != wantWeeks || result.TotalPages != wantTotalPages || len(result.Items) != 129 || !result.GeneratedAt.Equal(data.UpcomingCompletedAt) || result.Timezone != schedule.Timezone {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		if strings.Contains(query, "month=") {
			if result.Month == nil || *result.Month != 10 || result.Total != 129 || result.Items[len(result.Items)-1].Slug != "film-129" {
				t.Fatalf("month filter=%+v", result)
			}
		} else if result.Month != nil || result.Total != 130 {
			t.Fatalf("unfiltered=%+v", result)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		if len(wire) != 14 || string(wire["window"]) != "null" || string(wire["view"]) != `"history"` || string(wire["year"]) != "2026" {
			t.Fatalf("wire=%s", w.Body)
		}
		for _, private := range []string{"decision", "french_releases", "reason_codes", "assessment_status", "review_revision", "upcoming_active", "upcoming_excluded"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("private field %s", private)
			}
		}
	}
	for _, page := range []int{1, 2, 3, int(^uint(0) >> 1)} {
		w := request("view=history&year=2026&page=" + strconv.Itoa(page))
		var result schedule.UpcomingMoviesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, item := range result.Items {
			got = append(got, item.Slug)
		}
		want := []string{}
		if page <= 2 {
			want = wantPages[page-1]
		}
		if w.Code != 200 || result.Page != page || result.Total != 130 || result.TotalWeeks != 5 || result.TotalPages != 2 || !slices.Equal(got, want) || result.Items == nil || !slices.Equal(result.AvailableYears, []int{2026, 2025}) || !slices.Equal(result.AvailableMonths, []int{9, 10}) {
			t.Fatalf("history page=%d: %d %s", page, w.Code, w.Body)
		}
	}
	for _, query := range []string{"view=history&year=2023", "view=history&year=2026&month=1", "view=history&year=1&month=1", "view=history&year=9999&month=12"} {
		w := request(query)
		var result schedule.UpcomingMoviesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || result.Year == nil || result.Total != 0 || result.TotalWeeks != 0 || result.TotalPages != 0 || result.Items == nil || len(result.Items) != 0 || !slices.Equal(result.AvailableYears, []int{2026, 2025}) {
			t.Fatalf("empty explicit period=%d %s", w.Code, w.Body)
		}
	}
	for _, query := range []string{
		"view=other", "view=History", "view=", "view=history&view=history", "view=history&%76iew=history", "view=%ZZ",
		"year=2026", "month=1", "view=upcoming&year=2026", "view=upcoming&month=1",
		"view=history&year=0", "view=history&year=-1", "view=history&year=10000", "view=history&year=1.5",
		"view=history&year=%202026", "view=history&year=2026%20", "view=history&year=+2026", "view=history&year=one",
		"view=history&year=99999999999999999999999", "view=history&year=", "view=history&year", "view=history&year=%ZZ",
		"view=history&year=2026&year=2026", "view=history&year=2026&%79ear=2025",
		"view=history&month=0", "view=history&month=-1", "view=history&month=13", "view=history&month=1.5",
		"view=history&month=%201", "view=history&month=one", "view=history&month=", "view=history&month",
		"view=history&month=99999999999999999999999", "view=history&month=%ZZ", "view=history&month=1&month=1", "view=history&month=1&%6Donth=2",
		"view=history&page=0", "view=history&page=1&page=2", "view=history&unknown=1", "view=history&page_size=4", "view=history&year=2026;month=10",
	} {
		w := request(query)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_query"`) {
			t.Errorf("%s: %d %s", query, w.Code, w.Body)
		}
	}
	if w := request("view=upcoming&page=%2B01"); w.Code != 200 || !strings.Contains(w.Body.String(), `"view":"upcoming"`) {
		t.Fatalf("explicit upcoming=%d %s", w.Code, w.Body)
	}
	data.PublicMovies = nil
	source.view = schedule.NewSnapshotView(data, schedule.SnapshotRevision{EnrichmentVersion: 2})
	for _, query := range []string{"view=history", "view=history&month=10"} {
		w := request(query)
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || string(wire["year"]) != "null" || string(wire["month"]) != "null" || string(wire["available_years"]) != "[]" || string(wire["available_months"]) != "[]" || string(wire["items"]) != "[]" || string(wire["total_pages"]) != "0" {
			t.Fatalf("empty history=%d %s", w.Code, w.Body)
		}
	}
	data.UpcomingCompletedAt = time.Time{}
	source.view = schedule.NewSnapshotView(data)
	if w := request("view=history"); w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"upcoming_unavailable"`) {
		t.Fatalf("unpublished history=%d %s", w.Code, w.Body)
	}
	source.view = nil
	if w := request("view=history"); w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"schedule_unavailable"`) {
		t.Fatalf("missing catalog=%d %s", w.Code, w.Body)
	}
}
