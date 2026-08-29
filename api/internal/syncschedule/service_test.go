package syncschedule

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

type memoryScheduleStore struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]Schedule
	claims map[int64]Occurrence
}

func newMemoryScheduleStore() *memoryScheduleStore {
	return &memoryScheduleStore{rows: map[int64]Schedule{}, claims: map[int64]Occurrence{}}
}

func (s *memoryScheduleStore) List(context.Context) ([]Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]Schedule, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, cloneSchedule(row))
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := TargetOrder(rows[i].Target), TargetOrder(rows[j].Target)
		return left < right || left == right && rows[i].ID < rows[j].ID
	})
	return rows, nil
}

func (s *memoryScheduleStore) Get(_ context.Context, target Target, id int64) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || row.Target != target {
		return Schedule{}, ErrScheduleMissing
	}
	return cloneSchedule(row), nil
}

func (s *memoryScheduleStore) Create(_ context.Context, row Schedule) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	row.ID, row.Revision, row.UpdatedAt = s.nextID, 1, time.Now().UTC()
	s.rows[row.ID] = cloneSchedule(row)
	return cloneSchedule(row), nil
}

func (s *memoryScheduleStore) Update(_ context.Context, row Schedule) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[row.ID]
	if !ok || current.Target != row.Target {
		return Schedule{}, ErrScheduleMissing
	}
	row.Revision, row.UpdatedAt = current.Revision+1, time.Now().UTC()
	s.rows[row.ID] = cloneSchedule(row)
	return cloneSchedule(row), nil
}

func (s *memoryScheduleStore) Delete(_ context.Context, target Target, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || row.Target != target {
		return ErrScheduleMissing
	}
	delete(s.rows, id)
	delete(s.claims, id)
	return nil
}

func (s *memoryScheduleStore) ClaimOccurrence(_ context.Context, occurrence Occurrence) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[occurrence.ScheduleID]
	if !ok || row.Target != occurrence.Target || !row.Enabled || row.Revision != occurrence.Revision {
		return false, nil
	}
	current, exists := s.claims[occurrence.ScheduleID]
	if exists && (current.Revision > occurrence.Revision || current.Revision == occurrence.Revision && !current.ScheduledFor.Before(occurrence.ScheduledFor)) {
		return false, nil
	}
	s.claims[occurrence.ScheduleID] = occurrence
	return true, nil
}

type testStarter struct {
	available []Target
	calls     chan Occurrence
	result    Completion
	err       error
}

func (s *testStarter) AvailableTargets() []Target { return append([]Target(nil), s.available...) }
func (s *testStarter) StartScheduled(occurrence Occurrence) (<-chan Completion, error) {
	s.calls <- occurrence
	if s.err != nil {
		return nil, s.err
	}
	completion := make(chan Completion, 1)
	result := s.result
	if result == (Completion{}) {
		result.Succeeded = true
	}
	completion <- result
	close(completion)
	return completion, nil
}

type testScheduler struct {
	mu      sync.Mutex
	next    cron.EntryID
	jobs    map[cron.EntryID]cron.Job
	entries map[cron.EntryID]cron.Entry
}

func newTestScheduler() *testScheduler {
	return &testScheduler{jobs: map[cron.EntryID]cron.Job{}, entries: map[cron.EntryID]cron.Entry{}}
}
func (s *testScheduler) Schedule(_ cron.Schedule, job cron.Job) cron.EntryID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.jobs[s.next] = job
	return s.next
}
func (s *testScheduler) Remove(id cron.EntryID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
}
func (s *testScheduler) Entry(id cron.EntryID) cron.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id]
}
func (*testScheduler) Start() {}
func (*testScheduler) Stop() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
func (s *testScheduler) fire(id cron.EntryID, at time.Time) {
	s.mu.Lock()
	job := s.jobs[id]
	s.entries[id] = cron.Entry{ID: id, Prev: at}
	s.mu.Unlock()
	job.Run()
}

type testTicker struct{ c chan time.Time }

func (t testTicker) C() <-chan time.Time { return t.c }
func (testTicker) Stop()                 {}

type testTimer struct{ c chan time.Time }

