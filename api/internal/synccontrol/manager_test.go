package synccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type executorFunc func(context.Context, Target, Window) (ProviderOutcome, error)
type executorMapFunc func(context.Context, Target, Window) (map[Target]ProviderOutcome, error)

type memoryRunStore struct {
	mu   sync.Mutex
	next int
	runs []Status
}

type rejectingRunStore struct{ memoryRunStore }

type trackingRunLease struct {
	mu         sync.Mutex
	releases   int
	releaseErr error
	order      *[]string
}

func (l *trackingRunLease) Release(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releases++
	if l.order != nil {
		*l.order = append(*l.order, "release")
	}
	return l.releaseErr
}

func (l *trackingRunLease) releaseCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releases
}

type trackingRunLocker struct {
	lease      *trackingRunLease
	acquireErr error
	order      *[]string
}

func (l trackingRunLocker) Acquire(context.Context) (RunLease, error) {
	if l.order != nil {
		*l.order = append(*l.order, "acquire")
	}
	if l.acquireErr != nil {
		return nil, l.acquireErr
	}
	return l.lease, nil
}

type trackingRunStore struct {
	memoryRunStore
	order             *[]string
	reconcileCount    int
	reconcileErr      error
	reconcileErrAfter int
	createErr         error
	terminalUpdateErr error
}

func (s *trackingRunStore) ReconcileRunning(ctx context.Context, finishedAt time.Time) error {
	s.reconcileCount++
	if s.order != nil {
		*s.order = append(*s.order, "reconcile")
	}
	if s.reconcileErr != nil && (s.reconcileErrAfter == 0 || s.reconcileCount > s.reconcileErrAfter) {
		return s.reconcileErr
	}
	return s.memoryRunStore.ReconcileRunning(ctx, finishedAt)
}

func (s *trackingRunStore) Create(ctx context.Context, status Status) (Status, error) {
	if s.order != nil {
		*s.order = append(*s.order, "create")
	}
	if s.createErr != nil {
		return Status{}, s.createErr
	}
	return s.memoryRunStore.Create(ctx, status)
}

func (s *trackingRunStore) Update(ctx context.Context, status Status) error {
	if s.order != nil {
		*s.order = append(*s.order, "update:"+string(status.State))
	}
	if status.State != StateRunning && s.terminalUpdateErr != nil {
		return s.terminalUpdateErr
	}
	return s.memoryRunStore.Update(ctx, status)
}

type failingSnapshotStore struct{ memoryRunStore }

func (*failingSnapshotStore) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{}, errors.New("database secret")
}

func (*rejectingRunStore) Create(context.Context, Status) (Status, error) {
	return Status{}, errors.New("database secret")
}

func (s *memoryRunStore) Create(_ context.Context, status Status) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	status.ID = strconv.Itoa(s.next)
	s.runs = append([]Status{cloneStatus(status)}, s.runs...)
	return status, nil
}

func (s *memoryRunStore) Update(_ context.Context, status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.runs {
		if s.runs[i].ID == status.ID {
			s.runs[i] = cloneStatus(status)
			return nil
		}
	}
	return errors.New("missing run")
}

func (s *memoryRunStore) List(context.Context, int) ([]Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStatuses(s.runs), nil
}

func (s *memoryRunStore) Snapshot(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := Snapshot{Runs: []Status{}}
	for _, run := range s.runs {
		if run.State == StateRunning && snapshot.Job == nil {
			job := cloneStatus(run)
			snapshot.Job = &job
			continue
		}
		if run.State != StateRunning {
			snapshot.Runs = append(snapshot.Runs, cloneStatus(run))
		}
	}
	return snapshot, nil
}

func (s *memoryRunStore) ReconcileRunning(_ context.Context, finishedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.runs {
		if s.runs[i].State != StateRunning {
			continue
		}
		for provider, status := range s.runs[i].Providers {
			switch status.State {
			case ProviderRunning:
				s.runs[i].Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: FailureCanceled}
			case ProviderPending:
				s.runs[i].Providers[provider] = ProviderStatus{State: ProviderSkipped}
			}
		}
		s.runs[i].State = StateFailed
		finished := finishedAt.UTC()
		s.runs[i].FinishedAt = &finished
	}
	return nil
}

