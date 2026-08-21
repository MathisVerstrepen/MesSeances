package synccontrol

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"messeances/api/internal/schedule"
)

type Target string

const (
	TargetAll       Target = "all"
	TargetUGC       Target = "ugc"
	TargetKinepolis Target = "kinepolis"

	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"

	ProviderNotRequested = "not_requested"
	ProviderPending      = "pending"
	ProviderRunning      = "running"
	ProviderSucceeded    = "succeeded"
	ProviderFailed       = "failed"
	ProviderSkipped      = "skipped"
)

var (
	ErrInvalidTarget = errors.New("invalid sync target")
	ErrInProgress    = errors.New("sync already in progress")
	ErrClosed        = errors.New("sync manager closed")
)

type Window struct {
	From    string
	Through string
}

type Executor interface {
	Run(context.Context, Target, Window) (ProviderOutcome, error)
}

type ProviderStatus struct {
	State     string           `json:"state"`
	Outcome   *ProviderOutcome `json:"outcome,omitempty"`
	ErrorCode FailureCode      `json:"error_code,omitempty"`
}

type Status struct {
	ID         string                    `json:"id"`
	Target     Target                    `json:"target"`
	State      string                    `json:"state"`
	StartedAt  time.Time                 `json:"started_at"`
	FinishedAt *time.Time                `json:"finished_at"`
	From       string                    `json:"from"`
	Through    string                    `json:"through"`
	Providers  map[string]ProviderStatus `json:"providers"`
}

type Manager struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closed   bool
	now      func() time.Time
	executor Executor
	location *time.Location
	nextID   uint64
	status   Status
}

func NewManager(ctx context.Context, now func() time.Time, executor Executor) (*Manager, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil, err
	}
	if ctx == nil || executor == nil {
		return nil, errors.New("sync manager dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	executorCtx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: executorCtx, cancel: cancel, now: now, executor: executor, location: location}, nil
}

func ValidTarget(target Target) bool {
	return target == TargetAll || target == TargetUGC || target == TargetKinepolis
}

func (m *Manager) Start(target Target) (Status, error) {
	if !ValidTarget(target) {
		return Status{}, ErrInvalidTarget
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return Status{}, ErrClosed
	}
	if m.status.State == StateRunning {
		return Status{}, ErrInProgress
	}

	now := m.now()
	today := now.In(m.location)
	m.nextID++
	providers := map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderNotRequested},
		string(TargetKinepolis): {State: ProviderNotRequested},
	}
	if target == TargetAll || target == TargetUGC {
		providers[string(TargetUGC)] = ProviderStatus{State: ProviderPending}
	}
	if target == TargetAll || target == TargetKinepolis {
		providers[string(TargetKinepolis)] = ProviderStatus{State: ProviderPending}
	}
	m.status = Status{
		ID: strconv.FormatUint(m.nextID, 10), Target: target, State: StateRunning,
		StartedAt: now.UTC(), From: today.Format("2006-01-02"),
		Through: today.AddDate(0, 0, 7).Format("2006-01-02"), Providers: providers,
	}
	accepted := cloneStatus(m.status)
	m.wg.Add(1)
	go func(window Window) {
		defer m.wg.Done()
		m.run(target, window)
	}(Window{From: m.status.From, Through: m.status.Through})
	return accepted, nil
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

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneStatus(m.status)
}

func (m *Manager) run(target Target, window Window) {
	defer func() {
		if recover() != nil {
			m.finishFailure(FailureInternal)
		}
	}()
	providers := []Target{target}
	if target == TargetAll {
		providers = []Target{TargetUGC, TargetKinepolis}
	}
	for _, provider := range providers {
		m.setProvider(provider, ProviderRunning)
		outcome, err := m.executor.Run(m.ctx, provider, window)
		if err != nil {
			code := FailureInternal
			var runError *RunError
			if errors.As(err, &runError) {
				code = runError.Code
			}
			m.setProviderFailure(provider, code)
			m.finishFailure(code)
			return
		}
		m.setProviderSuccess(provider, outcome)
	}
	m.mu.Lock()
	m.status.State = StateSucceeded
	finished := m.now().UTC()
	m.status.FinishedAt = &finished
	m.mu.Unlock()
}

func (m *Manager) setProvider(provider Target, state string) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: state}
	m.mu.Unlock()
}

func (m *Manager) setProviderSuccess(provider Target, outcome ProviderOutcome) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: ProviderSucceeded, Outcome: cloneOutcome(&outcome)}
	m.mu.Unlock()
}

func (m *Manager) setProviderFailure(provider Target, code FailureCode) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: ProviderFailed, ErrorCode: code}
	m.mu.Unlock()
}

func (m *Manager) finishFailure(code FailureCode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for provider, status := range m.status.Providers {
		switch status.State {
		case ProviderRunning:
			m.status.Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: code}
		case ProviderPending:
			m.status.Providers[provider] = ProviderStatus{State: ProviderSkipped}
		}
	}
	m.status.State = StateFailed
	finished := m.now().UTC()
	m.status.FinishedAt = &finished
}

func cloneStatus(status Status) Status {
	copy := status
	if status.FinishedAt != nil {
		finished := *status.FinishedAt
		copy.FinishedAt = &finished
	}
	if status.Providers != nil {
		copy.Providers = make(map[string]ProviderStatus, len(status.Providers))
		for provider, state := range status.Providers {
			state.Outcome = cloneOutcome(state.Outcome)
			copy.Providers[provider] = state
		}
	}
	return copy
}

func cloneOutcome(outcome *ProviderOutcome) *ProviderOutcome {
	if outcome == nil {
		return nil
	}
	copy := *outcome
	if outcome.Enrichment.Counts != nil {
		counts := *outcome.Enrichment.Counts
		copy.Enrichment.Counts = &counts
	}
	return &copy
}
