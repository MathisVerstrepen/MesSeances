package veo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestCheckoutStartMismatchRetainsAdvertisedSession(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 29, 13, 45, 0, 0, loc)
	for _, segment := range []string{"r/", "reserver/r/"} {
		t.Run(segment, func(t *testing.T) {
			row := redirectTestRow("https://achat.veocinemas.fr/tulle/"+segment+"123", start, "W1900")
			row.EndTime, row.Room = start, "Salle source"
			rows := []schedule.ShowtimeRecord{row}
			body, calls := &gueretUnreadBody{}, 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if calls != 1 || req.URL.String() != row.BookingURL {
					t.Fatal("different start fetched or rejected redirect retried")
				}
				r := fixtureResponse(307, "", "text/html")
				r.Header.Set("Location", regularTestCanonical(row.BookingURL, start.Add(45*time.Minute)))
				r.Body = body
				return r, nil
			})
			summary := SyncSummary{}
			g := checkoutClientFixture{fixtureGetter: &fixtureGetter{}, client: c}
			if err := enrichCheckouts(t.Context(), g, rows, &summary, start.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rows, []schedule.ShowtimeRecord{row}) || summary.CheckoutStartMismatches != 1 || summary.Checkouts != 1 || summary.ExactEnds != 0 || summary.CheckoutRetryAttempts != 0 || summary.CheckoutTransientFailures != 0 || summary.CheckoutBudgetSkipped != 0 || calls != 1 || body.reads != 0 || body.closes != 1 {
				t.Fatal("mismatched booking supplied facts, lost source data or consumed retries", summary, calls, body)
			}
		})
	}
}

func TestCheckoutStartMismatchRequiresBoundFirst307(t *testing.T) {
	row := redirectTestRow("https://achat.veocinemas.fr/tulle/r/123", gueretTestStart(), "W1900")
	target := regularTestCanonical(row.BookingURL, row.StartTime.Add(45*time.Minute))
	for _, tc := range []struct {
		name, target  string
		status, count int
		want          bool
	}{
		{"observed", target, 307, 1, true},
		{"other status", target, 302, 1, false},
		{"duplicate location", target, 307, 2, false},
		{"other cinema", strings.Replace(target, "/tulle/", "/other/", 1), 307, 1, false},
		{"other host", strings.Replace(target, "achat.veocinemas.fr", "other.example", 1), 307, 1, false},
		{"query", target + "?", 307, 1, false},
		{"fragment", target + "#", 307, 1, false},
		{"relative", strings.TrimPrefix(target, "https://achat.veocinemas.fr"), 307, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if calls != 1 {
					t.Fatal("mismatched target sent")
				}
				r := fixtureResponse(tc.status, "unused", "text/html")
				for range tc.count {
					r.Header.Add("Location", tc.target)
				}
				return r, nil
			})
			_, err := c.Checkout(t.Context(), row.BookingURL, row.StartTime)
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || checkoutStartMismatch(err, row) != tc.want || calls != 1 {
				t.Fatal("redirect policy changed or unsafe failure made optional", err)
			}
			if !tc.want {
				return
			}
			for _, mutate := range []func(*schedule.ShowtimeRecord){
				func(r *schedule.ShowtimeRecord) { r.BookingURL += "1" },
				func(r *schedule.ShowtimeRecord) { r.StartTime = r.StartTime.Add(time.Minute) },
				func(r *schedule.ShowtimeRecord) { r.TheaterID = "veo-P1028" },
				func(r *schedule.ShowtimeRecord) { r.ProviderShowingID = "forged" },
			} {
				other := row
				mutate(&other)
				if checkoutStartMismatch(err, other) {
					t.Fatal("attestation reused for another row")
				}
			}
			if checkoutStartMismatch(fmt.Errorf("wrapped: %w", err), row) || checkoutStartMismatch(&RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureRedirect}, row) {
				t.Fatal("caller-supplied failure trusted")
			}
		})
	}
}

func TestSyncStartMismatchRetainsCoverageAndHealthyEnrichment(t *testing.T) {
	f := &fixtureGetter{modify: func(op Operation, _ string, body []byte) ([]byte, error) {
		if op == OperationCastelia {
			body = []byte(strings.ReplaceAll(string(body), "https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg", ""))
		}
		return body, nil
	}}
	baseline, err := acquire(t.Context(), f, fixtureOptions(), &SyncSummary{})
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
		if req.URL.String() == b.source {
			r := fixtureResponse(307, "unused", "text/html")
			start := b.start
			if start.Day() == 28 {
				start = start.Add(45 * time.Minute)
			}
			r.Header.Set("Location", regularTestCanonical(b.source, start))
			return r, nil
		}
		if b.start.Day() == 28 {
			t.Fatal("mismatched start fetched")
		}
		return fixtureResponse(200, `<div id=s_fin>fin prévue à 01h15</div><script>{"auditorium_showtime":"salle-2"}</script>`, "text/html"), nil
	})
	d, s, err := Sync(t.Context(), checkoutClientFixture{fixtureGetter: f, client: c}, fixtureOptions())
	if err != nil || len(d.Showtimes) != len(baseline.Showtimes) || !reflect.DeepEqual(d.Coverage, baseline.Coverage) || s.CheckoutStartMismatches != 2 || s.ExactEnds != 2 || s.Checkouts != 4 || s.CheckoutRetryAttempts != 0 {
		t.Fatal("start mismatch aborted sync or affected independent enrichment", s, err)
	}
	for i, row := range d.Showtimes {
		if row.StartTime.Day() == 28 && !reflect.DeepEqual(row, baseline.Showtimes[i]) {
			t.Fatal("mismatched booking changed advertised row")
		}
	}
}

func TestCheckoutStartMismatchCannotHealRejectedPageOrCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			row := retryTestRow(19)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				if canceled {
					cancel()
				}
				r := fixtureResponse(307, "unused", "text/html")
				r.Header.Set("Location", regularTestCanonical(row.BookingURL, row.StartTime.Add(45*time.Minute)))
				return r, nil
			})
			g := &retryCheckoutGetter{fetch: func(ctx context.Context, raw string, start time.Time, n int) (CheckoutResult, error) {
				if n == 1 && !canceled {
					return retryTestPage(start, true), nil
				}
				return c.Checkout(ctx, raw, start)
			}}
			rows, s := []schedule.ShowtimeRecord{row}, SyncSummary{}
			err := enrichCheckouts(ctx, g, rows, &s, row.StartTime.Add(-time.Hour))
			var rejected *RequestError
			if canceled && !errors.Is(err, context.Canceled) || !canceled && (!errors.As(err, &rejected) || rejected.Kind != syncproxy.FailureRedirect) || s.CheckoutStartMismatches != 0 || s.CheckoutRecoveries != 0 || !reflect.DeepEqual(rows, []schedule.ShowtimeRecord{row}) {
				t.Fatal("mismatch healed a rejected page or cancellation", s, err)
			}
		})
	}
}
