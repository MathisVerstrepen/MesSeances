package syncschedule

import (
	"testing"
	"time"
)

func TestMajesticScheduleOrderAndOccurrenceIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	store := NewPostgresStore(pool)
	for _, target := range []Target{TargetMetadataRefresh, TargetMajestic, TargetVeo} {
		if row, err := store.Create(ctx, Schedule{Target: target, Definition: Definition{Kind: KindDaily, Time: "03:00"}}); err != nil || row.Enabled {
			t.Fatal("disabled creation", err)
		}
	}
	rows, err := store.List(ctx)
	if err != nil || len(rows) != 3 || rows[0].Target != TargetVeo || rows[1].Target != TargetMajestic || rows[2].Target != TargetMetadataRefresh {
		t.Fatal("order", err)
	}
	row := rows[1]
	row.Enabled = true
	row, err = store.Update(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	o := Occurrence{ScheduleID: row.ID, Target: TargetMajestic, Revision: row.Revision, ScheduledFor: time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || !claimed {
		t.Fatal("claim", err)
	}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || claimed {
		t.Fatal("duplicate claim", err)
	}
	if err := store.Delete(ctx, TargetMajestic, row.ID); err != nil {
		t.Fatal(err)
	}
}