func (t testTimer) C() <-chan time.Time { return t.c }
func (testTimer) Stop() bool            { return true }

func newTestService(t *testing.T, store Store, starter Starter, scheduler scheduler) *Service {
	t.Helper()
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	service, err := newService(store, starter, location, serviceDependencies{
		now:       time.Now,
		newTicker: func(time.Duration) serviceTicker { return testTicker{c: make(chan time.Time)} },
		newTimer:  func(time.Time) serviceTimer { return testTimer{c: make(chan time.Time)} },
		scheduler: scheduler,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestServiceMultiEntryCRUDAvailabilityAndCanonicalization(t *testing.T) {
	store := newMemoryScheduleStore()
	starter := &testStarter{available: []Target{TargetUGC, TargetMetadataRefresh}, calls: make(chan Occurrence, 8)}
	service := newTestService(t, store, starter, newTestScheduler())
	defer service.Close()

	first, err := service.Create(context.Background(), TargetUGC, false, Definition{Kind: KindWeekly, Time: "09:15", Weekdays: []string{"fri", "mon", "fri"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(context.Background(), TargetUGC, true, Definition{Kind: KindDaily, Time: "19:30"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Revision != 1 || second.Revision != 1 || len(first.Definition.Weekdays) != 2 || first.Definition.Weekdays[0] != "mon" {
		t.Fatalf("unexpected created schedules: first=%+v second=%+v", first, second)
	}
	updated, err := service.Update(context.Background(), TargetUGC, first.ID, true, Definition{Kind: KindDaily, Time: "08:00"})
	if err != nil || updated.Revision != 2 || second.Revision != 1 {
		t.Fatalf("unexpected update: %+v %v", updated, err)
	}
	if _, err := service.Update(context.Background(), TargetCGR, first.ID, true, Definition{Kind: KindDaily, Time: "08:00"}); !errors.Is(err, ErrScheduleMissing) {
		t.Fatalf("target mismatch error=%v", err)
	}
	if _, err := service.Create(context.Background(), TargetCGR, true, Definition{Kind: KindDaily, Time: "08:00"}); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("enabled unavailable error=%v", err)
	}
	disabled, err := service.Create(context.Background(), TargetCGR, false, Definition{Kind: KindDaily, Time: "08:00"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), TargetUGC, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), TargetUGC, disabled.ID); !errors.Is(err, ErrScheduleMissing) {
		t.Fatalf("delete mismatch=%v", err)
	}
	rows, err := service.List(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	wantTargets := []Target{TargetUGC, TargetMetadataRefresh}
	if got := service.AvailableTargets(); len(got) != len(wantTargets) || got[0] != wantTargets[0] || got[1] != wantTargets[1] {
		t.Fatalf("available=%v", got)
	}
}

func TestServiceRegistersAndFiresDuplicateTargetsIndependently(t *testing.T) {
	store := newMemoryScheduleStore()
	first, _ := store.Create(context.Background(), Schedule{Target: TargetUGC, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:00"}})
	second, _ := store.Create(context.Background(), Schedule{Target: TargetUGC, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "19:00"}})
	_, _ = store.Create(context.Background(), Schedule{Target: TargetCGR, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:00"}})
	starter := &testStarter{available: []Target{TargetUGC}, calls: make(chan Occurrence, 8)}
	scheduler := newTestScheduler()
	service := newTestService(t, store, starter, scheduler)
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if len(service.entries) != 3 || service.entries[first.ID].entryID == 0 || service.entries[second.ID].entryID == 0 {
		t.Fatalf("registrations=%+v", service.entries)
	}
	for _, id := range []int64{first.ID, second.ID} {
		scheduler.fire(service.entries[id].entryID, time.Date(2026, 8, 29, 7, 0, 0, 0, time.UTC))
	}
	seen := map[int64]bool{}
	for range 2 {
		select {
		case occurrence := <-starter.calls:
			seen[occurrence.ScheduleID] = true
		case <-time.After(time.Second):
			t.Fatal("scheduled occurrence not dispatched")
		}
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("seen=%v", seen)
	}
}

func TestClaimOccurrenceValidationAndReplacement(t *testing.T) {
	store := newMemoryScheduleStore()
	row, _ := store.Create(context.Background(), Schedule{Target: TargetMetadataRefresh, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:00"}})
	service := newTestService(t, store, &testStarter{available: []Target{TargetMetadataRefresh}, calls: make(chan Occurrence, 1)}, newTestScheduler())
	defer service.Close()
	base := Occurrence{ScheduleID: row.ID, Target: row.Target, Revision: row.Revision, ScheduledFor: time.Now().UTC().Truncate(time.Minute), Attempt: 0}
	claimed, err := service.ClaimOccurrence(context.Background(), base)
	if err != nil || !claimed {
		t.Fatalf("first claim=%v err=%v", claimed, err)
	}
	claimed, err = service.ClaimOccurrence(context.Background(), base)
	if err != nil || claimed {
		t.Fatalf("duplicate claim=%v err=%v", claimed, err)
	}
	base.Attempt = 1
	if _, err := service.ClaimOccurrence(context.Background(), base); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("retry claim=%v", err)
	}
}

func TestDispatchRetriesTwiceAtFifteenMinutes(t *testing.T) {
	fixed := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	starter := &testStarter{available: []Target{TargetUGC}, calls: make(chan Occurrence, 8), result: Completion{FinalizationError: errors.New("finalization failed")}}
	service := newTestService(t, newMemoryScheduleStore(), starter, newTestScheduler())
	defer service.Close()
	service.deps.now = func() time.Time { return fixed }
	service.ctx = context.Background()
	key := occurrenceKey{scheduleID: 9, target: TargetUGC, revision: 3, scheduledFor: fixed.Add(-time.Hour)}
	service.active[key] = struct{}{}

	item := queueItem{key: key, attempt: 0}
	if !service.dispatchItem(context.Background(), item) {
		t.Fatal("attempt zero stopped dispatch")
	}
	if len(service.queue) != 1 || service.queue[0].attempt != 1 || !service.queue[0].readyAt.Equal(fixed.Add(RetryDelay)) {
		t.Fatalf("first retry=%+v", service.queue)
	}
	item = service.queue[0]
	service.queue = nil
	if !service.dispatchItem(context.Background(), item) {
		t.Fatal("attempt one stopped dispatch")
	}
	if len(service.queue) != 1 || service.queue[0].attempt != 2 || !service.queue[0].readyAt.Equal(fixed.Add(RetryDelay)) {
		t.Fatalf("second retry=%+v", service.queue)
	}
	item = service.queue[0]
	service.queue = nil
	if !service.dispatchItem(context.Background(), item) {
		t.Fatal("attempt two stopped dispatch")
	}
	if len(service.queue) != 0 {
		t.Fatalf("unexpected fourth attempt=%+v", service.queue)
	}
	if _, active := service.active[key]; active {
		t.Fatal("terminal failed chain remained active")
	}
	for attempt := range 3 {
		call := <-starter.calls
		if call.ScheduleID != key.scheduleID || call.Attempt != attempt {
			t.Fatalf("call=%+v want attempt=%d", call, attempt)
		}
	}
}

func TestDispatchContentionAndClaimConflictSemantics(t *testing.T) {
	fixed := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	starter := &testStarter{available: []Target{TargetUGC}, calls: make(chan Occurrence, 4), err: ErrInProgress}
	service := newTestService(t, newMemoryScheduleStore(), starter, newTestScheduler())
	defer service.Close()
	service.deps.now = func() time.Time { return fixed }
	service.ctx = context.Background()
	key := occurrenceKey{scheduleID: 10, target: TargetUGC, revision: 1, scheduledFor: fixed}
	service.active[key] = struct{}{}
	if !service.dispatchItem(context.Background(), queueItem{key: key, attempt: 1}) || len(service.queue) != 1 || service.queue[0].attempt != 1 || !service.queue[0].readyAt.Equal(fixed.Add(time.Second)) {
		t.Fatalf("contention queue=%+v", service.queue)
	}
	service.queue = nil
	starter.err = ErrOccurrenceClaimed
	if !service.dispatchItem(context.Background(), queueItem{key: key}) {
		t.Fatal("claim conflict stopped dispatch")
	}
	if _, active := service.active[key]; active {
		t.Fatal("claimed occurrence remained active")
	}
}
