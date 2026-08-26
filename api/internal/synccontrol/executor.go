package synccontrol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"messeances/api/internal/cgr"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/pathe"
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
	NewPathe         func() (pathe.Getter, error)
	NewCGR           func() (cgr.Getter, error)
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
	newPathe         func() (pathe.Getter, error)
	newCGR           func() (cgr.Getter, error)
	enrich           EnrichFunc
	now              func() time.Time
	logger           *slog.Logger
	observer         SyncObserver
	operationTimeout time.Duration
	syncUGC          func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	syncKinepolis    func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error)
	syncPathe        func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error)
	syncCGR          func(context.Context, cgr.Getter, cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error)
}

func NewProductionExecutor(options ProductionExecutorOptions) (*ProductionExecutor, error) {
	if options.Writer == nil || options.Now == nil || options.Logger == nil || options.OperationTimeout <= 0 || (options.NewUGC == nil && options.NewKinepolis == nil && options.NewPathe == nil && options.NewCGR == nil) {
		return nil, fmt.Errorf("sync executor dependencies are required")
	}
	return &ProductionExecutor{
		writer: options.Writer, newUGC: options.NewUGC, newKinepolis: options.NewKinepolis, newPathe: options.NewPathe, newCGR: options.NewCGR,
		enrich: options.Enrich, now: options.Now, logger: options.Logger, observer: options.Observer, operationTimeout: options.OperationTimeout,
		syncUGC: ugc.Sync, syncKinepolis: kinepolis.Sync, syncPathe: pathe.Sync, syncCGR: cgr.Sync,
	}, nil
}

func (e *ProductionExecutor) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	started := time.Now()
	providers := []Target{target}
	if target == TargetAll {
		providers = []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR}
	} else if target != TargetUGC && target != TargetKinepolis && target != TargetPathe && target != TargetCGR {
		return nil, newProviderRunError("", StageOrchestration, FailureInternal, ErrInvalidTarget)
	}
	datasets := make([]schedule.Dataset, 0, len(providers))
	syncOutcomes := make(map[Target]SyncOutcome, len(providers))
	providerLogs := make(map[Target][]string, len(providers))
	for _, provider := range providers {
		lines := []string{lifecycleLog(e.now().UTC(), provider, eventProviderStarted)}
		data, syncOutcome, err := e.prepare(ctx, provider, window, &lines)
		if err != nil {
			var runErr *RunError
			stage := StageOrchestration
			if errors.As(err, &runErr) {
				stage = runErr.Stage
			}
			lines = append(lines, failureLog(e.now().UTC(), provider, stage, failureDetails(provider, stage, err, syncOutcome)))
			providerLogs[provider] = lines
			e.observe(provider, ProviderOutcome{Sync: syncOutcome}, err, time.Since(started))
			return nil, attachRunLogs(err, providerLogs)
		}
		datasets = append(datasets, data)
		syncOutcomes[provider] = syncOutcome
		providerLogs[provider] = lines
	}
	for _, provider := range providers {
		providerLogs[provider] = append(providerLogs[provider], lifecycleLog(e.now().UTC(), provider, eventPublicationStarted))
	}
	writeCtx, cancel := context.WithTimeout(ctx, e.operationTimeout)
	publication, err := e.writer.Replace(writeCtx, datasets)
	cancel()
	if err != nil {
		runErr := stageError(ctx, "", StagePublication, FailureReplacement, err)
		for _, provider := range providers {
			failure := failureDetails(provider, StagePublication, runErr, syncOutcomes[provider])
			providerLogs[provider] = append(providerLogs[provider], failureLog(e.now().UTC(), provider, StagePublication, failure))
			e.observe(provider, ProviderOutcome{Sync: syncOutcomes[provider]}, runErr, time.Since(started))
		}
		return nil, attachRunLogs(runErr, providerLogs)
	}
	outcomes := make(map[Target]ProviderOutcome, len(providers))
	for i, provider := range providers {
		syncOutcome := syncOutcomes[provider]
		metrics, ok := publication.Providers[schedule.Provider(provider)]
		if !ok {
			runErr := newProviderRunError(provider, StagePublication, FailureReplacement, nil)
			failure := fallbackFailure(StagePublication, FailureReplacement)
			failure.Progress = outcomeProgress(syncOutcome)
			providerLogs[provider] = append(providerLogs[provider], failureLog(e.now().UTC(), provider, StagePublication, failure))
			return nil, attachRunLogs(runErr, providerLogs)
		}
		syncOutcome.Version = publication.Version
		syncOutcome.Movies = metrics.Movies
		syncOutcome.NewMovies = metrics.NewMovies
		syncOutcome.Showtimes = metrics.Showtimes
		syncOutcome.NewShowtimes = metrics.NewShowtimes
		outcome := ProviderOutcome{Sync: syncOutcome, Enrichment: e.runEnrichment(ctx, provider, datasets[i])}
		outcomes[provider] = outcome
		e.observe(provider, outcome, nil, time.Since(started))
	}
	return outcomes, nil
}

