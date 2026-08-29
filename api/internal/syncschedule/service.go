package syncschedule

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Starter interface {
	StartScheduled(Occurrence) (<-chan Completion, error)
	AvailableTargets() []Target
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

type serviceTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type timerTicker struct{ ticker *time.Ticker }

func (t timerTicker) C() <-chan time.Time { return t.ticker.C }
func (t timerTicker) Stop()               { t.ticker.Stop() }

type oneShotTimer struct{ timer *time.Timer }

func (t oneShotTimer) C() <-chan time.Time { return t.timer.C }
func (t oneShotTimer) Stop() bool          { return t.timer.Stop() }

type registration struct {
	schedule Schedule
	entryID  cron.EntryID
	ctx      context.Context
	cancel   context.CancelFunc
}

type serviceDependencies struct {
	now       func() time.Time
	newTicker func(time.Duration) serviceTicker
	newTimer  func(time.Time) serviceTimer
	scheduler scheduler
}

type occurrenceKey struct {
	scheduleID   int64
	target       Target
	revision     int64
	scheduledFor time.Time
}

type queueItem struct {
	key      occurrenceKey
	attempt  int
	readyAt  time.Time
	sequence uint64
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

	// mu protects service lifecycle, registrations, queue, active, and nextSeq.
	started   bool
	closed    bool
	ticker    serviceTicker
	refresh   sync.WaitGroup
	dispatch  sync.WaitGroup
	entries   map[int64]*registration
	available map[Target]struct{}
	queue     []queueItem
	active    map[occurrenceKey]struct{}
	wake      chan struct{}
	nextSeq   uint64
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
		newTimer: func(readyAt time.Time) serviceTimer {
			return oneShotTimer{timer: time.NewTimer(time.Until(readyAt))}
		},
		scheduler: cron.New(
			cron.WithLocation(location),
		),
	}
	return newService(store, starter, location, deps)
}

func newService(store Store, starter Starter, location *time.Location, deps serviceDependencies) (*Service, error) {
	if store == nil || starter == nil || location == nil || deps.now == nil || deps.newTicker == nil || deps.newTimer == nil || deps.scheduler == nil {
		return nil, errors.New("sync schedule dependencies are required")
	}
	shutdown, cancelShutdown := context.WithCancel(context.Background())
	return &Service{
		store: store, starter: starter, location: location, deps: deps,
		shutdown: shutdown, cancelShutdown: cancelShutdown, closeDone: make(chan struct{}),
		entries: make(map[int64]*registration), available: availableTargetSet(starter.AvailableTargets()),
		active: make(map[occurrenceKey]struct{}), wake: make(chan struct{}, 1),
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
	s.dispatch.Add(1)
	s.mu.Unlock()

	go s.refreshLoop(serviceCtx, ticker)
	go s.dispatchLoop(serviceCtx)
	return nil
}

// Close stops callbacks and queue dispatch without draining accepted backlog.
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
		s.queue = nil
		clear(s.active)
		s.signalDispatcherLocked()
	}
	s.mu.Unlock()

	// Persistence must observe shutdown cancellation before Close waits for the
	// operation lock held by Start, Save, or Refresh.
	s.opMu.Lock()
	s.opMu.Unlock()

	if started {
		s.refresh.Wait()
		<-s.deps.scheduler.Stop().Done()
		s.dispatch.Wait()
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

func (s *Service) AvailableTargets() []Target {
	targets := make([]Target, 0, len(s.available))
	for _, target := range []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR, TargetMetadataRefresh} {
		if _, ok := s.available[target]; ok {
			targets = append(targets, target)
		}
	}
	return targets
}

