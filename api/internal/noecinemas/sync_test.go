package noecinemas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type fixtureGetter struct {
	cinemas             json.RawMessage
	programs, schedules map[string]json.RawMessage
	movies              json.RawMessage
	mu                  sync.Mutex
	calls               map[Operation]int
	hook                func(Operation, string, []byte) ([]byte, error)
}

func fixture(t *testing.T) *fixtureGetter {
	t.Helper()
	body, err := os.ReadFile("testdata/source.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cinemas             json.RawMessage
		Programs, Schedules map[string]json.RawMessage
		Movies              json.RawMessage
	}
	if err = json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	return &fixtureGetter{cinemas: f.Cinemas, programs: f.Programs, schedules: f.Schedules, movies: f.Movies, calls: map[Operation]int{}}
}
func (g *fixtureGetter) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls[op]++
	u, _ := url.Parse(raw)
	var body []byte
	switch op {
	case OperationCinemas:
		body = g.cinemas
	case OperationProgram:
		body = g.programs[u.Query().Get("theaterId")]
	case OperationMovies:
		body = g.movies
	case OperationSchedule:
		var theater struct{ ID string }
		_ = json.Unmarshal([]byte(u.Query().Get("theaters")), &theater)
		body = g.schedules[theater.ID]
	case OperationRoom:
		body = []byte("\xe9{\"auditorium_showtime\":\"salle-2\"}")
	default:
		return nil, errors.New("unexpected fixture operation")
	}
	if g.hook != nil {
		return g.hook(op, raw, body)
	}
	return body, nil
}
func (g *fixtureGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, v := range g.calls {
		n += v
	}
	return n
}
func fixtureOptions() SyncOptions {
	return SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
}

func TestSyncCompleteSyntheticSource(t *testing.T) {
	g := fixture(t)
	d, s, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 3 || len(d.Showtimes) != 3 || d.Window.Through != "2028-07-15" || s.RoomsAttempted != 1 || s.RoomsRecovered != 1 || s.Requests != 8 {
		t.Fatalf("summary=%+v", s)
	}
	if g.calls[OperationSchedule] != 2 || g.calls[OperationProgram] != 3 || g.calls[OperationMovies] != 1 {
		t.Fatal("unexpected fanout")
	}
	if len(d.Theaters[2].AvailableDates) != 0 {
		t.Fatal("empty cinema lost")
	}
	byTheater := map[string]schedule.ShowtimeRecord{}
	for _, r := range d.Showtimes {
		if !r.EndTime.Equal(r.StartTime) || r.FirstPartDurationMinutes != 0 {
			t.Fatal("invented end")
		}
		if r.Movie.ProviderID == "cEvent_1" {
			if r.Movie.RuntimeMinutes != 0 || r.Movie.PosterURL != "" || r.Room != "Salle source" || r.Language != schedule.LanguageVOSTFR || r.Format != schedule.Format3D {
				t.Fatalf("event=%+v", r)
			}
			continue
		}
		theaterID := strings.TrimPrefix(r.TheaterID, "noecinemas-")
		byTheater[theaterID] = r
		digest := sha256.Sum256([]byte(theaterID + "\x00shared opaque session"))
		if r.ProviderShowingID != theaterID+"-"+hex.EncodeToString(digest[:]) {
			t.Fatal("unstable identity")
		}
	}
	a, b := byTheater["P8088"], byTheater["B0181"]
	if a.ID == b.ID || a.Room != "Salle 2" || a.Language != schedule.LanguageVFSTF || a.ProviderVersion != "VFSTF" || a.Format != schedule.FormatDolby || a.ServiceDate != "2026-09-14" || b.Language != schedule.LanguageVF || b.Format != schedule.Format2D {
		t.Fatal("normalization")
	}
}
func TestSyncDriftAndFailureAtomicity(t *testing.T) {
	for _, mode := range []string{"missing-film", "unexpected-day", "unexpected-film", "room-challenge", "room-404", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			g := fixture(t)
			g.hook = func(op Operation, _ string, b []byte) ([]byte, error) {
				switch {
				case mode == "missing-film" && op == OperationMovies:
					return []byte(`[]`), nil
				case mode == "unexpected-day" && op == OperationSchedule:
					return []byte(strings.ReplaceAll(string(b), "2028-07-15", "2028-07-16")), nil
				case mode == "unexpected-film" && op == OperationSchedule:
					return []byte(strings.ReplaceAll(string(b), `"cEvent_1"`, `"cOther"`)), nil
				case mode == "room-challenge" && op == OperationRoom:
					return nil, &RequestError{Kind: syncproxy.FailureChallenge}
				case mode == "room-404" && op == OperationRoom:
					return nil, &RequestError{StatusCode: 404, Kind: syncproxy.FailureStatus}
				}
				return b, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			d, s, err := Sync(ctx, g, fixtureOptions())
			if mode == "room-404" {
				if err != nil || len(d.Showtimes) != 3 || s.RoomsUnresolved != 1 {
					t.Fatal("optional room failed snapshot")
				}
				return
			}
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("partial snapshot")
			}
			if strings.HasPrefix(mode, "unexpected") || mode == "missing-film" {
				if g.calls[OperationCinemas] != 2 {
					t.Fatal("complete retry missing")
				}
			}
		})
	}
	g := fixture(t)
	g.hook = func(op Operation, _ string, b []byte) ([]byte, error) {
		if op == OperationMovies && g.calls[op] == 1 {
			return []byte(`[]`), nil
		}
		return b, nil
	}
	if _, _, err := Sync(t.Context(), g, fixtureOptions()); err != nil || g.calls[OperationCinemas] != 2 {
		t.Fatal("drift recovery")
	}
}

