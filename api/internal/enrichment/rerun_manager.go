package enrichment

import (
	"context"
	"errors"
	"sync"
	"time"
)

type RerunState string
type RerunFailureCode string

const (
	RerunRunning   RerunState = "running"
	RerunSucceeded RerunState = "succeeded"
	RerunFailed    RerunState = "failed"

	RerunFailure RerunFailureCode = "rerun_failed"
)

type RerunStatus struct {
	State      RerunState        `json:"state"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt *time.Time        `json:"finished_at"`
	Summary    *RerunSummary     `json:"summary"`
	ErrorCode  *RerunFailureCode `json:"error_code,omitempty"`
}

type RerunManager struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
	busy    bool
	now     func() time.Time
	service *RerunService
	status  *RerunStatus
}

func NewRerunManager(ctx context.Context, service *RerunService, now func() time.Time) (*RerunManager, error) {
	if ctx == nil || !service.available() {
		return nil, errors.New("TMDB rerun manager dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	workerCtx, cancel := context.WithCancel(ctx)
	return &RerunManager{ctx: workerCtx, cancel: cancel, now: now, service: service}, nil
}

func (m *RerunManager) Start() (RerunStatus, error) {
	if m == nil {
		return RerunStatus{}, ErrRerunUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return RerunStatus{}, ErrRerunUnavailable
	}
	if m.busy || !m.service.gate.tryAcquire() {
		return RerunStatus{}, ErrRerunInProgress
	}
	m.busy = true
	status := RerunStatus{State: RerunRunning, StartedAt: m.now().UTC()}
	m.status = &status
	m.wg.Add(1)
	go m.run()
	return cloneRerunStatus(status), nil
}

func (m *RerunManager) Snapshot() *RerunStatus {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return nil
	}
	status := cloneRerunStatus(*m.status)
	return &status
}

func (m *RerunManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *RerunManager) run() {
	defer m.wg.Done()
	summary, err := m.execute()
	finishedAt := m.now().UTC()

	m.mu.Lock()
	m.status.State = RerunSucceeded
	m.status.FinishedAt = &finishedAt
	m.status.Summary = &summary
	m.status.ErrorCode = nil
	if err != nil {
		code := RerunFailure
		m.status.State = RerunFailed
		m.status.Summary = nil
		m.status.ErrorCode = &code
	}
	m.busy = false
	m.service.gate.release()
	m.mu.Unlock()
}

func (m *RerunManager) execute() (summary RerunSummary, err error) {
	defer func() {
		if recover() != nil {
			summary = RerunSummary{}
			err = errors.New("TMDB rerun panicked")
		}
	}()
	return m.service.rerun(m.ctx)
}

func cloneRerunStatus(status RerunStatus) RerunStatus {
	if status.FinishedAt != nil {
		finishedAt := *status.FinishedAt
		status.FinishedAt = &finishedAt
	}
	if status.Summary != nil {
		summary := *status.Summary
		status.Summary = &summary
	}
	if status.ErrorCode != nil {
		code := *status.ErrorCode
		status.ErrorCode = &code
	}
	return status
}
