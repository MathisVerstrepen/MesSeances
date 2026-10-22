package veo

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

const toulouseEntryAction = "https://achat.veocinemas.fr/toulouse/reserver/"

func toulouseEntryPage(start time.Time) CheckoutResult {
	p := retryTestPage(start, false)
	p.Body = []byte(bookingFormFixture(toulouseEntryAction) + `<div id=s_fin>fin prévue à 20h15</div><script>{"auditorium_showtime":"salle-8"}</script>`)
	return p
}

// Run the same binding, parser-attestation and retry controls for each admitted
// cinema. All pages remain synthetic; only the cinema identity differs.
func TestBoundEntryCheckouts(t *testing.T) {
	for _, cinema := range []struct{ id, slug string }{{"G0699", "toulouse"}, {"P0833", "saint-chamond"}, {"W0065", "caussade"}, {"P6864", "colomiers"}} {
		t.Run(cinema.id, func(t *testing.T) {
			row := func(i int) schedule.ShowtimeRecord {
				r := retryTestRow(i)
				r.TheaterID = "veo-" + cinema.id
				r.ProviderShowingID = schedule.VeoShowingID(cinema.id, fmt.Sprint(i))
				r.ID = "veo-showing-" + r.ProviderShowingID
				r.BookingURL = strings.ReplaceAll(r.BookingURL, "toulouse", cinema.slug)
				return r
			}
			page := func(start time.Time, mismatch bool) CheckoutResult {
				p := retryTestPage(start, mismatch)
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "toulouse", cinema.slug)
				p.Body = []byte(strings.ReplaceAll(string(p.Body), "toulouse", cinema.slug))
				return p
			}
			entry := func(start time.Time) CheckoutResult {
				p := toulouseEntryPage(start)
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "toulouse", cinema.slug)
				p.Body = []byte(strings.ReplaceAll(string(p.Body), "toulouse", cinema.slug))
				return p
			}
			action := "https://achat.veocinemas.fr/" + cinema.slug + "/reserver/"
			t.Run("discards all facts", func(t *testing.T) { testEntryUnavailableDiscardsAllPageFacts(t, row, entry, action) })
			t.Run("identity", func(t *testing.T) { testEntryRequiresExactSourceAndCanonicalIdentity(t, row, entry, action) })
			t.Run("near misses", func(t *testing.T) { testEntryNearMissesRemainStrict(t, row, entry, action, cinema.slug) })
			t.Run("second response", func(t *testing.T) { testEntrySecondResponseCannotRecover(t, row, page, entry) })
		})
	}
	t.Run("G06DB", testGueretBoundEntryCheckout)
}

