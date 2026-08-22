package synccontrol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/schedule"
	"messeances/api/internal/ugc"
)

type EnrichFunc func(context.Context, []enrichment.Movie) (*enrichment.Summary, error)

type SyncObserver interface {
	ObserveSync(provider, result, stage, errorCode, enrichmentStatus string, duration time.Duration, records map[string]int)
}

type ProductionExecutorOptions struct {
	Writer           schedule.SnapshotWriter
	NewUGC           func() (ugc.Getter, error)
	NewKinepolis     func() (kinepolis.Fetcher, error)
	Enrich           EnrichFunc
	Now              func() time.Time
	Logger           *slog.Logger
	Observer         SyncObserver
	OperationTimeout time.Duration
}

type ProductionExecutor struct {
	writer           schedule.SnapshotWriter
	newUGC           func() (ugc.Getter, error)
	newKinepolis     func() (kinepolis.Fetcher, error)
	enrich           EnrichFunc
	now              func() time.Time
	logger           *slog.Logger
	observer         SyncObserver
	operationTimeout time.Duration
	syncUGC          func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	syncKinepolis    func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error)
}

func NewProductionExecutor(options ProductionExecutorOptions) (*ProductionExecutor, error) {
	if options.Writer == nil || options.Now == nil || options.Logger == nil || options.OperationTimeout <= 0 || (options.NewUGC == nil && options.NewKinepolis == nil) {
		return nil, fmt.Errorf("sync executor dependencies are required")
	}
	return &ProductionExecutor{
		writer: options.Writer, newUGC: options.NewUGC, newKinepolis: options.NewKinepolis,
		enrich: options.Enrich, now: options.Now, logger: options.Logger, observer: options.Observer, operationTimeout: options.OperationTimeout,
		syncUGC: ugc.Sync, syncKinepolis: kinepolis.Sync,
	}, nil
}

func (e *ProductionExecutor) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	started := time.Now()
	providers := []Target{target}
	if target == TargetAll {
		providers = []Target{TargetUGC, TargetKinepolis}
	} else if target != TargetUGC && target != TargetKinepolis {
		return nil, newProviderRunError("", StageOrchestration, FailureInternal, ErrInvalidTarget)
	}
	datasets := make([]schedule.Dataset, 0, len(providers))
	syncOutcomes := make(map[Target]SyncOutcome, len(providers))
	for _, provider := range providers {
		data, syncOutcome, err := e.prepare(ctx, provider, window)
		if err != nil {
			e.observe(provider, ProviderOutcome{}, err, time.Since(started))
			return nil, err
		}
		datasets = append(datasets, data)
		syncOutcomes[provider] = syncOutcome
	}
	writeCtx, cancel := context.WithTimeout(ctx, e.operationTimeout)
	version, err := e.writer.Replace(writeCtx, datasets)
	cancel()
	if err != nil {
		runErr := stageError(ctx, "", StagePublication, FailureReplacement, err)
		for _, provider := range providers {
			e.observe(provider, ProviderOutcome{}, runErr, time.Since(started))
		}
		return nil, runErr
	}
	outcomes := make(map[Target]ProviderOutcome, len(providers))
	for i, provider := range providers {
		syncOutcome := syncOutcomes[provider]
		syncOutcome.Version = version
		outcome := ProviderOutcome{Sync: syncOutcome, Enrichment: e.runEnrichment(ctx, provider, datasets[i])}
		outcomes[provider] = outcome
		e.observe(provider, outcome, nil, time.Since(started))
	}
	return outcomes, nil
}

