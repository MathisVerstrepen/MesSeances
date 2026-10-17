package main

import (
	"testing"
	"time"

	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

func TestMajesticScheduleStarterDispatch(t *testing.T) {
	provider := &fakeProviderScheduleStarter{}
	s := syncScheduleStarter{providers: provider}
	targets := s.AvailableTargets()
	if len(targets) != 13 || targets[10] != syncschedule.TargetVeo || targets[11] != syncschedule.TargetMajestic || targets[12] != syncschedule.TargetCineode {
		t.Fatal(targets)
	}
	result, err := s.StartScheduled(syncschedule.Occurrence{ScheduleID: 1, Target: syncschedule.TargetMajestic, Revision: 1, ScheduledFor: time.Now()})
	if err != nil || !(<-result).Succeeded || provider.occurrence.Provider != synccontrol.TargetMajestic {
		t.Fatal("dispatch", err)
	}
	result, err = s.StartScheduled(syncschedule.Occurrence{ScheduleID: 2, Target: syncschedule.TargetCineode, Revision: 3, ScheduledFor: time.Now(), Attempt: 1})
	if err != nil || !(<-result).Succeeded || provider.occurrence.Provider != synccontrol.TargetCineode || provider.occurrence.ScheduleID != 2 || provider.occurrence.Revision != 3 || provider.occurrence.Attempt != 1 {
		t.Fatal("Cineode dispatch", err)
	}
}