func newTestManager(ctx context.Context, now func() time.Time, executor Executor) (*Manager, error) {
	return NewManager(ctx, now, executor, &memoryRunStore{})
}

func (f executorMapFunc) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	return f(ctx, target, window)
}

func (f executorFunc) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	outcome, err := f(ctx, target, window)
	if err != nil {
		return nil, err
	}
	if outcome.Sync.Through == "" {
		outcome.Sync.Through = window.From
	}
	if target == TargetAll {
		return map[Target]ProviderOutcome{TargetUGC: outcome, TargetKinepolis: outcome, TargetPathe: outcome, TargetCGR: outcome}, nil
	}
	return map[Target]ProviderOutcome{target: outcome}, nil
}

func TestManagerOrdersAllAndRejectsOverlap(t *testing.T) {
	now := time.Date(2026, 8, 17, 23, 30, 0, 0, time.FixedZone("test", -4*60*60))
	started := make(chan Target, 1)
	release := make(chan struct{})
	manager, err := newTestManager(context.Background(), func() time.Time { return now }, executorFunc(func(_ context.Context, target Target, window Window) (ProviderOutcome, error) {
		if window != (Window{From: "2026-08-18"}) {
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
	if accepted.ID != "1" || accepted.State != StateRunning || accepted.Providers["ugc"].State != ProviderPending || accepted.Providers["kinepolis"].State != ProviderPending || accepted.Providers["pathe"].State != ProviderPending || accepted.Providers["cgr"].State != ProviderPending || len(accepted.Providers) != 4 || accepted.StartedAt.Location() != time.UTC {
		t.Fatalf("accepted=%+v", accepted)
	}
	if accepted.From != "2026-08-18" || accepted.Through != accepted.From {
		t.Fatalf("provisional window=%s..%s", accepted.From, accepted.Through)
	}
	if _, err := manager.Start(TargetUGC); !errors.Is(err, ErrInProgress) {
		t.Fatalf("overlap err=%v", err)
	}
	if target := <-started; target != TargetAll {
		t.Fatalf("target=%s", target)
	}
	status := manager.Status()
	if status.Providers["ugc"].State != ProviderRunning || status.Providers["kinepolis"].State != ProviderRunning || status.Providers["pathe"].State != ProviderRunning || status.Providers["cgr"].State != ProviderRunning {
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

func TestManagerRejectsRunBeforeExecutionWhenDurableInsertFails(t *testing.T) {
	executed := make(chan struct{}, 1)
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		executed <- struct{}{}
		return ProviderOutcome{}, nil
	}), &rejectingRunStore{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetUGC); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("start err=%v", err)
	}
	select {
	case <-executed:
		t.Fatal("executor started before durable insert")
	default:
	}
	if manager.Status().ID != "" || len(manager.Runs()) != 0 {
		t.Fatalf("status=%+v runs=%+v", manager.Status(), manager.Runs())
	}
}

func TestManagerFailurePanicCancellationAndTargets(t *testing.T) {
	tests := []struct {
		name      string
		target    Target
		executor  executorFunc
		cancel    context.CancelFunc
		wantUGC   ProviderState
		wantKin   ProviderState
		wantPathe ProviderState
		wantCGR   ProviderState
	}{
		{name: "failure skips other provider", target: TargetAll, executor: func(context.Context, Target, Window) (ProviderOutcome, error) {
			return ProviderOutcome{}, newProviderRunError(TargetUGC, StageDatasetValidation, FailureDatasetRejected, errors.New("secret"))
		}, wantUGC: ProviderFailed, wantKin: ProviderSkipped, wantPathe: ProviderSkipped, wantCGR: ProviderSkipped},
		{name: "panic becomes failure", target: TargetKinepolis, executor: func(context.Context, Target, Window) (ProviderOutcome, error) { panic("secret") }, wantUGC: ProviderNotRequested, wantKin: ProviderFailed, wantPathe: ProviderNotRequested, wantCGR: ProviderNotRequested},
		{name: "single provider succeeds", target: TargetPathe, executor: func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }, wantUGC: ProviderNotRequested, wantKin: ProviderNotRequested, wantPathe: ProviderSucceeded, wantCGR: ProviderNotRequested},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := newTestManager(context.Background(), time.Now, test.executor)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Start(test.target); err != nil {
				t.Fatal(err)
			}
			status := waitForTerminal(t, manager)
			if status.Providers["ugc"].State != test.wantUGC || status.Providers["kinepolis"].State != test.wantKin || status.Providers["pathe"].State != test.wantPathe || status.Providers["cgr"].State != test.wantCGR || len(status.Providers) != 4 || status.FinishedAt == nil {
				t.Fatalf("status=%+v", status)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	manager, err := newTestManager(ctx, time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
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
	manager, err := newTestManager(context.Background(), time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
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
	manager, err := newTestManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	manager.Close()
	if _, err := manager.Start(TargetUGC); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after pre-close err=%v", err)
	}

	for range 100 {
		manager, err := newTestManager(context.Background(), time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
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
	outcome := ProviderOutcome{Sync: SyncOutcome{Version: 9, Cinemas: 3, Showtimes: 12, Through: "2026-12-24"}, Enrichment: EnrichmentOutcome{Status: "complete", Counts: counts}}
	manager, err := newTestManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return outcome, nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetUGC); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	provider := status.Providers[string(TargetUGC)]
	if provider.State != ProviderSucceeded || provider.Outcome == nil || provider.Outcome.Sync.Version != 9 || provider.Outcome.Enrichment.Counts == nil || provider.Outcome.Enrichment.Counts.Matched != 2 || provider.ErrorCode != "" || status.Through != "2026-12-24" {
		t.Fatalf("provider=%+v", provider)
	}
	provider.Outcome.Enrichment.Counts.Matched = 99
	if manager.Status().Providers[string(TargetUGC)].Outcome.Enrichment.Counts.Matched != 2 {
		t.Fatal("returned outcome aliases manager state")
	}

	manager, err = newTestManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
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

func TestManagerTargetAllUsesLatestProviderEnd(t *testing.T) {
	manager, err := newTestManager(context.Background(), func() time.Time {
		return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	}, executorMapFunc(func(context.Context, Target, Window) (map[Target]ProviderOutcome, error) {
		return map[Target]ProviderOutcome{
			TargetUGC:       {Sync: SyncOutcome{Through: "2027-01-10"}},
			TargetKinepolis: {Sync: SyncOutcome{Through: "2026-11-20"}},
			TargetPathe:     {Sync: SyncOutcome{Through: "2026-12-15"}},
			TargetCGR:       {Sync: SyncOutcome{Through: "2026-10-30"}},
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetAll); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	if status.State != StateSucceeded || status.Through != "2027-01-10" {
		t.Fatalf("status=%+v", status)
	}
}

func TestManagerMarksEveryProviderFailedOnSharedPublicationFailure(t *testing.T) {
	manager, err := newTestManager(context.Background(), time.Now, executorMapFunc(func(context.Context, Target, Window) (map[Target]ProviderOutcome, error) {
		return nil, newProviderRunError("", StagePublication, FailureReplacement, errors.New("secret"))
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetAll); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	for _, provider := range []string{string(TargetUGC), string(TargetKinepolis), string(TargetPathe), string(TargetCGR)} {
		got := status.Providers[provider]
		if got.State != ProviderFailed || got.ErrorCode != FailureReplacement || got.Outcome != nil {
			t.Fatalf("provider=%s status=%+v", provider, got)
		}
	}
}

func TestManagerScheduledOccurrenceCompletionAndFinalization(t *testing.T) {
	order := []string{}
	lease := &trackingRunLease{order: &order}
	store := &trackingRunStore{order: &order, terminalUpdateErr: errors.New("database secret")}
	now := time.Date(2026, 8, 24, 12, 34, 56, 0, time.FixedZone("test", 2*60*60))
	manager, err := NewManager(context.Background(), func() time.Time { return now }, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		return ProviderOutcome{}, nil
	}), store, trackingRunLocker{lease: lease, order: &order})
	if err != nil {
		t.Fatal(err)
	}
	accepted, completion, err := manager.StartScheduled(Occurrence{
		Provider: TargetUGC, Revision: 9,
		ScheduledFor: time.Date(2026, 10, 25, 2, 30, 45, 0, time.FixedZone("CEST", 2*60*60)),
		Attempt:      2,
	})
	if err != nil {
		t.Fatal(err)
	}
	canonical := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	if accepted.Trigger != TriggerScheduled || accepted.Occurrence == nil || accepted.Occurrence.Revision != 9 || accepted.Occurrence.Attempt != 2 || !accepted.Occurrence.ScheduledFor.Equal(canonical) || accepted.Occurrence.ScheduledFor.Location() != time.UTC {
		t.Fatalf("accepted=%+v", accepted)
	}
	result, ok := <-completion
	if !ok || result.Status.State != StateSucceeded || result.FinalizationError == nil {
		t.Fatalf("completion=%+v open=%t", result, ok)
	}
	if lease.releaseCount() != 2 {
		t.Fatal("completion delivered before lease release")
	}
	if _, ok := <-completion; ok {
		t.Fatal("completion channel delivered more than once")
	}
	wantOrder := []string{"acquire", "reconcile", "release", "acquire", "reconcile", "create", "update:running", "update:succeeded", "release"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("order=%v want=%v", order, wantOrder)
	}
	payload, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(payload)
	for _, field := range []string{`"trigger":"scheduled"`, `"occurrence":{`, `"schedule_revision":9`, `"scheduled_for":"2026-10-25T00:30:00Z"`, `"attempt":2`} {
		if !strings.Contains(jsonText, field) {
			t.Fatalf("scheduled JSON missing %s: %s", field, jsonText)
		}
	}
}

func TestManagerScheduledClaimConflictReleasesWithoutExecution(t *testing.T) {
	executed := false
	lease := &trackingRunLease{}
	store := &trackingRunStore{createErr: ErrOccurrenceClaimed}
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		executed = true
		return ProviderOutcome{}, nil
	}), store, trackingRunLocker{lease: lease})
	if err != nil {
		t.Fatal(err)
	}
	status, completion, err := manager.StartScheduled(Occurrence{Provider: TargetPathe, Revision: 1, ScheduledFor: time.Now(), Attempt: 0})
	if !errors.Is(err, ErrOccurrenceClaimed) || status.ID != "" || completion != nil {
		t.Fatalf("status=%+v completion=%v err=%v", status, completion, err)
	}
	if executed || lease.releaseCount() != 2 || store.reconcileCount != 2 {
		t.Fatalf("executed=%t releases=%d reconciles=%d", executed, lease.releaseCount(), store.reconcileCount)
	}
}

func TestManagerReconcilesAbandonedRunDuringStartup(t *testing.T) {
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC)
	stale := Status{ID: "7", Target: TargetAll, State: StateRunning, Trigger: TriggerManual, StartedAt: now.Add(-time.Hour), From: "2026-08-24", Through: "2026-08-24", Providers: map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderRunning},
		string(TargetKinepolis): {State: ProviderPending},
	}}
	order := []string{}
	store := &trackingRunStore{memoryRunStore: memoryRunStore{runs: []Status{stale}}, order: &order}
	lease := &trackingRunLease{order: &order}
	executed := false
	manager, err := NewManager(context.Background(), func() time.Time { return now }, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		executed = true
		return ProviderOutcome{}, nil
	}), store, trackingRunLocker{lease: lease, order: &order})
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	snapshot, err := store.Snapshot(context.Background())
	if err != nil || snapshot.Job != nil || len(snapshot.Runs) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	got := snapshot.Runs[0]
	if got.ID != stale.ID || got.State != StateFailed || got.FinishedAt == nil || !got.FinishedAt.Equal(now) || got.Providers[string(TargetUGC)].ErrorCode != FailureCanceled || got.Providers[string(TargetKinepolis)].State != ProviderSkipped || got.Providers[string(TargetPathe)].State != ProviderNotRequested || got.Providers[string(TargetCGR)].State != ProviderNotRequested || len(got.Providers) != 4 {
		t.Fatalf("reconciled=%+v", got)
	}
	if executed || lease.releaseCount() != 1 || strings.Join(order, ",") != "acquire,reconcile,release" {
		t.Fatalf("executed=%t releases=%d order=%v", executed, lease.releaseCount(), order)
	}
}

func TestManagerStartupReconciliationFailuresAreRedactedAndReleaseLease(t *testing.T) {
	tests := []struct {
		name         string
		store        *trackingRunStore
		lease        *trackingRunLease
		acquireErr   error
		wantReleases int
	}{
		{name: "acquisition", store: &trackingRunStore{}, lease: &trackingRunLease{}, acquireErr: errors.New("database secret")},
		{name: "reconciliation", store: &trackingRunStore{reconcileErr: errors.New("database secret")}, lease: &trackingRunLease{}, wantReleases: 1},
		{name: "release", store: &trackingRunStore{}, lease: &trackingRunLease{releaseErr: errors.New("database secret")}, wantReleases: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
				t.Fatal("executor called during startup")
				return ProviderOutcome{}, nil
			}), test.store, trackingRunLocker{lease: test.lease, acquireErr: test.acquireErr})
			if manager != nil || err == nil || err.Error() != "sync run reconciliation failed" || strings.Contains(err.Error(), "secret") {
				t.Fatalf("manager=%v err=%v", manager, err)
			}
			if test.lease.releaseCount() != test.wantReleases {
				t.Fatalf("releases=%d want=%d", test.lease.releaseCount(), test.wantReleases)
			}
		})
	}
}

