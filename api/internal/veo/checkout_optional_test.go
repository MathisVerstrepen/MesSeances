package veo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestSyncRetainsScheduleOnTransientCheckoutFailure(t *testing.T) {
	for _, kind := range []syncproxy.FailureKind{syncproxy.FailureTransport, syncproxy.FailureResponseRead, syncproxy.FailureServer} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			g := &fixtureGetter{modify: func(op Operation, _ string, b []byte) ([]byte, error) {
				if op == OperationCheckout {
					return nil, &RequestError{Operation: op, Kind: kind, StatusCode: 503}
				}
				if op == OperationCastelia {
					b = []byte(strings.ReplaceAll(string(b), "https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg", ""))
				}
				return b, nil
			}}
			d, s, err := Sync(t.Context(), g, fixtureOptions())
			if err != nil || len(d.Theaters) != 3 || len(d.Showtimes) != 5 || s.Checkouts != 4 || s.CheckoutTransientFailures != 4 || s.CheckoutBudgetSkipped != 0 || s.ExactEnds != 0 {
				t.Fatal("validated sessions lost", s, err)
			}
			if err := schedule.ValidateDataset(d, true); err != nil {
				t.Fatal(err)
			}
			for _, row := range d.Showtimes {
				if row.TheaterID == "veo-EMS1361" {
					if row.EndTime.Sub(row.StartTime) != 100*time.Minute {
						t.Fatal("Castelia changed")
					}
					continue
				}
				if !row.EndTime.Equal(row.StartTime) || row.BookingURL != "https://achat.veocinemas.fr/fixture/r/123" || row.ServiceDate == "2026-10-15" && row.Room != "Salle source" || row.ServiceDate == "2026-10-28" && row.Room != "" {
					t.Fatal("failed lookup supplied metadata or lost source facts", row)
				}
			}
			g.modify = func(op Operation, _ string, b []byte) ([]byte, error) {
				if op == OperationCastelia {
					b = []byte(strings.ReplaceAll(string(b), "https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg", ""))
				}
				return b, nil
			}
			retried, recovered, err := Sync(t.Context(), g, fixtureOptions())
			if err != nil || len(retried.Showtimes) != len(d.Showtimes) || recovered.ExactEnds != 4 || recovered.CheckoutTransientFailures != 0 {
				t.Fatal("later sync could not recover optional enrichment", recovered, err)
			}
		})
	}
}

func TestCheckoutEnrichmentUsesOriginalSixteenWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	release := make(chan struct{})
	var started atomic.Int32
	rows := make([]schedule.ShowtimeRecord, 32)
	for i := range rows {
		rows[i] = retryTestRow(i)
	}
	g := &retryCheckoutGetter{fetch: func(ctx context.Context, _ string, _ time.Time, _ int) (CheckoutResult, error) {
		if started.Add(1) == 16 {
			close(release)
		}
		select {
		case <-release:
			return CheckoutResult{unavailableEnrichment: true}, nil
		case <-ctx.Done():
			return CheckoutResult{}, ctx.Err()
		}
	}}
	if err := enrichCheckouts(ctx, g, rows, &SyncSummary{}, rows[0].StartTime.Add(-time.Hour)); err != nil || started.Load() != 32 {
		t.Fatal("original checkout concurrency not restored", started.Load(), err)
	}
}

func TestCheckoutOptionalFailureBoundary(t *testing.T) {
	for _, kind := range []syncproxy.FailureKind{syncproxy.FailureCanceled, syncproxy.FailureNoClient, syncproxy.FailureInvalidURL, syncproxy.FailureRedirect, syncproxy.FailureResponseLarge, syncproxy.FailureChallenge, syncproxy.FailureStatus, syncproxy.FailureContentType, syncproxy.FailureEmptyResponse, syncproxy.FailureInvalidJSON} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			row := retryTestRow(1)
			rows := []schedule.ShowtimeRecord{row}
			g := &retryCheckoutGetter{fetch: func(context.Context, string, time.Time, int) (CheckoutResult, error) {
				return CheckoutResult{}, &RequestError{Operation: OperationCheckout, Kind: kind}
			}}
			s := SyncSummary{}
			if err := enrichCheckouts(t.Context(), g, rows, &s, row.StartTime.Add(-time.Hour)); err == nil || !reflect.DeepEqual(rows[0], row) || s.Checkouts != 0 {
				t.Fatal("unsafe failure downgraded", kind, err)
			}
		})
	}
	if transientCheckoutFailure(&RequestError{Operation: OperationMovies, Kind: syncproxy.FailureTransport}) || transientCheckoutFailure(&RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureServer, StatusCode: 404}) || transientCheckoutFailure(context.DeadlineExceeded) {
		t.Fatal("non-checkout failure or unrelated deadline treated as optional")
	}
}