type getterFunc func(context.Context, Operation, string) ([]byte, error)

func (f getterFunc) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	return f(ctx, op, raw)
}
func (getterFunc) RequestCount() int { return 0 }

func TestScheduleServerSplitBoundsAndExpiry(t *testing.T) {
	g := fixture(t)
	cinemas, err := parseCinemas(g.cinemas)
	if err != nil {
		t.Fatal(err)
	}
	movies, err := parseMovies(g.movies)
	if err != nil {
		t.Fatal(err)
	}
	l, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	var source scheduleResponse
	if err = json.Unmarshal(g.schedules["P8088"], &source); err != nil {
		t.Fatal(err)
	}
	base := source["P8088"].Schedule["1"]["2026-09-14"][0]
	p := map[string][]string{"1": {"2026-09-14", "2026-09-15", "2026-09-16"}}
	var theater cinema
	for _, c := range cinemas {
		if c.ID == "P8088" {
			theater = c
		}
	}
	for _, alwaysFail := range []bool{false, true} {
		calls := 0
		fetcher := scheduleFetcher{cinema: theater, movies: movies, location: l, cache: map[string][]schedule.ShowtimeRecord{}}
		fetcher.getter = getterFunc(func(_ context.Context, op Operation, raw string) ([]byte, error) {
			calls++
			u, _ := url.Parse(raw)
			from := u.Query().Get("from")[:10]
			to := u.Query().Get("to")[:10]
			start, _ := time.Parse(time.DateOnly, from)
			if op != OperationSchedule {
				t.Fatal("unexpected operation")
			}
			if alwaysFail || to != start.AddDate(0, 0, 1).Format(time.DateOnly) {
				return nil, &RequestError{Kind: syncproxy.FailureServer, StatusCode: 503}
			}
			r := base
			r.ID = "session-" + from
			r.StartsAt = from + "T20:00:00"
			return json.Marshal(map[string]any{"P8088": map[string]any{"schedule": map[string]any{"1": map[string]any{from: []showtimeResponse{r}}}}})
		})
		rows, err := fetcher.fetch(t.Context(), p, "2026-09-14", "2026-09-16")
		if alwaysFail {
			if err == nil || calls != 2 {
				t.Fatalf("unbounded failing split calls=%d err=%v", calls, err)
			}
		} else if err != nil || len(rows) != 3 || calls != 5 {
			t.Fatalf("split rows=%d calls=%d err=%v", len(rows), calls, err)
		}
	}
	for _, allow := range []string{"", "2026-09-14"} {
		_, err := parseSchedule([]byte(`{"P8088":{"schedule":{}}}`), theater, map[string][]string{"1": {"2026-09-14"}}, movies, l, allow)
		if (err == nil) != (allow != "") {
			t.Fatal("expiry allowance")
		}
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	batches := batchMovieIDs(ids)
	if len(batches) != 3 {
		t.Fatal("batch fanout")
	}
	for _, batch := range batches {
		if len(batch) > 50 || len(moviesURL(batch)) > MaxRequestURLBytes {
			t.Fatal("batch bounds")
		}
	}
}
