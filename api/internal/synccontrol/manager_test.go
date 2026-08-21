package synccontrol

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

type executorFunc func(context.Context, Target, Window) (ProviderOutcome, error)
type executorMapFunc func(context.Context, Target, Window) (map[Target]ProviderOutcome, error)

func (f executorMapFunc) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	return f(ctx, target, window)
}

func (f executorFunc) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	outcome, err := f(ctx, target, window)
	if err != nil {
		return nil, err
	}
	if target == TargetAll {
		return map[Target]ProviderOutcome{TargetUGC: outcome, TargetKinepolis: outcome}, nil
	}
	return map[Target]ProviderOutcome{target: outcome}, nil
}

func TestManagerOrdersAllAndRejectsOverlap(t *testing.T) {
	now := time.Date(2026, 8, 17, 23, 30, 0, 0, time.FixedZone("test", -4*60*60))
	started := make(chan Target, 1)
	release := make(chan struct{})
	manager, err := NewManager(context.Background(), func() time.Time { return now }, executorFunc(func(_ context.Context, target Target, window Window) (ProviderOutcome, error) {
		if window != (Window{From: "2026-08-18", Through: "2026-08-25"}) {
			t.Errorf("window=%+v", window)
		}
		started <- target
		<-release
		return ProviderOutcome{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := manager.Start(TargetAll)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.ID != "1" || accepted.State != StateRunning || accepted.Providers["ugc"].State != ProviderPending || accepted.Providers["kinepolis"].State != ProviderPending || accepted.StartedAt.Location() != time.UTC {
		t.Fatalf("accepted=%+v", accepted)
	}
	if _, err := manager.Start(TargetUGC); !errors.Is(err, ErrInProgress) {
		t.Fatalf("overlap err=%v", err)
	}
	if target := <-started; target != TargetAll {
		t.Fatalf("target=%s", target)
	}
	status := manager.Status()
	if status.Providers["ugc"].State != ProviderRunning || status.Providers["kinepolis"].State != ProviderRunning {
		t.Fatalf("status=%+v", status)
	}
	status.Providers["ugc"] = ProviderStatus{State: "mutated"}
	if manager.Status().Providers["ugc"].State == "mutated" {
		t.Fatal("status snapshot mutated manager state")
	}
	release <- struct{}{}
	status = waitForTerminal(t, manager)
	if status.State != StateSucceeded || status.FinishedAt == nil {
		t.Fatalf("terminal=%+v", status)
	}
}

func TestManagerFailurePanicCancellationAndTargets(t *testing.T) {
	tests := []struct {
		name     string
		target   Target
		executor executorFunc
		cancel   context.CancelFunc
		wantUGC  string
		wantKin  string
	}{
		{name: "failure skips other provider", target: TargetAll, executor: func(context.Context, Target, Window) (ProviderOutcome, error) {
			return ProviderOutcome{}, newProviderRunError(TargetUGC, StageDatasetValidation, FailureDatasetRejected, errors.New("secret"))
		}, wantUGC: ProviderFailed, wantKin: ProviderSkipped},
		{name: "panic becomes failure", target: TargetKinepolis, executor: func(context.Context, Target, Window) (ProviderOutcome, error) { panic("secret") }, wantUGC: ProviderNotRequested, wantKin: ProviderFailed},
		{name: "single provider succeeds", target: TargetUGC, executor: func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }, wantUGC: ProviderSucceeded, wantKin: ProviderNotRequested},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := NewManager(context.Background(), time.Now, test.executor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Start(test.target); err != nil {
				t.Fatal(err)
			}
			status := waitForTerminal(t, manager)
			if status.Providers["ugc"].State != test.wantUGC || status.Providers["kinepolis"].State != test.wantKin || status.FinishedAt == nil {
				t.Fatalf("status=%+v", status)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	manager, err := NewManager(ctx, time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
		close(entered)
		<-ctx.Done()
		return ProviderOutcome{}, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = manager.Start(TargetUGC)
	<-entered
	cancel()
	if status := waitForTerminal(t, manager); status.State != StateFailed || status.Providers["ugc"].State != ProviderFailed {
		t.Fatalf("canceled=%+v", status)
	}
	if _, err := manager.Start(Target("bad")); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("invalid err=%v", err)
	}
}

func waitForTerminal(t *testing.T, manager *Manager) Status {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status()
		if status.State != StateRunning {
			return status
		}
		runtime.Gosched()
	}
	t.Fatal("manager did not reach terminal state")
	return Status{}
}

func TestManagerCloseCancelsWaitsAndIsIdempotent(t *testing.T) {
	observedCancellation := make(chan struct{})
	release := make(chan struct{})
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
		<-ctx.Done()
		close(observedCancellation)
		<-release
		return ProviderOutcome{}, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetUGC); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		manager.Close()
		close(closed)
	}()
	select {
	case <-observedCancellation:
	case <-time.After(time.Second):
		t.Fatal("executor did not observe cancellation")
	}
	select {
	case <-closed:
		t.Fatal("Close returned before executor completed")
	default:
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not join executor")
	}
	manager.Close()
	if _, err := manager.Start(TargetKinepolis); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close err=%v", err)
	}
	status := manager.Status()
	if status.State != StateFailed || status.Providers[string(TargetUGC)].State != ProviderFailed || status.FinishedAt == nil {
		t.Fatalf("terminal status=%+v", status)
	}
}

func TestManagerCloseBeforeStartAndConcurrentStarts(t *testing.T) {
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	manager.Close()
	if _, err := manager.Start(TargetUGC); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after pre-close err=%v", err)
	}

	for range 100 {
		manager, err := NewManager(context.Background(), time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
			<-ctx.Done()
			return ProviderOutcome{}, ctx.Err()
		}))
		if err != nil {
			t.Fatal(err)
		}
		startDone := make(chan error, 1)
		go func() {
			_, err := manager.Start(TargetUGC)
			startDone <- err
		}()
		manager.Close()
		err = <-startDone
		if err != nil && !errors.Is(err, ErrClosed) {
			t.Fatalf("concurrent start err=%v", err)
		}
		if _, err := manager.Start(TargetUGC); !errors.Is(err, ErrClosed) {
			t.Fatalf("post-race start err=%v", err)
		}
	}
}

