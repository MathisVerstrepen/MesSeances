package syncschedule

import (
	"testing"
	"time"
)

func TestCinemovidaScheduleAvailabilityAndDisabledCreation(t *testing.T) {
	store := newMemoryScheduleStore()
	starter := &testStarter{available: []Target{TargetMetadataRefresh, TargetCinemovida, TargetCineode}, calls: make(chan Occurrence, 1)}
	service := newTestService(t, store, starter, newTestScheduler())
	defer service.Close()
	targets := service.AvailableTargets()
	if len(targets) != 3 || targets[0] != TargetCineode || targets[1] != TargetCinemovida || targets[2] != TargetMetadataRefresh {
		t.Fatal("available order", targets)
	}
	rows, err := service.List(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatal("schedule seeded", err)
	}
	row, err := service.Create(t.Context(), TargetCinemovida, false, Definition{Kind: KindDaily, Time: "03:00"})
	if err != nil || row.Enabled {
		t.Fatal("disabled creation", err)
	}
	row, err = service.Update(t.Context(), TargetCinemovida, row.ID, false, Definition{Kind: KindDaily, Time: "04:00"})
	if err != nil || row.Enabled || row.Revision != 2 {
		t.Fatal("disabled update", err)
	}
	if len(starter.calls) != 0 {
		t.Fatal("disabled schedule executed")
	}
	if err := service.Delete(t.Context(), TargetCinemovida, row.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCinemovidaScheduleOrderAndOccurrenceIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	store := NewPostgresStore(pool)
	for _, target := range []Target{TargetMetadataRefresh, TargetCinemovida, TargetCineode} {
		if row, err := store.Create(ctx, Schedule{Target: target, Definition: Definition{Kind: KindDaily, Time: "03:00"}}); err != nil || row.Enabled {
			t.Fatal("disabled creation", err)
		}
	}
	rows, err := store.List(ctx)
	if err != nil || len(rows) != 3 || rows[0].Target != TargetCineode || rows[1].Target != TargetCinemovida || rows[2].Target != TargetMetadataRefresh {
		t.Fatal("store order", err)
	}
	row := rows[1]
	row.Enabled = true
	row, err = store.Update(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	o := Occurrence{ScheduleID: row.ID, Target: TargetCinemovida, Revision: row.Revision, ScheduledFor: time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || !claimed {
		t.Fatal("claim", err)
	}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || claimed {
		t.Fatal("duplicate claim", err)
	}
	if err := store.Delete(ctx, TargetCinemovida, row.ID); err != nil {
		t.Fatal(err)
	}
}
