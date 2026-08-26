package geocoding_test

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

	"messeances/api/internal/database"
	"messeances/api/internal/geocoding"
)

func TestPostgresGeocodingRunStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate schema nonce failed")
	}
	schema := "movieflow_geocoding_run_test_" + hex.EncodeToString(nonce)
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
		if !strings.HasPrefix(schema, "movieflow_geocoding_run_test_") {
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

	store := geocoding.NewPostgresRunStore(pool)
	empty, err := store.Snapshot(ctx)
	if err != nil || empty != nil {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	started := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	running, err := store.Create(ctx, geocoding.RunStatus{State: geocoding.RunStateRunning, StartedAt: started})
	if err != nil || running.ID == "" {
		t.Fatalf("running=%+v err=%v", running, err)
	}
	if _, err := store.Create(ctx, geocoding.RunStatus{State: geocoding.RunStateRunning, StartedAt: started.Add(time.Second)}); !errors.Is(err, geocoding.ErrRunInProgress) {
		t.Fatalf("duplicate running error=%v", err)
	}
	active, err := store.Snapshot(ctx)
	if err != nil || active == nil || active.ID != running.ID || active.State != geocoding.RunStateRunning {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	finished := started.Add(time.Minute)
	summary := geocoding.RunSummary{Selected: 5, Skipped: 7, Matched: 2, Ambiguous: 1, NotFound: 2, Failed: 0, Written: 5}
	running.State, running.FinishedAt, running.Summary = geocoding.RunStateSucceeded, &finished, &summary
	if err := store.Finish(ctx, running); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.Snapshot(ctx)
	if err != nil || terminal == nil || terminal.State != geocoding.RunStateSucceeded || terminal.Summary == nil || terminal.Summary.Ambiguous != 1 || terminal.ErrorCode != nil {
		t.Fatalf("terminal=%+v err=%v", terminal, err)
	}

	stale, err := store.Create(ctx, geocoding.RunStatus{State: geocoding.RunStateRunning, StartedAt: started.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	reconciledAt := started.Add(3 * time.Minute)
	if err := store.ReconcileRunning(ctx, reconciledAt); err != nil {
		t.Fatal(err)
	}
	reconciled, err := store.Snapshot(ctx)
	if err != nil || reconciled == nil || reconciled.ID != stale.ID || reconciled.State != geocoding.RunStateFailed || reconciled.FinishedAt == nil || !reconciled.FinishedAt.Equal(reconciledAt) || reconciled.Summary != nil || reconciled.ErrorCode == nil || *reconciled.ErrorCode != geocoding.RunFailureCanceled {
		t.Fatalf("reconciled=%+v err=%v", reconciled, err)
	}

	lockerA := geocoding.NewPostgresRunLocker(pool)
	lockerB := geocoding.NewPostgresRunLocker(pool)
	lease, err := lockerA.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockerB.Acquire(ctx); !errors.Is(err, geocoding.ErrRunInProgress) {
		t.Fatalf("contended lock error=%v", err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	lease, err = lockerB.Acquire(ctx)
	if err != nil {
		t.Fatalf("released lock unavailable: %v", err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}

	for _, statement := range []string{
		`INSERT INTO theater_geocoding_runs (state,started_at,summary) VALUES ('running',now(),'{}')`,
		`INSERT INTO theater_geocoding_runs (state,started_at,finished_at,summary,error_code) VALUES ('succeeded',now(),now(),'{}','run_failed')`,
		`INSERT INTO theater_geocoding_runs (state,started_at,finished_at,error_code) VALUES ('failed',now(),now(),'provider secret')`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatalf("invalid durable status accepted: %s", statement)
		}
	}
}
