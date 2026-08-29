package synccontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRunStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate test schema nonce failed")
	}
	schema := "movieflow_sync_run_test_" + hex.EncodeToString(nonce)
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
		if !strings.HasPrefix(schema, "movieflow_sync_run_test_") {
			t.Error("unsafe integration schema cleanup rejected")
			return
		}
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("drop integration schema failed")
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `CREATE TABLE sync_runs (
		id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		target text NOT NULL CHECK (target IN ('all','ugc','kinepolis')),
		state text NOT NULL,
		started_at timestamptz NOT NULL,
		finished_at timestamptz,
		window_from date NOT NULL,
		window_through date NOT NULL,
		providers jsonb NOT NULL,
		trigger_source text NOT NULL DEFAULT 'manual',
		schedule_id bigint,
		schedule_revision bigint,
		scheduled_for timestamptz,
		schedule_attempt smallint,
		CHECK ((trigger_source='manual' AND schedule_id IS NULL AND schedule_revision IS NULL AND scheduled_for IS NULL AND schedule_attempt IS NULL)
			OR (trigger_source='scheduled' AND target IN ('ugc','kinepolis') AND schedule_id > 0 AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2))
	);
	CREATE UNIQUE INDEX sync_runs_scheduled_occurrence_attempt_idx
		ON sync_runs (schedule_id,schedule_revision,scheduled_for,schedule_attempt)
		WHERE trigger_source='scheduled'`); err != nil {
		t.Fatal("create sync run fixture failed")
	}

	store := NewPostgresRunStore(pool)
	empty, err := store.Snapshot(ctx)
	if err != nil || empty.Job != nil || empty.Runs == nil || len(empty.Runs) != 0 {
		t.Fatalf("empty snapshot=%+v err=%v", empty, err)
	}
	started := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	running := Status{Target: TargetAll, State: StateRunning, StartedAt: started, From: "2026-08-23", Through: "2026-08-30", Providers: map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderRunning},
		string(TargetKinepolis): {State: ProviderPending},
	}}
	created, err := store.Create(ctx, running)
	if err != nil || created.ID == "" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	active, err := store.Snapshot(ctx)
	if err != nil || active.Job == nil || active.Job.ID != created.ID || len(active.Runs) != 0 || active.Job.Trigger != TriggerManual || active.Job.Occurrence != nil {
		t.Fatalf("active snapshot=%+v err=%v", active, err)
	}
	finished := started.Add(time.Minute)
	created.State = StateSucceeded
	created.FinishedAt = &finished
	created.Through = "2027-01-10"
	created.Providers[string(TargetUGC)] = ProviderStatus{State: ProviderSucceeded, Outcome: &ProviderOutcome{Sync: SyncOutcome{Version: 3, Movies: 4, NewMovies: 2, Requests: 9, Showtimes: 8, NewShowtimes: 5, Through: "2027-01-10"}, Enrichment: EnrichmentOutcome{Status: EnrichmentSkipped}}}
	created.Providers[string(TargetKinepolis)] = ProviderStatus{State: ProviderSkipped}
	if err := store.Update(ctx, created); err != nil {
		t.Fatal(err)
	}
	running.Providers = map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderRunning},
		string(TargetKinepolis): {State: ProviderPending},
	}
	stale, err := store.Create(ctx, running)
	if err != nil {
		t.Fatal(err)
	}
	reconciledAt := finished.Add(time.Minute)
	if err := store.ReconcileRunning(ctx, reconciledAt); err != nil {
		t.Fatal(err)
	}
	runs, err := store.List(ctx, 50)
	if err != nil || len(runs) != 2 || runs[0].ID != stale.ID || runs[1].ID != created.ID {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if runs[0].State != StateFailed || runs[0].FinishedAt == nil || !runs[0].FinishedAt.Equal(reconciledAt) || runs[0].Providers[string(TargetUGC)].ErrorCode != FailureCanceled || len(runs[0].Providers[string(TargetUGC)].Log) != 1 || !strings.Contains(runs[0].Providers[string(TargetUGC)].Log[0], "category=canceled") || runs[0].Providers[string(TargetKinepolis)].State != ProviderSkipped {
		t.Fatalf("reconciled=%+v", runs[0])
	}
	outcome := runs[1].Providers[string(TargetUGC)].Outcome
	if outcome == nil || outcome.Sync.Movies != 4 || outcome.Sync.NewMovies != 2 || outcome.Sync.Requests != 9 || outcome.Sync.NewShowtimes != 5 || outcome.Sync.Through != "2027-01-10" || runs[1].Through != "2027-01-10" {
		t.Fatalf("persisted outcome=%+v", outcome)
	}

	occurrence := Occurrence{ScheduleID: 1, Provider: TargetUGC, Revision: 7, ScheduledFor: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), Attempt: 0}
	scheduled := running
	scheduled.Target = TargetUGC
	scheduled.Trigger = TriggerScheduled
	scheduled.Occurrence = &occurrence
	scheduled.StartedAt = started.Add(3 * time.Minute)
	scheduled.Providers[string(TargetKinepolis)] = ProviderStatus{State: ProviderNotRequested}
	claimed, err := store.Create(ctx, scheduled)
	if err != nil {
		t.Fatalf("create scheduled claim failed: %v", err)
	}
	if _, err := store.Create(ctx, scheduled); !errors.Is(err, ErrOccurrenceClaimed) {
		t.Fatalf("duplicate scheduled claim err=%v", err)
	}
	claimed.State = StateFailed
	claimFinished := claimed.StartedAt.Add(time.Minute)
	claimed.FinishedAt = &claimFinished
	validLog := failureLog(claimFinished, TargetUGC, StageProviderFetch, logFailure{Operation: operationShowings, Category: categoryHTTPStatus, HTTPStatus: 403, Attempt: 1, AttemptLimit: 4})
	claimed.Providers[string(TargetUGC)] = ProviderStatus{State: ProviderFailed, ErrorCode: FailureProviderSync, Log: []string{validLog, "https://user:proxy-password@proxy.example/?token=secret body=provider-secret"}}
	if err := store.Update(ctx, claimed); err != nil {
		t.Fatal("complete scheduled claim failed")
	}
	storedClaims, err := store.List(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var storedClaim *Status
	for i := range storedClaims {
		if storedClaims[i].ID == claimed.ID {
			storedClaim = &storedClaims[i]
			break
		}
	}
	if storedClaim == nil || len(storedClaim.Providers[string(TargetUGC)].Log) != 2 || storedClaim.Providers[string(TargetUGC)].Log[0] != validLog || !strings.Contains(storedClaim.Providers[string(TargetUGC)].Log[1], "event=log_truncated") || strings.Contains(strings.Join(storedClaim.Providers[string(TargetUGC)].Log, "\n"), "proxy-password") {
		t.Fatalf("sanitized persisted claim=%+v", storedClaim)
	}
	for _, attempt := range []int{1, 2} {
		retry := scheduled
		retry.Occurrence = &Occurrence{ScheduleID: 1, Provider: TargetUGC, Revision: 7, ScheduledFor: occurrence.ScheduledFor, Attempt: attempt}
		retry.StartedAt = scheduled.StartedAt.Add(time.Duration(attempt) * time.Minute)
		createdRetry, err := store.Create(ctx, retry)
		if err != nil {
			t.Fatalf("create attempt %d failed: %v", attempt, err)
		}
		createdRetry.State = StateFailed
		createdRetry.FinishedAt = &reconciledAt
		createdRetry.Providers[string(TargetUGC)] = ProviderStatus{State: ProviderFailed, ErrorCode: FailureProviderSync}
		if err := store.Update(ctx, createdRetry); err != nil {
			t.Fatalf("complete attempt %d failed: %v", attempt, err)
		}
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil || snapshot.Job != nil || len(snapshot.Runs) != 5 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	latest := snapshot.Runs[0]
	if latest.Trigger != TriggerScheduled || latest.Occurrence == nil || latest.Occurrence.ScheduleID != 1 || latest.Occurrence.Provider != TargetUGC || latest.Occurrence.Revision != 7 || latest.Occurrence.Attempt != 2 || !latest.Occurrence.ScheduledFor.Equal(occurrence.ScheduledFor) {
		t.Fatalf("scheduled round trip=%+v", latest)
	}

	lockerA := NewPostgresRunLocker(pool)
	lockerB := NewPostgresRunLocker(pool)
	startupStale := running
	startupStale.StartedAt = started.Add(10 * time.Minute)
	startupStale.Providers = map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderRunning},
		string(TargetKinepolis): {State: ProviderPending},
	}
	startupStale, err = store.Create(ctx, startupStale)
	if err != nil {
		t.Fatalf("create startup stale run failed: %v", err)
	}
	leaseA, err := lockerA.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire first global lease failed: %v", err)
	}
	if _, err := lockerB.Acquire(ctx); !errors.Is(err, ErrInProgress) {
		t.Fatalf("contended global lease err=%v", err)
	}
	startupExecutor := executorMapFunc(func(context.Context, Target, Window) (map[Target]ProviderOutcome, error) {
		t.Fatal("executor called during startup reconciliation")
		return nil, nil
	})
	busyManager, err := NewManager(ctx, time.Now, startupExecutor, store)
	if err != nil {
		t.Fatalf("manager startup under busy lease failed: %v", err)
	}
	busyManager.Close()
	snapshot, err = store.Snapshot(ctx)
	if err != nil || snapshot.Job == nil || snapshot.Job.ID != startupStale.ID || snapshot.Job.State != StateRunning || snapshot.Job.FinishedAt != nil {
		t.Fatalf("busy startup snapshot=%+v err=%v", snapshot, err)
	}
	if err := leaseA.Release(ctx); err != nil {
		t.Fatalf("release first global lease failed: %v", err)
	}
	startupFinished := started.Add(11 * time.Minute)
	reconciledManager, err := NewManager(ctx, func() time.Time { return startupFinished }, startupExecutor, store)
	if err != nil {
		t.Fatalf("manager startup reconciliation failed: %v", err)
	}
	reconciledManager.Close()
	snapshot, err = store.Snapshot(ctx)
	if err != nil || snapshot.Job != nil || len(snapshot.Runs) != 6 || snapshot.Runs[0].ID != startupStale.ID || snapshot.Runs[0].State != StateFailed || snapshot.Runs[0].FinishedAt == nil || !snapshot.Runs[0].FinishedAt.Equal(startupFinished) || snapshot.Runs[0].Providers[string(TargetUGC)].ErrorCode != FailureCanceled || snapshot.Runs[0].Providers[string(TargetKinepolis)].State != ProviderSkipped {
		t.Fatalf("reconciled startup snapshot=%+v err=%v", snapshot, err)
	}
	leaseB, err := lockerB.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire released global lease failed: %v", err)
	}
	brokenLease := leaseB.(*postgresRunLease)
	if err := brokenLease.session.Discard(ctx); err != nil {
		t.Fatalf("discard lease session failed: %v", err)
	}
	leaseAfterLoss, err := func() (RunLease, error) {
		acquireCtx, cancelAcquire := context.WithTimeout(ctx, 5*time.Second)
		defer cancelAcquire()
		retryTicker := time.NewTicker(10 * time.Millisecond)
		defer retryTicker.Stop()
		for {
			lease, acquireErr := lockerA.Acquire(acquireCtx)
			if acquireErr == nil {
				return lease, nil
			}
			if !errors.Is(acquireErr, ErrInProgress) {
				return nil, acquireErr
			}
			select {
			case <-acquireCtx.Done():
				return nil, errors.Join(
					errors.New("timed out waiting for PostgreSQL to release advisory lock"),
					acquireCtx.Err(),
					acquireErr,
				)
			case <-retryTicker.C:
			}
		}
	}()
	if err != nil {
		t.Fatalf("acquire after lease session loss failed: %v", err)
	}
	if err := leaseAfterLoss.Release(ctx); err != nil {
		t.Fatalf("release lease after session loss failed: %v", err)
	}

	retentionNow := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	retentionRows := []struct {
		state      JobState
		startedAt  time.Time
		finishedAt *time.Time
	}{
		{state: StateSucceeded, startedAt: retentionNow.Add(-31 * 24 * time.Hour), finishedAt: timePointer(retentionNow.Add(-30 * 24 * time.Hour))},
		{state: StateFailed, startedAt: retentionNow.Add(-10 * 24 * time.Hour), finishedAt: timePointer(retentionNow.Add(-10 * 24 * time.Hour))},
		{state: StateRunning, startedAt: retentionNow.Add(-60 * 24 * time.Hour)},
	}
	retentionIDs := make([]int64, 0, len(retentionRows))
	for _, row := range retentionRows {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO sync_runs
			(target,state,started_at,finished_at,window_from,window_through,providers)
			VALUES ('ugc',$1,$2,$3,'2026-08-01','2026-08-01','{}'::jsonb)
			RETURNING id`, row.state, row.startedAt, row.finishedAt).Scan(&id); err != nil {
			t.Fatal("insert retention fixture failed")
		}
		retentionIDs = append(retentionIDs, id)
	}
	if err := store.PurgeTerminalBefore(ctx, retentionNow.Add(-TerminalRunRetentionPeriod)); err != nil {
		t.Fatal("purge terminal runs failed")
	}
	var survivors []int64
	rows, err := pool.Query(ctx, `SELECT id FROM sync_runs WHERE id = ANY($1::bigint[]) ORDER BY id`, retentionIDs)
	if err != nil {
		t.Fatal("read retention survivors failed")
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal("scan retention survivor failed")
		}
		survivors = append(survivors, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal("iterate retention survivors failed")
	}
	rows.Close()
	if len(survivors) != 2 || survivors[0] != retentionIDs[1] || survivors[1] != retentionIDs[2] {
		t.Fatalf("retention survivors=%v want recent=%d active=%d", survivors, retentionIDs[1], retentionIDs[2])
	}
}

func timePointer(value time.Time) *time.Time { return &value }
