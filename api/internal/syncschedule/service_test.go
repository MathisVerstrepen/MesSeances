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
	getErrs []error
}

type blockingUpsertStore struct {
	*memoryStore
	started  chan struct{}
	canceled chan struct{}
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: make(map[synccontrol.Target]Schedule)}
}

func (s *memoryStore) List(context.Context) ([]Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if len(s.getErrs) != 0 {
		err := s.getErrs[0]
		s.getErrs = s.getErrs[1:]
		if err != nil {
			return Schedule{}, err
		}
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
	current, exists := s.rows[row.Provider]
	if exists {
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

func (s *memoryStore) queueGetErrors(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErrs = append(s.getErrs, errs...)
}

type fakeScheduler struct {
	mu      sync.Mutex
	next    cron.EntryID
	entries map[cron.EntryID]*fakeCronEntry
	started bool
}

type fakeCronEntry struct {
	schedule cron.Schedule
	job      cron.Job
	prev     time.Time
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{entries: make(map[cron.EntryID]*fakeCronEntry)}
}

func (s *fakeScheduler) Schedule(schedule cron.Schedule, job cron.Job) cron.EntryID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.entries[s.next] = &fakeCronEntry{schedule: schedule, job: job}
	return s.next
}

func (s *fakeScheduler) Remove(id cron.EntryID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
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
func (t *manualTicker) tick()               { t.ch <- time.Now() }

type startResponse struct {
	err        error
	completion <-chan synccontrol.Completion
}

type fakeStarter struct {
	mu        sync.Mutex
	responses []startResponse
	calls     []synccontrol.Occurrence
	called    chan struct{}
}

func (s *fakeStarter) StartScheduled(occurrence synccontrol.Occurrence) (synccontrol.Status, <-chan synccontrol.Completion, error) {
	s.mu.Lock()
	s.calls = append(s.calls, occurrence)
	var response startResponse
	if len(s.responses) != 0 {
		response = s.responses[0]
		s.responses = s.responses[1:]
	} else {
		response.completion = completionChannel(synccontrol.StateSucceeded, nil)
	}
	called := s.called
	s.mu.Unlock()
	if called != nil {
		select {
		case called <- struct{}{}:
		default:
		}
	}
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

func testService(t *testing.T, store Store, starter Starter, scheduler *fakeScheduler, ticker *manualTicker, wait func(context.Context, time.Duration) bool) *Service {
	t.Helper()
	if wait == nil {
		wait = func(context.Context, time.Duration) bool { return true }
	}
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	service, err := newService(store, starter, mustParis(t), serviceDependencies{
		now: func() time.Time { return now },
		newTicker: func(time.Duration) serviceTicker {
			return ticker
		},
		wait:      wait,
		scheduler: scheduler,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func configured(provider synccontrol.Target, revision int64, enabled bool, definition Definition) Schedule {
	return Schedule{Provider: provider, Revision: revision, Enabled: enabled, Definition: definition, UpdatedAt: time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)}
}

func TestServiceRegistersCommittedEnabledRevisionAndUsesEntryPrev(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 4, true, Definition{Kind: KindDaily, Time: "02:30"}))
	starter := &fakeStarter{}
	scheduler := newFakeScheduler()
	service := testService(t, store, starter, scheduler, newManualTicker(), nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if scheduler.count() != 1 {
		t.Fatalf("entries=%d", scheduler.count())
	}
	service.mu.Lock()
	id := service.entries[synccontrol.TargetUGC].entryID
	service.mu.Unlock()
	previous := time.Date(2026, 10, 25, 2, 30, 48, 0, time.FixedZone("CEST", 7200))
	<-scheduler.trigger(id, previous)
	calls := starter.occurrences()
	if len(calls) != 1 || calls[0].Revision != 4 || calls[0].Attempt != 0 || !calls[0].ScheduledFor.Equal(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("calls=%+v", calls)
	}
	secondPrevious := time.Date(2026, 10, 25, 2, 30, 0, 0, time.FixedZone("CET", 3600))
	<-scheduler.trigger(id, secondPrevious)
	calls = starter.occurrences()
	if len(calls) != 2 || calls[1].ScheduledFor.Equal(calls[0].ScheduledFor) || !calls[1].ScheduledFor.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("fall runtime calls=%+v", calls)
	}
}

func TestServiceStartupRejectsMalformedPersistedRowsBeforeCronStarts(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindCron, Expression: "@daily"}))
	scheduler := newFakeScheduler()
	service := testService(t, store, &fakeStarter{}, scheduler, newManualTicker(), nil)
	if err := service.Start(context.Background()); err == nil {
		t.Fatal("malformed persisted row started service")
	}
	if scheduler.started || scheduler.count() != 0 {
		t.Fatal("cron started before full snapshot validation")
	}
}

func TestServiceSaveAndRefreshConvergeWithoutSeedRows(t *testing.T) {
	store := newMemoryStore()
	tickerA, tickerB := newManualTicker(), newManualTicker()
	serviceA := testService(t, store, &fakeStarter{}, newFakeScheduler(), tickerA, nil)
	schedulerB := newFakeScheduler()
	serviceB := testService(t, store, &fakeStarter{}, schedulerB, tickerB, nil)
	if err := serviceA.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := serviceB.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(serviceA.Close)
	t.Cleanup(serviceB.Close)
	rows, err := serviceA.List(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("initial rows=%v err=%v", rows, err)
	}
	first, err := serviceA.Save(context.Background(), synccontrol.TargetUGC, false, Definition{Kind: KindWeekly, Time: "09:15", Weekdays: []string{"fri", "mon", "fri"}})
	if err != nil || first.Revision != 1 || !sameStrings(first.Definition.Weekdays, []string{"mon", "fri"}) {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if schedulerB.count() != 0 {
		t.Fatal("disabled schedule registered")
	}
	second, err := serviceA.Save(context.Background(), synccontrol.TargetUGC, true, first.Definition)
	if err != nil || second.Revision != 2 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	tickerB.tick()
	waitUntil(t, func() bool { return schedulerB.count() == 1 })

	store.remove(synccontrol.TargetUGC)
	tickerB.tick()
	waitUntil(t, func() bool { return schedulerB.count() == 0 })
}

func TestServiceFreshnessFailsClosedAndPollFailureRetainsRegistry(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	starter := &fakeStarter{}
	scheduler := newFakeScheduler()
	ticker := newManualTicker()
	service := testService(t, store, starter, scheduler, ticker, nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	service.mu.Lock()
	id := service.entries[synccontrol.TargetUGC].entryID
	service.mu.Unlock()

	store.failGet(errors.New("database unavailable"))
	<-scheduler.trigger(id, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	if len(starter.occurrences()) != 0 {
		t.Fatal("base started after freshness read failure")
	}
	store.failGet(nil)
	row, _ := store.Get(context.Background(), synccontrol.TargetUGC)
	row.Revision = 2
	store.set(row)
	<-scheduler.trigger(id, time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC))
	if len(starter.occurrences()) != 0 {
		t.Fatal("stale revision started")
	}

	store.failList(errors.New("database unavailable"))
	ticker.tick()
	time.Sleep(10 * time.Millisecond)
	if scheduler.count() != 1 {
		t.Fatal("poll failure removed registry")
	}
}

func TestRetryChainUsesTwoSlotsAndStopsOnSuccessOrClaim(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 3, true, Definition{Kind: KindCron, Expression: "0 8 * * *"}))
	waits := []time.Duration{}
	starter := &fakeStarter{responses: []startResponse{
		{completion: completionChannel(synccontrol.StateFailed, nil)},
		{err: synccontrol.ErrInProgress},
		{completion: completionChannel(synccontrol.StateSucceeded, nil)},
	}}
	service := testService(t, store, starter, newFakeScheduler(), newManualTicker(), func(_ context.Context, delay time.Duration) bool {
		waits = append(waits, delay)
		return true
	})
	scheduledFor := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	service.runChain(context.Background(), synccontrol.TargetUGC, 3, scheduledFor)
	calls := starter.occurrences()
	if len(calls) != 3 || calls[0].Attempt != 0 || calls[1].Attempt != 1 || calls[2].Attempt != 2 {
		t.Fatalf("calls=%+v", calls)
	}
	if len(waits) != 2 || waits[0] != RetryDelay || waits[1] != RetryDelay {
		t.Fatalf("waits=%v", waits)
	}

	claimed := &fakeStarter{responses: []startResponse{{err: synccontrol.ErrOccurrenceClaimed}}}
	service = testService(t, store, claimed, newFakeScheduler(), newManualTicker(), func(context.Context, time.Duration) bool {
		t.Fatal("claim conflict scheduled retry")
		return false
	})
	service.runChain(context.Background(), synccontrol.TargetUGC, 3, scheduledFor)
	if len(claimed.occurrences()) != 1 {
		t.Fatalf("claim calls=%+v", claimed.occurrences())
	}
}

func TestRetryFreshnessFailureConsumesSlotAndRetryClaimStopsChain(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 3, true, Definition{Kind: KindDaily, Time: "08:00"}))
	store.queueGetErrors(nil, errors.New("database unavailable"), nil)
	waits := 0
	starter := &fakeStarter{responses: []startResponse{
		{completion: completionChannel(synccontrol.StateFailed, nil)},
		{completion: completionChannel(synccontrol.StateSucceeded, nil)},
	}}
	service := testService(t, store, starter, newFakeScheduler(), newManualTicker(), func(context.Context, time.Duration) bool {
		waits++
		return true
	})
	service.runChain(context.Background(), synccontrol.TargetUGC, 3, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	calls := starter.occurrences()
	if waits != 2 || len(calls) != 2 || calls[0].Attempt != 0 || calls[1].Attempt != 2 {
		t.Fatalf("waits=%d calls=%+v", waits, calls)
	}

	claimStarter := &fakeStarter{responses: []startResponse{
		{completion: completionChannel(synccontrol.StateFailed, nil)},
		{err: synccontrol.ErrOccurrenceClaimed},
	}}
	waits = 0
	service = testService(t, store, claimStarter, newFakeScheduler(), newManualTicker(), func(context.Context, time.Duration) bool {
		waits++
		return true
	})
	service.runChain(context.Background(), synccontrol.TargetUGC, 3, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	if waits != 1 || len(claimStarter.occurrences()) != 2 {
		t.Fatalf("claim waits=%d calls=%+v", waits, claimStarter.occurrences())
	}
}

func TestFinalizationFailureRetriesAndDisableCancelsPendingRetry(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	waiting := make(chan struct{}, 1)
	starter := &fakeStarter{responses: []startResponse{{completion: completionChannel(synccontrol.StateSucceeded, errors.New("finalization"))}}}
	scheduler := newFakeScheduler()
	service := testService(t, store, starter, scheduler, newManualTicker(), func(ctx context.Context, _ time.Duration) bool {
		waiting <- struct{}{}
		<-ctx.Done()
		return false
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	service.mu.Lock()
	id := service.entries[synccontrol.TargetUGC].entryID
	service.mu.Unlock()
	done := scheduler.trigger(id, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	<-waiting
	saved, err := service.Save(context.Background(), synccontrol.TargetUGC, false, Definition{Kind: KindDaily, Time: "08:00"})
	if err != nil || saved.Revision != 2 || saved.Enabled {
		t.Fatalf("disabled=%+v err=%v", saved, err)
	}
	<-done
	if len(starter.occurrences()) != 1 || scheduler.count() != 0 {
		t.Fatalf("calls=%+v", starter.occurrences())
	}
}

func TestCronWrapperSkipsOverlappingProviderChain(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	completion := make(chan synccontrol.Completion, 1)
	starter := &fakeStarter{responses: []startResponse{{completion: completion}}, called: make(chan struct{}, 2)}
	scheduler := newFakeScheduler()
	service := testService(t, store, starter, scheduler, newManualTicker(), nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	service.mu.Lock()
	id := service.entries[synccontrol.TargetUGC].entryID
	service.mu.Unlock()
	first := scheduler.trigger(id, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	<-starter.called
	second := scheduler.trigger(id, time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC))
	<-second
	if len(starter.occurrences()) != 1 {
		t.Fatalf("overlap calls=%+v", starter.occurrences())
	}
	completion <- synccontrol.Completion{Status: synccontrol.Status{State: synccontrol.StateSucceeded}}
	close(completion)
	<-first
}

func TestServiceCloseCancelsPendingRetry(t *testing.T) {
	store := newMemoryStore()
	store.set(configured(synccontrol.TargetUGC, 1, true, Definition{Kind: KindDaily, Time: "08:00"}))
	waiting := make(chan struct{}, 1)
	starter := &fakeStarter{responses: []startResponse{{completion: completionChannel(synccontrol.StateFailed, nil)}}}
	scheduler := newFakeScheduler()
	service := testService(t, store, starter, scheduler, newManualTicker(), func(ctx context.Context, _ time.Duration) bool {
		waiting <- struct{}{}
		<-ctx.Done()
		return false
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	id := service.entries[synccontrol.TargetUGC].entryID
	service.mu.Unlock()
	done := scheduler.trigger(id, time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC))
	<-waiting
	service.Close()
	<-done
	if len(starter.occurrences()) != 1 {
		t.Fatalf("shutdown calls=%+v", starter.occurrences())
	}
}

func TestServiceCloseCancelsBlockedSaveBeforeWaitingForOperationLock(t *testing.T) {
	store := &blockingUpsertStore{
		memoryStore: newMemoryStore(),
		started:     make(chan struct{}),
		canceled:    make(chan struct{}),
	}
	service := testService(t, store, &fakeStarter{}, newFakeScheduler(), newManualTicker(), nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
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
		t.Fatal("blocked upsert did not observe service shutdown cancellation")
	}
	select {
	case err := <-saveDone:
		if err == nil {
			t.Fatal("canceled save succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("save did not finish after shutdown cancellation")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("close remained blocked behind canceled save")
	}

	// Repeated close waits for and observes the same completed shutdown.
	service.Close()
}

func TestServiceSavePreservesRequestCancellation(t *testing.T) {
	store := &blockingUpsertStore{
		memoryStore: newMemoryStore(),
		started:     make(chan struct{}),
		canceled:    make(chan struct{}),
	}
	service := testService(t, store, &fakeStarter{}, newFakeScheduler(), newManualTicker(), nil)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	saveDone := make(chan error, 1)
	go func() {
		_, err := service.Save(requestCtx, synccontrol.TargetUGC, true, Definition{Kind: KindDaily, Time: "08:00"})
		saveDone <- err
	}()
	<-store.started
	cancelRequest()
	select {
	case <-store.canceled:
	case <-time.After(time.Second):
		t.Fatal("blocked upsert did not observe request cancellation")
	}
	select {
	case err := <-saveDone:
		if err == nil {
			t.Fatal("request-canceled save succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("save did not finish after request cancellation")
	}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