func (e *ProductionExecutor) prepare(ctx context.Context, provider Target, window Window, lines *[]string) (schedule.Dataset, SyncOutcome, error) {
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
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary ugc.SyncSummary
		data, summary, err = e.syncUGC(ctx, client, ugc.SyncOptions{From: window.From, Now: e.now()})
		requests := summary.Requests
		if count := client.RequestCount(); count > requests {
			requests = count
		}
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Dates: summary.Dates, Requests: requests, Showtimes: summary.Showtimes, Skipped: summary.Skipped, GeneratedAt: summary.GeneratedAt}
	case TargetKinepolis:
		if e.newKinepolis == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newKinepolis()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary kinepolis.SyncSummary
		data, summary, err = e.syncKinepolis(ctx, client, kinepolis.SyncOptions{From: window.From, Now: e.now()})
		requests := 0
		if counter, ok := client.(interface{ RequestCount() int }); ok {
			requests = counter.RequestCount()
		}
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Requests: requests, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetPathe:
		if e.newPathe == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newPathe()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary pathe.SyncSummary
		data, summary, err = e.syncPathe(ctx, client, pathe.SyncOptions{From: window.From, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Requests: summary.Requests, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetCGR:
		if e.newCGR == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newCGR()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary cgr.SyncSummary
		data, summary, err = e.syncCGR(ctx, client, cgr.SyncOptions{From: window.From, Now: e.now()})
		requests := summary.Requests
		if count := client.RequestCount(); count > requests {
			requests = count
		}
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: requests, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	default:
		return data, outcome, newProviderRunError(provider, StageOrchestration, FailureInternal, ErrInvalidTarget)
	}
	if err != nil {
		if errors.Is(err, schedule.ErrDatasetValidation) {
			*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventFetchSucceeded))
			return data, outcome, stageError(ctx, provider, StageDatasetValidation, FailureDatasetRejected, err)
		}
		return data, outcome, stageError(ctx, provider, StageProviderFetch, FailureProviderSync, err)
	}
	*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventFetchSucceeded))
	if data.Scope != schedule.ScopeAll || data.Provider != schedule.Provider(provider) || schedule.ValidateDataset(data, true) != nil {
		return data, outcome, newProviderRunError(provider, StageDatasetValidation, FailureDatasetRejected, nil)
	}
	*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventValidationSucceeded))
	outcome.Through = data.Window.Through
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
	records := map[string]int{"cinemas": outcome.Sync.Cinemas, "movies": outcome.Sync.Movies, "new_movies": outcome.Sync.NewMovies, "dates": outcome.Sync.Dates, "requests": outcome.Sync.Requests, "showtimes": outcome.Sync.Showtimes, "new_showtimes": outcome.Sync.NewShowtimes, "skipped": outcome.Sync.Skipped}
	logArgs := []any{"component", "sync", "provider", string(provider), "result", result, "stage", string(stage), "error_code", string(code), "duration", duration.Seconds(), "cinemas", records["cinemas"], "movies", records["movies"], "new_movies", records["new_movies"], "dates", records["dates"], "requests", records["requests"], "showtimes", records["showtimes"], "new_showtimes", records["new_showtimes"], "skipped", records["skipped"]}
	if err != nil {
		result = "failed"
		code = FailureInternal
		var runError *RunError
		if errors.As(err, &runError) {
			code = runError.Code
			stage = runError.Stage
		}
		logArgs[5], logArgs[7], logArgs[9] = result, string(stage), string(code)
		logArgs = append(logArgs, providerFetchLogArgs(provider, err)...)
		e.logger.Error("sync_run_completed", logArgs...)
	} else {
		e.logger.Info("sync_run_completed", logArgs...)
	}
	if e.observer != nil {
		e.observer.ObserveSync(string(provider), result, string(stage), string(code), string(outcome.Enrichment.Status), duration, records)
	}
}

