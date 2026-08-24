package syncschedule

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"messeances/api/internal/synccontrol"
)

type Starter interface {
	StartScheduled(synccontrol.Occurrence) (synccontrol.Status, <-chan synccontrol.Completion, error)
}

type scheduler interface {
	Schedule(cron.Schedule, cron.Job) cron.EntryID
	Remove(cron.EntryID)
	Entry(cron.EntryID) cron.Entry
	Start()
	Stop() context.Context
}

type serviceTicker interface {
	C() <-chan time.Time
	Stop()
}

type timerTicker struct{ ticker *time.Ticker }

func (t timerTicker) C() <-chan time.Time { return t.ticker.C }
func (t timerTicker) Stop()               { t.ticker.Stop() }

type registration struct {
	schedule Schedule
	entryID  cron.EntryID
	ctx      context.Context
	cancel   context.CancelFunc
}

type serviceDependencies struct {
	now       func() time.Time
	newTicker func(time.Duration) serviceTicker
	wait      func(context.Context, time.Duration) bool
	scheduler scheduler
}

type Service struct {
	store    Store
	starter  Starter
	location *time.Location
	deps     serviceDependencies

	opMu sync.Mutex
	mu   sync.Mutex
	ctx  context.Context
	stop context.CancelFunc

	shutdown       context.Context
	cancelShutdown context.CancelFunc
	closeDone      chan struct{}

	started bool
	closed  bool
	ticker  serviceTicker
	refresh sync.WaitGroup
	entries map[synccontrol.Target]*registration
}

func NewService(store Store, starter Starter) (*Service, error) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, errors.New("sync schedule timezone unavailable")
	}
	deps := serviceDependencies{
		now: time.Now,
		newTicker: func(interval time.Duration) serviceTicker {
			return timerTicker{ticker: time.NewTicker(interval)}
		},
		wait: waitForDelay,
		scheduler: cron.New(
			cron.WithLocation(location),
		),
	}
	return newService(store, starter, location, deps)
}

func newService(store Store, starter Starter, location *time.Location, deps serviceDependencies) (*Service, error) {
	if store == nil || starter == nil || location == nil || deps.now == nil || deps.newTicker == nil || deps.wait == nil || deps.scheduler == nil {
		return nil, errors.New("sync schedule dependencies are required")
	}
	shutdown, cancelShutdown := context.WithCancel(context.Background())
	return &Service{
		store: store, starter: starter, location: location, deps: deps,
		shutdown: shutdown, cancelShutdown: cancelShutdown, closeDone: make(chan struct{}),
		entries: make(map[synccontrol.Target]*registration),
	}, nil
}

// Start validates the complete PostgreSQL snapshot before enabling any firing.
func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sync schedule context is required")
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("sync schedule service closed")
	}
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	loadCtx, cancelLoad := contextWithShutdown(ctx, s.shutdown)
	rows, err := s.store.List(loadCtx)
	cancelLoad()
	if err != nil {
		return errors.New("sync schedule load failed")
	}
	prepared, err := s.prepareRows(rows)
	if err != nil {
		return errors.New("sync schedule configuration invalid")
	}

	serviceCtx, stop := context.WithCancel(ctx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stop()
		return errors.New("sync schedule service closed")
	}
	s.ctx = serviceCtx
	s.stop = stop
	s.started = true
	s.applyPreparedLocked(prepared, true)
	s.ticker = s.deps.newTicker(RefreshInterval)
	ticker := s.ticker
	s.deps.scheduler.Start()
	s.refresh.Add(1)
	s.mu.Unlock()

	go s.refreshLoop(serviceCtx, ticker)
	return nil
}

// Close stops refreshes, cancels pending retry chains, then waits for cron jobs.
func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return
	}
	s.closed = true
	s.cancelShutdown()
	started := s.started
	if started {
		s.stop()
		s.ticker.Stop()
		for _, entry := range s.entries {
			if entry.cancel != nil {
				entry.cancel()
			}
		}
	}
	s.mu.Unlock()

	// Persistence must observe shutdown cancellation before Close waits for the
	// operation lock held by Start, Save, or Refresh.
	s.opMu.Lock()
	s.opMu.Unlock()

	if started {
		s.refresh.Wait()
		<-s.deps.scheduler.Stop().Done()
	}
	close(s.closeDone)
}

