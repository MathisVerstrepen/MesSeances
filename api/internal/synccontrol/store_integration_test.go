package synccontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
		target text NOT NULL,
		state text NOT NULL,
		started_at timestamptz NOT NULL,
		finished_at timestamptz,
		window_from date NOT NULL,
		window_through date NOT NULL,
		providers jsonb NOT NULL
	)`); err != nil {
		t.Fatal("create sync run fixture failed")
	}

	store := NewPostgresRunStore(pool)
	started := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	running := Status{Target: TargetAll, State: StateRunning, StartedAt: started, From: "2026-08-23", Through: "2026-08-30", Providers: map[string]ProviderStatus{
		string(TargetUGC):       {State: ProviderRunning},
		string(TargetKinepolis): {State: ProviderPending},
	}}
	created, err := store.Create(ctx, running)
	if err != nil || created.ID == "" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	finished := started.Add(time.Minute)
	created.State = StateSucceeded
	created.FinishedAt = &finished
	created.Providers[string(TargetUGC)] = ProviderStatus{State: ProviderSucceeded, Outcome: &ProviderOutcome{Sync: SyncOutcome{Version: 3, Movies: 4, NewMovies: 2, Requests: 9, Showtimes: 8, NewShowtimes: 5}, Enrichment: EnrichmentOutcome{Status: EnrichmentSkipped}}}
	created.Providers[string(TargetKinepolis)] = ProviderStatus{State: ProviderSkipped}
	if err := store.Update(ctx, created); err != nil {
		t.Fatal(err)
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
	if runs[0].State != StateFailed || runs[0].FinishedAt == nil || !runs[0].FinishedAt.Equal(reconciledAt) || runs[0].Providers[string(TargetUGC)].ErrorCode != FailureCanceled || runs[0].Providers[string(TargetKinepolis)].State != ProviderSkipped {
		t.Fatalf("reconciled=%+v", runs[0])
	}
	outcome := runs[1].Providers[string(TargetUGC)].Outcome
	if outcome == nil || outcome.Sync.Movies != 4 || outcome.Sync.NewMovies != 2 || outcome.Sync.Requests != 9 || outcome.Sync.NewShowtimes != 5 {
		t.Fatalf("persisted outcome=%+v", outcome)
	}
}
