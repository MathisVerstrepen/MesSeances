package syncschedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"messeances/api/internal/synccontrol"
)

type memoryStore struct {
	mu      sync.Mutex
	rows    map[synccontrol.Target]Schedule
	listErr error
	getErr  error
	listed  chan struct{}
}

type blockingUpsertStore struct {
	*memoryStore
	started  chan struct{}
	canceled chan struct{}
}

type blockingGetStore struct {
	*memoryStore
	started chan struct{}
	release chan struct{}
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: make(map[synccontrol.Target]Schedule), listed: make(chan struct{}, 16)}
}

func (s *memoryStore) List(context.Context) ([]Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.listed <- struct{}{}:
	default:
	}
	if s.listErr != nil {
		return nil, s.listErr
	}
	result := make([]Schedule, 0, len(s.rows))
	for _, provider := range []synccontrol.Target{synccontrol.TargetUGC, synccontrol.TargetKinepolis} {
		if row, ok := s.rows[provider]; ok {
			result = append(result, cloneSchedule(row))
		}
	}
	return result, nil
}

func (s *memoryStore) Get(_ context.Context, provider synccontrol.Target) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return Schedule{}, s.getErr
	}
	row, ok := s.rows[provider]
	if !ok {
		return Schedule{}, ErrScheduleMissing
	}
	return cloneSchedule(row), nil
}

func (s *memoryStore) Upsert(_ context.Context, row Schedule) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.rows[row.Provider]; exists {
		row.Revision = current.Revision + 1
	} else {
		row.Revision = 1
	}
	row.UpdatedAt = time.Date(2026, 8, 24, 12, int(row.Revision), 0, 0, time.UTC)
	s.rows[row.Provider] = cloneSchedule(row)
	return cloneSchedule(row), nil
}

func (s *blockingUpsertStore) Upsert(ctx context.Context, _ Schedule) (Schedule, error) {
	close(s.started)
	<-ctx.Done()
	close(s.canceled)
	return Schedule{}, ctx.Err()
}

func (s *blockingGetStore) Get(ctx context.Context, provider synccontrol.Target) (Schedule, error) {
	close(s.started)
	select {
	case <-ctx.Done():
		return Schedule{}, ctx.Err()
	case <-s.release:
		return s.memoryStore.Get(ctx, provider)
	}
}

func (s *memoryStore) set(row Schedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[row.Provider] = cloneSchedule(row)
}

func (s *memoryStore) remove(provider synccontrol.Target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, provider)
}

func (s *memoryStore) failGet(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr = err
}

func (s *memoryStore) failList(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listErr = err
}

type fakeScheduler struct {
	mu      sync.Mutex
	next    cron.EntryID
	entries map[cron.EntryID]*fakeCronEntry
	started bool
	changed chan struct{}
}

type fakeCronEntry struct {
	schedule cron.Schedule
	job      cron.Job
	prev     time.Time
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{entries: make(map[cron.EntryID]*fakeCronEntry), changed: make(chan struct{}, 16)}
}

func (s *fakeScheduler) Schedule(schedule cron.Schedule, job cron.Job) cron.EntryID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.entries[s.next] = &fakeCronEntry{schedule: schedule, job: job}
	s.changed <- struct{}{}
	return s.next
}

func (s *fakeScheduler) Remove(id cron.EntryID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
	s.changed <- struct{}{}
}

func (s *fakeScheduler) Entry(id cron.EntryID) cron.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[id]
	if entry == nil {
		return cron.Entry{}
	}
	return cron.Entry{ID: id, Schedule: entry.schedule, Prev: entry.prev, Job: entry.job}
}

func (s *fakeScheduler) Start() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

func (*fakeScheduler) Stop() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func (s *fakeScheduler) trigger(id cron.EntryID, previous time.Time) <-chan struct{} {
	s.mu.Lock()
	entry := s.entries[id]
	if entry == nil {
		s.mu.Unlock()
		done := make(chan struct{})
		close(done)
		return done
	}
	entry.prev = previous
	job := entry.job
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		job.Run()
	}()
	return done
}

func (s *fakeScheduler) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

type manualTicker struct{ ch chan time.Time }

func newManualTicker() *manualTicker        { return &manualTicker{ch: make(chan time.Time, 8)} }
func (t *manualTicker) C() <-chan time.Time { return t.ch }
func (*manualTicker) Stop()                 {}
func (t *manualTicker) tick()               { t.ch <- time.Time{} }

