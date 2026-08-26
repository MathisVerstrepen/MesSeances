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
type TriggerSource string

const (
	TargetAll       Target = "all"
	TargetUGC       Target = "ugc"
	TargetKinepolis Target = "kinepolis"
	TargetPathe     Target = "pathe"
	TargetCGR       Target = "cgr"

	StateRunning   JobState = "running"
	StateSucceeded JobState = "succeeded"
	StateFailed    JobState = "failed"

	ProviderNotRequested ProviderState = "not_requested"
	ProviderPending      ProviderState = "pending"
	ProviderRunning      ProviderState = "running"
	ProviderSucceeded    ProviderState = "succeeded"
	ProviderFailed       ProviderState = "failed"
	ProviderSkipped      ProviderState = "skipped"

	TriggerManual    TriggerSource = "manual"
	TriggerScheduled TriggerSource = "scheduled"
)

var (
	ErrInvalidTarget     = errors.New("invalid sync target")
	ErrInvalidOccurrence = errors.New("invalid scheduled occurrence")
	ErrInProgress        = errors.New("sync already in progress")
	ErrOccurrenceClaimed = errors.New("scheduled occurrence already claimed")
	ErrClosed            = errors.New("sync manager closed")
)

type Window struct {
	From string
}

type Executor interface {
	Run(context.Context, Target, Window) (map[Target]ProviderOutcome, error)
}

type ProviderStatus struct {
	State     ProviderState    `json:"state"`
	Outcome   *ProviderOutcome `json:"outcome,omitempty"`
	ErrorCode FailureCode      `json:"error_code,omitempty"`
	Log       []string         `json:"log,omitempty"`
}

type Occurrence struct {
	Provider     Target    `json:"-"`
	Revision     int64     `json:"schedule_revision"`
	ScheduledFor time.Time `json:"scheduled_for"`
	Attempt      int       `json:"attempt"`
}

type Completion struct {
	Status            Status
	FinalizationError error
}

type Status struct {
	ID         string                    `json:"id"`
	Target     Target                    `json:"target"`
	State      JobState                  `json:"state"`
	Trigger    TriggerSource             `json:"trigger"`
	Occurrence *Occurrence               `json:"occurrence,omitempty"`
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
	busy     bool
	now      func() time.Time
	executor Executor
	store    RunStore
	locker   RunLocker
	location *time.Location
	status   Status
}

// NewManager accepts one optional locker to keep construction compatible while
// callers move to the PostgreSQL-backed global lease.
func NewManager(ctx context.Context, now func() time.Time, executor Executor, store RunStore, lockers ...RunLocker) (*Manager, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil, err
	}
	if ctx == nil || executor == nil || store == nil || len(lockers) > 1 {
		return nil, errors.New("sync manager dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	locker := RunLocker(localRunLocker{})
	if postgresStore, ok := store.(*PostgresRunStore); ok {
		locker = NewPostgresRunLocker(postgresStore.pool)
	}
	if len(lockers) == 1 {
		if lockers[0] == nil {
			return nil, errors.New("sync manager dependencies are required")
		}
		locker = lockers[0]
	}
	if err := reconcileManagerStartup(ctx, now().UTC(), store, locker); err != nil {
		return nil, err
	}
	executorCtx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: executorCtx, cancel: cancel, now: now, executor: executor, store: store, locker: locker, location: location}, nil
}

func reconcileManagerStartup(ctx context.Context, finishedAt time.Time, store RunStore, locker RunLocker) error {
	lease, err := locker.Acquire(ctx)
	if errors.Is(err, ErrInProgress) {
		return nil
	}
	if err != nil {
		return errors.New("sync run reconciliation failed")
	}
	reconcileErr := store.ReconcileRunning(ctx, finishedAt)
	releaseErr := releaseRunLease(ctx, lease)
	if reconcileErr != nil || releaseErr != nil {
		return errors.New("sync run reconciliation failed")
	}
	return nil
}

