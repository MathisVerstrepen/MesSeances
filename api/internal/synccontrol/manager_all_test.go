package synccontrol

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var allTestProviders = []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR, TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas}

func TestManagerAllIsolatesProviderFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		failures []Target
		mode     string
	}{
		{name: "success"},
		{name: "first", failures: []Target{TargetUGC}},
		{name: "middle", failures: []Target{TargetMegarama}},
		{name: "last", failures: []Target{TargetNoeCinemas}},
		{name: "multiple", failures: []Target{TargetUGC, TargetCGR, TargetGrandEcran}},
		{name: "all", failures: allTestProviders},
		{name: "panic", failures: []Target{TargetKinepolis}, mode: "panic"},
		{name: "missing outcome", failures: []Target{TargetKinepolis}, mode: "missing"},
		{name: "invalid outcome", failures: []Target{TargetKinepolis}, mode: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryRunStore{}
			lease := &trackingRunLease{}
			var calls []Target
			outcomes := make(map[Target]ProviderOutcome)
			manager, err := NewManager(t.Context(), func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) }, executorMapFunc(func(ctx context.Context, provider Target, window Window) (map[Target]ProviderOutcome, error) {
				calls = append(calls, provider)
				if provider == TargetAll || window.From != "2026-08-17" {
					t.Errorf("execution target=%s window=%+v", provider, window)
				}
				// Previous results must be durable while the same run and lease remain active.
				snapshot, err := store.Snapshot(ctx)
				if err != nil || snapshot.Job == nil || snapshot.Job.ID != "1" || snapshot.Job.Target != TargetAll || snapshot.Job.FinishedAt != nil || len(snapshot.Runs) != 0 || lease.releaseCount() != 1 {
					t.Errorf("intermediate snapshot=%+v err=%v releases=%d", snapshot, err, lease.releaseCount())
				} else {
					for _, previous := range calls[:len(calls)-1] {
						want := ProviderSucceeded
						if slices.Contains(test.failures, previous) {
							want = ProviderFailed
						}
						if got := snapshot.Job.Providers[string(previous)]; got.State != want {
							t.Errorf("previous provider=%s status=%+v want=%s", previous, got, want)
						}
					}
					if snapshot.Job.Providers[string(provider)].State != ProviderRunning {
						t.Errorf("current provider not running: %+v", snapshot.Job)
					}
				}
				if slices.Contains(test.failures, provider) {
					switch test.mode {
					case "panic":
						panic("secret")
					case "missing":
						return nil, nil
					case "invalid":
						return map[Target]ProviderOutcome{provider: {Sync: SyncOutcome{Through: "invalid"}}}, nil
					default:
						return nil, newProviderRunError(provider, StageProviderFetch, FailureProviderSync, errors.New("secret"))
					}
				}
				outcome := ProviderOutcome{Sync: SyncOutcome{Version: int64(len(calls)), Through: "2026-09-01", Cinemas: 2, Showtimes: 7}, Enrichment: EnrichmentOutcome{Status: EnrichmentComplete, Counts: &EnrichmentCounts{Matched: 3}}}
				outcomes[provider] = outcome
				return map[Target]ProviderOutcome{provider: outcome}, nil
			}), store, trackingRunLocker{lease: lease})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			if _, err := manager.Start(TargetAll); err != nil {
				t.Fatal(err)
			}
			status := waitForTerminal(t, manager)
			manager.Close()
			wantState, wantThrough := StateSucceeded, "2026-09-01"
			if len(test.failures) > 0 {
				wantState = StateFailed
			}
			if len(test.failures) == len(allTestProviders) {
				wantThrough = "2026-08-17"
			}
			if !slices.Equal(calls, allTestProviders) || status.State != wantState || status.Through != wantThrough || status.FinishedAt == nil || lease.releaseCount() != 2 {
				t.Fatalf("calls=%v status=%+v releases=%d", calls, status, lease.releaseCount())
			}
			for _, provider := range allTestProviders {
				got := status.Providers[string(provider)]
				if slices.Contains(test.failures, provider) {
					code := FailureProviderSync
					if test.mode != "" {
						code = FailureInternal
					}
					if got.State != ProviderFailed || got.ErrorCode != code || got.Outcome != nil || len(got.Log) == 0 || strings.Contains(strings.Join(got.Log, "\n"), "secret") {
						t.Errorf("failed provider=%s status=%+v", provider, got)
					}
				} else if want := outcomes[provider]; got.State != ProviderSucceeded || got.ErrorCode != "" || !reflect.DeepEqual(got.Outcome, &want) {
					t.Errorf("successful provider=%s status=%+v want=%+v", provider, got, want)
				}
			}
			snapshot, err := manager.Snapshot(t.Context())
			if err != nil || snapshot.Job != nil || len(snapshot.Runs) != 1 || !reflect.DeepEqual(snapshot.Runs[0], status) {
				t.Fatalf("persisted snapshot=%+v err=%v", snapshot, err)
			}
		})
	}
}

func TestManagerAllCancellationStopsFurtherProviders(t *testing.T) {
	for _, duringFetch := range []bool{false, true} {
		name := "between providers"
		if duringFetch {
			name = "during provider"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			lease := &trackingRunLease{}
			var calls []Target
			entered := make(chan struct{})
			manager, err := NewManager(ctx, time.Now, executorFunc(func(ctx context.Context, provider Target, window Window) (ProviderOutcome, error) {
				calls = append(calls, provider)
				if provider == TargetKinepolis {
					if duringFetch {
						close(entered)
						<-ctx.Done()
						return ProviderOutcome{}, ctx.Err()
					}
					cancel()
				}
				return ProviderOutcome{Sync: SyncOutcome{Through: window.From}}, nil
			}), &memoryRunStore{}, trackingRunLocker{lease: lease})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			if _, err := manager.Start(TargetAll); err != nil {
				t.Fatal(err)
			}
			if duringFetch {
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("provider did not start")
				}
				manager.Close()
			}
			status := waitForTerminal(t, manager)
			manager.Close()
			if !slices.Equal(calls, []Target{TargetUGC, TargetKinepolis}) || status.State != StateFailed || status.FinishedAt == nil || lease.releaseCount() != 2 {
				t.Fatalf("calls=%v status=%+v releases=%d", calls, status, lease.releaseCount())
			}
			for _, provider := range allTestProviders {
				got := status.Providers[string(provider)]
				want := ProviderSkipped
				if provider == TargetUGC || (provider == TargetKinepolis && !duringFetch) {
					want = ProviderSucceeded
				} else if provider == TargetKinepolis {
					want = ProviderFailed
					if got.ErrorCode != FailureCanceled || len(got.Log) == 0 {
						t.Errorf("canceled provider=%+v", got)
					}
				}
				if got.State != want {
					t.Errorf("provider=%s state=%s want=%s", provider, got.State, want)
				}
			}
			snapshot, err := manager.Snapshot(t.Context())
			if err != nil || len(snapshot.Runs) != 1 || !reflect.DeepEqual(snapshot.Runs[0], status) {
				t.Fatalf("canceled run not persisted: snapshot=%+v err=%v", snapshot, err)
			}
		})
	}
}
