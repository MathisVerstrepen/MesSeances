package veo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fixtureGetter struct {
	mu     sync.Mutex
	calls  int
	modify func(Operation, string, []byte) ([]byte, error)
}

func (f *fixtureGetter) RequestCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fixtureGetter) Checkout(ctx context.Context, raw string, _ time.Time) ([]byte, error) {
	return f.Get(ctx, OperationCheckout, raw)
}
func (f *fixtureGetter) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	u, _ := url.Parse(raw)
	id := "P1028"
	if u.Host == "sete.veocinemas.fr" {
		id = "W0067"
	}
	var b []byte
	switch op {
	case OperationCinemas:
		b = []byte(fmt.Sprintf(`{"data":{"allTheater":{"nodes":[{"id":%q,"name":"Véo fixture","timeZone":"Europe/Paris","practicalInfo":{"location":{"address":"1 rue du cinéma","city":"Fixture","zip":"75001"}}}]}}}`, id))
	case OperationProgram:
		b = []byte(`{"movieIds":{"titleAsc":[123],"releaseAsc":[123]},"scheduledDays":{"123":["2026-10-14","2026-10-15","2026-10-28"]}}`)
	case OperationMovies:
		b = []byte(`[{"id":123,"title":"Fixture movie","runtime":7200,"releaseDate":"2027-01-01","poster":"https://fr.web.img5.acsta.net/c_310_420/pictures/fixture.jpg"}]`)
	case OperationSchedule:
		b = []byte(fmt.Sprintf(`{%q:{"schedule":{"123":{"2026-10-15":[%s],"2026-10-28":[%s]}}}}`, id, fixtureSession("same-id", "2026-10-15T23:30:00", "Salle source"), fixtureSession("later-id", "2026-10-28T20:00:00", "")))
	case OperationCheckout:
		b = []byte(`<div id="s_fin">fin prévue à 01h15</div><script>{"auditorium_showtime":"salle-2"}</script>`)
	case OperationCastelia:
		b = []byte(`{"jsonrpc":"2.0","id":1,"result":{"schedule":{"name":"Castelia","address":"1 rue","city":"Castelginest","zip_code":"31780","ems":{"id":"1361"},"events":[{"id":"A12B3","title":"Ticket movie","duration":90,"bill_url":"https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg","sessions":[{"id":"emsx136100000001","date":"202610152000","version":"VO","features":["ST"],"first_part_duration":10,"hall_name":"Salle 1"}]}]}}}`)
	default:
		return nil, errShape
	}
	if f.modify != nil {
		return f.modify(op, raw, b)
	}
	return b, nil
}
func fixtureSession(id, start, room string) string {
	return fmt.Sprintf(`{"id":%q,"startsAt":%q,"tags":["Localization.Version.Original","Format.Projection.3D"],"screen":{"name":%q},"data":{"ticketing":[{"provider":"default","type":"DESKTOP","urls":["https://achat.veocinemas.fr/fixture/r/123"]}]}}`, id, start, room)
}
func fixtureOptions() SyncOptions {
	return SyncOptions{From: "2026-10-15", Now: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
}
func TestSyncAtomicFullHorizonAndExactEnds(t *testing.T) {
	f := &fixtureGetter{}
	// Use an empty source poster here: ticketing posters are identity-bound.
	f.modify = func(op Operation, _ string, b []byte) ([]byte, error) {
		if op == OperationCastelia {
			b = []byte(strings.ReplaceAll(string(b), `https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg`, ``))
		}
		return b, nil
	}
	d, s, err := Sync(t.Context(), f, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 3 || len(d.Showtimes) != 5 || d.Window.Through != "2026-10-28" || s.Checkouts != 4 || s.ExactEnds != 4 {
		t.Fatalf("counts dataset=%+v summary=%+v", d, s)
	}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	for _, r := range d.Showtimes {
		if r.Movie.ReleaseDate != "" {
			t.Fatal("unverified release copied")
		}
		if r.TheaterID == "veo-EMS1361" {
			if r.EndTime.Sub(r.StartTime) != 100*time.Minute || r.Language != schedule.LanguageVOSTFR || r.ProviderVersion != "VOSTF" {
				t.Fatalf("ticketing row %+v", r)
			}
			continue
		}
		if r.EndTime.Format("15:04") != "01:15" || r.Language != schedule.LanguageVOSTFR || r.Format != schedule.Format3D {
			t.Fatalf("box row %+v", r)
		}
		if r.ServiceDate == "2026-10-15" && r.Room != "Salle source" || r.ServiceDate == "2026-10-28" && r.Room != "Salle 2" {
			t.Fatalf("room overwritten/not backfilled: %+v", r)
		}
	}
	for _, c := range d.Coverage {
		if c.TheaterID == "veo-EMS1361" {
			if c.Status != schedule.CoverageUnknown {
				t.Fatal(c)
			}
		} else if c.Status != schedule.CoverageComplete {
			t.Fatal(c)
		}
	}
}
func TestSyncFailureNeverReturnsPartialDataset(t *testing.T) {
	for _, op := range []Operation{OperationCinemas, OperationProgram, OperationMovies, OperationSchedule, OperationCheckout, OperationCastelia} {
		t.Run(string(op), func(t *testing.T) {
			f := &fixtureGetter{modify: func(got Operation, _ string, b []byte) ([]byte, error) {
				if got == op {
					return nil, errors.New("fixture failure")
				}
				if got == OperationCastelia {
					return []byte(strings.ReplaceAll(string(b), `https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg`, ``)), nil
				}
				return b, nil
			}}
			d, _, err := Sync(t.Context(), f, fixtureOptions())
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatalf("partial failure: %+v %v", d, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d, _, err := Sync(ctx, &fixtureGetter{}, fixtureOptions())
	if !errors.Is(err, context.Canceled) || len(d.Theaters) != 0 {
		t.Fatal(err)
	}
}

func TestSyncRetainsStartedSessionsWithStableCutoff(t *testing.T) {
	o := fixtureOptions()
	o.Now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) // 14:00 Paris
	cutoff := o.Now
	f := &fixtureGetter{modify: func(op Operation, raw string, b []byte) ([]byte, error) {
		if op == OperationCinemas {
			// Model acquisition advancing well past the initial cutoff. The run
			// must use its captured value, not a clock re-read or mutable option.
			o.Now = cutoff.AddDate(1, 0, 0)
		}
		if op == OperationSchedule {
			id := "P1028"
			if strings.HasPrefix(raw, SeteOrigin) {
				id = "W0067"
			}
			return []byte(fmt.Sprintf(`{%q:{"schedule":{"123":{"2026-10-15":[%s,%s,%s],"2026-10-28":[%s]}}}}`, id,
				fixtureSession("started", "2026-10-15T13:59:59", ""),
				fixtureSession("boundary", "2026-10-15T14:00:00", "API room"),
				fixtureSession("future", "2026-10-15T14:00:01", "API future room"),
				fixtureSession("later", "2026-10-28T20:00:00", ""))), nil
		}
		if op == OperationCastelia {
			// A started Castelia session still has its runtime + first-part end.
			return []byte(strings.ReplaceAll(strings.ReplaceAll(string(b), `https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg`, ``), `202610152000`, `202610151300`)), nil
		}
		return b, nil
	}}
	d, s, err := Sync(t.Context(), f, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Showtimes) != 9 || s.Checkouts != 4 || s.ExactEnds != 4 || !d.GeneratedAt.Equal(cutoff) || !s.GeneratedAt.Equal(cutoff) {
		t.Fatal("cutoff changed, sessions dropped or wrong checkout count", len(d.Showtimes), s)
	}
	started := 0
	for _, row := range d.Showtimes {
		if row.TheaterID == "veo-EMS1361" {
			if row.EndTime.Sub(row.StartTime) != 100*time.Minute {
				t.Fatal("started Castelia timing changed")
			}
			continue
		}
		if !row.StartTime.After(cutoff) {
			started++
			if !row.EndTime.Equal(row.StartTime) || row.Room != "" && row.Room != "API room" {
				t.Fatal("started room/end overwritten", row)
			}
		} else if !row.EndTime.After(row.StartTime) {
			t.Fatal("future checkout skipped after acquisition advanced")
		}
	}
	if started != 4 {
		t.Fatal("boundary session not retained", started)
	}
	for _, theater := range d.Theaters {
		if theater.ProviderID != "EMS1361" && strings.Join(theater.AvailableDates, ",") != "2026-10-15,2026-10-28" {
			t.Fatal("advertised dates changed", theater.AvailableDates)
		}
	}
	for _, c := range d.Coverage {
		if c.TheaterID != "veo-EMS1361" && c.Status != schedule.CoverageComplete {
			t.Fatal("started policy changed advertised evidence", c)
		}
	}
}

func TestStartedCheckoutsSkippedButFutureFailureStillAtomic(t *testing.T) {
	for _, future := range []bool{false, true} {
		t.Run(fmt.Sprint(future), func(t *testing.T) {
			o := fixtureOptions()
			if !future {
				o.Now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			} else {
				o.Now = time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC)
			}
			var checkouts atomic.Int32
			f := &fixtureGetter{modify: func(op Operation, _ string, b []byte) ([]byte, error) {
				if op == OperationCheckout {
					checkouts.Add(1)
					return nil, errors.New("future checkout failure")
				}
				if op == OperationCastelia {
					return []byte(strings.ReplaceAll(string(b), `https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg`, ``)), nil
				}
				return b, nil
			}}
			d, s, err := Sync(t.Context(), f, o)
			if future {
				if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 || checkouts.Load() == 0 {
					t.Fatal("future failure published a partial dataset", err)
				}
				return
			}
			if err != nil || checkouts.Load() != 0 || len(d.Showtimes) != 5 || s.Checkouts != 0 || s.ExactEnds != 0 {
				t.Fatal("started checkouts requested or sessions dropped", err, checkouts.Load())
			}
			for _, row := range d.Showtimes {
				if row.TheaterID != "veo-EMS1361" && (!row.EndTime.Equal(row.StartTime) || row.ServiceDate == "2026-10-28" && row.Room != "") {
					t.Fatal("started row enriched", row)
				}
			}
		})
	}
}

func TestEmptyProgramCinemaRemainsDiscoverableWithoutInventedCoverage(t *testing.T) {
	f := &fixtureGetter{modify: func(op Operation, _ string, b []byte) ([]byte, error) {
		if op == OperationProgram {
			return []byte(`{"movieIds":{"titleAsc":[],"releaseAsc":[]},"scheduledDays":{}}`), nil
		}
		if op == OperationCastelia {
			return []byte(strings.ReplaceAll(string(b), `https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg`, ``)), nil
		}
		return b, nil
	}}
	d, _, err := Sync(t.Context(), f, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 3 || len(d.Showtimes) != 1 {
		t.Fatal("empty cinemas lost")
	}
	for _, cinema := range d.Theaters {
		if cinema.ProviderID != "EMS1361" && len(cinema.AvailableDates) != 0 {
			t.Fatal("invented dates")
		}
	}
	for _, coverage := range d.Coverage {
		if coverage.TheaterID != "veo-EMS1361" {
			t.Fatal("invented empty completeness")
		}
	}
}
func TestProgramAndScheduleFailClosed(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	p, err := parseProgram([]byte(`{"movieIds":{"titleAsc":["123"],"releaseAsc":[123]},"scheduledDays":{"123":["2026-10-14","2026-10-15","2026-10-28"]}}`), "2026-10-15", loc)
	if err != nil || len(p["123"]) != 2 {
		t.Fatalf("%v %+v", err, p)
	}
	for _, body := range []string{`null`, `{}`, `{"movieIds":{"titleAsc":[],"releaseAsc":[]},"scheduledDays":{"123":["2026-10-15"]}}`, `{"movieIds":{"titleAsc":[123],"releaseAsc":[123]},"scheduledDays":{"123":["2026-02-30"]}}`} {
		if _, err := parseProgram([]byte(body), "2026-10-15", loc); err == nil {
			t.Fatal(body)
		}
	}
	c := cinema{ID: "P1028"}
	movies := map[string]schedule.MovieRecord{"123": {Provider: schedule.ProviderVeo, ProviderID: "123", Slug: "veo-film-123", Title: "Title", RuntimeMinutes: 0}}
	scheduleBody := func(sessions string) []byte {
		return []byte(`{"P1028":{"schedule":{"123":{"2026-10-15":` + sessions + `}}}}`)
	}
	if _, _, err := parseSchedule(scheduleBody("["+fixtureSession("s", "2026-10-15T20:00:00", "")+"]"), c, map[string][]string{}, movies, loc, "2026-10-15", "2026-10-15", "2026-10-28"); !errors.Is(err, errSnapshotChanged) {
		t.Fatal(err)
	}
	_, unknown, err := parseSchedule(scheduleBody("[]"), c, map[string][]string{"123": {"2026-10-15"}}, movies, loc, "2026-10-15", "2026-10-15", "2026-10-28")
	if err != nil || !unknown["2026-10-15"] {
		t.Fatal(err, unknown)
	}
	if _, _, err := parseSchedule(scheduleBody("[]"), c, p, movies, loc, "", "2026-10-15", "2026-10-28"); !errors.Is(err, errSnapshotChanged) {
		t.Fatal(err)
	}
	duplicate := fixtureSession("same", "2026-10-15T20:00:00", "")
	rows, _, err := parseSchedule(scheduleBody("["+duplicate+","+duplicate+"]"), c, map[string][]string{"123": {"2026-10-15"}}, movies, loc, "", "2026-10-15", "2026-10-28")
	if err != nil || len(rows) != 1 {
		t.Fatal(err, len(rows))
	}
	if _, _, err := parseSchedule(scheduleBody("["+duplicate+","+fixtureSession("same", "2026-10-15T21:00:00", "")+"]"), c, map[string][]string{"123": {"2026-10-15"}}, movies, loc, "", "2026-10-15", "2026-10-28"); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}
func TestRequestShapesAndBatchBounds(t *testing.T) {
	valid := []struct {
		op  Operation
		raw string
	}{{OperationCinemas, MainOrigin + CatalogPath}, {OperationProgram, programURL(MainOrigin, "P1028")}, {OperationMovies, moviesURL(SeteOrigin, []string{"123", "cabcdef"})}, {OperationSchedule, scheduleURL(MainOrigin, "P1028", "2026-10-15", "2026-10-28")}, {OperationCastelia, ProgramURL}, {OperationCheckout, "https://achat.veocinemas.fr/fixture/r/1"}}
	for _, v := range valid {
		u, _ := url.Parse(v.raw)
		if !operationMatchesURL(v.op, u) {
			t.Fatal(v.raw)
		}
	}
	for _, raw := range []string{MainOrigin + CatalogPath + "?x=1", "http://veocinemas.fr" + CatalogPath, "https://veocinemas.fr.evil.test" + CatalogPath, "https://user@veocinemas.fr" + CatalogPath, "https://veocinemas.fr:443" + CatalogPath, "https://veocinemas.fr/%2e%2e" + CatalogPath} {
		u, _ := url.Parse(raw)
		if allowedURL(u) {
			t.Fatal(raw)
		}
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	for _, batch := range batchMovieIDs(MainOrigin, ids) {
		if len(batch) > MovieBatchSize || len(moviesURL(MainOrigin, batch)) > MaxRequestURLBytes {
			t.Fatal(batch)
		}
	}
	q, _ := url.Parse(scheduleURL(MainOrigin, "P1028", "2026-10-15", "2026-10-28"))
	if q.Query().Get("to") != "2026-10-29T03:00:00" || q.Query().Get("theaters") != `{"id":"P1028","timeZone":"Europe/Paris"}` {
		t.Fatal(q)
	}
}
func TestCheckoutConcurrencyBoundAndCancellation(t *testing.T) {
	var active, peak atomic.Int32
	g := getterFunc(func(ctx context.Context, _ Operation, _ string) ([]byte, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
			return []byte(`{"auditorium_showtime":null}`), nil
		}
	})
	rows := make([]schedule.ShowtimeRecord, 50)
	now := fixtureOptions().Now
	for i := range rows {
		rows[i] = schedule.ShowtimeRecord{TheaterID: "veo-P1028", BookingURL: fmt.Sprintf("https://achat.veocinemas.fr/fixture/r/%d", i+1), StartTime: now.Add(time.Hour)}
	}
	if err := enrichCheckouts(t.Context(), g, rows, &SyncSummary{}, now); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > WorkerCount || peak.Load() < 2 {
		t.Fatal(peak.Load())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := enrichCheckouts(ctx, g, rows, &SyncSummary{}, now); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type getterFunc func(context.Context, Operation, string) ([]byte, error)

func (f getterFunc) Get(ctx context.Context, op Operation, u string) ([]byte, error) {
	return f(ctx, op, u)
}
func (getterFunc) RequestCount() int { return 0 }
func (f getterFunc) Checkout(ctx context.Context, raw string, _ time.Time) ([]byte, error) {
	return f(ctx, OperationCheckout, raw)
}
func TestMovieRuntimePosterAndVersions(t *testing.T) {
	for _, tc := range []struct {
		runtime string
		want    int
		valid   bool
	}{{"null", 0, true}, {"7200", 120, true}, {"60", 1, true}, {"-1", 0, false}, {"0", 0, false}, {"59", 0, false}} {
		m, err := parseMovies([]byte(`[{"id":123,"title":"Title","runtime":` + tc.runtime + `,"poster":""}]`))
		if (err == nil) != tc.valid {
			t.Fatalf("%s %v", tc.runtime, err)
		}
		if tc.valid && m["123"].RuntimeMinutes != tc.want {
			t.Fatal(m)
		}
	}
	for _, tc := range []struct {
		tags     []string
		language schedule.Language
		version  string
	}{{[]string{"Localization.Version.Original"}, schedule.LanguageVOSTFR, "VOSTF"}, {[]string{"Localization.Language.French"}, schedule.LanguageVF, "VF"}, {[]string{"Localization.Language.French", "Showtime.Accessibility.OpenCaption"}, schedule.LanguageVFSTF, "VFSTF"}} {
		l, v, err := normalizeVersion(tc.tags)
		if err != nil || l != tc.language || v != tc.version {
			t.Fatal(l, v, err)
		}
	}
	var n minutes
	if json.Unmarshal([]byte(`"20"`), &n) != nil || n != 20 {
		t.Fatal(n)
	}
}