// Create commits first, then installs the returned database revision locally.
func (s *Service) Create(ctx context.Context, target Target, enabled bool, definition Definition) (Schedule, error) {
	if !ValidTarget(target) {
		return Schedule{}, ErrInvalidSchedule
	}
	if enabled && !s.targetAvailable(target) {
		return Schedule{}, ErrTargetUnavailable
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
	committed, err := s.store.Create(saveCtx, Schedule{Target: target, Enabled: enabled, Definition: parsed.definition})
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

// Update keeps target and ID immutable and increments only the selected row.
func (s *Service) Update(ctx context.Context, target Target, id int64, enabled bool, definition Definition) (Schedule, error) {
	if !ValidTarget(target) || id <= 0 {
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
	checkCtx, cancelCheck := contextWithShutdown(ctx, shutdown)
	_, err = s.store.Get(checkCtx, target, id)
	cancelCheck()
	if errors.Is(err, ErrScheduleMissing) {
		return Schedule{}, ErrScheduleMissing
	}
	if err != nil {
		return Schedule{}, errors.New("sync schedule update failed")
	}
	if enabled && !s.targetAvailable(target) {
		return Schedule{}, ErrTargetUnavailable
	}
	updateCtx, cancelUpdate := contextWithShutdown(ctx, shutdown)
	committed, err := s.store.Update(updateCtx, Schedule{ID: id, Target: target, Enabled: enabled, Definition: parsed.definition})
	cancelUpdate()
	if errors.Is(err, ErrScheduleMissing) {
		return Schedule{}, ErrScheduleMissing
	}
	if err != nil {
		return Schedule{}, errors.New("sync schedule update failed")
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

func (s *Service) Delete(ctx context.Context, target Target, id int64) error {
	if !ValidTarget(target) || id <= 0 {
		return ErrInvalidSchedule
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	deleteCtx, cancelDelete := contextWithShutdown(ctx, s.shutdown)
	err := s.store.Delete(deleteCtx, target, id)
	cancelDelete()
	if errors.Is(err, ErrScheduleMissing) {
		return ErrScheduleMissing
	}
	if err != nil {
		return errors.New("sync schedule delete failed")
	}
	s.mu.Lock()
	if current := s.entries[id]; current != nil {
		s.removeLocked(current)
		delete(s.entries, id)
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) ClaimOccurrence(ctx context.Context, occurrence Occurrence) (bool, error) {
	if occurrence.ScheduleID <= 0 || !ValidTarget(occurrence.Target) || occurrence.Revision <= 0 || occurrence.ScheduledFor.IsZero() || occurrence.Attempt != 0 {
		return false, ErrInvalidSchedule
	}
	return s.store.ClaimOccurrence(ctx, occurrence)
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
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ID]; ok {
			return nil, ErrInvalidSchedule
		}
		seen[row.ID] = struct{}{}
		item, err := s.prepareSchedule(row)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func (s *Service) prepareSchedule(schedule Schedule) (preparedSchedule, error) {
	if schedule.ID <= 0 || !ValidTarget(schedule.Target) || schedule.Revision <= 0 || schedule.UpdatedAt.IsZero() {
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
	incoming := make(map[int64]preparedSchedule, len(prepared))
	for _, item := range prepared {
		incoming[item.schedule.ID] = item
	}
	if complete {
		for id, current := range s.entries {
			if _, ok := incoming[id]; ok {
				continue
			}
			s.removeLocked(current)
			delete(s.entries, id)
		}
	}
	for id, item := range incoming {
		current := s.entries[id]
		if current != nil && current.schedule.Revision >= item.schedule.Revision {
			continue
		}
		if current != nil {
			s.removeLocked(current)
		}
		entry := &registration{schedule: cloneSchedule(item.schedule)}
		s.entries[id] = entry
		if !item.schedule.Enabled || !s.targetAvailable(item.schedule.Target) {
			continue
		}
		entry.ctx, entry.cancel = context.WithCancel(s.ctx)
		job := cron.FuncJob(func() { s.runRegistration(entry) })
		entry.entryID = s.deps.scheduler.Schedule(item.parsed, job)
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
	ctx := entry.ctx
	s.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
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
	fresh, err := s.fresh(ctx, entry.schedule.Target, entry.schedule.ID, entry.schedule.Revision)
	if err != nil || !fresh {
		return
	}

	key := occurrenceKey{
		scheduleID: entry.schedule.ID, target: entry.schedule.Target, revision: entry.schedule.Revision, scheduledFor: scheduledFor,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.entries[key.scheduleID]
	if !s.started || s.closed || ctx.Err() != nil || current != entry || current.schedule.Revision != key.revision {
		return
	}
	if _, exists := s.active[key]; exists {
		return
	}
	s.active[key] = struct{}{}
	s.enqueueLocked(queueItem{key: key, readyAt: s.deps.now()})
}

func (s *Service) dispatchLoop(ctx context.Context) {
	defer s.dispatch.Done()
	for {
		item, readyAt, ok := s.nextReady()
		if ok {
			if !s.dispatchItem(ctx, item) {
				return
			}
			continue
		}
		if readyAt.IsZero() {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			}
			continue
		}
		timer := s.deps.newTimer(readyAt)
		if !readyAt.After(s.deps.now()) {
			stopAndDrainTimer(timer)
			continue
		}
		select {
		case <-ctx.Done():
			stopAndDrainTimer(timer)
			return
		case <-s.wake:
			stopAndDrainTimer(timer)
		case <-timer.C():
		}
	}
}

func (s *Service) nextReady() (queueItem, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx == nil || s.ctx.Err() != nil {
		return queueItem{}, time.Time{}, false
	}
	now := s.deps.now()
	readyIndex := -1
	var readySequence uint64
	var earliest time.Time
	for i, item := range s.queue {
		if item.readyAt.After(now) {
			if earliest.IsZero() || item.readyAt.Before(earliest) {
				earliest = item.readyAt
			}
			continue
		}
		if readyIndex == -1 || item.sequence < readySequence {
			readyIndex = i
			readySequence = item.sequence
		}
	}
	if readyIndex >= 0 {
		item := s.queue[readyIndex]
		s.queue = append(s.queue[:readyIndex], s.queue[readyIndex+1:]...)
		return item, time.Time{}, true
	}
	if earliest.IsZero() {
		return queueItem{}, time.Time{}, false
	}
	return queueItem{}, earliest, false
}

func stopAndDrainTimer(timer serviceTimer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C():
	default:
	}
}

func (s *Service) dispatchItem(ctx context.Context, item queueItem) bool {
	completion, err := s.starter.StartScheduled(Occurrence{
		ScheduleID: item.key.scheduleID, Target: item.key.target, Revision: item.key.revision,
		ScheduledFor: item.key.scheduledFor, Attempt: item.attempt,
	})
	if errors.Is(err, ErrInProgress) {
		s.requeue(item, item.attempt, s.deps.now().Add(time.Second))
		return true
	}
	if errors.Is(err, ErrOccurrenceClaimed) {
		s.finish(item.key)
		return true
	}
	if err != nil {
		s.retryOrFinish(item)
		return true
	}
	if completion == nil {
		s.retryOrFinish(item)
		return true
	}

	var result Completion
	var received bool
	select {
	case <-ctx.Done():
		return false
	case result, received = <-completion:
	}
	if received && result.Succeeded && result.FinalizationError == nil {
		s.finish(item.key)
		return true
	}
	s.retryOrFinish(item)
	return true
}

func (s *Service) retryOrFinish(item queueItem) {
	if item.attempt >= 2 {
		s.finish(item.key)
		return
	}
	s.requeue(item, item.attempt+1, s.deps.now().Add(RetryDelay))
}

func (s *Service) requeue(item queueItem, attempt int, readyAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	item.attempt = attempt
	item.readyAt = readyAt
	s.enqueueLocked(item)
}

func (s *Service) finish(key occurrenceKey) {
	s.mu.Lock()
	delete(s.active, key)
	s.mu.Unlock()
}

func (s *Service) enqueueLocked(item queueItem) {
	s.nextSeq++
	item.sequence = s.nextSeq
	s.queue = append(s.queue, item)
	s.signalDispatcherLocked()
}

func (s *Service) signalDispatcherLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) fresh(ctx context.Context, target Target, id, revision int64) (bool, error) {
	schedule, err := s.store.Get(ctx, target, id)
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

func (s *Service) targetAvailable(target Target) bool {
	_, ok := s.available[target]
	return ok
}

func availableTargetSet(targets []Target) map[Target]struct{} {
	available := make(map[Target]struct{}, len(targets))
	for _, target := range targets {
		if ValidTarget(target) {
			available[target] = struct{}{}
		}
	}
	return available
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
