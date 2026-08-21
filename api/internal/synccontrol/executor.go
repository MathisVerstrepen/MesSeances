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

const operationTimeout = 2 * time.Minute

type EnrichFunc func(context.Context, []enrichment.Movie) (*enrichment.Summary, error)

type SyncObserver interface {
	ObserveSync(provider, result, errorCode, enrichmentStatus string, duration time.Duration, records map[string]int)
}

type ProductionExecutorOptions struct {
	Writer       schedule.SnapshotWriter
	NewUGC       func() (ugc.Getter, error)
	NewKinepolis func() (kinepolis.Fetcher, error)
	Enrich       EnrichFunc
	Now          func() time.Time
	Logger       *slog.Logger
	Observer     SyncObserver
}

type ProductionExecutor struct {
	writer        schedule.SnapshotWriter
	newUGC        func() (ugc.Getter, error)
	newKinepolis  func() (kinepolis.Fetcher, error)
	enrich        EnrichFunc
	now           func() time.Time
	logger        *slog.Logger
	observer      SyncObserver
	syncUGC       func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	syncKinepolis func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error)
}

func NewProductionExecutor(options ProductionExecutorOptions) (*ProductionExecutor, error) {
	if options.Writer == nil || options.Now == nil || options.Logger == nil || (options.NewUGC == nil && options.NewKinepolis == nil) {
		return nil, fmt.Errorf("sync executor dependencies are required")
	}
	return &ProductionExecutor{
		writer: options.Writer, newUGC: options.NewUGC, newKinepolis: options.NewKinepolis,
		enrich: options.Enrich, now: options.Now, logger: options.Logger, observer: options.Observer,
		syncUGC: ugc.Sync, syncKinepolis: kinepolis.Sync,
	}, nil
}

func (e *ProductionExecutor) Run(ctx context.Context, provider Target, window Window) (outcome ProviderOutcome, runErr error) {
	started := time.Now()
	defer func() { e.observe(provider, outcome, runErr, time.Since(started)) }()

	var data schedule.Dataset
	var syncOutcome SyncOutcome
	var err error
	switch provider {
	case TargetUGC:
		if e.newUGC == nil {
			return outcome, NewRunError(FailureInternal, nil)
		}
		client, clientErr := e.newUGC()
		if clientErr != nil {
			return outcome, NewRunError(FailureClientCreation, clientErr)
		}
		var summary ugc.SyncSummary
		data, summary, err = e.syncUGC(ctx, client, ugc.SyncOptions{From: window.From, Through: window.Through, Now: e.now()})
		syncOutcome = SyncOutcome{Cinemas: summary.Cinemas, Dates: summary.Dates, Requests: summary.Requests, Showtimes: summary.Showtimes, Skipped: summary.Skipped, GeneratedAt: summary.GeneratedAt}
	case TargetKinepolis:
		if e.newKinepolis == nil {
			return outcome, NewRunError(FailureInternal, nil)
		}
		client, clientErr := e.newKinepolis()
		if clientErr != nil {
			return outcome, NewRunError(FailureClientCreation, clientErr)
		}
		var summary kinepolis.SyncSummary
		data, summary, err = e.syncKinepolis(ctx, client, kinepolis.SyncOptions{From: window.From, Through: window.Through, Now: e.now()})
		syncOutcome = SyncOutcome{Cinemas: summary.Cinemas, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	default:
		return outcome, NewRunError(FailureInternal, ErrInvalidTarget)
	}
	if err != nil {
		return outcome, stageError(ctx, FailureProviderSync, err)
	}
	if data.Scope != schedule.ScopeAll || data.Provider != string(provider) || schedule.ValidateDataset(data, true) != nil {
		return outcome, NewRunError(FailureDatasetRejected, nil)
	}
	writeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	version, err := e.writer.Replace(writeCtx, data)
	cancel()
	if err != nil {
		return outcome, stageError(ctx, FailureReplacement, err)
	}
	syncOutcome.Version = version
	outcome.Sync = syncOutcome
	outcome.Enrichment = e.runEnrichment(ctx, provider, data)
	return outcome, nil
}

func (e *ProductionExecutor) runEnrichment(ctx context.Context, provider Target, data schedule.Dataset) EnrichmentOutcome {
	if e.enrich == nil {
		return EnrichmentOutcome{Status: "skipped"}
	}
	enrichCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	summary, err := e.enrich(enrichCtx, enrichmentMovies(provider, data))
	cancel()
	if summary == nil && err == nil {
		return EnrichmentOutcome{Status: "skipped"}
	}
	outcome := EnrichmentOutcome{Status: "complete"}
	if summary != nil {
		outcome.Counts = &EnrichmentCounts{Reused: summary.Reused, Matched: summary.Matched, ReviewRequired: summary.ReviewRequired, Unmatched: summary.Unmatched, Failed: summary.Failed}
	}
	if err != nil {
		outcome.Status = "degraded"
		e.logger.WarnContext(ctx, "sync_enrichment_degraded", "component", "sync", "provider", string(provider), "error_code", "enrichment_failed")
	}
	return outcome
}

func stageError(ctx context.Context, code FailureCode, cause error) error {
	if ctx.Err() != nil {
		return NewRunError(FailureCanceled, cause)
	}
	return NewRunError(code, cause)
}

func (e *ProductionExecutor) observe(provider Target, outcome ProviderOutcome, err error, duration time.Duration) {
	result, code := "succeeded", FailureNone
	if err != nil {
		result = "failed"
		code = FailureInternal
		var runError *RunError
		if errors.As(err, &runError) {
			code = runError.Code
		}
		e.logger.Error("sync_run_completed", "component", "sync", "provider", string(provider), "result", result, "error_code", string(code), "duration", duration.Seconds())
	} else {
		e.logger.Info("sync_run_completed", "component", "sync", "provider", string(provider), "result", result, "error_code", string(code), "duration", duration.Seconds())
	}
	if e.observer != nil {
		records := map[string]int{"cinemas": outcome.Sync.Cinemas, "dates": outcome.Sync.Dates, "requests": outcome.Sync.Requests, "showtimes": outcome.Sync.Showtimes, "skipped": outcome.Sync.Skipped}
		e.observer.ObserveSync(string(provider), result, string(code), outcome.Enrichment.Status, duration, records)
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
