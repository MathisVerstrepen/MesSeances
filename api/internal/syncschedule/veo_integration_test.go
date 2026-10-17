package syncschedule

import (
	"errors"
	"testing"
	"time"
)

func TestVeoScheduleCRUDIntegration(t *testing.T) {
	ctx, pool := scheduleIntegrationPool(t)
	store := NewPostgresStore(pool)
	for _, target := range []Target{TargetMetadataRefresh, TargetVeo, TargetNoeCinemas} {
		if row, err := store.Create(ctx, Schedule{Target: target, Definition: Definition{Kind: KindDaily, Time: "03:00"}}); err != nil || row.Enabled {
			t.Fatal("disabled creation", err)
		}
	}
	rows, err := store.List(ctx)
	if err != nil || len(rows) != 3 || rows[0].Target != TargetNoeCinemas || rows[1].Target != TargetVeo || rows[2].Target != TargetMetadataRefresh {
		t.Fatal("Véo order", err)
	}
	row, err := store.Get(ctx, TargetVeo, rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	o := Occurrence{ScheduleID: row.ID, Target: TargetVeo, Revision: row.Revision, ScheduledFor: time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || claimed {
		t.Fatal("disabled occurrence", err)
	}
	row.Enabled = true
	row, err = store.Update(ctx, row)
	if err != nil || row.Revision != 2 {
		t.Fatal(err)
	}
	o.Revision = row.Revision
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || !claimed {
		t.Fatal("first claim", err)
	}
	if claimed, err := store.ClaimOccurrence(ctx, o); err != nil || claimed {
		t.Fatal("duplicate claim", err)
	}
	if err := store.Delete(ctx, TargetVeo, row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, TargetVeo, row.ID); !errors.Is(err, ErrScheduleMissing) {
		t.Fatal(err)
	}
}
