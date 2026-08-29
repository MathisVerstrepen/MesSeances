package syncschedule

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
)

func TestPostgresScheduleStoreIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	store := NewPostgresStore(pool)
	first, err := store.Create(ctx, Schedule{Target: TargetUGC, Enabled: true, Definition: Definition{Kind: KindDaily, Time: "08:15"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, Schedule{Target: TargetUGC, Enabled: false, Definition: Definition{Kind: KindDaily, Time: "19:45"}})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Create(ctx, Schedule{Target: TargetMetadataRefresh, Enabled: true, Definition: Definition{Kind: KindCron, Expression: "0 3 * * *"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID <= 0 || second.ID <= first.ID || metadata.ID <= second.ID || first.Revision != 1 || second.Revision != 1 {
		t.Fatalf("created=%+v %+v %+v", first, second, metadata)
	}
	updated, err := store.Update(ctx, Schedule{ID: second.ID, Target: TargetUGC, Enabled: true, Definition: Definition{Kind: KindWeekly, Time: "20:00", Weekdays: []string{"mon", "fri"}}})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if _, err := store.Update(ctx, Schedule{ID: second.ID, Target: TargetCGR, Definition: Definition{Kind: KindDaily, Time: "10:00"}}); !errors.Is(err, ErrScheduleMissing) {
		t.Fatalf("target mismatch=%v", err)
	}
	rows, err := store.List(ctx)
	if err != nil || len(rows) != 3 || rows[0].ID != first.ID || rows[1].ID != second.ID || rows[2].Target != TargetMetadataRefresh {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}

	occurrence := Occurrence{ScheduleID: first.ID, Target: first.Target, Revision: first.Revision, ScheduledFor: time.Date(2026, 8, 29, 6, 0, 0, 0, time.UTC), Attempt: 0}
	claimed, err := store.ClaimOccurrence(ctx, occurrence)
	if err != nil || !claimed {
		t.Fatalf("first claim=%v err=%v", claimed, err)
	}
	claimed, err = store.ClaimOccurrence(ctx, occurrence)
	if err != nil || claimed {
		t.Fatalf("duplicate claim=%v err=%v", claimed, err)
	}
	occurrence.ScheduledFor = occurrence.ScheduledFor.Add(time.Hour)
	claimed, err = store.ClaimOccurrence(ctx, occurrence)
	if err != nil || !claimed {
		t.Fatalf("newer claim=%v err=%v", claimed, err)
	}
	if err := store.Delete(ctx, TargetUGC, first.ID); err != nil {
		t.Fatal(err)
	}
	var claims int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedule_occurrence_claims WHERE schedule_id=$1`, first.ID).Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("claims=%d err=%v", claims, err)
	}
	if _, err := store.Get(ctx, TargetUGC, first.ID); !errors.Is(err, ErrScheduleMissing) {
		t.Fatalf("deleted get=%v", err)
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
