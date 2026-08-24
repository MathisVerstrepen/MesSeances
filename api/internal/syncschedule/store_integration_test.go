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
	rows, err = storeB.List(ctx)
	if err != nil || len(rows) != 2 || rows[0].Provider != synccontrol.TargetUGC || rows[1].Provider != synccontrol.TargetKinepolis || rows[0].Revision != 2 {
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
	executorA := &integrationExecutor{}
	executorB := &integrationExecutor{}
	managerA := integrationManager(t, ctx, pool, executorA)
	managerB := integrationManager(t, ctx, pool, executorB)
	scheduledFor := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	serviceA, err := NewService(scheduleStore, managerA)
	if err != nil {
		t.Fatal(err)
	}
	serviceB, err := NewService(scheduleStore, managerB)
	if err != nil {
		t.Fatal(err)
	}
	serviceA.runChain(ctx, synccontrol.TargetUGC, committed.Revision, scheduledFor)
	serviceB.runChain(ctx, synccontrol.TargetUGC, committed.Revision, scheduledFor)
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
	executorA := &integrationExecutor{fail: true}
	executorB := &integrationExecutor{fail: true}
	managerA := integrationManager(t, ctx, pool, executorA)
	managerB := integrationManager(t, ctx, pool, executorB)
	waiter := &controlledWaiter{entered: make(chan int, 2), release: make(chan struct{}, 2)}
	serviceA := integrationService(t, scheduleStore, managerA, waiter.wait)
	serviceB := integrationService(t, scheduleStore, managerB, func(context.Context, time.Duration) bool {
		t.Error("duplicate base occurrence created retry chain")
		return false
	})
	scheduledFor := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	done := make(chan struct{})
	go func() {
		defer close(done)
		serviceA.runChain(ctx, synccontrol.TargetUGC, committed.Revision, scheduledFor)
	}()
	if attempt := <-waiter.entered; attempt != 1 {
		t.Fatalf("first wait=%d", attempt)
	}
	serviceB.runChain(ctx, synccontrol.TargetUGC, committed.Revision, scheduledFor)
	waiter.release <- struct{}{}
	if attempt := <-waiter.entered; attempt != 2 {
		t.Fatalf("second wait=%d", attempt)
	}
	waiter.release <- struct{}{}
	<-done
	if executorA.count() != 3 || executorB.count() != 0 {
		t.Fatalf("executor calls A=%d B=%d", executorA.count(), executorB.count())
	}
	assertAttempts(t, ctx, pool, scheduledFor, []int{0, 1, 2})
}

type integrationExecutor struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (e *integrationExecutor) Run(_ context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	e.mu.Lock()
	e.calls++
	fail := e.fail
	e.mu.Unlock()
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

type controlledWaiter struct {
	mu      sync.Mutex
	next    int
	entered chan int
	release chan struct{}
}

func (w *controlledWaiter) wait(ctx context.Context, delay time.Duration) bool {
	if delay != RetryDelay {
		return false
	}
	w.mu.Lock()
	w.next++
	attempt := w.next
	w.mu.Unlock()
	w.entered <- attempt
	select {
	case <-ctx.Done():
		return false
	case <-w.release:
		return true
	}
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

func integrationService(t *testing.T, store Store, starter Starter, wait func(context.Context, time.Duration) bool) *Service {
	t.Helper()
	location := mustParis(t)
	service, err := newService(store, starter, location, serviceDependencies{
		now: func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) },
		newTicker: func(time.Duration) serviceTicker {
			return newManualTicker()
		},
		wait:      wait,
		scheduler: newFakeScheduler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertAttempts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scheduledFor time.Time, expected []int) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT schedule_attempt,schedule_revision,scheduled_for,target FROM sync_runs ORDER BY schedule_attempt`)
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