func TestManagerLeaseContentionSkipsReconciliationAndHistory(t *testing.T) {
	stale := Status{ID: "7", Target: TargetUGC, State: StateRunning, StartedAt: time.Now().Add(-time.Hour), From: "2026-08-24", Through: "2026-08-24", Providers: map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderRunning},
		string(TargetKinepolis): {State: ProviderNotRequested},
	}}
	store := &trackingRunStore{memoryRunStore: memoryRunStore{runs: []Status{stale}}}
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		return ProviderOutcome{}, nil
	}), store, trackingRunLocker{acquireErr: ErrInProgress})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(TargetUGC); !errors.Is(err, ErrInProgress) {
		t.Fatalf("start err=%v", err)
	}
	snapshot, snapshotErr := store.Snapshot(context.Background())
	if store.reconcileCount != 0 || snapshotErr != nil || snapshot.Job == nil || snapshot.Job.ID != stale.ID || snapshot.Job.State != StateRunning || snapshot.Job.FinishedAt != nil {
		t.Fatalf("reconciles=%d snapshot=%+v err=%v", store.reconcileCount, snapshot, snapshotErr)
	}
}

func TestManagerReleasesLeaseAfterPreExecutionFailures(t *testing.T) {
	tests := []struct {
		name  string
		store *trackingRunStore
	}{
		{name: "reconciliation", store: &trackingRunStore{reconcileErr: errors.New("database secret"), reconcileErrAfter: 1}},
		{name: "creation", store: &trackingRunStore{createErr: errors.New("database secret")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lease := &trackingRunLease{}
			executed := false
			manager, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
				executed = true
				return ProviderOutcome{}, nil
			}), test.store, trackingRunLocker{lease: lease})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Start(TargetUGC); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("start err=%v", err)
			}
			if executed || lease.releaseCount() != 2 {
				t.Fatalf("executed=%t releases=%d", executed, lease.releaseCount())
			}
		})
	}
}