func releaseRunLease(ctx context.Context, lease RunLease) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return lease.Release(releaseCtx)
}

func ValidTarget(target Target) bool {
	return target == TargetAll || target == TargetUGC || target == TargetKinepolis || target == TargetPathe || target == TargetCGR
}

func (m *Manager) Start(target Target) (Status, error) {
	status, _, err := m.start(target, nil)
	return status, err
}

func (m *Manager) StartScheduled(occurrence Occurrence) (Status, <-chan Completion, error) {
	if occurrence.Provider != TargetUGC && occurrence.Provider != TargetKinepolis && occurrence.Provider != TargetPathe && occurrence.Provider != TargetCGR {
		return Status{}, nil, ErrInvalidOccurrence
	}
	if occurrence.Revision <= 0 || occurrence.ScheduledFor.IsZero() || occurrence.Attempt < 0 || occurrence.Attempt > 2 {
		return Status{}, nil, ErrInvalidOccurrence
	}
	occurrence.ScheduledFor = occurrence.ScheduledFor.UTC().Truncate(time.Minute)
	if occurrence.ScheduledFor.IsZero() {
		return Status{}, nil, ErrInvalidOccurrence
	}
	return m.start(occurrence.Provider, &occurrence)
}

func (m *Manager) start(target Target, occurrence *Occurrence) (Status, <-chan Completion, error) {
	if !ValidTarget(target) {
		return Status{}, nil, ErrInvalidTarget
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return Status{}, nil, ErrClosed
	}
	if m.busy {
		m.mu.Unlock()
		return Status{}, nil, ErrInProgress
	}
	m.busy = true
	m.wg.Add(1)
	m.mu.Unlock()
	startTracked := true
	defer func() {
		if startTracked {
			m.wg.Done()
		}
	}()

	lease, err := m.locker.Acquire(m.ctx)
	if err != nil {
		m.clearBusy()
		if errors.Is(err, ErrInProgress) {
			return Status{}, nil, ErrInProgress
		}
		return Status{}, nil, errors.New("sync run lease acquisition failed")
	}
	failed := true
	defer func() {
		if failed {
			m.releaseAfterRejectedStart(lease)
		}
	}()

	if err := m.store.ReconcileRunning(m.ctx, m.now().UTC()); err != nil {
		return Status{}, nil, errors.New("sync run reconciliation failed")
	}
	if m.ctx.Err() != nil {
		return Status{}, nil, ErrClosed
	}

	now := m.now()
	today := now.In(m.location)
	providers := map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderNotRequested},
		string(TargetKinepolis): {State: ProviderNotRequested},
		string(TargetPathe):     {State: ProviderNotRequested},
		string(TargetCGR):       {State: ProviderNotRequested},
	}
	if target == TargetAll || target == TargetUGC {
		providers[string(TargetUGC)] = ProviderStatus{State: ProviderPending}
	}
	if target == TargetAll || target == TargetKinepolis {
		providers[string(TargetKinepolis)] = ProviderStatus{State: ProviderPending}
	}
	if target == TargetAll || target == TargetPathe {
		providers[string(TargetPathe)] = ProviderStatus{State: ProviderPending}
	}
	if target == TargetAll || target == TargetCGR {
		providers[string(TargetCGR)] = ProviderStatus{State: ProviderPending}
	}
	status := Status{
		Target: target, State: StateRunning, Trigger: TriggerManual,
		StartedAt: now.UTC(), From: today.Format("2006-01-02"),
		Through: today.Format("2006-01-02"), Providers: providers,
	}
	if occurrence != nil {
		copy := *occurrence
		status.Trigger = TriggerScheduled
		status.Occurrence = &copy
	}
	created, err := m.store.Create(m.ctx, status)
	if err != nil {
		if errors.Is(err, ErrOccurrenceClaimed) {
			return Status{}, nil, ErrOccurrenceClaimed
		}
		return Status{}, nil, errors.New("sync run creation failed")
	}

	var completion chan Completion
	if occurrence != nil {
		completion = make(chan Completion, 1)
	}
	m.mu.Lock()
	m.status = created
	accepted := cloneStatus(created)
	m.mu.Unlock()
	failed = false
	startTracked = false
	go func(window Window) {
		defer m.wg.Done()
		m.run(target, window, lease, completion)
	}(Window{From: created.From})
	return accepted, completion, nil
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

