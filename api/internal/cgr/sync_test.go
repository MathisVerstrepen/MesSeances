package cgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fakeGetter struct {
	mu                    sync.Mutex
	requests              []string
	maxScheduleWindowDays int
	malformedMovies       bool
	scheduleUnavailable   bool
}

type nationalGetter struct {
	mu             sync.Mutex
	requests       int
	failTheater    string
	staleSchedules int
	cinemasBody    []byte
}

func newNationalGetter(t *testing.T, count int) *nationalGetter {
	t.Helper()
	nodes := make([]map[string]any, count)
	for index := range nodes {
		id := fmt.Sprintf("A%04d", index+1)
		nodes[index] = map[string]any{
			"id": id, "name": "CGR Synthetic " + id, "path": "/theaters/" + strings.ToLower(id) + "-cgr-synthetic", "timeZone": schedule.Timezone,
			"practicalInfo": map[string]any{"location": map[string]any{"address": "1 rue Synthetic", "city": "Test", "zip": "75000"}},
		}
	}
	body, err := json.Marshal(map[string]any{"data": map[string]any{"allTheater": map[string]any{"nodes": nodes}}})
	if err != nil {
		t.Fatal(err)
	}
	return &nationalGetter{cinemasBody: body}
}

func (g *nationalGetter) Get(_ context.Context, operation Operation, rawURL string) ([]byte, error) {
	g.mu.Lock()
	g.requests++
	g.mu.Unlock()
	parsed, _ := url.Parse(rawURL)
	switch operation {
	case OperationCinemas:
		return append([]byte(nil), g.cinemasBody...), nil
	case OperationProgram:
		return []byte(`{"movieIds":{"releaseAsc":["1001"],"releaseDesc":["1001"],"titleAsc":["1001"]},"scheduledDays":{"1001":["2026-08-25"]}}`), nil
	case OperationMovies:
		return []byte(`[{"id":"1001","title":"Synthetic","runtime":5400,"poster":null,"genres":"Drame"}]`), nil
	case OperationSchedule:
		var theater struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal([]byte(parsed.Query().Get("theaters")), &theater)
		if theater.ID == g.failTheater {
			return nil, &RequestError{Operation: OperationSchedule, Category: CategoryStatus, StatusCode: 400}
		}
		g.mu.Lock()
		if g.staleSchedules > 0 {
			g.staleSchedules--
			g.mu.Unlock()
			return []byte(fmt.Sprintf(`{"%s":{"schedule":{}}}`, theater.ID)), nil
		}
		g.mu.Unlock()
		body := fmt.Sprintf(`{"%s":{"schedule":{"1001":{"2026-08-25":[{"id":"show-%s","startsAt":"2026-08-25T20:00:00","tags":["Localization.Language.French"],"data":{"ticketing":[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/synthetic/r/%d"]}]}}]}}}}`, theater.ID, theater.ID, 10000+g.RequestCount())
		return []byte(body), nil
	default:
		return nil, fmt.Errorf("unexpected operation")
	}
}

func (g *nationalGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.requests
}

func (g *fakeGetter) Get(_ context.Context, operation Operation, rawURL string) ([]byte, error) {
	g.mu.Lock()
	g.requests = append(g.requests, rawURL)
	g.mu.Unlock()
	if operation == OperationCinemas {
		return readFixture("cinemas.json")
	}
	parsed, _ := url.Parse(rawURL)
	switch operation {
	case OperationProgram:
		return readFixture(map[string]string{"W8010": "program_w8010.json", "P0867": "program_p0867.json"}[parsed.Query().Get("theaterId")])
	case OperationMovies:
		if g.malformedMovies {
			return []byte(`[{"id":"1001","title":"Film","runtime":5400,"poster":null,"genres":42}]`), nil
		}
		return readFixture("movies.json")
	case OperationSchedule:
		if g.scheduleUnavailable {
			return nil, &RequestError{Operation: OperationSchedule, Category: CategoryServer, StatusCode: 500}
		}
		if g.maxScheduleWindowDays > 0 {
			from, _ := time.Parse("2006-01-02T15:04:05", parsed.Query().Get("from"))
			to, _ := time.Parse("2006-01-02T15:04:05", parsed.Query().Get("to"))
			if int(to.Sub(from).Hours()/24) > g.maxScheduleWindowDays {
				return nil, &RequestError{Operation: OperationSchedule, Category: CategoryServer, StatusCode: 500}
			}
		}
		var theater struct {
			ID string `json:"id"`
		}
		_ = jsonUnmarshal([]byte(parsed.Query().Get("theaters")), &theater)
		return readFixture(map[string]string{"W8010": "schedule_w8010.json", "P0867": "schedule_p0867.json"}[theater.ID])
	default:
		return nil, fmt.Errorf("unexpected operation")
	}
}

func TestScheduleWindowFallbackStopsWhenSingleDayProbeFails(t *testing.T) {
	getter := &fakeGetter{scheduleUnavailable: true}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	program := map[string][]string{"1001": {"2026-08-25", "2026-08-26"}}
	_, err = fetchCompleteSchedule(context.Background(), getter, cinema{id: "W8010", timeZone: schedule.Timezone}, program, nil, location, "2026-08-25", "2026-08-26", "")
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryServer || getter.RequestCount() != 2 {
		t.Fatalf("requests=%d err=%v", getter.RequestCount(), err)
	}
}

