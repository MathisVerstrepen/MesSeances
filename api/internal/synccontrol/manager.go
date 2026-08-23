package synccontrol

import (
	"context"
	"errors"
	"sync"
	"time"

	"messeances/api/internal/schedule"
)

type Target string
type JobState string
type ProviderState string

const (
	TargetAll       Target = "all"
	TargetUGC       Target = "ugc"
	TargetKinepolis Target = "kinepolis"

	StateRunning   JobState = "running"
	StateSucceeded JobState = "succeeded"
	StateFailed    JobState = "failed"

	ProviderNotRequested ProviderState = "not_requested"
	ProviderPending      ProviderState = "pending"
	ProviderRunning      ProviderState = "running"
	ProviderSucceeded    ProviderState = "succeeded"
	ProviderFailed       ProviderState = "failed"
	ProviderSkipped      ProviderState = "skipped"
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
	Run(context.Context, Target, Window) (map[Target]ProviderOutcome, error)
}

type ProviderStatus struct {
	State     ProviderState    `json:"state"`
	Outcome   *ProviderOutcome `json:"outcome,omitempty"`
	ErrorCode FailureCode      `json:"error_code,omitempty"`
}

type Status struct {
	ID         string                    `json:"id"`
	Target     Target                    `json:"target"`
	State      JobState                  `json:"state"`
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
	store    RunStore
	location *time.Location
	status   Status
	runs     []Status
}

func NewManager(ctx context.Context, now func() time.Time, executor Executor, store RunStore) (*Manager, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil, err
	}
	if ctx == nil || executor == nil || store == nil {
		return nil, errors.New("sync manager dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if err := store.ReconcileRunning(ctx, now().UTC()); err != nil {
		return nil, errors.New("sync run reconciliation failed")
	}
	runs, err := store.List(ctx, historyLimit)
	if err != nil {
		return nil, errors.New("sync run history load failed")
	}
	executorCtx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: executorCtx, cancel: cancel, now: now, executor: executor, store: store, location: location, runs: cloneStatuses(runs)}, nil
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
	status := Status{
		Target: target, State: StateRunning,
		StartedAt: now.UTC(), From: today.Format("2006-01-02"),
		Through: today.AddDate(0, 0, 7).Format("2006-01-02"), Providers: providers,
	}
	created, err := m.store.Create(m.ctx, status)
	if err != nil {
		return Status{}, errors.New("sync run creation failed")
	}
	m.status = created
	m.prependRunLocked(created)
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

func (m *Manager) Runs() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	runs := make([]Status, 0, len(m.runs))
	for _, run := range m.runs {
		if run.ID == m.status.ID {
			continue
		}
		runs = append(runs, cloneStatus(run))
	}
	return runs
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
	}
	outcomes, err := m.executor.Run(m.ctx, target, window)
	if err != nil {
		code, failedProvider := FailureInternal, Target("")
		var runError *RunError
		if errors.As(err, &runError) {
			code, failedProvider = runError.Code, runError.Provider
		}
		m.finishOperationFailure(code, failedProvider)
		return
	}
	for _, provider := range providers {
		outcome, ok := outcomes[provider]
		if !ok {
			m.finishOperationFailure(FailureInternal, "")
			return
		}
		m.setProviderSuccess(provider, outcome)
	}
	m.mu.Lock()
	m.status.State = StateSucceeded
	finished := m.now().UTC()
	m.status.FinishedAt = &finished
	status := cloneStatus(m.status)
	m.mu.Unlock()
	m.persist(status)
}

func (m *Manager) finishOperationFailure(code FailureCode, failedProvider Target) {
	m.mu.Lock()
	for provider, status := range m.status.Providers {
		if status.State != ProviderRunning && status.State != ProviderPending {
			continue
		}
		if failedProvider == "" || provider == string(failedProvider) || code == FailureReplacement {
			m.status.Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: code}
		} else {
			m.status.Providers[provider] = ProviderStatus{State: ProviderSkipped}
		}
	}
	m.status.State = StateFailed
	finished := m.now().UTC()
	m.status.FinishedAt = &finished
	status := cloneStatus(m.status)
	m.mu.Unlock()
	m.persist(status)
}

func (m *Manager) setProvider(provider Target, state ProviderState) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: state}
	m.mu.Unlock()
	m.persist(m.Status())
}

func (m *Manager) setProviderSuccess(provider Target, outcome ProviderOutcome) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: ProviderSucceeded, Outcome: cloneOutcome(&outcome)}
	m.mu.Unlock()
	m.persist(m.Status())
}

func (m *Manager) setProviderFailure(provider Target, code FailureCode) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: ProviderFailed, ErrorCode: code}
	m.mu.Unlock()
	m.persist(m.Status())
}

func (m *Manager) finishFailure(code FailureCode) {
	m.mu.Lock()
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
	status := cloneStatus(m.status)
	m.mu.Unlock()
	m.persist(status)
}

func (m *Manager) persist(status Status) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	defer cancel()
	if m.store.Update(ctx, status) != nil {
		return
	}
	m.mu.Lock()
	for i := range m.runs {
		if m.runs[i].ID == status.ID {
			m.runs[i] = cloneStatus(status)
			m.mu.Unlock()
			return
		}
	}
	m.prependRunLocked(status)
	m.mu.Unlock()
}

func (m *Manager) prependRunLocked(status Status) {
	m.runs = append([]Status{cloneStatus(status)}, m.runs...)
	if len(m.runs) > historyLimit {
		m.runs = m.runs[:historyLimit]
	}
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

func cloneStatuses(statuses []Status) []Status {
	clones := make([]Status, len(statuses))
	for i := range statuses {
		clones[i] = cloneStatus(statuses[i])
	}
	return clones
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