func TestCheckoutBudgetRetainsCompletedFactsAndAllSessions(t *testing.T) {
	rows := make([]schedule.ShowtimeRecord, 40)
	for i := range rows {
		rows[i] = retryTestRow(i)
		rows[i].Room = "API room"
	}
	before := append([]schedule.ShowtimeRecord(nil), rows...)
	fast := rows[0].BookingURL
	g := &retryCheckoutGetter{fetch: func(ctx context.Context, raw string, start time.Time, _ int) (CheckoutResult, error) {
		if raw == fast {
			return retryTestPage(start, false), nil
		}
		<-ctx.Done()
		return CheckoutResult{}, ctx.Err()
	}}
	s := SyncSummary{}
	if err := enrichCheckoutsWithinBudget(t.Context(), g, rows, &s, rows[0].StartTime.Add(-time.Hour), 100*time.Millisecond); err != nil {
		t.Fatal("optional budget aborted schedule", err)
	}
	if s.ExactEnds != 1 || s.CheckoutBudgetSkipped != len(rows)-1 || s.CheckoutTransientFailures != 0 || s.Checkouts != g.RequestCount() || s.Checkouts > WorkerCount+1 || !rows[0].EndTime.After(rows[0].StartTime) {
		t.Fatal("completed results or skipped counts lost", s)
	}
	before[0].EndTime = rows[0].EndTime
	if !reflect.DeepEqual(rows, before) {
		t.Fatal("source metadata or unresolved sessions changed")
	}

	// An already expired budget launches no requests; later syncs can retry.
	s = SyncSummary{}
	calls := g.RequestCount()
	if err := enrichCheckoutsWithinBudget(t.Context(), g, rows, &s, rows[0].StartTime.Add(-time.Hour), 0); err != nil || s.CheckoutBudgetSkipped != len(rows) || g.RequestCount() != calls {
		t.Fatal("expired budget sent requests or became fatal", s, err)
	}
	for _, r := range rows {
		if !r.EndTime.Equal(r.StartTime) {
			t.Fatal("budget reused an unverified end")
		}
	}
}

func TestCheckoutBudgetDoesNotHideRejectedPageOrParentCancellation(t *testing.T) {
	row := retryTestRow(1)
	for _, expire := range []bool{false, true} {
		rows := []schedule.ShowtimeRecord{row}
		g := &retryCheckoutGetter{fetch: func(ctx context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
			if n == 1 {
				return retryTestPage(start, true), nil
			}
			if expire {
				<-ctx.Done()
				return CheckoutResult{}, ctx.Err()
			}
			return CheckoutResult{}, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureTransport}
		}}
		err := enrichCheckoutsWithinBudget(t.Context(), g, rows, &SyncSummary{}, row.StartTime.Add(-time.Hour), 100*time.Millisecond)
		var rejected *CheckoutParseError
		if !errors.As(err, &rejected) || !reflect.DeepEqual(rows[0], row) {
			t.Fatal("rejected first page excused", err)
		}
	}
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		if deadline {
			cancel()
			ctx, cancel = context.WithTimeout(t.Context(), 0)
		} else {
			cancel()
		}
		rows := []schedule.ShowtimeRecord{row}
		err := enrichCheckoutsWithinBudget(ctx, &fixtureGetter{}, rows, &SyncSummary{}, row.StartTime.Add(-time.Hour), time.Second)
		cancel()
		if !errors.Is(err, ctx.Err()) || !reflect.DeepEqual(rows[0], row) {
			t.Fatal("parent cancellation downgraded", err)
		}
	}
}
