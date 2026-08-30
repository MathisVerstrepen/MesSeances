package database

import (
	"context"
	"testing"
	"time"
)

func TestMultiSyncSchedulesMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "movieflow_multi_schedule_migration_test_")
	installMigrationPrefix(t, ctx, pool, 28, "028_movie_imdb_id.sql")
	if _, err := pool.Exec(ctx, `INSERT INTO sync_schedules (provider,revision,enabled,schedule_kind,local_time) VALUES
		('ugc',3,true,'daily','08:00'),('kinepolis',2,false,'daily','19:00')`); err != nil {
		t.Fatal("insert legacy schedules failed")
	}
	var scheduledRunID, manualRunID int64
	scheduledFor := time.Date(2026, 8, 29, 6, 0, 0, 0, time.UTC)
	if err := pool.QueryRow(ctx, `INSERT INTO sync_runs
		(target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_revision,scheduled_for,schedule_attempt)
		VALUES ('ugc','failed','2026-08-29T06:00:00Z','2026-08-29T06:01:00Z','2026-08-29','2026-08-29','{}','scheduled',3,$1,0) RETURNING id`, scheduledFor).Scan(&scheduledRunID); err != nil {
		t.Fatal("insert legacy scheduled run failed")
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sync_runs
		(target,state,started_at,finished_at,window_from,window_through,providers)
		VALUES ('ugc','failed','2026-08-29T05:00:00Z','2026-08-29T05:01:00Z','2026-08-29','2026-08-29','{}') RETURNING id`).Scan(&manualRunID); err != nil {
		t.Fatal("insert legacy manual run failed")
	}

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run migration 029 failed: %v", err)
	}
	var ugcID, kinepolisID int64
	var ugcRevision int64
	if err := pool.QueryRow(ctx, `SELECT id,revision FROM sync_schedules WHERE target='ugc'`).Scan(&ugcID, &ugcRevision); err != nil || ugcID <= 0 || ugcRevision != 3 {
		t.Fatalf("migrated UGC id=%d revision=%d err=%v", ugcID, ugcRevision, err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM sync_schedules WHERE target='kinepolis'`).Scan(&kinepolisID); err != nil || kinepolisID <= 0 || kinepolisID == ugcID {
		t.Fatalf("migrated Kinepolis id=%d err=%v", kinepolisID, err)
	}
	var backfilledID *int64
	if err := pool.QueryRow(ctx, `SELECT schedule_id FROM sync_runs WHERE id=$1`, scheduledRunID).Scan(&backfilledID); err != nil || backfilledID == nil || *backfilledID != ugcID {
		t.Fatalf("scheduled run schedule_id=%v err=%v", backfilledID, err)
	}
	var manualScheduleID *int64
	if err := pool.QueryRow(ctx, `SELECT schedule_id FROM sync_runs WHERE id=$1`, manualRunID).Scan(&manualScheduleID); err != nil || manualScheduleID != nil {
		t.Fatalf("manual run schedule_id=%v err=%v", manualScheduleID, err)
	}

	var secondUGCID, metadataID int64
	if err := pool.QueryRow(ctx, `INSERT INTO sync_schedules (target,enabled,schedule_kind,local_time) VALUES ('ugc',true,'daily','20:00') RETURNING id`).Scan(&secondUGCID); err != nil {
		t.Fatal("insert duplicate target schedule failed")
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sync_schedules (target,enabled,schedule_kind,cron_expression) VALUES ('tmdb_metadata_refresh',false,'cron','0 3 * * *') RETURNING id`).Scan(&metadataID); err != nil {
		t.Fatal("insert metadata schedule failed")
	}
	runSQL := `INSERT INTO sync_runs
		(target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_id,schedule_revision,scheduled_for,schedule_attempt)
		VALUES ('ugc','failed','2026-08-30T06:00:00Z','2026-08-30T06:01:00Z','2026-08-30','2026-08-30','{}','scheduled',$1,1,'2026-08-30T06:00:00Z',0)`
	if _, err := pool.Exec(ctx, runSQL, ugcID); err != nil {
		t.Fatal("insert first per-entry occurrence failed")
	}
	if _, err := pool.Exec(ctx, runSQL, secondUGCID); err != nil {
		t.Fatal("insert second per-entry occurrence failed")
	}
	if _, err := pool.Exec(ctx, runSQL, secondUGCID); err == nil {
		t.Fatal("duplicate per-entry occurrence accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sync_schedule_occurrence_claims (schedule_id,schedule_revision,scheduled_for) VALUES ($1,1,'2026-08-30T06:00:00Z')`, metadataID); err != nil {
		t.Fatal("insert metadata claim failed")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM sync_schedules WHERE id=$1`, metadataID); err != nil {
		t.Fatal("delete metadata schedule failed")
	}
	var claimCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedule_occurrence_claims WHERE schedule_id=$1`, metadataID).Scan(&claimCount); err != nil || claimCount != 0 {
		t.Fatalf("claim cleanup count=%d err=%v", claimCount, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM sync_schedules WHERE id=$1`, ugcID); err != nil {
		t.Fatal("delete schedule with history failed")
	}
	var retained int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_runs WHERE schedule_id=$1`, ugcID).Scan(&retained); err != nil || retained < 2 {
		t.Fatalf("retained history=%d err=%v", retained, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sync_runs
		(target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_id)
		VALUES ('ugc','failed',now(),now(),CURRENT_DATE,CURRENT_DATE,'{}','manual',$1)`, secondUGCID); err == nil {
		t.Fatal("manual run with occurrence identity accepted")
	}
}