func TestSyncSplitsRejectedScheduleWindowsWithoutDroppingDates(t *testing.T) {
	getter := &fakeGetter{maxScheduleWindowDays: 1}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-25", Now: now})
	if err != nil || len(data.Showtimes) != 3 || summary.Showtimes != 3 || summary.Requests != 8 {
		t.Fatalf("showtimes=%d summary=%+v err=%v", len(data.Showtimes), summary, err)
	}
	windows := []string{}
	getter.mu.Lock()
	for _, rawURL := range getter.requests {
		parsed, _ := url.Parse(rawURL)
		if parsed.Path == "/api/gatsby-source-boxofficeapi/schedule" {
			var theater struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal([]byte(parsed.Query().Get("theaters")), &theater)
			if theater.ID == "W8010" {
				windows = append(windows, parsed.Query().Get("from")+".."+parsed.Query().Get("to"))
			}
		}
	}
	getter.mu.Unlock()
	sort.Strings(windows)
	want := []string{"2026-08-25T03:00:00..2026-08-26T03:00:00", "2026-08-25T03:00:00..2026-08-27T03:00:00", "2026-08-26T03:00:00..2026-08-27T03:00:00"}
	if !reflect.DeepEqual(windows, want) {
		t.Fatalf("windows=%v", windows)
	}
}

func TestSyncFailureSummaryRetainsCompletedPhaseAndRequestCounters(t *testing.T) {
	getter := &fakeGetter{malformedMovies: true}
	_, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-25", Now: time.Now()})
	if err == nil || summary.Cinemas != 2 || summary.Movies != 0 || summary.Jobs != 0 || summary.Requests != 4 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func (g *fakeGetter) RequestCount() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.requests) }

func readFixture(name string) ([]byte, error)          { return os.ReadFile(filepath.Join("testdata", name)) }
func jsonUnmarshal(body []byte, destination any) error { return json.Unmarshal(body, destination) }

func TestSyncBuildsCompleteDeterministicDataset(t *testing.T) {
	getter := &fakeGetter{}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.FixedZone("test", 3600))
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-25", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if data.Provider != schedule.ProviderCGR || data.Scope != schedule.ScopeAll || data.Window.Through != "2026-08-26" || len(data.Theaters) != 2 || len(data.Showtimes) != 3 || summary.Cinemas != 2 || summary.Movies != 2 || summary.Requests != 6 {
		t.Fatalf("data=%+v summary=%+v", data, summary)
	}
	if data.GeneratedAt.Location() != time.UTC || data.Theaters[0].ID != "cgr-P0867" || data.Showtimes[0].TheaterID != "cgr-P0867" {
		t.Fatalf("ordering=%+v", data)
	}
	secondGetter := &fakeGetter{}
	second, _, err := Sync(context.Background(), secondGetter, SyncOptions{From: "2026-08-25", Now: now})
	if err != nil || !reflect.DeepEqual(data, second) {
		t.Fatalf("second err=%v equal=%v", err, reflect.DeepEqual(data, second))
	}
}

func TestSyncRequiresEveryTheaterInNationalFanout(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	getter := newNationalGetter(t, 73)
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-25", Now: now})
	if err != nil || len(data.Theaters) != 73 || len(data.Showtimes) != 73 || summary.Cinemas != 73 || summary.Movies != 1 || summary.Jobs != 73 || summary.Showtimes != 73 || summary.Requests != 148 {
		t.Fatalf("theaters=%d showtimes=%d summary=%+v err=%v", len(data.Theaters), len(data.Showtimes), summary, err)
	}
	failed := newNationalGetter(t, 73)
	failed.failTheater = "A0037"
	data, summary, err = Sync(context.Background(), failed, SyncOptions{From: "2026-08-25", Now: now})
	if err == nil || len(data.Theaters) != 0 || len(data.Showtimes) != 0 || summary.Cinemas != 73 || summary.Movies != 1 || summary.Jobs != 73 || summary.Showtimes != 0 {
		t.Fatalf("failed theaters=%d showtimes=%d summary=%+v err=%v", len(data.Theaters), len(data.Showtimes), summary, err)
	}
}

func TestSyncRetriesOneChangedProviderSnapshot(t *testing.T) {
	getter := newNationalGetter(t, 1)
	getter.staleSchedules = 1
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-25", Now: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)})
	if err != nil || len(data.Theaters) != 1 || len(data.Showtimes) != 1 || summary.Cinemas != 1 || summary.Movies != 1 || summary.Showtimes != 1 || summary.Requests != 6 {
		t.Fatalf("theaters=%d showtimes=%d summary=%+v err=%v", len(data.Theaters), len(data.Showtimes), summary, err)
	}
}

func TestMovieBatchesNeverExceedFifty(t *testing.T) {
	ids := make([]string, 121)
	for index := range ids {
		ids[index] = fmt.Sprint(index + 1)
	}
	batches := batchMovieIDs(ids)
	if len(batches) != 3 || len(batches[0]) != 50 || len(batches[1]) != 50 || len(batches[2]) != 21 {
		t.Fatalf("batch sizes=%d/%d/%d", len(batches[0]), len(batches[1]), len(batches[2]))
	}
	for _, batch := range batches {
		parsed, _ := url.Parse(moviesURL(batch))
		if !operationMatchesURL(OperationMovies, parsed) {
			t.Fatalf("invalid batch URL with %d IDs", len(batch))
		}
	}
}
