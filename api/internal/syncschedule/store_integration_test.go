package syncschedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
	"messeances/api/internal/synccontrol"
)

func TestPostgresScheduleStoreIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	storeA := NewPostgresStore(pool)
	storeB := NewPostgresStore(pool)
	rows, err := storeA.List(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("initial rows=%v err=%v", rows, err)
	}
	if _, err := storeA.Get(ctx, synccontrol.TargetUGC); !errors.Is(err, ErrScheduleMissing) {
		t.Fatalf("missing err=%v", err)
	}
	first, err := storeA.Upsert(ctx, Schedule{Provider: synccontrol.TargetUGC, Enabled: false, Definition: Definition{Kind: KindDaily, Time: "08:15"}})
	if err != nil || first.Revision != 1 || first.UpdatedAt.IsZero() {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := storeB.Upsert(ctx, Schedule{Provider: synccontrol.TargetUGC, Enabled: true, Definition: Definition{Kind: KindWeekly, Time: "19:45", Weekdays: []string{"mon", "fri"}}})
	if err != nil || second.Revision != 2 || !second.Enabled {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	third, err := storeA.Upsert(ctx, Schedule{Provider: synccontrol.TargetKinepolis, Enabled: true, Definition: Definition{Kind: KindCron, Expression: "0 7 * * *"}})
	if err != nil || third.Revision != 1 {
		t.Fatalf("third=%+v err=%v", third, err)
	}
	fourth, err := storeA.Upsert(ctx, Schedule{Provider: synccontrol.TargetPathe, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "06:30"}})
	if err != nil || fourth.Revision != 1 {
		t.Fatalf("fourth=%+v err=%v", fourth, err)
	}
	rows, err = storeB.List(ctx)
	if err != nil || len(rows) != 3 || rows[0].Provider != synccontrol.TargetUGC || rows[1].Provider != synccontrol.TargetKinepolis || rows[2].Provider != synccontrol.TargetPathe || rows[0].Revision != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestScheduledOccurrenceClaimAcrossReplicasAfterSuccessIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	scheduleStore := NewPostgresStore(pool)
	committed, err := scheduleStore.Upsert(ctx, Schedule{Provider: synccontrol.TargetUGC, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:00"}})
	if err != nil {
		t.Fatal(err)
	}
	executorA := newIntegrationExecutor(false)
	executorB := newIntegrationExecutor(false)
	managerA := integrationManager(t, ctx, pool, executorA)
	managerB := integrationManager(t, ctx, pool, executorB)
	scheduledFor := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	serviceA, schedulerA, _ := integrationService(t, scheduleStore, managerA)
	serviceB, schedulerB, _ := integrationService(t, scheduleStore, managerB)
	fire(t, schedulerA, entryID(t, serviceA, synccontrol.TargetUGC), scheduledFor)
	waitIntegrationCall(t, executorA)
	waitServiceChain(t, serviceA, occurrenceKey{provider: synccontrol.TargetUGC, revision: committed.Revision, scheduledFor: scheduledFor})
	fire(t, schedulerB, entryID(t, serviceB, synccontrol.TargetUGC), scheduledFor)
	waitServiceChain(t, serviceB, occurrenceKey{provider: synccontrol.TargetUGC, revision: committed.Revision, scheduledFor: scheduledFor})
	if executorA.count() != 1 || executorB.count() != 0 {
		t.Fatalf("executor calls A=%d B=%d", executorA.count(), executorB.count())
	}
	assertAttempts(t, ctx, pool, scheduledFor, []int{0})
}

func TestScheduledOccurrenceClaimAcrossReplicasAfterFailureIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	scheduleStore := NewPostgresStore(pool)
	committed, err := scheduleStore.Upsert(ctx, Schedule{Provider: synccontrol.TargetUGC, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:00"}})
	if err != nil {
		t.Fatal(err)
	}
	executorA := newIntegrationExecutor(true)
	executorB := newIntegrationExecutor(true)
	managerA := integrationManager(t, ctx, pool, executorA)
	managerB := integrationManager(t, ctx, pool, executorB)
	serviceA, schedulerA, clockA := integrationService(t, scheduleStore, managerA)
	serviceB, schedulerB, _ := integrationService(t, scheduleStore, managerB)
	scheduledFor := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, schedulerA, entryID(t, serviceA, synccontrol.TargetUGC), scheduledFor)
	for attempt := 0; attempt <= 2; attempt++ {
		waitIntegrationCall(t, executorA)
		if attempt < 2 {
			waitForTimer(t, clockA, RetryDelay)
			clockA.Advance(RetryDelay)
		}
	}
	waitServiceChain(t, serviceA, occurrenceKey{provider: synccontrol.TargetUGC, revision: committed.Revision, scheduledFor: scheduledFor})
	fire(t, schedulerB, entryID(t, serviceB, synccontrol.TargetUGC), scheduledFor)
	waitServiceChain(t, serviceB, occurrenceKey{provider: synccontrol.TargetUGC, revision: committed.Revision, scheduledFor: scheduledFor})
	if executorA.count() != 3 || executorB.count() != 0 {
		t.Fatalf("executor calls A=%d B=%d", executorA.count(), executorB.count())
	}
	assertAttempts(t, ctx, pool, scheduledFor, []int{0, 1, 2})
}

func TestQueuedOccurrenceRetriesGlobalLeaseContentionIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	scheduleStore := NewPostgresStore(pool)
	committed, err := scheduleStore.Upsert(ctx, Schedule{Provider: synccontrol.TargetUGC, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:00"}})
	if err != nil {
		t.Fatal(err)
	}
	holder := newBlockingIntegrationExecutor()
	holderManager := integrationManager(t, ctx, pool, holder)
	queuedExecutor := newIntegrationExecutor(false)
	queuedManager := integrationManager(t, ctx, pool, queuedExecutor)
	if _, err := holderManager.Start(synccontrol.TargetKinepolis); err != nil {
		t.Fatal(err)
	}
	select {
	case <-holder.started:
	case <-time.After(time.Second):
		t.Fatal("lease holder did not start")
	}

	service, scheduler, clock := integrationService(t, scheduleStore, queuedManager)
	scheduledFor := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	fire(t, scheduler, entryID(t, service, synccontrol.TargetUGC), scheduledFor)
	waitForTimer(t, clock, time.Second)
	assertAttempts(t, ctx, pool, scheduledFor, nil)
	close(holder.release)
	select {
	case <-holder.finished:
	case <-time.After(time.Second):
		t.Fatal("lease holder did not finish")
	}
	holderManager.Close()
	clock.Advance(time.Second)
	waitIntegrationCall(t, queuedExecutor)
	waitServiceChain(t, service, occurrenceKey{provider: synccontrol.TargetUGC, revision: committed.Revision, scheduledFor: scheduledFor})
	assertAttempts(t, ctx, pool, scheduledFor, []int{0})
}

type integrationExecutor struct {
	mu     sync.Mutex
	calls  int
	fail   bool
	called chan struct{}
}

func newIntegrationExecutor(fail bool) *integrationExecutor {
	return &integrationExecutor{fail: fail, called: make(chan struct{}, 8)}
}

func (e *integrationExecutor) Run(_ context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	e.mu.Lock()
	e.calls++
	fail := e.fail
	e.mu.Unlock()
	e.called <- struct{}{}
	if fail {
		return nil, errors.New("synthetic executor failure")
	}
	return map[synccontrol.Target]synccontrol.ProviderOutcome{
		target: {Sync: synccontrol.SyncOutcome{Through: window.From}},
	}, nil
}

func (e *integrationExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

type blockingIntegrationExecutor struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func newBlockingIntegrationExecutor() *blockingIntegrationExecutor {
	return &blockingIntegrationExecutor{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
}

func (e *blockingIntegrationExecutor) Run(ctx context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	close(e.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.release:
	}
	close(e.finished)
	return map[synccontrol.Target]synccontrol.ProviderOutcome{
		target: {Sync: synccontrol.SyncOutcome{Through: window.From}},
	}, nil
}

func integrationManager(t *testing.T, ctx context.Context, pool *pgxpool.Pool, executor synccontrol.Executor) *synccontrol.Manager {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) }
	manager, err := synccontrol.NewManager(ctx, now, executor, synccontrol.NewPostgresRunStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func integrationService(t *testing.T, store Store, starter Starter) (*Service, *fakeScheduler, *manualClock) {
	t.Helper()
	location := mustParis(t)
	scheduler := newFakeScheduler()
	clock := newManualClock()
	service, err := newService(store, starter, location, serviceDependencies{
		now: clock.Now,
		newTicker: func(time.Duration) serviceTicker {
			return newManualTicker()
		},
		newTimer:  clock.NewTimer,
		scheduler: scheduler,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, scheduler, clock
}

func waitIntegrationCall(t *testing.T, executor *integrationExecutor) {
	t.Helper()
	select {
	case <-executor.called:
	case <-time.After(5 * time.Second):
		t.Fatal("executor call not observed")
	}
}

func waitServiceChain(t *testing.T, service *Service, key occurrenceKey) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		service.mu.Lock()
		_, active := service.active[key]
		service.mu.Unlock()
		if !active {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("queued occurrence did not finish")
		}
	}
}

func assertAttempts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scheduledFor time.Time, expected []int) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT schedule_attempt,schedule_revision,scheduled_for,target FROM sync_runs WHERE trigger_source='scheduled' ORDER BY schedule_attempt`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var attempts []int
	for rows.Next() {
		var attempt int
		var revision int64
		var storedFor time.Time
		var target synccontrol.Target
		if err := rows.Scan(&attempt, &revision, &storedFor, &target); err != nil {
			t.Fatal(err)
		}
		if revision != 1 || target != synccontrol.TargetUGC || !storedFor.Equal(scheduledFor) {
			t.Fatalf("occurrence revision=%d target=%s scheduled_for=%v", revision, target, storedFor)
		}
		attempts = append(attempts, attempt)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if len(attempts) != len(expected) {
		t.Fatalf("attempts=%v want=%v", attempts, expected)
	}
	for i := range expected {
		if attempts[i] != expected[i] {
			t.Fatalf("attempts=%v want=%v", attempts, expected)
		}
	}
}

func scheduleIntegrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate schema nonce failed")
	}
	schema := "movieflow_sync_schedule_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if !strings.HasPrefix(schema, "movieflow_sync_schedule_test_") {
			t.Error("unsafe integration schema cleanup rejected")
			return
		}
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error("drop integration schema failed")
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run migrations failed")
	}
	return ctx, pool
}