func (m *Manager) Snapshot(ctx context.Context) (Snapshot, error) {
	snapshot, err := m.store.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, errors.New("sync run snapshot failed")
	}
	if snapshot.Runs == nil {
		snapshot.Runs = []Status{}
	}
	if snapshot.Job != nil {
		job := cloneStatus(*snapshot.Job)
		snapshot.Job = &job
	}
	snapshot.Runs = cloneStatuses(snapshot.Runs)
	return snapshot, nil
}

// Runs is retained for the existing controller seam until it switches to Snapshot.
// Read failures return no history, never replica-local cached data.
func (m *Manager) Runs() []Status {
	snapshot, err := m.Snapshot(m.ctx)
	if err != nil {
		return []Status{}
	}
	return snapshot.Runs
}

func (m *Manager) run(target Target, window Window, lease RunLease, completion chan Completion) {
	terminal := m.execute(target, window)
	m.finalize(terminal, lease, completion)
}

func (m *Manager) execute(target Target, window Window) (terminal Status) {
	defer func() {
		if recover() != nil {
			terminal = m.markFailure(FailureInternal, StageOrchestration)
		}
	}()
	providers := []Target{target}
	if target == TargetAll {
		providers = []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR}
	}
	for _, provider := range providers {
		m.setProvider(provider, ProviderRunning)
	}
	outcomes, err := m.executor.Run(m.ctx, target, window)
	if err != nil {
		code, stage, failedProvider := FailureInternal, StageOrchestration, Target("")
		var logs map[Target][]string
		var runError *RunError
		if errors.As(err, &runError) {
			code, stage, failedProvider, logs = runError.Code, runError.Stage, runError.Provider, runError.logs
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || m.ctx.Err() != nil {
			code = FailureCanceled
		}
		return m.markOperationFailure(code, stage, failedProvider, logs)
	}
	latestThrough := window.From
	for _, provider := range providers {
		outcome, ok := outcomes[provider]
		if !ok || !validDiscoveredThrough(window.From, outcome.Sync.Through) {
			return m.markOperationFailure(FailureInternal, StageOrchestration, "", nil)
		}
		if outcome.Sync.Through > latestThrough {
			latestThrough = outcome.Sync.Through
		}
	}
	m.mu.Lock()
	for _, provider := range providers {
		outcome := outcomes[provider]
		m.status.Providers[string(provider)] = ProviderStatus{State: ProviderSucceeded, Outcome: cloneOutcome(&outcome)}
	}
	m.status.Through = latestThrough
	m.status.State = StateSucceeded
	finished := m.now().UTC()
	m.status.FinishedAt = &finished
	terminal = cloneStatus(m.status)
	m.mu.Unlock()
	return terminal
}

func (m *Manager) markOperationFailure(code FailureCode, stage FailureStage, failedProvider Target, logs map[Target][]string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	finished := m.now().UTC()
	for provider, status := range m.status.Providers {
		if status.State != ProviderRunning && status.State != ProviderPending {
			continue
		}
		if failedProvider == "" || provider == string(failedProvider) || code == FailureReplacement {
			target := Target(provider)
			lines := append([]string(nil), logs[target]...)
			if len(lines) == 0 {
				lines = []string{failureLog(finished, target, stage, fallbackFailure(stage, code))}
			}
			finishedAt := finished
			lines = normalizeProviderLog(target, lines, m.status.StartedAt, &finishedAt)
			m.status.Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: code, Log: lines}
		} else {
			m.status.Providers[provider] = ProviderStatus{State: ProviderSkipped}
		}
	}
	m.status.State = StateFailed
	m.status.FinishedAt = &finished
	return cloneStatus(m.status)
}