func TestManagerReleasesLeaseForEveryTerminalPath(t *testing.T) {
	tests := []struct {
		name     string
		executor executorFunc
		want     JobState
	}{
		{name: "success", executor: func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }, want: StateSucceeded},
		{name: "failure", executor: func(context.Context, Target, Window) (ProviderOutcome, error) {
			return ProviderOutcome{}, errors.New("provider failed")
		}, want: StateFailed},
		{name: "panic", executor: func(context.Context, Target, Window) (ProviderOutcome, error) { panic("provider panic") }, want: StateFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lease := &trackingRunLease{}
			manager, err := NewManager(context.Background(), time.Now, test.executor, &trackingRunStore{}, trackingRunLocker{lease: lease})
			if err != nil {
				t.Fatal(err)
			}
			_, completion, err := manager.StartScheduled(Occurrence{Provider: TargetUGC, Revision: 1, ScheduledFor: time.Now(), Attempt: 0})
			if err != nil {
				t.Fatal(err)
			}
			result := <-completion
			if result.Status.State != test.want || result.FinalizationError != nil || lease.releaseCount() != 2 {
				t.Fatalf("completion=%+v releases=%d", result, lease.releaseCount())
			}
		})
	}
}

func TestManagerCloseFinalizesAndReleasesScheduledLease(t *testing.T) {
	lease := &trackingRunLease{}
	entered := make(chan struct{})
	manager, err := NewManager(context.Background(), time.Now, executorFunc(func(ctx context.Context, _ Target, _ Window) (ProviderOutcome, error) {
		close(entered)
		<-ctx.Done()
		return ProviderOutcome{}, ctx.Err()
	}), &trackingRunStore{}, trackingRunLocker{lease: lease})
	if err != nil {
		t.Fatal(err)
	}
	_, completion, err := manager.StartScheduled(Occurrence{Provider: TargetKinepolis, Revision: 1, ScheduledFor: time.Now(), Attempt: 0})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	manager.Close()
	result := <-completion
	if result.Status.State != StateFailed || result.FinalizationError != nil || lease.releaseCount() != 2 {
		t.Fatalf("completion=%+v releases=%d", result, lease.releaseCount())
	}
}