type manualTimer struct {
	clock   *manualClock
	due     time.Time
	ch      chan time.Time
	stopped bool
}

func (t *manualTimer) C() <-chan time.Time { return t.ch }
func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	active := !t.stopped
	t.stopped = true
	t.clock.mu.Unlock()
	return active
}

type manualClock struct {
	mu          sync.Mutex
	now         time.Time
	timers      []*manualTimer
	created     chan time.Time
	beforeTimer func(time.Time)
}

func newManualClock() *manualClock {
	return &manualClock{
		now:     time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
		created: make(chan time.Time, 64),
	}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) NewTimer(readyAt time.Time) serviceTimer {
	if c.beforeTimer != nil {
		c.beforeTimer(readyAt)
	}
	c.mu.Lock()
	timer := &manualTimer{clock: c, due: readyAt, ch: make(chan time.Time, 1)}
	c.timers = append(c.timers, timer)
	if !readyAt.After(c.now) {
		timer.stopped = true
		timer.ch <- c.now
	}
	c.mu.Unlock()
	c.created <- readyAt
	return timer
}

func (c *manualClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	now := c.now
	for _, timer := range c.timers {
		if !timer.stopped && !timer.due.After(now) {
			timer.stopped = true
			timer.ch <- now
		}
	}
	c.mu.Unlock()
}

type startResponse struct {
	err        error
	completion <-chan synccontrol.Completion
}

type fakeStarter struct {
	mu        sync.Mutex
	responses []startResponse
	calls     []synccontrol.Occurrence
	called    chan synccontrol.Occurrence
}

func newFakeStarter(responses ...startResponse) *fakeStarter {
	return &fakeStarter{responses: responses, called: make(chan synccontrol.Occurrence, 64)}
}

func (s *fakeStarter) StartScheduled(occurrence synccontrol.Occurrence) (synccontrol.Status, <-chan synccontrol.Completion, error) {
	s.mu.Lock()
	s.calls = append(s.calls, occurrence)
	response := startResponse{completion: completionChannel(synccontrol.StateSucceeded, nil)}
	if len(s.responses) != 0 {
		response = s.responses[0]
		s.responses = s.responses[1:]
	}
	s.mu.Unlock()
	s.called <- occurrence
	return synccontrol.Status{State: synccontrol.StateRunning}, response.completion, response.err
}

func (s *fakeStarter) occurrences() []synccontrol.Occurrence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]synccontrol.Occurrence(nil), s.calls...)
}

func completionChannel(state synccontrol.JobState, finalization error) <-chan synccontrol.Completion {
	result := make(chan synccontrol.Completion, 1)
	result <- synccontrol.Completion{Status: synccontrol.Status{State: state}, FinalizationError: finalization}
	close(result)
	return result
}

func configured(provider synccontrol.Target, revision int64, enabled bool, definition Definition) Schedule {
	return Schedule{
		Provider: provider, Revision: revision, Enabled: enabled, Definition: definition,
		UpdatedAt: time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
	}
}