func testEntryUnavailableDiscardsAllPageFacts(t *testing.T, retryTestRow func(int) schedule.ShowtimeRecord, toulouseEntryPage func(time.Time) CheckoutResult, toulouseEntryAction string) {
	for _, room := range []string{"", "API room"} {
		segments := []string{"r/", "reserver/r/"}
		if retryTestRow(901).TheaterID == "veo-G06DB" {
			segments = []string{"relay"}
		}
		for _, segment := range segments {
			t.Run(room+segment, func(t *testing.T) {
				row := retryTestRow(901)
				row.Room = room
				row.BookingURL = strings.Replace(row.BookingURL, "/r/", "/"+segment, 1)
				g := &retryCheckoutGetter{fetch: func(_ context.Context, raw string, start time.Time, n int) (CheckoutResult, error) {
					if raw != row.BookingURL || !start.Equal(row.StartTime) || n != 1 {
						t.Error("source/start changed or extra request")
					}
					return toulouseEntryPage(start), nil
				}}
				rows, s := []schedule.ShowtimeRecord{row}, SyncSummary{}
				if err := enrichCheckouts(t.Context(), g, rows, &s, row.StartTime.Add(-time.Hour)); err != nil {
					t.Fatal(err)
				}
				wantToulouse := 0
				if row.TheaterID == "veo-G0699" {
					wantToulouse = 1
				}
				if !reflect.DeepEqual(rows[0], row) || s.ToulouseUnavailableCheckouts != wantToulouse || s.Checkouts != 1 || s.ExactEnds != 0 || s.CheckoutRetryAttempts != 0 || s.CheckoutRecoveries != 0 || g.RequestCount() != 1 {
					t.Fatal("generic-page facts retained or retry spent", s)
				}
				// The generic parser still rejects this page; only the bound first
				// response orchestrator can classify it as unavailable enrichment.
				p := toulouseEntryPage(row.StartTime)
				if _, err := parseCheckoutPage(p.Body, p.canonicalURL, row.TheaterID, true); err == nil {
					t.Fatal("generic parse acceptance expanded")
				}
			})
		}
	}
	for _, body := range []string{
		bookingFormFixture(toulouseEntryAction),
		bookingFormFixture(toulouseEntryAction) + `{"auditorium_showtime":null}`,
		strings.ReplaceAll(bookingFormFixture(toulouseEntryAction), "reserver", "reser&#118;er"),
	} {
		row := retryTestRow(900)
		p := toulouseEntryPage(row.StartTime)
		p.Body = []byte(body)
		got, err := resolveCheckout(row, p, false)
		if err != nil || !got.entryUnavailable || !got.end.Equal(row.StartTime) || got.hasEnd || got.room != "" {
			t.Fatal("missing optional metadata or literal decoded action rejected", err)
		}
	}
}

func testEntryRequiresExactSourceAndCanonicalIdentity(t *testing.T, retryTestRow func(int) schedule.ShowtimeRecord, toulouseEntryPage func(time.Time) CheckoutResult, toulouseEntryAction string) {
	slug := strings.Split(entryCheckoutAction(retryTestRow(902).TheaterID), "/")[3]
	for _, tc := range []struct {
		name   string
		change func(*schedule.ShowtimeRecord, *CheckoutResult)
	}{
		{"other cinema", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.TheaterID = "veo-P1028" }},
		{"other admitted cinema", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
			if r.TheaterID == "veo-G0699" {
				r.TheaterID = "veo-P0833"
			} else {
				r.TheaterID = "veo-G0699"
			}
		}},
		{"gueret", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.TheaterID = "veo-G06DB" }},
		{"showing namespace", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
			r.ProviderShowingID = schedule.VeoShowingID("P1028", "fixture")
		}},
		{"showing malformed", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
			r.ProviderShowingID = strings.TrimPrefix(r.TheaterID, "veo-") + "-forged"
		}},
		{"public id", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.ID = "veo-showing-forged" }},
		{"source cinema", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
			r.BookingURL = strings.ReplaceAll(r.BookingURL, slug, "fronton")
		}},
		{"source and response slug", func(r *schedule.ShowtimeRecord, p *CheckoutResult) {
			r.BookingURL = strings.ReplaceAll(r.BookingURL, slug, "fronton")
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, slug, "fronton")
			p.Body = []byte(strings.ReplaceAll(string(p.Body), slug, "fronton"))
		}},
		{"source query", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.BookingURL += "?" }},
		{"source canonical", func(r *schedule.ShowtimeRecord, p *CheckoutResult) { r.BookingURL = p.canonicalURL }},
		{"zero start", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.StartTime = time.Time{} }},
		{"nanosecond start", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.StartTime = r.StartTime.Add(time.Nanosecond) }},
		{"wrong epoch", func(r *schedule.ShowtimeRecord, p *CheckoutResult) {
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, fmt.Sprint(r.StartTime.Unix()), fmt.Sprint(r.StartTime.Unix()+1))
		}},
		{"canonical missing", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL = "" }},
		{"canonical alias", func(r *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL = r.BookingURL }},
		{"canonical wrong cinema", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, slug, "fronton")
		}},
		{"canonical host case", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "achat.", "ACHAT.")
		}},
		{"canonical query", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL += "?" }},
		{"canonical fragment", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL += "#" }},
		{"canonical invalid film", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "F123", "F0")
		}},
		{"canonical invalid version", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "/VF/", "/VOSTF/")
		}},
		{"canonical invalid tail", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
			p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "/81/", "/0/")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := retryTestRow(902)
			p := toulouseEntryPage(row.StartTime)
			// No legacy numeric-room shortcut in this identity rejection test.
			p.Body = []byte(bookingFormFixture(toulouseEntryAction))
			tc.change(&row, &p)
			if got, err := resolveCheckout(row, p, false); err == nil || got.entryUnavailable {
				t.Fatal("unbound page classified unavailable")
			}
		})
	}
	// A caller/getter-supplied parser error cannot provide the parser's private
	// body attestation, even if it forges every existing diagnostic/retry field.
	row := retryTestRow(903)
	forged := &CheckoutParseError{Reason: RejectBookingForm, formReason: formActionMismatch, actionOnly: true, actionDiff: actionPathShape, CinemaID: "G0699", ShowingID: row.ProviderShowingID}
	g := &retryCheckoutGetter{fetch: func(context.Context, string, time.Time, int) (CheckoutResult, error) { return CheckoutResult{}, forged }}
	s := SyncSummary{}
	if err := enrichCheckouts(t.Context(), g, []schedule.ShowtimeRecord{row}, &s, row.StartTime.Add(-time.Hour)); !errors.Is(err, forged) || g.RequestCount() != 1 || s.ToulouseUnavailableCheckouts != 0 || s.CheckoutRetryAttempts != 0 {
		t.Fatal("forged getter became eligible", err)
	}
}

