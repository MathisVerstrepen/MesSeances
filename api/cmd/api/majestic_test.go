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
	if len(targets) != 12 || targets[10] != syncschedule.TargetVeo || targets[11] != syncschedule.TargetMajestic {
		t.Fatal(targets)
	}
	result, err := s.StartScheduled(syncschedule.Occurrence{ScheduleID: 1, Target: syncschedule.TargetMajestic, Revision: 1, ScheduledFor: time.Now()})
	if err != nil || !(<-result).Succeeded || provider.occurrence.Provider != synccontrol.TargetMajestic {
		t.Fatal("dispatch", err)
	}
}
