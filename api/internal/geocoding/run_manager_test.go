package geocoding

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type runExecutorFunc func(context.Context, RunOptions) (Summary, error)

func (f runExecutorFunc) Run(ctx context.Context, options RunOptions) (Summary, error) {
	return f(ctx, options)
}

type memoryRunStore struct {
	mu             sync.Mutex
	status         *RunStatus
	reconciled     int
	createErr      error
	finishErr      error
	snapshotErr    error
	finishStatuses []RunStatus
	finished       chan struct{}
}

func (s *memoryRunStore) Create(_ context.Context, status RunStatus) (RunStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return RunStatus{}, s.createErr
	}
	status.ID = "1"
	copy := cloneRunStatus(status)
	s.status = &copy
	return status, nil
}

func (s *memoryRunStore) Finish(_ context.Context, status RunStatus) error {
	s.mu.Lock()
	s.finishStatuses = append(s.finishStatuses, cloneRunStatus(status))
	copy := cloneRunStatus(status)
	s.status = &copy
	finished := s.finished
	err := s.finishErr
	s.mu.Unlock()
	if finished != nil {
		select {
		case <-finished:
		default:
			close(finished)
		}
	}
	return err
}

func (s *memoryRunStore) Snapshot(context.Context) (*RunStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	if s.status == nil {
		return nil, nil
	}
	copy := cloneRunStatus(*s.status)
	return &copy, nil
}

func (s *memoryRunStore) ReconcileRunning(context.Context, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconciled++
	return nil
}

type fakeRunLocker struct {
	mu         sync.Mutex
	acquireErr error
	acquired   int
	released   int
}

func (l *fakeRunLocker) Acquire(context.Context) (RunLease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.acquireErr != nil {
		return nil, l.acquireErr
	}
	l.acquired++
	return fakeRunLease{locker: l}, nil
}

type fakeRunLease struct{ locker *fakeRunLocker }

func (l fakeRunLease) Release(context.Context) error {
	l.locker.mu.Lock()
	l.locker.released++
	l.locker.mu.Unlock()
	return nil
}

