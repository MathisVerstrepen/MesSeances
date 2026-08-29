package main

import (
	"context"
	"errors"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

type providerScheduleStarter interface {
	StartScheduled(synccontrol.Occurrence) (synccontrol.Status, <-chan synccontrol.Completion, error)
}

type metadataScheduleStarter interface {
	StartScheduled(enrichment.MetadataRefreshClaim) (<-chan enrichment.MetadataRefreshCompletion, error)
}

type scheduleOccurrenceClaimer interface {
	ClaimOccurrence(context.Context, syncschedule.Occurrence) (bool, error)
}

type syncScheduleStarter struct {
	providers providerScheduleStarter
	metadata  metadataScheduleStarter
	claimer   scheduleOccurrenceClaimer
}

func (s syncScheduleStarter) AvailableTargets() []syncschedule.Target {
	targets := make([]syncschedule.Target, 0, 5)
	if s.providers != nil {
		targets = append(targets, syncschedule.TargetUGC, syncschedule.TargetKinepolis, syncschedule.TargetPathe, syncschedule.TargetCGR)
	}
	if s.metadata != nil && s.claimer != nil {
		targets = append(targets, syncschedule.TargetMetadataRefresh)
	}
	return targets
}

func (s syncScheduleStarter) StartScheduled(occurrence syncschedule.Occurrence) (<-chan syncschedule.Completion, error) {
	if occurrence.Target == syncschedule.TargetMetadataRefresh {
		return s.startMetadata(occurrence)
	}
	if s.providers == nil {
		return nil, syncschedule.ErrTargetUnavailable
	}
	provider := synccontrol.Target(occurrence.Target)
	_, completion, err := s.providers.StartScheduled(synccontrol.Occurrence{
		ScheduleID: occurrence.ScheduleID,
		Provider:   provider, Revision: occurrence.Revision,
		ScheduledFor: occurrence.ScheduledFor, Attempt: occurrence.Attempt,
	})
	if err != nil {
		return nil, mapScheduleStartError(err)
	}
	if completion == nil {
		return nil, nil
	}
	result := make(chan syncschedule.Completion, 1)
	go func() {
		providerCompletion, ok := <-completion
		if ok {
			result <- syncschedule.Completion{
				Succeeded:         providerCompletion.Status.State == synccontrol.StateSucceeded,
				FinalizationError: providerCompletion.FinalizationError,
			}
		}
		close(result)
	}()
	return result, nil
}

func (s syncScheduleStarter) startMetadata(occurrence syncschedule.Occurrence) (<-chan syncschedule.Completion, error) {
	if s.metadata == nil || s.claimer == nil {
		return nil, syncschedule.ErrTargetUnavailable
	}
	completion, err := s.metadata.StartScheduled(func(ctx context.Context) (bool, error) {
		if occurrence.Attempt > 0 {
			return true, nil
		}
		return s.claimer.ClaimOccurrence(ctx, occurrence)
	})
	if err != nil {
		return nil, mapScheduleStartError(err)
	}
	if completion == nil {
		return nil, nil
	}
	result := make(chan syncschedule.Completion, 1)
	go func() {
		metadataCompletion, ok := <-completion
		if ok {
			result <- syncschedule.Completion{Succeeded: metadataCompletion.Succeeded}
		}
		close(result)
	}()
	return result, nil
}

func mapScheduleStartError(err error) error {
	switch {
	case errors.Is(err, synccontrol.ErrInProgress), errors.Is(err, enrichment.ErrMetadataRefreshInProgress):
		return syncschedule.ErrInProgress
	case errors.Is(err, synccontrol.ErrOccurrenceClaimed), errors.Is(err, enrichment.ErrMetadataRefreshOccurrenceClaimed):
		return syncschedule.ErrOccurrenceClaimed
	default:
		return err
	}
}
