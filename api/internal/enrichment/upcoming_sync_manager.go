package enrichment

import (
	"context"
	"errors"
	"sync"
	"time"

	"messeances/api/internal/syncschedule"
)

type UpcomingLease interface{ Release(context.Context) error }
type UpcomingLocker interface {
	Acquire(context.Context) (UpcomingLease, error)
}
type UpcomingClaim func(context.Context) (bool, error)

type UpcomingState string
type UpcomingFailureCode string

const (
	UpcomingRunning   UpcomingState       = "running"
	UpcomingSucceeded UpcomingState       = "succeeded"
	UpcomingFailed    UpcomingState       = "failed"
	UpcomingFailure   UpcomingFailureCode = "sync_failed"
)

type UpcomingStatus struct {
	State      UpcomingState        `json:"state"`
	StartedAt  time.Time            `json:"started_at"`
	FinishedAt *time.Time           `json:"finished_at"`
	ErrorCode  *UpcomingFailureCode `json:"error_code,omitempty"`
}

type UpcomingManager struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
	service *UpcomingService
	locker  UpcomingLocker
	status  *UpcomingStatus // mu protects status and closed; gate serializes admission and execution.
}

func NewUpcomingManager(ctx context.Context, service *UpcomingService, locker UpcomingLocker) (*UpcomingManager, error) {
	if ctx == nil || service == nil || service.store == nil || service.provider == nil || locker == nil {
		return nil, syncschedule.ErrTargetUnavailable
	}
	workerCtx, cancel := context.WithCancel(ctx)
	return &UpcomingManager{ctx: workerCtx, cancel: cancel, service: service, locker: locker}, nil
}

// StartScheduled acquires both locks before consuming an occurrence. Accepted jobs belong to the manager, not the registration.
func (m *UpcomingManager) StartScheduled(claim UpcomingClaim) (<-chan syncschedule.Completion, error) {
	if m == nil || claim == nil {
		return nil, syncschedule.ErrTargetUnavailable
	}
	_, completion, err := m.start(claim)
	return completion, err
}

// Start accepts a manual run without claiming or modifying a schedule occurrence.
func (m *UpcomingManager) Start() (UpcomingStatus, error) {
	if m == nil {
		return UpcomingStatus{}, syncschedule.ErrTargetUnavailable
	}
	status, _, err := m.start(nil)
	return status, err
}

func (m *UpcomingManager) start(claim UpcomingClaim) (status UpcomingStatus, completion <-chan syncschedule.Completion, err error) {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return UpcomingStatus{}, nil, syncschedule.ErrTargetUnavailable
	}
	if !m.service.gate.tryAcquire() {
		m.mu.Unlock()
		return UpcomingStatus{}, nil, syncschedule.ErrInProgress
	}
	m.wg.Add(1)
	m.mu.Unlock()
	accepted := false
	var lease UpcomingLease
	defer func() {
		if recover() != nil {
			err = syncschedule.ErrTargetUnavailable
		}
		if !accepted {
			if lease != nil {
				_ = releaseUpcomingLease(lease)
			}
			m.service.gate.release()
			m.wg.Done()
		}
	}()
	lease, err = m.locker.Acquire(m.ctx)
	if err != nil {
		return UpcomingStatus{}, nil, err
	}
	if claim != nil {
		claimed, err := claim(m.ctx)
		if err != nil {
			return UpcomingStatus{}, nil, syncschedule.ErrTargetUnavailable
		}
		if !claimed {
			return UpcomingStatus{}, nil, syncschedule.ErrOccurrenceClaimed
		}
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return UpcomingStatus{}, nil, syncschedule.ErrTargetUnavailable
	}
	running := UpcomingStatus{State: UpcomingRunning, StartedAt: m.service.now().UTC()}
	m.status = &running
	var result chan syncschedule.Completion
	if claim != nil {
		result = make(chan syncschedule.Completion, 1)
	}
	response := cloneUpcomingStatus(running)
	m.mu.Unlock()
	accepted = true
	go m.run(lease, result)
	return response, result, nil
}

func (m *UpcomingManager) run(lease UpcomingLease, result chan syncschedule.Completion) {
	defer m.wg.Done()
	err := m.execute()
	releaseErr := releaseUpcomingLease(lease)
	finishedAt := m.service.now().UTC()
	succeeded := err == nil && releaseErr == nil
	m.mu.Lock()
	m.status.State = UpcomingSucceeded
	m.status.FinishedAt = &finishedAt
	if !succeeded {
		code := UpcomingFailure
		m.status.State = UpcomingFailed
		m.status.ErrorCode = &code
	}
	m.service.gate.release()
	m.mu.Unlock()
	if result != nil {
		result <- syncschedule.Completion{Succeeded: succeeded}
		close(result)
	}
}

// Snapshot retains only the latest accepted manual or scheduled run in this process.
func (m *UpcomingManager) Snapshot() *UpcomingStatus {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return nil
	}
	status := cloneUpcomingStatus(*m.status)
	return &status
}

func cloneUpcomingStatus(status UpcomingStatus) UpcomingStatus {
	if status.FinishedAt != nil {
		finishedAt := *status.FinishedAt
		status.FinishedAt = &finishedAt
	}
	if status.ErrorCode != nil {
		code := *status.ErrorCode
		status.ErrorCode = &code
	}
	return status
}

func (m *UpcomingManager) execute() (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("upcoming import failed")
		}
	}()
	return m.service.sync(m.ctx)
}

func releaseUpcomingLease(lease UpcomingLease) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return lease.Release(ctx)
}

func (m *UpcomingManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}
