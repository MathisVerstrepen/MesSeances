package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func screeningSummaryHandler(t *testing.T, source schedule.Source, now time.Time) http.Handler {
	t.Helper()
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service, "http://localhost:3000")
}

func TestMoviesScreeningSummaryTransportAndOrdinaryOmission(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 10, 0, 0, time.UTC)
	handler := screeningSummaryHandler(t, fixtureSource{view: schedule.NewSnapshotView(fixtureDataset(t))}, now)
	response := performRequest(t, handler, "/api/v1/movies?screening_summary=TrUe&currently_screened=true&theaters=ugc-25,ugc-25&sort=title_asc&page_size=100")
	var result schedule.MovieCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Slug != "tmdb-film-42" || result.Items[0].ShowtimeCount != 1 || result.Items[0].RemainingShowtimeCount == nil || *result.Items[0].RemainingShowtimeCount != 0 || result.Items[0].Next7DaysShowtimeCount == nil || *result.Items[0].Next7DaysShowtimeCount != 0 || !strings.Contains(response.Body.String(), `"remaining_showtime_count":0`) || !strings.Contains(response.Body.String(), `"next_7_days_showtime_count":0`) {
		t.Fatalf("status=%d result=%+v", response.Code, result)
	}
	wantWindow := &schedule.MovieScreeningWindow{AsOf: now, Timezone: schedule.Timezone, From: "2026-08-15", Through: "2026-08-18", DayCount: 4}
	if !reflect.DeepEqual(result.ScreeningWindow, wantWindow) || result.ScreeningWindow.AsOf.Equal(result.GeneratedAt) {
		t.Fatalf("window=%+v want=%+v", result.ScreeningWindow, wantWindow)
	}
	ordinary := performRequest(t, handler, "/api/v1/movies?theaters=ugc-25")
	falseSummary := performRequest(t, handler, "/api/v1/movies?theaters=ugc-25&screening_summary=FaLsE")
	if ordinary.Code != http.StatusOK || ordinary.Body.String() != falseSummary.Body.String() {
		t.Fatalf("false changed response: ordinary=%s false=%s", ordinary.Body, falseSummary.Body)
	}
	for _, target := range []string{
		"/api/v1/movies?theaters=ugc-25",
		"/api/v1/cities/lille",
		"/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15",
	} {
		response := performRequest(t, handler, target)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "remaining_showtime_count") || strings.Contains(response.Body.String(), "next_7_days_showtime_count") || strings.Contains(response.Body.String(), "screening_window") {
			t.Fatalf("summary leaked into %s: status=%d body=%s", target, response.Code, response.Body)
		}
	}
	empty := performRequest(t, handler, "/api/v1/movies?screening_summary=true&search=absent")
	if err := json.Unmarshal(empty.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if empty.Code != http.StatusOK || result.Total != 0 || len(result.Items) != 0 || !reflect.DeepEqual(result.ScreeningWindow, wantWindow) {
		t.Fatalf("empty status=%d result=%+v", empty.Code, result)
	}
}