func providerFetchLogArgs(provider Target, err error) []any {
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Stage != StageProviderFetch {
		return nil
	}
	details := failureDetails(provider, runErr.Stage, err, SyncOutcome{})
	args := []any{"fetch_category", string(details.Category), "request_operation", string(details.Operation)}
	if details.HTTPStatus >= 100 && details.HTTPStatus <= 599 {
		args = append(args, "http_status", details.HTTPStatus)
	}
	if details.Attempt >= 1 && details.AttemptLimit >= details.Attempt && details.AttemptLimit <= 10 {
		args = append(args, "attempt", details.Attempt, "attempt_limit", details.AttemptLimit)
	}
	return args
}

func safePatheFetchCategory(category pathe.ErrorCategory) logCategory {
	switch category {
	case pathe.CategoryCanceled:
		return categoryCanceled
	case pathe.CategoryInvalidURL:
		return categoryInvalidURL
	case pathe.CategoryNoProxy:
		return categoryTransportUnavailable
	case pathe.CategoryTransport:
		return categoryTransport
	case pathe.CategoryRedirect:
		return categoryRedirect
	case pathe.CategoryResponseRead:
		return categoryResponseRead
	case pathe.CategoryResponseLarge:
		return categoryResponseTooLarge
	case pathe.CategoryChallenge:
		return categoryChallenge
	case pathe.CategoryServer, pathe.CategoryStatus:
		return categoryHTTPStatus
	case pathe.CategoryContentType:
		return categoryContentType
	case pathe.CategoryInvalidJSON:
		return categoryInvalidPayload
	case pathe.CategoryEmptyResponse:
		return categoryEmptyResponse
	default:
		return categoryUnknown
	}
}

func safePatheRequestOperation(operation pathe.Operation) logOperation {
	switch operation {
	case pathe.OperationCinemas:
		return operationCinemas
	case pathe.OperationShows:
		return operationProgram
	case pathe.OperationCinemaProgram:
		return operationProgram
	case pathe.OperationMovieTimes:
		return operationShowings
	case pathe.OperationEventTimes:
		return operationShowings
	default:
		return operationUnknown
	}
}

func attachRunLogs(err error, logs map[Target][]string) error {
	var runErr *RunError
	if errors.As(err, &runErr) {
		return runErr.withLogs(logs)
	}
	return err
}

func failureDetails(provider Target, stage FailureStage, err error, outcome SyncOutcome) logFailure {
	details := fallbackFailure(stage, FailureInternal)
	details.Progress = outcomeProgress(outcome)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		details.Category = categoryCanceled
		return details
	}
	if stage != StageProviderFetch {
		return details
	}
	details = logFailure{Operation: operationUnknown, Category: categoryUnknown, Progress: outcomeProgress(outcome)}
	switch provider {
	case TargetUGC:
		var requestErr *ugc.RequestError
		if errors.As(err, &requestErr) {
			details.Operation = safeUGCOperation(requestErr.Operation)
			details.Category = safeUGCCategory(requestErr.Category)
			details.HTTPStatus = requestErr.StatusCode
			details.Attempt = requestErr.Attempt
			details.AttemptLimit = requestErr.AttemptLimit
		}
	case TargetPathe:
		var requestErr *pathe.RequestError
		if errors.As(err, &requestErr) {
			details.Operation = safePatheRequestOperation(requestErr.Operation)
			details.Category = safePatheFetchCategory(requestErr.Category)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetCGR:
		var requestErr *cgr.RequestError
		if errors.As(err, &requestErr) {
			details.Operation = safeCGROperation(requestErr.Operation)
			details.Category = safeCGRCategory(requestErr.Category)
			details.HTTPStatus = requestErr.StatusCode
		}
	}
	if details.HTTPStatus < 100 || details.HTTPStatus > 599 || details.Category != categoryHTTPStatus {
		details.HTTPStatus = 0
	}
	if details.Attempt < 1 || details.AttemptLimit < details.Attempt || details.AttemptLimit > 10 {
		details.Attempt, details.AttemptLimit = 0, 0
	}
	return details
}

