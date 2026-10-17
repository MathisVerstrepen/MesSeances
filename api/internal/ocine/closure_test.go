package ocine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type closureGetter struct {
	*fixtureGetter
	checkout func(context.Context, string) ([]byte, error)
	page     func(context.Context, string, string) ([]byte, error)
}

func (g *closureGetter) Checkout(ctx context.Context, raw string) ([]byte, error) {
	return g.checkout(ctx, raw)
}

func (g *closureGetter) Page(ctx context.Context, id, route string) ([]byte, error) {
	if g.page != nil {
		return g.page(ctx, id, route)
	}
	return g.fixtureGetter.Page(ctx, id, route)
}

func closureFixture() *fixtureGetter {
	g := fullFixture()
	for _, id := range cinemaIDs {
		version := "VF"
		if id == "310" {
			version = "VO"
		}
		row := fixtureSession(id, "123", version, "4", "18h00")
		if id == "310" {
			row = strings.ReplaceAll(row, `<img class="tag-SAL04">`, "")
		}
		g.grids[id] = fixtureGrid(id, fixtureFilm(id, "123", row))
	}
	return g
}

func closedCheckoutError() error {
	return &RequestError{Kind: syncproxy.FailureStatus, StatusCode: http.StatusNotFound}
}

func TestCheckoutClosureObservedWithinStartWindow(t *testing.T) {
	start := fixtureInstant("18h00")
	for _, test := range []struct {
		name     string
		observed time.Time
		crossing bool
		wrapped  bool
		wantErr  bool
	}{
		{name: "started", observed: start.Add(time.Minute)},
		{name: "exact-start", observed: start},
		{name: "in-flight-crossing", observed: start.Add(-time.Nanosecond), crossing: true},
		{name: "in-flight-crossing-window", observed: start.Add(-2*time.Minute - time.Nanosecond), crossing: true},
		{name: "wrapped-status", observed: start, wrapped: true},
		{name: "just-before-start", observed: start.Add(-time.Nanosecond)},
		{name: "within-early-window", observed: start.Add(-time.Minute)},
		{name: "exact-early-window", observed: start.Add(-2 * time.Minute)},
		{name: "outside-early-window", observed: start.Add(-2*time.Minute - time.Nanosecond), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := fixtureOptions() // Frozen programme time is well before start.
			var observed atomic.Int64
			observed.Store(test.observed.UnixNano())
			options.Clock = func() time.Time { return time.Unix(0, observed.Load()) }
			g := &closureGetter{fixtureGetter: closureFixture()}
			g.checkout = func(context.Context, string) ([]byte, error) {
				g.requests.Add(1)
				if test.crossing {
					observed.Store(start.UnixNano())
				}
				err := closedCheckoutError()
				if test.wrapped {
					err = fmt.Errorf("checkout: %w", err)
				}
				return nil, err
			}
			d, summary, err := Sync(t.Context(), g, options)
			if test.wantErr {
				if err == nil || len(d.Showtimes) != 0 || len(d.Theaters) != 0 {
					t.Fatal("future closure published a partial dataset", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, _, err := Sync(t.Context(), closureFixture(), fixtureOptions())
			if err != nil {
				t.Fatal(err)
			}
			for i := range want.Showtimes {
				want.Showtimes[i].EndTime = want.Showtimes[i].StartTime
				if want.Showtimes[i].TheaterID == "ocine-310" {
					want.Showtimes[i].Room = ""
				}
			}
			if !reflect.DeepEqual(want, d) || summary.EndsAttempted != 3 || summary.EndsRecovered != 0 || summary.RoomsMissing != 1 {
				t.Fatal("closure changed source identity, metadata or summary", summary)
			}
			if _, err := schedule.PreparePublication(d); err != nil {
				t.Fatal("closed checkout cannot be published", err)
			}
		})
	}
}

func TestQueuedCheckoutCrossesStart(t *testing.T) {
	start := fixtureInstant("18h00")
	var observed atomic.Int64
	observed.Store(start.Add(-2*time.Minute - time.Nanosecond).UnixNano())
	options := fixtureOptions()
	options.Clock = func() time.Time { return time.Unix(0, observed.Load()) }
	entered, release := make(chan struct{}, 2), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	g := &closureGetter{fixtureGetter: closureFixture()}
	g.checkout = func(ctx context.Context, raw string) ([]byte, error) {
		if strings.Contains(raw, "/dunkerque/") {
			g.requests.Add(1)
			return nil, closedCheckoutError()
		}
		entered <- struct{}{}
		select {
		case <-release:
			return g.fixtureGetter.Checkout(ctx, raw)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	type outcome struct {
		dataset schedule.Dataset
		summary SyncSummary
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		d, s, err := Sync(ctx, g, options)
		done <- outcome{d, s, err}
	}()
	for range 2 {
		select {
		case <-entered:
		case result := <-done:
			t.Fatal("sync ended before both workers entered", result.err)
		case <-time.After(5 * time.Second):
			t.Fatal("workers did not enter")
		}
	}
	observed.Store(start.UnixNano())
	close(release)
	result := <-done
	if result.err != nil || result.summary.EndsRecovered != 2 || len(result.dataset.Showtimes) != 3 {
		t.Fatal("queued closure aborted complete publication", result.err, result.summary)
	}
	for _, s := range result.dataset.Showtimes {
		if s.TheaterID == "ocine-461" && (!s.EndTime.Equal(s.StartTime) || s.Room != "Salle 4" || s.BookingURL == "") {
			t.Fatal("queued closure lost grid metadata")
		}
	}
}

func TestCheckoutClosureDoesNotHideOtherFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		body []byte
	}{
		{name: "untyped-404", err: errors.New("HTTP 404")},
		{name: "challenge", err: &RequestError{Kind: syncproxy.FailureChallenge, StatusCode: 404}},
		{name: "forbidden", err: &RequestError{Kind: syncproxy.FailureStatus, StatusCode: 403}},
		{name: "gone", err: &RequestError{Kind: syncproxy.FailureStatus, StatusCode: 410}},
		{name: "server", err: &RequestError{Kind: syncproxy.FailureServer, StatusCode: 503}},
		{name: "transport", err: &RequestError{Kind: syncproxy.FailureTransport}},
		{name: "redirect", err: &RequestError{Kind: syncproxy.FailureRedirect}},
		{name: "canceled", err: &RequestError{Kind: syncproxy.FailureCanceled, cause: context.Canceled}},
		{name: "malformed", body: []byte("<html>incomplete")},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := &closureGetter{fixtureGetter: closureFixture(), checkout: func(context.Context, string) ([]byte, error) { return test.body, test.err }}
			options := fixtureOptions()
			options.Clock = func() time.Time { return fixtureInstant("18h01") }
			d, _, err := Sync(t.Context(), g, options)
			if err == nil || len(d.Showtimes) != 0 || len(d.Theaters) != 0 {
				t.Fatal("non-closure failure escaped atomic abort", err)
			}
		})
	}
	for _, route := range []string{"seances", "plan-d-acces"} {
		t.Run(route+"-404", func(t *testing.T) {
			g := &closureGetter{fixtureGetter: closureFixture()}
			g.checkout = g.fixtureGetter.Checkout
			g.page = func(ctx context.Context, id, requested string) ([]byte, error) {
				if requested == route {
					return nil, closedCheckoutError()
				}
				return g.fixtureGetter.Page(ctx, id, requested)
			}
			options := fixtureOptions()
			options.Clock = func() time.Time { return fixtureInstant("18h01") }
			d, _, err := Sync(t.Context(), g, options)
			if err == nil || len(d.Showtimes) != 0 || len(d.Theaters) != 0 {
				t.Fatal("page 404 accepted as checkout closure", err)
			}
		})
	}
}
