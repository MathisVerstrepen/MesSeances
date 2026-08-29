package enrichment

import (
	"context"
	"errors"
	"sync"
	"time"
)

type MetadataRefreshState string
type MetadataRefreshFailureCode string

const (
	MetadataRefreshRunning   MetadataRefreshState = "running"
	MetadataRefreshSucceeded MetadataRefreshState = "succeeded"
	MetadataRefreshFailed    MetadataRefreshState = "failed"

	MetadataRefreshFailure MetadataRefreshFailureCode = "refresh_failed"
)

type MetadataRefreshStatus struct {
	State      MetadataRefreshState        `json:"state"`
	StartedAt  time.Time                   `json:"started_at"`
	FinishedAt *time.Time                  `json:"finished_at"`
	Summary    *MetadataRefreshSummary     `json:"summary"`
	ErrorCode  *MetadataRefreshFailureCode `json:"error_code,omitempty"`
}

type MetadataRefreshManager struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
	busy    bool
	now     func() time.Time
	service *MetadataRefreshService
	status  *MetadataRefreshStatus
}

func NewMetadataRefreshManager(ctx context.Context, service *MetadataRefreshService, now func() time.Time) (*MetadataRefreshManager, error) {
	if ctx == nil || !service.available() {
		return nil, errors.New("TMDB metadata refresh manager dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	workerCtx, cancel := context.WithCancel(ctx)
	return &MetadataRefreshManager{ctx: workerCtx, cancel: cancel, now: now, service: service}, nil
}

func (m *MetadataRefreshManager) Start() (MetadataRefreshStatus, error) {
	if m == nil {
		return MetadataRefreshStatus{}, ErrMetadataRefreshUnavailable
	}
	status, _, err := m.start(nil)
	return status, err
}

func (m *MetadataRefreshManager) start(claim MetadataRefreshClaim) (MetadataRefreshStatus, <-chan MetadataRefreshCompletion, error) {
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return MetadataRefreshStatus{}, nil, ErrMetadataRefreshUnavailable
	}
	if m.busy || !m.service.gate.tryAcquire() {
		m.mu.Unlock()
		return MetadataRefreshStatus{}, nil, ErrMetadataRefreshInProgress
	}
	m.busy = true
	m.wg.Add(1)
	m.mu.Unlock()

	if claim != nil {
		claimed, err := claim(m.ctx)
		if err != nil || !claimed {
			m.rejectScheduledStart()
			if err != nil {
				return MetadataRefreshStatus{}, nil, ErrMetadataRefreshUnavailable
			}
			return MetadataRefreshStatus{}, nil, ErrMetadataRefreshOccurrenceClaimed
		}
	}

	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		m.rejectScheduledStart()
		return MetadataRefreshStatus{}, nil, ErrMetadataRefreshUnavailable
	}
	status := MetadataRefreshStatus{
		State:     MetadataRefreshRunning,
		StartedAt: m.now().UTC(),
	}
	m.status = &status
	var completion chan MetadataRefreshCompletion
	if claim != nil {
		completion = make(chan MetadataRefreshCompletion, 1)
	}
	accepted := cloneMetadataRefreshStatus(status)
	m.mu.Unlock()
	go m.run(completion)
	return accepted, completion, nil
}

func (m *MetadataRefreshManager) Snapshot() *MetadataRefreshStatus {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return nil
	}
	status := cloneMetadataRefreshStatus(*m.status)
	return &status
}

func (m *MetadataRefreshManager) Close() {
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

func (m *MetadataRefreshManager) run(completion chan MetadataRefreshCompletion) {
	defer m.wg.Done()
	summary, err := m.execute()
	finishedAt := m.now().UTC()

	m.mu.Lock()
	m.status.State = MetadataRefreshSucceeded
	m.status.FinishedAt = &finishedAt
	m.status.Summary = &summary
	m.status.ErrorCode = nil
	if err != nil {
		code := MetadataRefreshFailure
		m.status.State = MetadataRefreshFailed
		m.status.Summary = nil
		m.status.ErrorCode = &code
	}
	m.busy = false
	m.service.gate.release()
	m.mu.Unlock()
	if completion != nil {
		completion <- MetadataRefreshCompletion{Succeeded: err == nil}
		close(completion)
	}
}

func (m *MetadataRefreshManager) rejectScheduledStart() {
	m.mu.Lock()
	m.busy = false
	m.service.gate.release()
	m.mu.Unlock()
	m.wg.Done()
}

func (m *MetadataRefreshManager) execute() (summary MetadataRefreshSummary, err error) {
	defer func() {
		if recover() != nil {
			summary = MetadataRefreshSummary{}
			err = errors.New("TMDB metadata refresh panicked")
		}
	}()
	return m.service.refresh(m.ctx)
}

func cloneMetadataRefreshStatus(status MetadataRefreshStatus) MetadataRefreshStatus {
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
