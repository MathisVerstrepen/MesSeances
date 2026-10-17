package syncschedule

import (
	"testing"
	"time"
)

func TestOcineScheduleAvailabilityAndDisabledCreation(t *testing.T) {
	store := newMemoryScheduleStore()
	starter := &testStarter{available: []Target{TargetMetadataRefresh, TargetOcine, TargetCinemovida}, calls: make(chan Occurrence, 1)}
	service := newTestService(t, store, starter, newTestScheduler())
	defer service.Close()
	targets := service.AvailableTargets()
	if len(targets) != 3 || targets[0] != TargetCinemovida || targets[1] != TargetOcine || targets[2] != TargetMetadataRefresh {
		t.Fatal("available order", targets)
	}
	rows, err := service.List(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatal("schedule seeded", err)
	}
	row, err := service.Create(t.Context(), TargetOcine, false, Definition{Kind: KindDaily, Time: "03:00"})
	if err != nil || row.Enabled {
		t.Fatal("disabled create", err)
	}
	row, err = service.Update(t.Context(), TargetOcine, row.ID, true, Definition{Kind: KindDaily, Time: "04:00"})
	if err != nil || !row.Enabled || row.Revision != 2 {
		t.Fatal("enable", err)
	}
	if err := service.Delete(t.Context(), TargetOcine, row.ID); err != nil {
		t.Fatal(err)
	}
}
func TestOcineScheduleOrderAndOccurrenceIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	store := NewPostgresStore(pool)
	for _, target := range []Target{TargetMetadataRefresh, TargetOcine, TargetCinemovida} {
		if row, err := store.Create(ctx, Schedule{Target: target, Definition: Definition{Kind: KindDaily, Time: "03:00"}}); err != nil || row.Enabled {
			t.Fatal("disabled creation", err)
		}
	}
	rows, err := store.List(ctx)
	if err != nil || len(rows) != 3 || rows[0].Target != TargetCinemovida || rows[1].Target != TargetOcine || rows[2].Target != TargetMetadataRefresh {
		t.Fatal("store order", err)
	}
	row := rows[1]
	row.Enabled = true
	row, err = store.Update(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	o := Occurrence{ScheduleID: row.ID, Target: TargetOcine, Revision: row.Revision, ScheduledFor: time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || !claimed {
		t.Fatal("claim", err)
	}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || claimed {
		t.Fatal("duplicate claim", err)
	}
	if err := store.Delete(ctx, TargetOcine, row.ID); err != nil {
		t.Fatal(err)
	}
}
