package veo

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// Opt-in read-only sample: never invokes Sync or publication, never a chain
// checkout crawl. Logical calls (not retry attempts) are separately capped.
func TestProxyScheduleContractIntegration(t *testing.T) {
	path, ok := os.LookupEnv("VEO_LIVE_PROXY_FILE")
	if !ok {
		t.Skip("set VEO_LIVE_PROXY_FILE for bounded proxy contract")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" {
		t.Fatal("live contract requires approved proxy path")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("approved proxy file unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("invalid proxy configuration")
	}
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 290*time.Second)
	defer cancel()
	now := time.Now()
	logical, checkoutPages := 0, 0
	defer func() {
		t.Logf("logical_requests=%d executor_attempts=%d checkout_pages=%d", logical, c.RequestCount(), checkoutPages)
	}()
	get := func(op Operation, u string, start ...time.Time) []byte {
		t.Helper()
		logical++
		if logical > 16 {
			t.Fatal("logical request budget exceeded")
		}
		if op == OperationCheckout {
			checkoutPages++
			if checkoutPages > 4 {
				t.Fatal("checkout budget exceeded")
			}
		}
		var b []byte
		var err error
		if op == OperationCheckout {
			if len(start) != 1 {
				t.Fatal("checkout requires source session start")
			}
			b, err = c.Checkout(ctx, u, start[0])
		} else {
			b, err = c.Get(ctx, op, u)
		}
		if err != nil {
			t.Fatalf("source blocked: %v", err)
		}
		return b
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	from := serviceDate(now.In(loc))
	type checkoutSample struct {
		cinema string
		rows   []schedule.ShowtimeRecord
	}
	samples := []checkoutSample{}
	for _, sample := range []struct{ origin, id string }{{MainOrigin, "W3351"}, {SeteOrigin, "W0067"}} {
		cinemas, err := parseCinemas(get(OperationCinemas, sample.origin+CatalogPath), sample.origin)
		if err != nil {
			t.Fatal("source blocked: catalog structure")
		}
		var cinema cinema
		for _, item := range cinemas {
			if item.ID == sample.id {
				cinema = item
			}
		}
		if cinema.ID == "" {
			t.Fatal("source blocked: representative cinema absent")
		}
		p, err := parseProgram(get(OperationProgram, programURL(sample.origin, cinema.ID)), from, loc)
		if err != nil {
			t.Fatal("source blocked: advertised index structure/consistency")
		}
		_, through := programDates(p)
		if through == "" {
			t.Fatal("source blocked: representative index empty; no fallback horizon")
		}
		ids := []string{}
		for id, dates := range p {
			if len(dates) > 0 {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(batchMovieIDs(sample.origin, ids)) != 1 {
			t.Fatal("source blocked: sample exceeds single bounded metadata batch")
		}
		movies, err := parseMovies(get(OperationMovies, moviesURL(sample.origin, ids)))
		if err != nil {
			t.Fatal("source blocked: movie structure")
		}
		rows, _, err := parseSchedule(get(OperationSchedule, scheduleURL(sample.origin, cinema.ID, from, through)), cinema, p, movies, loc, from, from, through)
		if err != nil {
			t.Fatalf("source blocked: schedule consistency (%v)", err)
		}
		if len(rows) == 0 {
			t.Fatal("source blocked: no representative sessions")
		}
		selected := futureCheckoutSamples(rows, now, 2)
		if len(selected) == 0 {
			t.Fatal("source blocked: no future representative checkout")
		}
		samples = append(samples, checkoutSample{sample.id, selected})
		t.Logf("circuit=%s catalog=%d movies=%d sessions=%d through=%s future_checkout_samples=%d", sample.id, len(cinemas), len(movies), len(rows), through, len(selected))
	}
	_, rows, err := parseCastelia(get(OperationCastelia, ProgramURL), from, loc)
	if err != nil {
		t.Fatal("source blocked: Castelia structure")
	}
	t.Logf("castelia_sessions=%d", len(rows))
	// Acquire all three surfaces before visiting any checkout, so a failed page
	// cannot hide circuit or Castelia acquisition evidence.
	for _, sample := range samples {
		exact := 0
		knownRuntime := 0
		missingRoom := 0
		for _, row := range sample.rows {
			if row.Movie.RuntimeMinutes > 0 {
				knownRuntime++
			}
			facts, err := parseCheckout(get(OperationCheckout, row.BookingURL, row.StartTime))
			if err != nil {
				t.Fatal("source blocked: checkout structure")
			}
			if _, err := exactEnd(row.StartTime, facts); err != nil {
				t.Fatal("source blocked: checkout end ambiguity")
			}
			if facts.hasEnd {
				exact++
			}
			if row.Room == "" && facts.room == "" {
				missingRoom++
			}
		}
		t.Logf("circuit=%s checkout_pages=%d known_runtime=%d exact_ends=%d source_gap_unpublished_end=%d source_gap_missing_room=%d", sample.cinema, len(sample.rows), knownRuntime, exact, len(sample.rows)-exact, missingRoom)
	}
}

// Prefer known-runtime near-term films to exercise published end clocks, but do
// not invent a clock when the valid future booking page does not publish one.
func futureCheckoutSamples(rows []schedule.ShowtimeRecord, now time.Time, limit int) []schedule.ShowtimeRecord {
	future := make([]schedule.ShowtimeRecord, 0, len(rows))
	for _, row := range rows {
		if row.StartTime.After(now) {
			future = append(future, row)
		}
	}
	sort.Slice(future, func(i, j int) bool {
		a, b := future[i], future[j]
		if (a.Movie.RuntimeMinutes > 0) != (b.Movie.RuntimeMinutes > 0) {
			return a.Movie.RuntimeMinutes > 0
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	selected := []schedule.ShowtimeRecord{}
	seen := map[string]bool{}
	for _, row := range future {
		if len(selected) >= limit {
			break
		}
		if !seen[row.BookingURL] {
			selected = append(selected, row)
			seen[row.BookingURL] = true
		}
	}
	return selected
}

func TestFutureCheckoutSampleCutoffAndPreference(t *testing.T) {
	now := fixtureOptions().Now
	rows := []schedule.ShowtimeRecord{
		{ID: "past", BookingURL: "past", StartTime: now.Add(-time.Second)},
		{ID: "boundary", BookingURL: "boundary", StartTime: now},
		{ID: "unknown", BookingURL: "unknown", StartTime: now.Add(time.Second)},
		{ID: "later", BookingURL: "later", StartTime: now.Add(48 * time.Hour), Movie: schedule.MovieRecord{RuntimeMinutes: 120}},
		{ID: "near", BookingURL: "near", StartTime: now.Add(time.Hour), Movie: schedule.MovieRecord{RuntimeMinutes: 90}},
		{ID: "duplicate", BookingURL: "near", StartTime: now.Add(2 * time.Hour), Movie: schedule.MovieRecord{RuntimeMinutes: 90}},
	}
	selected := futureCheckoutSamples(rows, now, 2)
	if len(selected) != 2 || selected[0].ID != "near" || selected[1].ID != "later" || rows[0].ID != "past" {
		t.Fatal("wrong sample, cutoff, or mutation", selected)
	}
	selected = futureCheckoutSamples(rows[:3], now, 2)
	if len(selected) != 1 || selected[0].ID != "unknown" {
		t.Fatal("started rows selected or unknown runtime discarded", selected)
	}
}