func (m *Manager) setProvider(provider Target, state ProviderState) {
	m.mu.Lock()
	m.status.Providers[string(provider)] = ProviderStatus{State: state}
	status := cloneStatus(m.status)
	m.mu.Unlock()
	m.persistIntermediate(status)
}

func validDiscoveredThrough(from, through string) bool {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return false
	}
	fromDate, fromErr := time.ParseInLocation("2006-01-02", from, location)
	throughDate, throughErr := time.ParseInLocation("2006-01-02", through, location)
	return fromErr == nil && throughErr == nil && throughDate.Format("2006-01-02") == through && schedule.ValidInclusiveDateWindow(fromDate, throughDate)
}

func (m *Manager) markFailure(code FailureCode, stage FailureStage) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	finished := m.now().UTC()
	for provider, status := range m.status.Providers {
		switch status.State {
		case ProviderRunning:
			target := Target(provider)
			line := failureLog(finished, target, stage, fallbackFailure(stage, code))
			m.status.Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: code, Log: []string{line}}
		case ProviderPending:
			m.status.Providers[provider] = ProviderStatus{State: ProviderSkipped}
		}
	}
	m.status.State = StateFailed
	m.status.FinishedAt = &finished
	return cloneStatus(m.status)
}

func (m *Manager) persistIntermediate(status Status) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	defer cancel()
	_ = m.store.Update(ctx, status)
}

func (m *Manager) finalize(status Status, lease RunLease, completion chan Completion) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	updateErr := m.store.Update(ctx, status)
	cancel()

	releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	_ = lease.Release(releaseCtx)
	releaseCancel()

	m.clearBusy()
	if completion != nil {
		result := Completion{Status: cloneStatus(status)}
		if updateErr != nil {
			result.FinalizationError = errors.New("sync run finalization failed")
		}
		completion <- result
		close(completion)
	}
}

func (m *Manager) releaseAfterRejectedStart(lease RunLease) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), 5*time.Second)
	defer cancel()
	_ = lease.Release(ctx)
	m.clearBusy()
}

func (m *Manager) clearBusy() {
	m.mu.Lock()
	m.busy = false
	m.mu.Unlock()
}

func cloneStatus(status Status) Status {
	copy := sanitizeStatusLogs(status)
	if status.FinishedAt != nil {
		finished := *status.FinishedAt
		copy.FinishedAt = &finished
	}
	if status.Occurrence != nil {
		occurrence := *status.Occurrence
		copy.Occurrence = &occurrence
	}
	if copy.Providers != nil {
		for provider, state := range copy.Providers {
			state.Outcome = cloneOutcome(state.Outcome)
			state.Log = append([]string(nil), state.Log...)
			copy.Providers[provider] = state
		}
		for _, provider := range []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR} {
			if _, exists := copy.Providers[string(provider)]; !exists {
				copy.Providers[string(provider)] = ProviderStatus{State: ProviderNotRequested}
			}
		}
	}
	return copy
}

func fallbackFailure(stage FailureStage, code FailureCode) logFailure {
	switch {
	case code == FailureCanceled:
		return logFailure{Operation: operationUnknown, Category: categoryCanceled}
	case stage == StageClientCreation:
		return logFailure{Operation: operationClient, Category: categoryInternal}
	case stage == StageDatasetValidation:
		return logFailure{Operation: operationDatasetValidation, Category: categoryValidation}
	case stage == StagePublication:
		return logFailure{Operation: operationPublication, Category: categoryPublication}
	case stage == StageProviderFetch:
		return logFailure{Operation: operationUnknown, Category: categoryUnknown}
	default:
		return logFailure{Operation: operationOrchestration, Category: categoryInternal}
	}
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