func testService(t *testing.T, store Store, starter Starter, scheduler *fakeScheduler, ticker *manualTicker, clock *manualClock) *Service {
	t.Helper()
	service, err := newService(store, starter, mustParis(t), serviceDependencies{
		now: clock.Now,
		newTicker: func(time.Duration) serviceTicker {
			return ticker
		},
		newTimer:  clock.NewTimer,
		scheduler: scheduler,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func startTestService(t *testing.T, store Store, starter Starter) (*Service, *fakeScheduler, *manualTicker, *manualClock) {
	t.Helper()
	scheduler := newFakeScheduler()
	ticker := newManualTicker()
	clock := newManualClock()
	service := testService(t, store, starter, scheduler, ticker, clock)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, scheduler, ticker, clock
}

func entryID(t *testing.T, service *Service, provider synccontrol.Target) cron.EntryID {
	t.Helper()
	service.mu.Lock()
	defer service.mu.Unlock()
	entry := service.entries[provider]
	if entry == nil || entry.entryID == 0 {
		t.Fatalf("provider %s has no registration", provider)
	}
	return entry.entryID
}

func fire(t *testing.T, scheduler *fakeScheduler, id cron.EntryID, scheduledFor time.Time) {
	t.Helper()
	select {
	case <-scheduler.trigger(id, scheduledFor):
	case <-time.After(time.Second):
		t.Fatal("cron callback did not return")
	}
}

func nextCall(t *testing.T, starter *fakeStarter) synccontrol.Occurrence {
	t.Helper()
	select {
	case occurrence := <-starter.called:
		return occurrence
	case <-time.After(time.Second):
		t.Fatal("scheduled start not observed")
		return synccontrol.Occurrence{}
	}
}

func assertNoCall(t *testing.T, starter *fakeStarter) {
	t.Helper()
	select {
	case occurrence := <-starter.called:
		t.Fatalf("unexpected scheduled start: %+v", occurrence)
	default:
	}
}

func waitForTimer(t *testing.T, clock *manualClock, expected time.Duration) {
	t.Helper()
	readyAt := clock.Now().Add(expected)
	for {
		select {
		case createdAt := <-clock.created:
			if createdAt.Equal(readyAt) {
				return
			}
		case <-time.After(time.Second):
			t.Fatalf("timer %v not created", expected)
		}
	}
}

func TestDispatcherUsesAbsoluteReadyAtAcrossTimerCreationClockAdvance(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	starter := newFakeStarter(startResponse{completion: completionChannel(synccontrol.StateFailed, nil)})
	scheduler := newFakeScheduler()
	ticker := newManualTicker()
	clock := newManualClock()
	timerStarted := make(chan time.Time, 1)
	createTimer := make(chan struct{})
	var releaseTimer sync.Once
	clock.beforeTimer = func(readyAt time.Time) {
		timerStarted <- readyAt
		<-createTimer
	}
	service := testService(t, store, starter, scheduler, ticker, clock)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		releaseTimer.Do(func() { close(createTimer) })
		service.Close()
	})
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
	if call := nextCall(t, starter); call.Attempt != 0 {
		t.Fatalf("base call=%+v", call)
	}
	select {
	case readyAt := <-timerStarted:
		if want := clock.Now().Add(RetryDelay); !readyAt.Equal(want) {
			t.Fatalf("readyAt=%v want=%v", readyAt, want)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not reach timer creation barrier")
	}
	clock.Advance(RetryDelay)
	releaseTimer.Do(func() { close(createTimer) })
	if call := nextCall(t, starter); call.Attempt != 1 {
		t.Fatalf("retry call=%+v", call)
	}
}

func waitSchedulerCount(t *testing.T, scheduler *fakeScheduler, expected int) {
	t.Helper()
	for scheduler.count() != expected {
		select {
		case <-scheduler.changed:
		case <-time.After(time.Second):
			t.Fatalf("scheduler entries=%d want=%d", scheduler.count(), expected)
		}
	}
}

func waitStoreList(t *testing.T, store *memoryStore) {
	t.Helper()
	select {
	case <-store.listed:
	case <-time.After(time.Second):
		t.Fatal("store list not observed")
	}
}

func TestServiceRegistersCommittedRevisionAndUsesEntryPrev(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 4, true, Definition{Kind: KindDaily, Time: "02:30"}))
	starter := newFakeStarter()
	service, scheduler, _, _ := startTestService(t, store, starter)
	id := entryID(t, service, synccontrol.TargetUGC)

	first := time.Date(2026, 10, 25, 2, 30, 48, 0, time.FixedZone("CEST", 7200))
	fire(t, scheduler, id, first)
	call := nextCall(t, starter)
	if call.Revision != 4 || call.Attempt != 0 || !call.ScheduledFor.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("first call=%+v", call)
	}
	second := time.Date(2026, 10, 25, 2, 30, 0, 0, time.FixedZone("CET", 3600))
	fire(t, scheduler, id, second)
	call = nextCall(t, starter)
	if !call.ScheduledFor.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("second call=%+v", call)
	}
}