// List reads and validates PostgreSQL on every call.
func (s *Service) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, errors.New("sync schedule list failed")
	}
	prepared, err := s.prepareRows(rows)
	if err != nil {
		return nil, errors.New("sync schedule configuration invalid")
	}
	result := make([]Schedule, len(prepared))
	for i := range prepared {
		result[i] = cloneSchedule(prepared[i].schedule)
	}
	return result, nil
}

// Save commits first, then installs the returned database revision locally.
func (s *Service) Save(ctx context.Context, provider synccontrol.Target, enabled bool, definition Definition) (Schedule, error) {
	if !validProvider(provider) {
		return Schedule{}, ErrInvalidSchedule
	}
	parsed, err := parseDefinition(definition, s.location, s.deps.now())
	if err != nil {
		return Schedule{}, ErrInvalidSchedule
	}

	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Schedule{}, errors.New("sync schedule service closed")
	}
	shutdown := s.shutdown
	s.mu.Unlock()
	saveCtx, cancelSave := contextWithShutdown(ctx, shutdown)
	committed, err := s.store.Upsert(saveCtx, Schedule{Provider: provider, Enabled: enabled, Definition: parsed.definition})
	cancelSave()
	if err != nil {
		return Schedule{}, errors.New("sync schedule save failed")
	}
	prepared, err := s.prepareSchedule(committed)
	if err != nil {
		return Schedule{}, errors.New("sync schedule committed configuration invalid")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Schedule{}, errors.New("sync schedule service closed")
	}
	if s.started {
		s.applyPreparedLocked([]preparedSchedule{prepared}, false)
	}
	s.mu.Unlock()
	return cloneSchedule(prepared.schedule), nil
}

func (s *Service) NextRuns(definition Definition) ([]time.Time, error) {
	now := s.deps.now()
	parsed, err := parseDefinition(definition, s.location, now)
	if err != nil {
		return nil, ErrInvalidSchedule
	}
	return nextOccurrences(parsed.schedule, now, s.location)
}

// Refresh converges this process on one complete PostgreSQL snapshot. Failed or
// malformed snapshots leave the current registry untouched.
func (s *Service) Refresh(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	refreshCtx, cancelRefresh := contextWithShutdown(ctx, s.shutdown)
	rows, err := s.store.List(refreshCtx)
	cancelRefresh()
	if err != nil {
		return errors.New("sync schedule refresh failed")
	}
	prepared, err := s.prepareRows(rows)
	if err != nil {
		return errors.New("sync schedule configuration invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.closed {
		return nil
	}
	s.applyPreparedLocked(prepared, true)
	return nil
}

type preparedSchedule struct {
	schedule Schedule
	parsed   cron.Schedule
}

func (s *Service) prepareRows(rows []Schedule) ([]preparedSchedule, error) {
	prepared := make([]preparedSchedule, 0, len(rows))
	seen := make(map[synccontrol.Target]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.Provider]; ok {
			return nil, ErrInvalidSchedule
		}
		seen[row.Provider] = struct{}{}
		item, err := s.prepareSchedule(row)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func (s *Service) prepareSchedule(schedule Schedule) (preparedSchedule, error) {
	if !validProvider(schedule.Provider) || schedule.Revision <= 0 || schedule.UpdatedAt.IsZero() {
		return preparedSchedule{}, ErrInvalidSchedule
	}
	parsed, err := parseDefinition(schedule.Definition, s.location, s.deps.now())
	if err != nil {
		return preparedSchedule{}, err
	}
	schedule.Definition = parsed.definition
	return preparedSchedule{schedule: cloneSchedule(schedule), parsed: parsed.schedule}, nil
}

func (s *Service) applyPreparedLocked(prepared []preparedSchedule, complete bool) {
	incoming := make(map[synccontrol.Target]preparedSchedule, len(prepared))
	for _, item := range prepared {
		incoming[item.schedule.Provider] = item
	}
	if complete {
		for provider, current := range s.entries {
			if _, ok := incoming[provider]; ok {
				continue
			}
			s.removeLocked(current)
			delete(s.entries, provider)
		}
	}
	for provider, item := range incoming {
		current := s.entries[provider]
		if current != nil && current.schedule.Revision >= item.schedule.Revision {
			continue
		}
		if current != nil {
			s.removeLocked(current)
		}
		entry := &registration{schedule: cloneSchedule(item.schedule)}
		s.entries[provider] = entry
		if !item.schedule.Enabled {
			continue
		}
		entry.ctx, entry.cancel = context.WithCancel(s.ctx)
		job := cron.FuncJob(func() { s.runRegistration(entry) })
		entry.entryID = s.deps.scheduler.Schedule(item.parsed, cron.SkipIfStillRunning(cron.DiscardLogger)(job))
	}
}

func (s *Service) removeLocked(entry *registration) {
	if entry.cancel != nil {
		entry.cancel()
	}
	if entry.entryID != 0 {
		s.deps.scheduler.Remove(entry.entryID)
	}
}

func (s *Service) refreshLoop(ctx context.Context, ticker serviceTicker) {
	defer s.refresh.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			_ = s.Refresh(ctx)
		}
	}
}