func newGeocodingManagerForTest(t *testing.T, ctx context.Context, executor RunExecutor, store *memoryRunStore, locker *fakeRunLocker) *Manager {
	t.Helper()
	manager, err := NewManager(ctx, func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) }, executor, store, locker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func TestManagerStartsFixedBatchWithoutBlockingAndRejectsOverlap(t *testing.T) {
	started := make(chan RunOptions, 1)
	release := make(chan struct{})
	store := &memoryRunStore{finished: make(chan struct{})}
	locker := &fakeRunLocker{}
	executor := runExecutorFunc(func(_ context.Context, options RunOptions) (Summary, error) {
		started <- options
		<-release
		return Summary{Selected: 7, Skipped: 4, Matched: 2, Ambiguous: 3, NotFound: 2, Written: 7}, nil
	})
	manager := newGeocodingManagerForTest(t, context.Background(), executor, store, locker)
	accepted, err := manager.Start()
	if err != nil || accepted.ID != "1" || accepted.State != RunStateRunning || accepted.FinishedAt != nil || accepted.Summary != nil || accepted.ErrorCode != nil {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	select {
	case options := <-started:
		if !options.RetryAmbiguous || !options.PreserveMatched || options.DryRun || options.Limit != 0 || options.Filters != (Filters{}) {
			t.Fatalf("options=%+v", options)
		}
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	if _, err := manager.Start(); !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("overlap error=%v", err)
	}
	close(release)
	select {
	case <-store.finished:
	case <-time.After(time.Second):
		t.Fatal("run did not finish")
	}
	status, err := manager.Snapshot(context.Background())
	if err != nil || status == nil || status.State != RunStateSucceeded || status.Summary == nil || status.Summary.Selected != 7 || status.Summary.Written != 7 || status.ErrorCode != nil {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	locker.mu.Lock()
	acquired, released := locker.acquired, locker.released
	locker.mu.Unlock()
	if acquired != 2 || released != 2 || store.reconciled != 2 {
		t.Fatalf("acquired=%d released=%d reconciled=%d", acquired, released, store.reconciled)
	}
}

func TestManagerPersistsSafeFailurePanicAndCancellation(t *testing.T) {
	tests := []struct {
		name        string
		executor    RunExecutor
		cancel      bool
		wantCode    RunFailureCode
		wantSummary bool
	}{
		{name: "runner failure", executor: runExecutorFunc(func(context.Context, RunOptions) (Summary, error) {
			return Summary{Selected: 3, Failed: 1, Written: 2}, errors.New("secret provider body")
		}), wantCode: RunFailureFailed, wantSummary: true},
		{name: "panic", executor: runExecutorFunc(func(context.Context, RunOptions) (Summary, error) {
			panic("secret panic")
		}), wantCode: RunFailureInternal, wantSummary: false},
		{name: "cancellation", executor: runExecutorFunc(func(ctx context.Context, _ RunOptions) (Summary, error) {
			<-ctx.Done()
			return Summary{Selected: 1, Failed: 1}, ctx.Err()
		}), cancel: true, wantCode: RunFailureCanceled, wantSummary: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &memoryRunStore{finished: make(chan struct{})}
			locker := &fakeRunLocker{}
			manager := newGeocodingManagerForTest(t, ctx, test.executor, store, locker)
			if _, err := manager.Start(); err != nil {
				t.Fatal(err)
			}
			if test.cancel {
				cancel()
			}
			select {
			case <-store.finished:
			case <-time.After(time.Second):
				t.Fatal("run did not finish")
			}
			status, err := manager.Snapshot(context.Background())
			if err != nil || status == nil || status.State != RunStateFailed || status.ErrorCode == nil || *status.ErrorCode != test.wantCode || (status.Summary != nil) != test.wantSummary {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			if strings.Contains(strings.ToLower(status.ID+string(*status.ErrorCode)), "secret") {
				t.Fatalf("secret leaked in status=%+v", status)
			}
		})
	}
}

func TestManagerCloseCancelsAndWaitsForAcceptedRun(t *testing.T) {
	started := make(chan struct{})
	store := &memoryRunStore{finished: make(chan struct{})}
	manager := newGeocodingManagerForTest(t, context.Background(), runExecutorFunc(func(ctx context.Context, _ RunOptions) (Summary, error) {
		close(started)
		<-ctx.Done()
		return Summary{}, ctx.Err()
	}), store, &fakeRunLocker{})
	if _, err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	<-started
	manager.Close()
	select {
	case <-store.finished:
	default:
		t.Fatal("Close returned before terminal persistence")
	}
	if _, err := manager.Start(); !errors.Is(err, ErrRunClosed) {
		t.Fatalf("closed start error=%v", err)
	}
}

func TestManagerStartupReconciliationContentionAndErrors(t *testing.T) {
	executor := runExecutorFunc(func(context.Context, RunOptions) (Summary, error) { return Summary{}, nil })
	store := &memoryRunStore{}
	contended := &fakeRunLocker{acquireErr: ErrRunInProgress}
	manager, err := NewManager(context.Background(), time.Now, executor, store, contended)
	if err != nil || store.reconciled != 0 {
		t.Fatalf("manager=%v reconciled=%d err=%v", manager, store.reconciled, err)
	}
	manager.Close()
	broken := &fakeRunLocker{acquireErr: errors.New("database secret")}
	_, err = NewManager(context.Background(), time.Now, executor, store, broken)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("startup error=%v", err)
	}
}

func TestManagerRejectedStartReleasesLeaseAndClearsBusy(t *testing.T) {
	store := &memoryRunStore{createErr: errors.New("database secret")}
	locker := &fakeRunLocker{}
	manager := newGeocodingManagerForTest(t, context.Background(), runExecutorFunc(func(context.Context, RunOptions) (Summary, error) {
		return Summary{}, nil
	}), store, locker)
	if _, err := manager.Start(); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("start error=%v", err)
	}
	locker.mu.Lock()
	acquired, released := locker.acquired, locker.released
	locker.mu.Unlock()
	if acquired != 2 || released != 2 {
		t.Fatalf("acquired=%d released=%d", acquired, released)
	}
	store.mu.Lock()
	store.createErr = nil
	store.finished = make(chan struct{})
	finished := store.finished
	store.mu.Unlock()
	if _, err := manager.Start(); err != nil {
		t.Fatalf("retry after rejected start failed: %v", err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("retry did not finish")
	}
}
