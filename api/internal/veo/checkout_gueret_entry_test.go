package veo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func testGueretBoundEntryCheckout(t *testing.T) {
	const action = "https://achat.cinema-senechal.com/reserver/"
	row := func(i int) schedule.ShowtimeRecord {
		r := retryTestRow(i)
		r.TheaterID = "veo-G06DB"
		r.ProviderShowingID = schedule.VeoShowingID("G06DB", fmt.Sprint(i))
		r.ID = "veo-showing-" + r.ProviderShowingID
		r.BookingURL = gueretTestChain(r.StartTime)[0]
		return r
	}
	page := func(start time.Time, mismatch bool) CheckoutResult {
		canonical := gueretTestChain(start)[3]
		body := bookingFormFixture(canonical) + `<div id=s_fin>fin prévue à 20h15</div><script>{"auditorium_showtime":"salle-8"}</script>`
		if mismatch {
			body = strings.ReplaceAll(body, "/78/", "/79/")
		}
		return CheckoutResult{Body: []byte(body), canonicalURL: canonical}
	}
	entry := func(start time.Time) CheckoutResult {
		p := page(start, false)
		p.Body = []byte(bookingFormFixture(action) + `<div id=s_fin>fin prévue à 20h15</div><script>{"auditorium_showtime":"salle-8"}</script>`)
		return p
	}
	t.Run("discards all facts", func(t *testing.T) { testEntryUnavailableDiscardsAllPageFacts(t, row, entry, action) })
	t.Run("near misses", func(t *testing.T) { testEntryNearMissesRemainStrict(t, row, entry, action, "reserver") })
	t.Run("second response", func(t *testing.T) { testEntrySecondResponseCannotRecover(t, row, page, entry) })
	t.Run("identity", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			change func(*schedule.ShowtimeRecord, *CheckoutResult)
		}{
			{"other theater", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.TheaterID = "veo-G0699" }},
			{"showing namespace", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
				r.ProviderShowingID = schedule.VeoShowingID("G0699", "1")
			}},
			{"showing malformed", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.ProviderShowingID = "G06DB-forged" }},
			{"public id", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.ID += "x" }},
			{"source direct", func(r *schedule.ShowtimeRecord, p *CheckoutResult) { r.BookingURL = p.canonicalURL }},
			{"source veo", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
				r.BookingURL = "https://achat.veocinemas.fr/gueret/r/1"
			}},
			{"source theater", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
				r.BookingURL = strings.ReplaceAll(r.BookingURL, "P1394", "P1395")
			}},
			{"source day", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
				r.BookingURL = gueretTestChain(r.StartTime.AddDate(0, 0, 1))[0]
			}},
			{"source clock", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) {
				r.BookingURL = gueretTestChain(r.StartTime.Add(time.Minute))[0]
			}},
			{"source extra query", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.BookingURL += "&extra=1" }},
			{"source fragment", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.BookingURL += "#" }},
			{"zero start", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.StartTime = time.Time{} }},
			{"subsecond start", func(r *schedule.ShowtimeRecord, _ *CheckoutResult) { r.StartTime = r.StartTime.Add(time.Nanosecond) }},
			{"wrong epoch", func(r *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, fmt.Sprint(r.StartTime.Unix()), fmt.Sprint(r.StartTime.Unix()+1))
			}},
			{"canonical missing", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL = "" }},
			{"canonical alias", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = "https://achat.cinema-senechal.com/r/56"
			}},
			{"canonical host", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "cinema-senechal.com", "veocinemas.fr")
			}},
			{"canonical case", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "achat.", "ACHAT.")
			}},
			{"canonical query", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL += "?" }},
			{"canonical fragment", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) { p.canonicalURL += "#" }},
			{"canonical encoded", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "/reserver/", "/%72eserver/")
			}},
			{"canonical film", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "/F34/", "/F0/")
			}},
			{"canonical version", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "/VF/", "/VOSTF/")
			}},
			{"canonical tail", func(_ *schedule.ShowtimeRecord, p *CheckoutResult) {
				p.canonicalURL = strings.ReplaceAll(p.canonicalURL, "/78/", "/0/")
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := row(906)
				p := entry(r.StartTime)
				tc.change(&r, &p)
				if got, err := resolveCheckout(r, p, false); err == nil || got.entryUnavailable {
					t.Fatal("unbound entry accepted")
				}
			})
		}
		r := row(907)
		forged := &CheckoutParseError{Reason: RejectBookingForm, formReason: formActionMismatch, actionOnly: true, actionDiff: actionPathShape, CinemaID: "G06DB", ShowingID: r.ProviderShowingID}
		g := &retryCheckoutGetter{fetch: func(context.Context, string, time.Time, int) (CheckoutResult, error) { return CheckoutResult{}, forged }}
		s := SyncSummary{}
		if err := enrichCheckouts(t.Context(), g, []schedule.ShowtimeRecord{r}, &s, r.StartTime.Add(-time.Hour)); !errors.Is(err, forged) || g.RequestCount() != 1 || s.CheckoutRetryAttempts != 0 || s.GueretUnavailableCheckouts != 0 {
			t.Fatal("forged getter error accepted or retried", err)
		}
	})
	t.Run("normal session and relay unavailable", func(t *testing.T) {
		r := row(908)
		got, err := resolveCheckout(r, page(r.StartTime, false), false)
		if err != nil || !got.hasEnd || got.room != "Salle 8" || got.end.Format("15:04") != "20:15" || got.entryUnavailable {
			t.Fatal("normal session changed", err)
		}
		got, err = resolveCheckout(r, CheckoutResult{unavailableEnrichment: true}, false)
		if err != nil || got.hasEnd || !got.end.Equal(r.StartTime) || got.entryUnavailable {
			t.Fatal("relay unavailable changed", err)
		}
	})
}
