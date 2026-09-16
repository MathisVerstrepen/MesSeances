package grandecran

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type fakeGetter struct {
	calls atomic.Int64
	get   func(context.Context, Operation, string) ([]byte, error)
}

func (f *fakeGetter) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	f.calls.Add(1)
	return f.get(ctx, op, raw)
}
func (f *fakeGetter) RequestCount() int { return int(f.calls.Load()) }

func fixtureGetter(t *testing.T) *fakeGetter {
	t.Helper()
	cinemas := []cinema{testCinema("P9488"), testCinema("G028P")}
	return &fakeGetter{get: func(ctx context.Context, op Operation, raw string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u, _ := url.Parse(raw)
		switch op {
		case OperationCinemas:
			return testJSON(t, map[string]any{"data": map[string]any{"allTheater": map[string]any{"nodes": cinemas}}}), nil
		case OperationProgram:
			if u.Query().Get("theaterId") == "G028P" {
				return []byte(`{"scheduledDays":{}}`), nil
			}
			return []byte(`{"movieIds":{"titleAsc":[1,"cEvent_1"]},"scheduledDays":{"1":["2026-09-15"],"cEvent_1":["2029-07-01"]}}`), nil
		case OperationMovies:
			return []byte(`[{"id":1,"title":"Film","runtime":7200},{"id":"cEvent_1","title":"Event"}]`), nil
		case OperationSchedule:
			s := testSession(t, "same", "2026-09-15T07:30:00", "https://achat.grandecran.fr/test/r/1")
			e := testSession(t, "same", "2029-07-02T01:00:00", "https://achat.grandecran.fr/test/r/2")
			return testJSON(t, scheduleResponse{"P9488": {Schedule: map[string]map[string][]showtimeResponse{"1": {"2026-09-15": {s}}, "cEvent_1": {"2029-07-01": {e}}}}}), nil
		case OperationRoom:
			return []byte(`<script>{"auditorium_showtime":"salle-2"}</script>`), nil
		}
		return nil, errors.New("unexpected operation")
	}}
}
func TestSyncDynamicHorizonEmptyCinemaStableRecovery(t *testing.T) {
	g := fixtureGetter(t)
	o := SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	base, _, err := syncSnapshot(t.Context(), g, o)
	if err != nil {
		t.Fatal(err)
	}
	d, s, err := Sync(t.Context(), g, o)
	if err != nil || d.Window.Through != "2029-07-01" || len(d.Theaters) != 2 || len(d.Theaters[0].AvailableDates) != 0 || len(d.Showtimes) != 2 || s.RoomsAttempted != 2 || s.RoomsRecovered != 2 {
		t.Fatalf("summary=%+v err=%v", s, err)
	}
	for i, r := range d.Showtimes {
		if r.ID != base.Showtimes[i].ID || r.Room != "Salle 2" || !r.StartTime.Equal(r.EndTime) {
			t.Fatal("identity/source-end drift")
		}
	}
	if d.Showtimes[1].Movie.ProviderID != "cEvent_1" || d.Showtimes[1].Movie.RuntimeMinutes != 0 {
		t.Fatal("event lost")
	}
}
func TestSyncFontenayBookingRoute(t *testing.T) {
	for _, tc := range []struct {
		route string
		valid bool
	}{{"r/12345", true}, {"reserver/r/12345", true}, {"reserver/reserver/r/12345", false}} {
		t.Run(tc.route, func(t *testing.T) {
			const theaterID = "G034G"
			booking := "https://achat.grandecran.fr/fontenay-le-comte/" + tc.route
			var rooms atomic.Int64
			g := &fakeGetter{get: func(_ context.Context, op Operation, raw string) ([]byte, error) {
				switch op {
				case OperationCinemas:
					return testJSON(t, map[string]any{"data": map[string]any{"allTheater": map[string]any{"nodes": []cinema{testCinema(theaterID)}}}}), nil
				case OperationProgram:
					return []byte(`{"movieIds":{"titleAsc":[1]},"scheduledDays":{"1":["2026-09-16"]}}`), nil
				case OperationMovies:
					return []byte(`[{"id":1,"title":"Film","runtime":7200}]`), nil
				case OperationSchedule:
					s := testSession(t, "fontenay-session", "2026-09-16T20:00:00", booking)
					return testJSON(t, scheduleResponse{theaterID: {Schedule: map[string]map[string][]showtimeResponse{"1": {"2026-09-16": {s}}}}}), nil
				case OperationRoom:
					rooms.Add(1)
					u, err := url.Parse(raw)
					if err != nil || raw != booking || !operationMatchesURL(op, u) {
						return nil, errors.New("unexpected room booking URL")
					}
					return []byte(`{"auditorium_showtime":"salle-2"}`), nil
				default:
					return nil, errors.New("unexpected operation")
				}
			}}
			d, s, err := Sync(t.Context(), g, SyncOptions{From: "2026-09-16", Now: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)})
			if !tc.valid {
				if err == nil || len(d.Showtimes) != 0 || rooms.Load() != 0 {
					t.Fatal("invalid booking must abort acquisition before room enrichment")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.Cinemas != 1 || s.Movies != 1 || s.Showtimes != 1 || len(d.Theaters) != 1 || len(d.Showtimes) != 1 || rooms.Load() != 1 || s.RoomsRecovered != 1 {
				t.Fatalf("incomplete acquisition: summary=%+v room_requests=%d", s, rooms.Load())
			}
			if r := d.Showtimes[0]; r.TheaterID != "grandecran-"+theaterID || r.BookingURL != booking || r.Room != "Salle 2" {
				t.Fatal("Fontenay showing or booking URL not preserved")
			}
		})
	}
}

func TestDiscoveryFallbackRetryAndCoverageAtomicity(t *testing.T) {
	for _, mode := range []string{"asset", "stale", "coverage", "block"} {
		t.Run(mode, func(t *testing.T) {
			g := fixtureGetter(t)
			original := g.get
			var discovery, cinemas atomic.Int64
			g.get = func(ctx context.Context, op Operation, raw string) ([]byte, error) {
				if op == OperationCinemas {
					cinemas.Add(1)
					if raw == CinemasURL && mode != "coverage" {
						if mode == "block" {
							return nil, &RequestError{Kind: syncproxy.FailureChallenge}
						}
						return nil, &RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}
					}
					if mode == "stale" {
						return nil, &RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}
					}
				}
				if op == OperationDiscovery {
					discovery.Add(1)
					return []byte(`https://cms-assets.webediamovies.pro/prod/grandecran/current-build/public/`), nil
				}
				if mode == "coverage" && op == OperationSchedule {
					return []byte(`{"P9488":{"schedule":{}}}`), nil
				}
				return original(ctx, op, raw)
			}
			d, _, err := Sync(t.Context(), g, SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)})
			if mode == "asset" {
				if err != nil || len(d.Showtimes) != 2 || discovery.Load() != 1 {
					t.Fatal("asset fallback", err)
				}
				return
			}
			if err == nil || len(d.Showtimes) != 0 {
				t.Fatal("partial acquisition published")
			}
			if mode == "stale" && (discovery.Load() != 2 || cinemas.Load() != 4) {
				t.Fatal("unbounded stale retry")
			}
			if mode == "coverage" && cinemas.Load() != 2 {
				t.Fatal("coverage retry missing")
			}
			if mode == "block" && discovery.Load() != 0 {
				t.Fatal("challenge fallback")
			}
		})
	}
}
func TestSplitWindowTerminatesAndCaches(t *testing.T) {
	g := fixtureGetter(t)
	original := g.get
	var full, single atomic.Int64
	g.get = func(ctx context.Context, op Operation, raw string) ([]byte, error) {
		u, _ := url.Parse(raw)
		from, _ := time.Parse("2006-01-02T15:04:05", u.Query().Get("from"))
		to, _ := time.Parse("2006-01-02T15:04:05", u.Query().Get("to"))
		if op == OperationSchedule && to.Sub(from) > 24*time.Hour {
			full.Add(1)
			return nil, &RequestError{Kind: syncproxy.FailureServer, StatusCode: 500}
		}
		if op == OperationSchedule {
			single.Add(1)
		}
		return original(ctx, op, raw)
	}
	d, _, err := syncSnapshot(t.Context(), g, SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil || len(d.Showtimes) != 2 || single.Load() != 2 || full.Load() > 30 {
		t.Fatalf("split full=%d single=%d err=%v", full.Load(), single.Load(), err)
	}
}
func TestRoomParsingCacheLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`<form> Salle 1 </form>`, ""}, {`{"auditorium_showtime":"salle-2"}`, "Salle 2"}, {`{\"auditorium_showtime\":\"salle-12\"}`, "Salle 12"}, {`{"auditorium_showtime":"salle-0"}`, ""}, {`{"auditorium_showtime":"salle-1","auditorium_showtime":"salle-2"}`, ""}, {`{"auditorium_showtime":"salle-1","auditorium_showtime":null}`, ""},
	} {
		if got := parseRoom([]byte(tc.body)); got != tc.want {
			t.Errorf("marker got=%q want=%q", got, tc.want)
		}
	}
	rows := []schedule.ShowtimeRecord{{BookingURL: "https://achat.grandecran.fr/test/r/1"}, {BookingURL: "https://achat.grandecran.fr/test/r/1"}, {BookingURL: "https://achat.grandecran.fr/test/r/2"}, {Room: "API room", BookingURL: "https://achat.grandecran.fr/test/r/3"}}
	g := &fakeGetter{get: func(context.Context, Operation, string) ([]byte, error) {
		return []byte(`{"auditorium_showtime":"salle-1"}`), nil
	}}
	s := SyncSummary{}
	if err := enrichRooms(t.Context(), g, rows, &s, time.Second, 1); err != nil || g.RequestCount() != 1 || s.RoomsAttempted != 1 || s.RoomsRecovered != 1 || s.RoomsBudgetSkipped != 1 || s.RoomsUnresolved != 1 || rows[0].Room != rows[1].Room || rows[3].Room != "API room" {
		t.Fatalf("summary=%+v err=%v", s, err)
	}
	for _, mode := range []string{"timeout", "parent", "block", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			g := &fakeGetter{get: func(ctx context.Context, _ Operation, _ string) ([]byte, error) {
				if mode == "block" {
					return nil, &RequestError{Kind: syncproxy.FailureChallenge}
				}
				if mode == "ordinary" {
					return nil, &RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}
				}
				if mode == "parent" {
					cancel()
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			r := []schedule.ShowtimeRecord{{BookingURL: rows[0].BookingURL}}
			before := append([]schedule.ShowtimeRecord(nil), r...)
			err := enrichRooms(ctx, g, r, &SyncSummary{}, time.Millisecond, 1)
			if (mode == "parent" || mode == "block") != (err != nil) || !reflect.DeepEqual(r, before) {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
	if parseRoom([]byte(strings.Repeat("x", MaxHTMLBytes+1))) != "" {
		t.Fatal("oversized marker")
	}
}