func (e *ProductionExecutor) prepare(ctx context.Context, provider Target, window Window) (schedule.Dataset, SyncOutcome, error) {
	var data schedule.Dataset
	var outcome SyncOutcome
	var err error
	switch provider {
	case TargetUGC:
		if e.newUGC == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newUGC()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		var summary ugc.SyncSummary
		data, summary, err = e.syncUGC(ctx, client, ugc.SyncOptions{From: window.From, Through: window.Through, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Dates: summary.Dates, Requests: summary.Requests, Showtimes: summary.Showtimes, Skipped: summary.Skipped, GeneratedAt: summary.GeneratedAt}
	case TargetKinepolis:
		if e.newKinepolis == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newKinepolis()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		var summary kinepolis.SyncSummary
		data, summary, err = e.syncKinepolis(ctx, client, kinepolis.SyncOptions{From: window.From, Through: window.Through, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	default:
		return data, outcome, newProviderRunError(provider, StageOrchestration, FailureInternal, ErrInvalidTarget)
	}
	if err != nil {
		if errors.Is(err, schedule.ErrDatasetValidation) {
			return data, outcome, stageError(ctx, provider, StageDatasetValidation, FailureDatasetRejected, err)
		}
		return data, outcome, stageError(ctx, provider, StageProviderFetch, FailureProviderSync, err)
	}
	if data.Scope != schedule.ScopeAll || data.Provider != schedule.Provider(provider) || schedule.ValidateDataset(data, true) != nil {
		return data, outcome, newProviderRunError(provider, StageDatasetValidation, FailureDatasetRejected, nil)
	}
	return data, outcome, nil
}

func (e *ProductionExecutor) runEnrichment(ctx context.Context, provider Target, data schedule.Dataset) EnrichmentOutcome {
	started := time.Now()
	if e.enrich == nil {
		return EnrichmentOutcome{Status: EnrichmentSkipped}
	}
	enrichCtx, cancel := context.WithTimeout(ctx, e.operationTimeout)
	summary, err := e.enrich(enrichCtx, enrichmentMovies(provider, data))
	cancel()
	if summary == nil && err == nil {
		return EnrichmentOutcome{Status: EnrichmentSkipped}
	}
	outcome := EnrichmentOutcome{Status: EnrichmentComplete}
	if summary != nil {
		outcome.Counts = &EnrichmentCounts{Reused: summary.Reused, Matched: summary.Matched, ReviewRequired: summary.ReviewRequired, Unmatched: summary.Unmatched, Failed: summary.Failed}
	}
	if err != nil {
		outcome.Status = EnrichmentDegraded
		counts := EnrichmentCounts{}
		if outcome.Counts != nil {
			counts = *outcome.Counts
		}
		e.logger.WarnContext(ctx, "sync_enrichment_degraded", "component", "sync", "provider", string(provider), "result", "degraded", "stage", string(StageEnrichment), "error_code", "enrichment_failed", "duration", time.Since(started).Seconds(), "reused", counts.Reused, "matched", counts.Matched, "review_required", counts.ReviewRequired, "unmatched", counts.Unmatched, "failed", counts.Failed)
	}
	return outcome
}

func stageError(ctx context.Context, provider Target, stage FailureStage, code FailureCode, cause error) error {
	if ctx.Err() != nil {
		return newProviderRunError(provider, stage, FailureCanceled, cause)
	}
	return newProviderRunError(provider, stage, code, cause)
}

func (e *ProductionExecutor) observe(provider Target, outcome ProviderOutcome, err error, duration time.Duration) {
	result, code, stage := "succeeded", FailureNone, StageNone
	records := map[string]int{"cinemas": outcome.Sync.Cinemas, "dates": outcome.Sync.Dates, "requests": outcome.Sync.Requests, "showtimes": outcome.Sync.Showtimes, "skipped": outcome.Sync.Skipped}
	if err != nil {
		result = "failed"
		code = FailureInternal
		var runError *RunError
		if errors.As(err, &runError) {
			code = runError.Code
			stage = runError.Stage
		}
		e.logger.Error("sync_run_completed", "component", "sync", "provider", string(provider), "result", result, "stage", string(stage), "error_code", string(code), "duration", duration.Seconds(), "cinemas", records["cinemas"], "dates", records["dates"], "requests", records["requests"], "showtimes", records["showtimes"], "skipped", records["skipped"])
	} else {
		e.logger.Info("sync_run_completed", "component", "sync", "provider", string(provider), "result", result, "stage", string(stage), "error_code", string(code), "duration", duration.Seconds(), "cinemas", records["cinemas"], "dates", records["dates"], "requests", records["requests"], "showtimes", records["showtimes"], "skipped", records["skipped"])
	}
	if e.observer != nil {
		e.observer.ObserveSync(string(provider), result, string(stage), string(code), string(outcome.Enrichment.Status), duration, records)
	}
}

func enrichmentMovies(provider Target, data schedule.Dataset) []enrichment.Movie {
	unique := make(map[string]enrichment.Movie)
	for _, showing := range data.Showtimes {
		unique[showing.Movie.ProviderID] = enrichment.Movie{SourceProvider: string(provider), ProviderID: showing.Movie.ProviderID, Title: showing.Movie.Title, RuntimeMinutes: showing.Movie.RuntimeMinutes}
	}
	movies := make([]enrichment.Movie, 0, len(unique))
	for _, movie := range unique {
		movies = append(movies, movie)
	}
	return movies
}