func TestManagerPublishesTypedOutcomeAndStableFailureCode(t *testing.T) {
	counts := &EnrichmentCounts{Matched: 2}
	outcome := ProviderOutcome{Sync: SyncOutcome{Version: 9, Cinemas: 3, Showtimes: 12}, Enrichment: EnrichmentOutcome{Status: "complete", Counts: counts}}
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return outcome, nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetUGC); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	provider := status.Providers[string(TargetUGC)]
	if provider.State != ProviderSucceeded || provider.Outcome == nil || provider.Outcome.Sync.Version != 9 || provider.Outcome.Enrichment.Counts == nil || provider.Outcome.Enrichment.Counts.Matched != 2 || provider.ErrorCode != "" {
		t.Fatalf("provider=%+v", provider)
	}
	provider.Outcome.Enrichment.Counts.Matched = 99
	if manager.Status().Providers[string(TargetUGC)].Outcome.Enrichment.Counts.Matched != 2 {
		t.Fatal("returned outcome aliases manager state")
	}

	manager, err = NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		return ProviderOutcome{}, NewRunError(FailureDatasetRejected, errors.New("secret"))
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = manager.Start(TargetKinepolis)
	provider = waitForTerminal(t, manager).Providers[string(TargetKinepolis)]
	if provider.State != ProviderFailed || provider.ErrorCode != FailureDatasetRejected || provider.Outcome != nil {
		t.Fatalf("provider=%+v", provider)
	}
}

func TestManagerMarksEveryProviderFailedOnSharedPublicationFailure(t *testing.T) {
	manager, err := NewManager(context.Background(), time.Now, executorMapFunc(func(context.Context, Target, Window) (map[Target]ProviderOutcome, error) {
		return nil, newProviderRunError("", StagePublication, FailureReplacement, errors.New("secret"))
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetAll); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	for _, provider := range []string{string(TargetUGC), string(TargetKinepolis)} {
		got := status.Providers[provider]
		if got.State != ProviderFailed || got.ErrorCode != FailureReplacement || got.Outcome != nil {
			t.Fatalf("provider=%s status=%+v", provider, got)
		}
	}
}