func TestServiceStartupSaveRefreshAndFreshnessGates(t *testing.T) {
	malformed := newMemoryStore()
	malformed.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindCron, Expression: "@daily"}))
	scheduler := newFakeScheduler()
	service := testService(t, malformed, newFakeStarter(), scheduler, newManualTicker(), newManualClock())
	if err := service.Start(context.Background()); err == nil || scheduler.started || scheduler.count() != 0 {
		t.Fatal("malformed snapshot started cron")
	}

	store := newMemoryStore()
	serviceA, _, _, _ := startTestService(t, store, newFakeStarter())
	starterB := newFakeStarter()
	serviceB, schedulerB, tickerB, _ := startTestService(t, store, starterB)
	for len(store.listed) != 0 {
		<-store.listed
	}
	first, err := serviceA.Save(context.Background(), synccontrol.TargetUGC, false, Definition{Kind: KindWeekly, Time: "09:15", Weekdays: []string{"fri", "mon", "fri"}})
	if err != nil || first.Revision != 1 || !sameStrings(first.Definition.Weekdays, []string{"mon", "fri"}) {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := serviceA.Save(context.Background(), synccontrol.TargetUGC, true, first.Definition)
	if err != nil || second.Revision != 2 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	tickerB.tick()
	waitStoreList(t, store)
	waitSchedulerCount(t, schedulerB, 1)
	id := entryID(t, serviceB, synccontrol.TargetUGC)
	store.failGet(errors.New("database unavailable"))
	fire(t, schedulerB, id, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	assertNoCall(t, starterB)
	store.failGet(nil)
	row, _ := store.Get(context.Background(), synccontrol.TargetUGC)
	row.Revision++
	store.set(row)
	fire(t, schedulerB, id, time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC))
	assertNoCall(t, starterB)
	store.failList(errors.New("database unavailable"))
	tickerB.tick()
	waitStoreList(t, store)
	if schedulerB.count() != 1 {
		t.Fatal("failed refresh changed registry")
	}
	store.failList(nil)
	store.remove(synccontrol.TargetUGC)
	tickerB.tick()
	waitSchedulerCount(t, schedulerB, 0)
}

func TestQueueRetainsSameAndCrossProviderOverlapsInFIFOOrder(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	store.set(configured(synccontrol.TargetKinepolis, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	blocked := make(chan synccontrol.Completion, 1)
	starter := newFakeStarter(startResponse{completion: blocked})
	service, scheduler, _, _ := startTestService(t, store, starter)
	ugcID := entryID(t, service, synccontrol.TargetUGC)
	kinepolisID := entryID(t, service, synccontrol.TargetKinepolis)

	firstAt := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(24 * time.Hour)
	fire(t, scheduler, ugcID, firstAt)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetUGC || !call.ScheduledFor.Equal(firstAt) {
		t.Fatalf("first call=%+v", call)
	}
	fire(t, scheduler, ugcID, secondAt)
	fire(t, scheduler, ugcID, secondAt.Add(30*time.Second))
	fire(t, scheduler, kinepolisID, firstAt)
	assertNoCall(t, starter)
	blocked <- synccontrol.Completion{Status: synccontrol.Status{State: synccontrol.StateSucceeded}}
	close(blocked)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetUGC || !call.ScheduledFor.Equal(secondAt) {
		t.Fatalf("second call=%+v", call)
	}
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetKinepolis || !call.ScheduledFor.Equal(firstAt) {
		t.Fatalf("third call=%+v", call)
	}
}

func TestQueueDeduplicatesExactKeyButAcceptsDistinctMinuteAndRevision(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	blocked := make(chan synccontrol.Completion, 1)
	starter := newFakeStarter(startResponse{completion: blocked})
	service, scheduler, _, _ := startTestService(t, store, starter)
	id := entryID(t, service, synccontrol.TargetUGC)
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, id, base)
	nextCall(t, starter)
	fire(t, scheduler, id, base.Add(30*time.Second))
	fire(t, scheduler, id, base.Add(time.Minute))

	updated, err := service.Save(context.Background(), synccontrol.TargetUGC, true, Definition{Kind: KindDaily, Time: "08:00"})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
	blocked <- synccontrol.Completion{Status: synccontrol.Status{State: synccontrol.StateSucceeded}}
	close(blocked)
	second := nextCall(t, starter)
	third := nextCall(t, starter)
	if second.Revision != 1 || !second.ScheduledFor.Equal(base.Add(time.Minute)) || third.Revision != 2 || !third.ScheduledFor.Equal(base) {
		t.Fatalf("calls=%+v", starter.occurrences())
	}
}

func TestDelayedRetryDoesNotBlockReadyBaseAndWakePreemptsTimer(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	store.set(configured(synccontrol.TargetKinepolis, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	starter := newFakeStarter(startResponse{completion: completionChannel(synccontrol.StateFailed, nil)})
	service, scheduler, _, clock := startTestService(t, store, starter)
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
	nextCall(t, starter)
	waitForTimer(t, clock, RetryDelay)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetKinepolis), base)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetKinepolis || call.Attempt != 0 {
		t.Fatalf("ready base call=%+v", call)
	}
	clock.Advance(RetryDelay)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetUGC || call.Attempt != 1 {
		t.Fatalf("retry call=%+v", call)
	}
}

func TestErrInProgressRequeuesSameAttemptBehindOtherReadyWork(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	store.set(configured(synccontrol.TargetKinepolis, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	starter := newFakeStarter(startResponse{err: synccontrol.ErrInProgress})
	service, scheduler, _, clock := startTestService(t, store, starter)
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
	if call := nextCall(t, starter); call.Attempt != 0 {
		t.Fatalf("contended call=%+v", call)
	}
	waitForTimer(t, clock, time.Second)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetKinepolis), base)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetKinepolis {
		t.Fatalf("ready call=%+v", call)
	}
	clock.Advance(time.Second)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetUGC || call.Attempt != 0 {
		t.Fatalf("requeued call=%+v", call)
	}
}

func TestAcceptedFailuresSpendExactlyTwoRetriesAndClaimStopsChain(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 3, true, Definition{Kind: KindDaily, Time: "08:00"}))
	starter := newFakeStarter(
		startResponse{completion: completionChannel(synccontrol.StateFailed, nil)},
		startResponse{completion: nil},
		startResponse{completion: completionChannel(synccontrol.StateSucceeded, errors.New("finalization"))},
	)
	service, scheduler, _, clock := startTestService(t, store, starter)
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
	for attempt := 0; attempt <= 2; attempt++ {
		if call := nextCall(t, starter); call.Attempt != attempt {
			t.Fatalf("attempt %d call=%+v", attempt, call)
		}
		if attempt < 2 {
			waitForTimer(t, clock, RetryDelay)
			clock.Advance(RetryDelay)
		}
	}
	if len(starter.occurrences()) != 3 {
		t.Fatalf("calls=%+v", starter.occurrences())
	}

	claimStore := newMemoryStore()
	claimStore.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	claimed := newFakeStarter(startResponse{err: synccontrol.ErrOccurrenceClaimed})
	claimService, claimScheduler, _, _ := startTestService(t, claimStore, claimed)
	fire(t, claimScheduler, entryID(t, claimService, synccontrol.TargetUGC), base)
	nextCall(t, claimed)
	if len(claimed.occurrences()) != 1 {
		t.Fatalf("claimed calls=%+v", claimed.occurrences())
	}
}