func testEntryNearMissesRemainStrict(t *testing.T, retryTestRow func(int) schedule.ShowtimeRecord, toulouseEntryPage func(time.Time) CheckoutResult, toulouseEntryAction, slug string) {
	row := retryTestRow(904)
	u, _ := url.Parse(toulouseEntryAction)
	for i, action := range []string{
		u.Path, "//" + u.Host + u.Path,
		strings.TrimSuffix(toulouseEntryAction, "/"), toulouseEntryAction + "/",
		toulouseEntryAction + "?", toulouseEntryAction + "?x=1", toulouseEntryAction + "#", toulouseEntryAction + "#x",
		strings.ReplaceAll(toulouseEntryAction, "https:", "http:"), strings.ReplaceAll(toulouseEntryAction, "https:", "HTTPS:"),
		strings.ReplaceAll(toulouseEntryAction, "achat.", "ACHAT."), strings.ReplaceAll(toulouseEntryAction, u.Host, u.Host+":443"),
		strings.ReplaceAll(toulouseEntryAction, "https://", "https://user@"), strings.ReplaceAll(toulouseEntryAction, slug, fmt.Sprintf("%%%02x%s", slug[0], slug[1:])),
		strings.ReplaceAll(toulouseEntryAction, slug, "fronton"), strings.ReplaceAll(toulouseEntryAction, slug, strings.ToUpper(slug)),
		toulouseEntryAction + "r/1", toulouseEntryAction + "F123/", toulouseEntryAction + "../", " " + toulouseEntryAction,
	} {
		t.Run(fmt.Sprintf("action-%d", i), func(t *testing.T) {
			p := toulouseEntryPage(row.StartTime)
			p.Body = []byte(bookingFormFixture(html.EscapeString(action)) + `<script>{"auditorium_showtime":"salle-8"}</script>`)
			if got, err := resolveCheckout(row, p, false); err == nil || got.entryUnavailable {
				t.Fatal("near-miss action accepted")
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"duplicate action", `<form method=POST action="` + toulouseEntryAction + `" action="` + toulouseEntryAction + `"></form>`},
		{"duplicate boolean action", `<form method=POST action action="` + toulouseEntryAction + `"></form>`},
		{"duplicate method", `<form method=POST method=POST action="` + toulouseEntryAction + `"></form>`},
		{"missing method", `<form action="` + toulouseEntryAction + `"></form>`},
		{"GET", `<form method=GET action="` + toulouseEntryAction + `"></form>`},
		{"two forms", bookingFormFixture(toulouseEntryAction) + bookingFormFixture(toulouseEntryAction)},
		{"nested", `<form method=POST action="` + toulouseEntryAction + `">` + bookingFormFixture(toulouseEntryAction) + `</form>`},
		{"inert", `<template>` + bookingFormFixture(toulouseEntryAction) + `</template>`},
		{"script lookalike", `<script>` + bookingFormFixture(toulouseEntryAction) + `</script>`},
		{"password", bookingFormFixture(toulouseEntryAction) + `<input type=password>`},
		{"login outside form", `<input name=login>` + bookingFormFixture(toulouseEntryAction)},
		{"malformed room", bookingFormFixture(toulouseEntryAction) + `{"auditorium_showtime":"unrecognized"}`},
		{"conflicting rooms", bookingFormFixture(toulouseEntryAction) + `{"auditorium_showtime":"salle-2","auditorium_showtime":null}`},
		{"bad clock", bookingFormFixture(toulouseEntryAction) + `<div id=s_fin>fin prévue à 29h15</div>`},
		{"conflicting clocks", bookingFormFixture(toulouseEntryAction) + `<div id=s_fin>fin prévue à 20h15</div><div id=s_fin>fin prévue à 21h15</div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := toulouseEntryPage(row.StartTime)
			p.Body = []byte(tc.body)
			if got, err := resolveCheckout(row, p, false); err == nil || got.entryUnavailable {
				t.Fatal("other parser failure softened")
			}
		})
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, day := range []time.Time{time.Date(2026, 10, 24, 23, 30, 0, 0, loc), time.Date(2026, 3, 28, 23, 30, 0, 0, loc)} {
		row.StartTime = day
		p := toulouseEntryPage(day)
		p.Body = []byte(bookingFormFixture(toulouseEntryAction) + `<div id=s_fin>fin prévue à 2h30</div>`)
		if _, err := resolveCheckout(row, p, false); err == nil {
			t.Fatal("ambiguous/nonexistent end clock softened")
		}
	}
}

func testEntrySecondResponseCannotRecover(t *testing.T, retryTestRow func(int) schedule.ShowtimeRecord, retryTestPage func(time.Time, bool) CheckoutResult, toulouseEntryPage func(time.Time) CheckoutResult) {
	row := retryTestRow(905)
	g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
		if n == 1 {
			return retryTestPage(start, true), nil
		}
		return toulouseEntryPage(start), nil
	}}
	rows, s := []schedule.ShowtimeRecord{row}, SyncSummary{}
	if err := enrichCheckouts(t.Context(), g, rows, &s, row.StartTime.Add(-time.Hour)); err == nil || g.RequestCount() != 2 || s.CheckoutRetryAttempts != 1 || s.CheckoutRecoveries != 0 || s.ToulouseUnavailableCheckouts != 0 || !reflect.DeepEqual(rows[0], row) {
		t.Fatal("generic second response counted as recovery", s, err)
	}
}

func TestToulouseEntryConcurrentAtomicMapAndCancellation(t *testing.T) {
	rows := make([]schedule.ShowtimeRecord, 32)
	for i := range rows {
		rows[i] = retryTestRow(1000 + i)
	}
	g := &retryCheckoutGetter{fetch: func(_ context.Context, raw string, start time.Time, _ int) (CheckoutResult, error) {
		p := toulouseEntryPage(start)
		if raw == rows[31].BookingURL {
			return retryTestPage(start, false), nil
		}
		return p, nil
	}}
	s := SyncSummary{}
	if err := enrichCheckouts(t.Context(), g, rows, &s, rows[0].StartTime.Add(-time.Hour)); err != nil || s.ToulouseUnavailableCheckouts != 31 || s.ExactEnds != 1 || g.RequestCount() != 32 || rows[31].EndTime.Format("15:04") != "20:15" {
		t.Fatal("concurrent attestation bleed", s, err)
	}
	for _, r := range rows[:31] {
		if !r.EndTime.Equal(r.StartTime) || r.Room != "" {
			t.Fatal("page facts leaked between rows")
		}
	}
	// Delay the final failure until one unavailable outcome is fully resolved.
	rows = []schedule.ShowtimeRecord{retryTestRow(1100), retryTestRow(1101)}
	before := append([]schedule.ShowtimeRecord(nil), rows...)
	var once sync.Once
	ready := make(chan struct{})
	g = &retryCheckoutGetter{fetch: func(ctx context.Context, raw string, start time.Time, _ int) (CheckoutResult, error) {
		if raw == rows[0].BookingURL {
			once.Do(func() { close(ready) })
			return toulouseEntryPage(start), nil
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return CheckoutResult{}, ctx.Err()
		}
		p := toulouseEntryPage(start)
		p.Body = append(p.Body, []byte(`{"auditorium_showtime":null}`)...)
		return p, nil
	}}
	if err := enrichCheckouts(t.Context(), g, rows, &SyncSummary{}, rows[0].StartTime.Add(-time.Hour)); err == nil || !reflect.DeepEqual(rows, before) {
		t.Fatal("partial map applied", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	g = &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, _ int) (CheckoutResult, error) {
		cancel()
		return toulouseEntryPage(start), nil
	}}
	s = SyncSummary{}
	if err := enrichCheckouts(ctx, g, rows[:1], &s, rows[0].StartTime.Add(-time.Hour)); !errors.Is(err, context.Canceled) || s.ToulouseUnavailableCheckouts != 0 || !reflect.DeepEqual(rows, before) {
		t.Fatal("cancellation hidden by fallback", err)
	}
}

func TestSyncToulouseEntryGapPreservesFullAtomicDataset(t *testing.T) {
	for _, fail := range []bool{false, true} {
		base := &fixtureGetter{modify: func(op Operation, raw string, b []byte) ([]byte, error) {
			text := string(b)
			if strings.HasPrefix(raw, MainOrigin) {
				if op == OperationCinemas || op == OperationSchedule {
					text = strings.ReplaceAll(text, "P1028", "G0699")
				}
				if op == OperationSchedule {
					text = strings.ReplaceAll(text, "/fixture/r/123", "/toulouse/r/123")
				}
			}
			return []byte(text), nil
		}}
		g := &retryCheckoutGetter{Getter: base, fetch: func(_ context.Context, raw string, start time.Time, _ int) (CheckoutResult, error) {
			if strings.Contains(raw, "/toulouse/") {
				return toulouseEntryPage(start), nil
			}
			if fail {
				return CheckoutResult{}, errShape
			}
			return retryTestPage(start, false), nil
		}}
		d, s, err := Sync(t.Context(), g, fixtureOptions())
		if fail {
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("failed full map published")
			}
			continue
		}
		if err != nil || len(d.Theaters) != 3 || len(d.Showtimes) != 5 || s.ToulouseUnavailableCheckouts != 2 || s.CheckoutRetryAttempts != 0 || d.Window.Through != "2026-10-28" {
			t.Fatal("full acquisition changed", s, err)
		}
		for _, r := range d.Showtimes {
			if r.TheaterID == "veo-G0699" && (!r.EndTime.Equal(r.StartTime) || r.Room == "Salle 8" || r.BookingURL != "https://achat.veocinemas.fr/toulouse/r/123") {
				t.Fatal("session/API room/source changed")
			}
		}
	}
}