func TestManagerDatabaseSnapshotIsSharedAndReadFailuresDoNotFallback(t *testing.T) {
	store := &memoryRunStore{}
	release := make(chan struct{})
	managerA, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		<-release
		return ProviderOutcome{}, nil
	}), store)
	if err != nil {
		t.Fatal(err)
	}
	managerB, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		return ProviderOutcome{}, nil
	}), store)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := managerA.Start(TargetUGC)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managerB.Snapshot(context.Background())
	if err != nil || snapshot.Job == nil || snapshot.Job.ID != accepted.ID || len(snapshot.Runs) != 0 {
		t.Fatalf("running snapshot=%+v err=%v", snapshot, err)
	}
	close(release)
	waitForTerminal(t, managerA)
	managerA.Close()
	snapshot, err = managerB.Snapshot(context.Background())
	if err != nil || snapshot.Job != nil || len(snapshot.Runs) != 1 || snapshot.Runs[0].ID != accepted.ID || snapshot.Runs[0].Trigger != TriggerManual || snapshot.Runs[0].Occurrence != nil {
		t.Fatalf("terminal snapshot=%+v err=%v", snapshot, err)
	}

	failing, err := NewManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		return ProviderOutcome{}, nil
	}), &failingSnapshotStore{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := failing.Snapshot(context.Background()); err == nil || snapshot.Job != nil || snapshot.Runs != nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("failed snapshot=%+v err=%v", snapshot, err)
	}
}

func TestManagerValidatesScheduledOccurrence(t *testing.T) {
	manager, err := newTestManager(context.Background(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) {
		return ProviderOutcome{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, occurrence := range []Occurrence{
		{Provider: TargetAll, Revision: 1, ScheduledFor: time.Now(), Attempt: 0},
		{Provider: TargetUGC, Revision: 0, ScheduledFor: time.Now(), Attempt: 0},
		{Provider: TargetUGC, Revision: 1, Attempt: 0},
		{Provider: TargetUGC, Revision: 1, ScheduledFor: time.Time{}.Add(30 * time.Second), Attempt: 0},
		{Provider: TargetUGC, Revision: 1, ScheduledFor: time.Now(), Attempt: -1},
		{Provider: TargetUGC, Revision: 1, ScheduledFor: time.Now(), Attempt: 3},
	} {
		if _, completion, err := manager.StartScheduled(occurrence); !errors.Is(err, ErrInvalidOccurrence) || completion != nil {
			t.Fatalf("occurrence=%+v completion=%v err=%v", occurrence, completion, err)
		}
	}
}
