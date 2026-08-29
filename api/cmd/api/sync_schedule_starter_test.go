package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

type fakeProviderScheduleStarter struct {
	occurrence synccontrol.Occurrence
	err        error
}

func (s *fakeProviderScheduleStarter) StartScheduled(occurrence synccontrol.Occurrence) (synccontrol.Status, <-chan synccontrol.Completion, error) {
	s.occurrence = occurrence
	if s.err != nil {
		return synccontrol.Status{}, nil, s.err
	}
	completion := make(chan synccontrol.Completion, 1)
	completion <- synccontrol.Completion{Status: synccontrol.Status{State: synccontrol.StateSucceeded}}
	close(completion)
	return synccontrol.Status{State: synccontrol.StateRunning}, completion, nil
}

type fakeMetadataScheduleStarter struct {
	claims int
	err    error
}

func (s *fakeMetadataScheduleStarter) StartScheduled(claim enrichment.MetadataRefreshClaim) (<-chan enrichment.MetadataRefreshCompletion, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.claims++
	claimed, err := claim(context.Background())
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, enrichment.ErrMetadataRefreshOccurrenceClaimed
	}
	completion := make(chan enrichment.MetadataRefreshCompletion, 1)
	completion <- enrichment.MetadataRefreshCompletion{Succeeded: true}
	close(completion)
	return completion, nil
}

type fakeScheduleClaimer struct {
	calls      int
	occurrence syncschedule.Occurrence
	claimed    bool
	err        error
}

func (c *fakeScheduleClaimer) ClaimOccurrence(_ context.Context, occurrence syncschedule.Occurrence) (bool, error) {
	c.calls++
	c.occurrence = occurrence
	return c.claimed, c.err
}

func TestSyncScheduleStarterAvailabilityAndProviderMapping(t *testing.T) {
	providers := &fakeProviderScheduleStarter{}
	metadata := &fakeMetadataScheduleStarter{}
	claimer := &fakeScheduleClaimer{claimed: true}
	starter := syncScheduleStarter{providers: providers, metadata: metadata, claimer: claimer}
	targets := starter.AvailableTargets()
	if len(targets) != 5 || targets[0] != syncschedule.TargetUGC || targets[4] != syncschedule.TargetMetadataRefresh {
		t.Fatalf("targets=%v", targets)
	}
	occurrence := syncschedule.Occurrence{ScheduleID: 12, Target: syncschedule.TargetPathe, Revision: 3, ScheduledFor: time.Now(), Attempt: 1}
	completion, err := starter.StartScheduled(occurrence)
	if err != nil {
		t.Fatal(err)
	}
	result := <-completion
	if !result.Succeeded || providers.occurrence.ScheduleID != 12 || providers.occurrence.Provider != synccontrol.TargetPathe || providers.occurrence.Attempt != 1 {
		t.Fatalf("provider occurrence=%+v result=%+v", providers.occurrence, result)
	}
}

func TestSyncScheduleStarterMetadataClaimsOnlyAttemptZero(t *testing.T) {
	metadata := &fakeMetadataScheduleStarter{}
	claimer := &fakeScheduleClaimer{claimed: true}
	starter := syncScheduleStarter{metadata: metadata, claimer: claimer}
	occurrence := syncschedule.Occurrence{ScheduleID: 8, Target: syncschedule.TargetMetadataRefresh, Revision: 2, ScheduledFor: time.Now(), Attempt: 0}
	completion, err := starter.StartScheduled(occurrence)
	if err != nil || !(<-completion).Succeeded || claimer.calls != 1 || claimer.occurrence.ScheduleID != 8 {
		t.Fatalf("attempt zero completion=%v err=%v claims=%d occurrence=%+v", completion, err, claimer.calls, claimer.occurrence)
	}
	occurrence.Attempt = 1
	completion, err = starter.StartScheduled(occurrence)
	if err != nil || !(<-completion).Succeeded || claimer.calls != 1 {
		t.Fatalf("retry err=%v claims=%d", err, claimer.calls)
	}
}

func TestSyncScheduleStarterMapsContentionAndClaims(t *testing.T) {
	starter := syncScheduleStarter{providers: &fakeProviderScheduleStarter{err: synccontrol.ErrInProgress}}
	_, err := starter.StartScheduled(syncschedule.Occurrence{Target: syncschedule.TargetUGC})
	if !errors.Is(err, syncschedule.ErrInProgress) {
		t.Fatalf("contention=%v", err)
	}
	starter = syncScheduleStarter{metadata: &fakeMetadataScheduleStarter{}, claimer: &fakeScheduleClaimer{claimed: false}}
	_, err = starter.StartScheduled(syncschedule.Occurrence{Target: syncschedule.TargetMetadataRefresh})
	if !errors.Is(err, syncschedule.ErrOccurrenceClaimed) {
		t.Fatalf("claim=%v", err)
	}
}