func (s *Service) runRegistration(entry *registration) {
	s.mu.Lock()
	entryID := entry.entryID
	s.mu.Unlock()
	if entry.ctx.Err() != nil {
		return
	}
	previous := s.deps.scheduler.Entry(entryID).Prev
	if previous.IsZero() {
		return
	}
	scheduledFor := previous.UTC().Truncate(time.Minute)
	if scheduledFor.IsZero() {
		return
	}
	s.runChain(entry.ctx, entry.schedule.Provider, entry.schedule.Revision, scheduledFor)
}

func (s *Service) runChain(ctx context.Context, provider synccontrol.Target, revision int64, scheduledFor time.Time) {
	fresh, err := s.fresh(ctx, provider, revision)
	if err != nil || !fresh {
		return
	}
	completion, err := s.start(provider, revision, scheduledFor, 0)
	if errors.Is(err, synccontrol.ErrInProgress) || errors.Is(err, synccontrol.ErrOccurrenceClaimed) || err != nil {
		return
	}
	if completedSuccessfully(completion) {
		return
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if !s.deps.wait(ctx, RetryDelay) {
			return
		}
		fresh, err := s.fresh(ctx, provider, revision)
		if err != nil {
			continue
		}
		if !fresh {
			return
		}
		completion, err := s.start(provider, revision, scheduledFor, attempt)
		if errors.Is(err, synccontrol.ErrOccurrenceClaimed) {
			return
		}
		if err != nil {
			continue
		}
		if completedSuccessfully(completion) {
			return
		}
	}
}

func (s *Service) start(provider synccontrol.Target, revision int64, scheduledFor time.Time, attempt int) (<-chan synccontrol.Completion, error) {
	_, completion, err := s.starter.StartScheduled(synccontrol.Occurrence{
		Provider: provider, Revision: revision, ScheduledFor: scheduledFor, Attempt: attempt,
	})
	return completion, err
}

func (s *Service) fresh(ctx context.Context, provider synccontrol.Target, revision int64) (bool, error) {
	schedule, err := s.store.Get(ctx, provider)
	if errors.Is(err, ErrScheduleMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := s.prepareSchedule(schedule); err != nil {
		return false, err
	}
	return schedule.Enabled && schedule.Revision == revision, nil
}

func completedSuccessfully(completion <-chan synccontrol.Completion) bool {
	if completion == nil {
		return false
	}
	result, ok := <-completion
	return ok && result.Status.State == synccontrol.StateSucceeded && result.FinalizationError == nil
}

func waitForDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func contextWithShutdown(parent context.Context, shutdown context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stopShutdown := context.AfterFunc(shutdown, cancel)
	if shutdown.Err() != nil {
		cancel()
	}
	return ctx, func() {
		stopShutdown()
		cancel()
	}
}