func TestMoviesScreeningSummaryQueryValidation(t *testing.T) {
	handler := testHandler(t)
	for _, suffix := range []string{
		"screening_summary=",
		"screening_summary=1",
		"screening_summary=0",
		"screening_summary=yes",
		"screening_summary=%20true",
		"screening_summary=true&date=",
		"screening_summary=true&date=today",
		"screening_summary=true&date=2026-08-15",
		"screening_summary=true&date_to=",
		"screening_summary=true&date_to=2026-08-18",
		"screening_summary=true&date=2026-08-15&date_to=2026-08-18",
		"screening_summary=true&theaters=unknown",
		"screening_summary=true&theaters=",
		"screening_summary=true&page_size=101",
	} {
		t.Run(suffix, func(t *testing.T) {
			response := performRequest(t, handler, "/api/v1/movies?"+suffix)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_query"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
	for _, suffix := range []string{"screening_summary=false&date=today", "screening_summary=false&date=2026-08-15&date_to=2026-08-18"} {
		response := performRequest(t, handler, "/api/v1/movies?"+suffix)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "screening_window") {
			t.Fatalf("ordinary dates changed: status=%d body=%s", response.Code, response.Body)
		}
	}
}

type disappearingSummarySource struct {
	view  *schedule.SnapshotView
	calls int
}

func (s *disappearingSummarySource) Snapshot() *schedule.SnapshotView {
	s.calls++
	if s.calls == 1 {
		return s.view
	}
	return nil
}

func TestMoviesScreeningSummaryRequiresCapturedSchedule(t *testing.T) {
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	imported := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, CatalogPublishedAt: now, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Imported film", UpdatedAt: now}}}
	view := schedule.NewSnapshotView(imported, schedule.SnapshotRevision{EnrichmentVersion: 1})
	disappearing := &disappearingSummarySource{view: schedule.NewSnapshotView(fixtureDataset(t))}
	for _, tc := range []struct {
		name   string
		source schedule.Source
	}{
		{"nil snapshot", fixtureSource{}},
		{"catalog only", fixtureSource{view: view}},
		{"snapshot lost after catalog middleware", disappearing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := screeningSummaryHandler(t, tc.source, now)
			response := performRequest(t, handler, "/api/v1/movies?screening_summary=true")
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"code":"schedule_unavailable"`) || strings.Contains(response.Body.String(), "screening_window") {
				t.Fatalf("status=%d body=%s cache=%s", response.Code, response.Body, response.Header().Get("Cache-Control"))
			}
		})
	}
	if disappearing.calls != 2 {
		t.Fatalf("snapshot reads=%d want middleware + captured service view", disappearing.calls)
	}
	handler := screeningSummaryHandler(t, fixtureSource{view: view}, now)
	for _, suffix := range []string{"", "&screening_summary=false"} {
		response := performRequest(t, handler, "/api/v1/movies?include_ended=true"+suffix)
		var result schedule.MovieCatalog
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || result.Total != 1 || result.Items[0].Slug != "film-1" || result.ScreeningWindow != nil || result.Items[0].RemainingShowtimeCount != nil || result.Items[0].Next7DaysShowtimeCount != nil {
			t.Fatalf("ordinary import changed: status=%d result=%+v", response.Code, result)
		}
	}
}

func TestMoviesScreeningSummarySevenDayTransport(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                 string
		start                time.Time
		remaining, next7Days int
	}{
		{"past grace", now.Add(-time.Minute), 0, 0},
		{"exact now", now, 1, 1},
		{"after Tuesday within seven days", now.Add(24 * time.Hour), 0, 1},
		{"just before cutoff", now.Add(168*time.Hour - time.Nanosecond), 0, 1},
		{"exact cutoff", now.Add(168 * time.Hour), 0, 0},
		{"after cutoff", now.Add(168*time.Hour + time.Nanosecond), 0, 0},
		{"two months away", now.AddDate(0, 2, 0), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := readinessFixtureDataset(t)
			data.Showtimes = data.Showtimes[:2]
			data.Window = schedule.Window{From: "2026-10-13", Through: "2026-12-13"}
			for i, start := range []time.Time{tc.start, now.Add(time.Hour)} {
				showing := &data.Showtimes[i]
				showing.StartTime = start.In(location)
				showing.EndTime = showing.StartTime.Add(100 * time.Minute)
				showing.ServiceDate = showing.StartTime.Format("2006-01-02")
				data.Theaters[i].AvailableDates = []string{showing.ServiceDate}
			}
			data.Theaters[2].AvailableDates = nil
			if err := schedule.ValidateDataset(data, true); err != nil {
				t.Fatal(err)
			}
			handler := screeningSummaryHandler(t, fixtureSource{view: schedule.NewSnapshotView(data)}, now)
			for _, selected := range []bool{true, false} {
				target := "/api/v1/movies?screening_summary=true"
				remaining, next7Days, current := tc.remaining, tc.next7Days, 1
				if selected {
					target += "&theaters=ugc-25"
				} else {
					remaining++
					next7Days++
					current++
				}
				response := performRequest(t, handler, target)
				var result schedule.MovieCatalog
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusOK || result.Total != 1 || len(result.Items) != 1 || result.ScreeningWindow == nil || !result.ScreeningWindow.AsOf.Equal(now) || result.ScreeningWindow.DayCount != 1 {
					t.Fatalf("selected=%v status=%d result=%+v", selected, response.Code, result)
				}
				item := result.Items[0]
				if item.Slug != "tmdb-film-42" || item.ShowtimeCount != current || item.RemainingShowtimeCount == nil || *item.RemainingShowtimeCount != remaining || item.Next7DaysShowtimeCount == nil || *item.Next7DaysShowtimeCount != next7Days {
					t.Fatalf("selected=%v item=%+v want current=%d remaining=%d next7Days=%d", selected, item, current, remaining, next7Days)
				}
			}
		})
	}
}
