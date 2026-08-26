package geocoding

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrRunInProgress = errors.New("theater geocoding already in progress")
	ErrRunClosed     = errors.New("theater geocoding manager closed")
)

type RunExecutor interface {
	Run(context.Context, RunOptions) (Summary, error)
}

type Manager struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closed   bool
	busy     bool
	now      func() time.Time
	executor RunExecutor
	store    RunStore
	locker   RunLocker
}

func NewManager(ctx context.Context, now func() time.Time, executor RunExecutor, store RunStore, locker RunLocker) (*Manager, error) {
	if ctx == nil || executor == nil || store == nil || locker == nil {
		return nil, errors.New("theater geocoding manager dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if err := reconcileManagerStartup(ctx, now().UTC(), store, locker); err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: workerCtx, cancel: cancel, now: now, executor: executor, store: store, locker: locker}, nil
}

func reconcileManagerStartup(ctx context.Context, finishedAt time.Time, store RunStore, locker RunLocker) error {
	lease, err := locker.Acquire(ctx)
	if errors.Is(err, ErrRunInProgress) {
		return nil
	}
	if err != nil {
		return errors.New("theater geocoding run reconciliation failed")
	}
	reconcileErr := store.ReconcileRunning(ctx, finishedAt)
	releaseErr := releaseRunLease(ctx, lease)
	if reconcileErr != nil || releaseErr != nil {
		return errors.New("theater geocoding run reconciliation failed")
	}
	return nil
}

func releaseRunLease(ctx context.Context, lease RunLease) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return lease.Release(releaseCtx)
}

func (m *Manager) Start() (RunStatus, error) {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return RunStatus{}, ErrRunClosed
	}
	if m.busy {
		m.mu.Unlock()
		return RunStatus{}, ErrRunInProgress
	}
	m.busy = true
	m.wg.Add(1)
	m.mu.Unlock()
	tracked := true
	defer func() {
		if tracked {
			m.wg.Done()
		}
	}()

	lease, err := m.locker.Acquire(m.ctx)
	if err != nil {
		m.clearBusy()
		if errors.Is(err, ErrRunInProgress) {
			return RunStatus{}, ErrRunInProgress
		}
		return RunStatus{}, errors.New("theater geocoding run lease acquisition failed")
	}
	rejected := true
	defer func() {
		if rejected {
			m.releaseAfterRejectedStart(lease)
		}
	}()
	if err := m.store.ReconcileRunning(m.ctx, m.now().UTC()); err != nil {
		return RunStatus{}, errors.New("theater geocoding run reconciliation failed")
	}
	if m.ctx.Err() != nil {
		return RunStatus{}, ErrRunClosed
	}
	created, err := m.store.Create(m.ctx, RunStatus{State: RunStateRunning, StartedAt: m.now().UTC()})
	if err != nil {
		if errors.Is(err, ErrRunInProgress) {
			return RunStatus{}, ErrRunInProgress
		}
		return RunStatus{}, errors.New("theater geocoding run creation failed")
	}
	rejected = false
	tracked = false
	go m.run(created, lease)
	return cloneRunStatus(created), nil
}

func (m *Manager) Snapshot(ctx context.Context) (*RunStatus, error) {
	status, err := m.store.Snapshot(ctx)
	if err != nil {
		return nil, errors.New("theater geocoding run snapshot failed")
	}
	if status == nil {
		return nil, nil
	}
	copy := cloneRunStatus(*status)
	return &copy, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) run(status RunStatus, lease RunLease) {
	defer m.wg.Done()
	defer func() {
		if recover() != nil {
			finished := m.now().UTC()
			code := RunFailureInternal
			status.State = RunStateFailed
			status.FinishedAt = &finished
			status.Summary = nil
			status.ErrorCode = &code
		}
		m.finalize(status, lease)
	}()

	summary, err := m.executor.Run(m.ctx, RunOptions{RetryAmbiguous: true, PreserveMatched: true})
	finished := m.now().UTC()
	status.FinishedAt = &finished
	status.Summary = &RunSummary{
		Selected: summary.Selected, Skipped: summary.Skipped, Matched: summary.Matched,
		Ambiguous: summary.Ambiguous, NotFound: summary.NotFound, Failed: summary.Failed, Written: summary.Written,
	}
	if err == nil {
		status.State = RunStateSucceeded
		return
	}
	status.State = RunStateFailed
	code := RunFailureFailed
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || m.ctx.Err() != nil {
		code = RunFailureCanceled
	}
	status.ErrorCode = &code
}

func (m *Manager) finalize(status RunStatus, lease RunLease) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	_ = m.store.Finish(ctx, status)
	cancel()
	_ = releaseRunLease(m.ctx, lease)
	m.clearBusy()
}

func (m *Manager) releaseAfterRejectedStart(lease RunLease) {
	_ = releaseRunLease(m.ctx, lease)
	m.clearBusy()
}

func (m *Manager) clearBusy() {
	m.mu.Lock()
	m.busy = false
	m.mu.Unlock()
}