func TestNonContentionStartErrorAndClosedCompletionSpendAttempts(t *testing.T) {
	tests := []struct {
		name     string
		response startResponse
	}{
		{name: "start error", response: startResponse{err: errors.New("synthetic start failure")}},
		{name: "closed completion", response: startResponse{completion: closedCompletionChannel()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
			starter := newFakeStarter(test.response)
			service, scheduler, _, clock := startTestService(t, store, starter)
			base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
			fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
			if call := nextCall(t, starter); call.Attempt != 0 {
				t.Fatalf("base call=%+v", call)
			}
			waitForTimer(t, clock, RetryDelay)
			clock.Advance(RetryDelay)
			if call := nextCall(t, starter); call.Attempt != 1 {
				t.Fatalf("retry call=%+v", call)
			}
		})
	}
}

func closedCompletionChannel() <-chan synccontrol.Completion {
	result := make(chan synccontrol.Completion)
	close(result)
	return result
}

func TestQueuedOldRevisionAndRetrySurviveDisableOrEdit(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	store.set(configured(synccontrol.TargetKinepolis, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	blocked := make(chan synccontrol.Completion, 1)
	starter := newFakeStarter(
		startResponse{completion: blocked},
		startResponse{completion: completionChannel(synccontrol.StateFailed, nil)},
	)
	service, scheduler, _, clock := startTestService(t, store, starter)
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetKinepolis), base)
	nextCall(t, starter)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), base)
	disabled, err := service.Save(context.Background(), synccontrol.TargetUGC, false, Definition{Kind: KindDaily, Time: "08:00"})
	if err != nil || disabled.Revision != 2 || disabled.Enabled {
		t.Fatalf("disabled=%+v err=%v", disabled, err)
	}
	blocked <- synccontrol.Completion{Status: synccontrol.Status{State: synccontrol.StateSucceeded}}
	close(blocked)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetUGC || call.Revision != 1 {
		t.Fatalf("queued old revision=%+v", call)
	}
	waitForTimer(t, clock, RetryDelay)
	_, err = service.Save(context.Background(), synccontrol.TargetUGC, true, Definition{Kind: KindDaily, Time: "09:00"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(RetryDelay)
	if call := nextCall(t, starter); call.Provider != synccontrol.TargetUGC || call.Revision != 1 || call.Attempt != 1 {
		t.Fatalf("old retry=%+v", call)
	}
}

func TestEditWinningCallbackAcceptanceRejectsStaleRegistration(t *testing.T) {
	baseStore := newMemoryStore()
	baseStore.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	store := &blockingGetStore{memoryStore: baseStore, started: make(chan struct{}), release: make(chan struct{})}
	starter := newFakeStarter()
	service, scheduler, _, _ := startTestService(t, store, starter)
	done := scheduler.trigger(entryID(t, service, synccontrol.TargetUGC), time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	<-store.started
	if _, err := service.Save(context.Background(), synccontrol.TargetUGC, true, Definition{Kind: KindDaily, Time: "09:00"}); err != nil {
		t.Fatal(err)
	}
	close(store.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stale callback did not return")
	}
	assertNoCall(t, starter)
}

func TestCloseDiscardsBacklogAndFutureWorkWithoutWaitingForCompletion(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	blocked := make(chan synccontrol.Completion)
	starter := newFakeStarter(startResponse{completion: blocked})
	service, scheduler, _, _ := startTestService(t, store, starter)
	id := entryID(t, service, synccontrol.TargetUGC)
	base := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, id, base)
	nextCall(t, starter)
	fire(t, scheduler, id, base.Add(time.Minute))

	closed := make(chan struct{})
	go func() {
		service.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close waited for manager completion or backlog")
	}
	service.Close()
	if len(starter.occurrences()) != 1 {
		t.Fatalf("shutdown calls=%+v", starter.occurrences())
	}
}

func TestCloseDiscardsDelayedRetry(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	starter := newFakeStarter(startResponse{completion: completionChannel(synccontrol.StateFailed, nil)})
	service, scheduler, _, clock := startTestService(t, store, starter)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	nextCall(t, starter)
	waitForTimer(t, clock, RetryDelay)
	service.Close()
	clock.Advance(RetryDelay)
	if len(starter.occurrences()) != 1 {
		t.Fatalf("shutdown calls=%+v", starter.occurrences())
	}
}

func TestServiceCloseCancelsBlockedSaveBeforeWaitingForOperationLock(t *testing.T) {
	store := &blockingUpsertStore{
		memoryStore: newMemoryStore(), started: make(chan struct{}), canceled: make(chan struct{}),
	}
	service, _, _, _ := startTestService(t, store, newFakeStarter())
	saveDone := make(chan error, 1)
	go func() {
		_, err := service.Save(context.Background(), synccontrol.TargetUGC, true, Definition{Kind: KindDaily, Time: "08:00"})
		saveDone <- err
	}()
	<-store.started
	closeDone := make(chan struct{})
	go func() {
		service.Close()
		close(closeDone)
	}()
	select {
	case <-store.canceled:
	case <-time.After(time.Second):
		t.Fatal("blocked upsert did not observe shutdown")
	}
	if err := <-saveDone; err == nil {
		t.Fatal("canceled save succeeded")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("close remained blocked behind save")
	}
}

func TestServiceSavePreservesRequestCancellation(t *testing.T) {
	store := &blockingUpsertStore{
		memoryStore: newMemoryStore(), started: make(chan struct{}), canceled: make(chan struct{}),
	}
	service, _, _, _ := startTestService(t, store, newFakeStarter())
	requestCtx, cancel := context.WithCancel(context.Background())
	saveDone := make(chan error, 1)
	go func() {
		_, err := service.Save(requestCtx, synccontrol.TargetUGC, true, Definition{Kind: KindDaily, Time: "08:00"})
		saveDone <- err
	}()
	<-store.started
	cancel()
	select {
	case <-store.canceled:
	case <-time.After(time.Second):
		t.Fatal("blocked upsert did not observe request cancellation")
	}
	if err := <-saveDone; err == nil {
		t.Fatal("request-canceled save succeeded")
	}
}