func outcomeProgress(outcome SyncOutcome) logProgress {
	progress := logProgress{}
	if outcome.Requests > 0 {
		progress.Requests = intPointer(outcome.Requests)
	}
	if outcome.Cinemas > 0 {
		progress.Cinemas = intPointer(outcome.Cinemas)
	}
	if outcome.Movies > 0 {
		progress.Movies = intPointer(outcome.Movies)
	}
	if outcome.Dates > 0 {
		progress.Dates = intPointer(outcome.Dates)
	}
	if outcome.Showtimes > 0 {
		progress.Showtimes = intPointer(outcome.Showtimes)
	}
	if outcome.Skipped > 0 {
		progress.Skipped = intPointer(outcome.Skipped)
	}
	return progress
}

func safeUGCOperation(operation ugc.Operation) logOperation {
	switch operation {
	case ugc.OperationSitemap:
		return operationSitemap
	case ugc.OperationCinema:
		return operationCinema
	case ugc.OperationShowings:
		return operationShowings
	default:
		return operationUnknown
	}
}

func safeUGCCategory(category ugc.ErrorCategory) logCategory {
	switch category {
	case ugc.CategoryCanceled:
		return categoryCanceled
	case ugc.CategoryInvalidURL:
		return categoryInvalidURL
	case ugc.CategoryTransportUnavailable:
		return categoryTransportUnavailable
	case ugc.CategoryTransport:
		return categoryTransport
	case ugc.CategoryRedirect:
		return categoryRedirect
	case ugc.CategoryResponseRead:
		return categoryResponseRead
	case ugc.CategoryResponseLarge:
		return categoryResponseTooLarge
	case ugc.CategoryChallenge:
		return categoryChallenge
	case ugc.CategoryHTTPStatus:
		return categoryHTTPStatus
	case ugc.CategoryInvalidPayload:
		return categoryInvalidPayload
	default:
		return categoryUnknown
	}
}

func safeCGROperation(operation cgr.Operation) logOperation {
	switch operation {
	case cgr.OperationCinemas:
		return operationCinemas
	case cgr.OperationProgram:
		return operationProgram
	case cgr.OperationSchedule:
		return operationShowings
	case cgr.OperationMovies:
		return operationMovies
	default:
		return operationUnknown
	}
}

func safeCGRCategory(category cgr.ErrorCategory) logCategory {
	switch category {
	case cgr.CategoryCanceled:
		return categoryCanceled
	case cgr.CategoryInvalidURL:
		return categoryInvalidURL
	case cgr.CategoryNoClient:
		return categoryTransportUnavailable
	case cgr.CategoryTransport:
		return categoryTransport
	case cgr.CategoryRedirect:
		return categoryRedirect
	case cgr.CategoryResponseRead:
		return categoryResponseRead
	case cgr.CategoryResponseLarge:
		return categoryResponseTooLarge
	case cgr.CategoryServer, cgr.CategoryStatus:
		return categoryHTTPStatus
	case cgr.CategoryContentType:
		return categoryContentType
	case cgr.CategoryInvalidJSON:
		return categoryInvalidPayload
	case cgr.CategoryEmptyResponse:
		return categoryEmptyResponse
	default:
		return categoryUnknown
	}
}

func enrichmentMovies(provider Target, data schedule.Dataset) []enrichment.Movie {
	unique := make(map[string]enrichment.Movie)
	for _, showing := range data.Showtimes {
		if showing.Movie.RuntimeMinutes == 0 {
			continue
		}
		movie, found := unique[showing.Movie.ProviderID]
		if !found {
			movie = enrichment.Movie{SourceProvider: string(provider), ProviderID: showing.Movie.ProviderID, Title: showing.Movie.Title, RuntimeMinutes: showing.Movie.RuntimeMinutes}
		}
		if !showing.StartTime.IsZero() && (movie.FirstShowingAt.IsZero() || showing.StartTime.Before(movie.FirstShowingAt)) {
			movie.FirstShowingAt = showing.StartTime
		}
		unique[showing.Movie.ProviderID] = movie
	}
	movies := make([]enrichment.Movie, 0, len(unique))
	for _, movie := range unique {
		movies = append(movies, movie)
	}
	return movies
}
