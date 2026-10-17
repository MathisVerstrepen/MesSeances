package synccontrol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"messeances/api/internal/cgr"
	"messeances/api/internal/cinemovida"
	"messeances/api/internal/cineode"
	"messeances/api/internal/cineville"
	"messeances/api/internal/cinewest"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/grandecran"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/majestic"
	"messeances/api/internal/megarama"
	"messeances/api/internal/mk2"
	"messeances/api/internal/noecinemas"
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/ugc"
	"messeances/api/internal/veo"
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
	NewMegarama      func() (megarama.Getter, error)
	NewCineville     func() (cineville.Fetcher, error)
	NewMK2           func() (mk2.Fetcher, error)
	NewCinewest      func() (cinewest.Fetcher, error)
	NewGrandEcran    func() (grandecran.Getter, error)
	NewNoeCinemas    func() (noecinemas.Getter, error)
	NewVeo           func() (veo.Getter, error)
	NewMajestic      func() (majestic.Getter, error)
	NewCineode       func() (cineode.Getter, error)
	NewCinemovida    func() (cinemovida.Getter, error)
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
	newMegarama      func() (megarama.Getter, error)
	newCineville     func() (cineville.Fetcher, error)
	newMK2           func() (mk2.Fetcher, error)
	newCinewest      func() (cinewest.Fetcher, error)
	newGrandEcran    func() (grandecran.Getter, error)
	newNoeCinemas    func() (noecinemas.Getter, error)
	newVeo           func() (veo.Getter, error)
	newMajestic      func() (majestic.Getter, error)
	newCineode       func() (cineode.Getter, error)
	newCinemovida    func() (cinemovida.Getter, error)
	enrich           EnrichFunc
	now              func() time.Time
	logger           *slog.Logger
	observer         SyncObserver
	operationTimeout time.Duration
	syncUGC          func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	syncKinepolis    func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error)
	syncPathe        func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error)
	syncCGR          func(context.Context, cgr.Getter, cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error)
	syncMegarama     func(context.Context, megarama.Getter, megarama.SyncOptions) (schedule.Dataset, megarama.SyncSummary, error)
	syncCineville    func(context.Context, cineville.Fetcher, cineville.SyncOptions) (schedule.Dataset, cineville.SyncSummary, error)
	syncMK2          func(context.Context, mk2.Fetcher, mk2.SyncOptions) (schedule.Dataset, mk2.SyncSummary, error)
	syncCinewest     func(context.Context, cinewest.Fetcher, cinewest.SyncOptions) (schedule.Dataset, cinewest.SyncSummary, error)
	syncGrandEcran   func(context.Context, grandecran.Getter, grandecran.SyncOptions) (schedule.Dataset, grandecran.SyncSummary, error)
	syncNoeCinemas   func(context.Context, noecinemas.Getter, noecinemas.SyncOptions) (schedule.Dataset, noecinemas.SyncSummary, error)
	syncVeo          func(context.Context, veo.Getter, veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error)
	syncMajestic     func(context.Context, majestic.Getter, majestic.SyncOptions) (schedule.Dataset, majestic.SyncSummary, error)
	syncCineode      func(context.Context, cineode.Getter, cineode.SyncOptions) (schedule.Dataset, cineode.SyncSummary, error)
	syncCinemovida   func(context.Context, cinemovida.Getter, cinemovida.SyncOptions) (schedule.Dataset, cinemovida.SyncSummary, error)
}

func NewProductionExecutor(options ProductionExecutorOptions) (*ProductionExecutor, error) {
	if options.Writer == nil || options.Now == nil || options.Logger == nil || options.OperationTimeout <= 0 || (options.NewUGC == nil && options.NewKinepolis == nil && options.NewPathe == nil && options.NewCGR == nil && options.NewMegarama == nil && options.NewCineville == nil && options.NewMK2 == nil && options.NewCinewest == nil && options.NewGrandEcran == nil && options.NewNoeCinemas == nil && options.NewVeo == nil && options.NewMajestic == nil && options.NewCineode == nil && options.NewCinemovida == nil) {
		return nil, fmt.Errorf("sync executor dependencies are required")
	}
	return &ProductionExecutor{
		writer: options.Writer, newUGC: options.NewUGC, newKinepolis: options.NewKinepolis, newPathe: options.NewPathe, newCGR: options.NewCGR,
		newMegarama: options.NewMegarama, syncMegarama: megarama.Sync,
		newCineville: options.NewCineville, syncCineville: cineville.Sync,
		newMK2: options.NewMK2, syncMK2: mk2.Sync,
		newCinewest: options.NewCinewest, syncCinewest: cinewest.Sync,
		newGrandEcran: options.NewGrandEcran, syncGrandEcran: grandecran.Sync,
		newNoeCinemas: options.NewNoeCinemas, syncNoeCinemas: noecinemas.Sync,
		newVeo: options.NewVeo, syncVeo: veo.Sync,
		newMajestic: options.NewMajestic, syncMajestic: majestic.Sync,
		newCineode: options.NewCineode, syncCineode: cineode.Sync,
		newCinemovida: options.NewCinemovida, syncCinemovida: cinemovida.Sync,
		enrich: options.Enrich, now: options.Now, logger: options.Logger, observer: options.Observer, operationTimeout: options.OperationTimeout,
		syncUGC: ugc.Sync, syncKinepolis: kinepolis.Sync, syncPathe: pathe.Sync, syncCGR: cgr.Sync,
	}, nil
}

func (e *ProductionExecutor) Run(ctx context.Context, target Target, window Window) (map[Target]ProviderOutcome, error) {
	started := time.Now()
	providers := []Target{target}
	if target == TargetAll {
		providers = []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR, TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida}
	} else if !ValidTarget(target) {
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
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
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
	case TargetMegarama:
		if e.newMegarama == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newMegarama()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary megarama.SyncSummary
		data, summary, err = e.syncMegarama(ctx, client, megarama.SyncOptions{From: window.From, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetCinewest:
		if e.newCinewest == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newCinewest()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary cinewest.SyncSummary
		data, summary, err = e.syncCinewest(ctx, client, cinewest.SyncOptions{From: window.From, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, Skipped: summary.Skipped, GeneratedAt: summary.GeneratedAt}
	case TargetGrandEcran:
		if e.newGrandEcran == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newGrandEcran()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary grandecran.SyncSummary
		data, summary, err = e.syncGrandEcran(ctx, client, grandecran.SyncOptions{From: window.From, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetNoeCinemas:
		if e.newNoeCinemas == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newNoeCinemas()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary noecinemas.SyncSummary
		data, summary, err = e.syncNoeCinemas(ctx, client, noecinemas.SyncOptions{From: window.From, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetVeo:
		if e.newVeo == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newVeo()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary veo.SyncSummary
		data, summary, err = e.syncVeo(ctx, client, veo.SyncOptions{From: window.From, Now: e.now()})
		if args := veoCheckoutRetryLogArgs(summary); len(args) != 0 {
			e.logger.Info("veo_checkout_retry_summary", args...)
		}
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetMajestic:
		if e.newMajestic == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newMajestic()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary majestic.SyncSummary
		data, summary, err = e.syncMajestic(ctx, client, majestic.SyncOptions{From: window.From, Now: e.now()})
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetCinemovida:
		if e.newCinemovida == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newCinemovida()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary cinemovida.SyncSummary
		// Per-session source ends need a crawl budget, not the short database/metadata operation timeout.
		fetchCtx, cancel := context.WithTimeout(ctx, 25*time.Minute)
		data, summary, err = e.syncCinemovida(fetchCtx, client, cinemovida.SyncOptions{From: window.From, Now: e.now()})
		cancel()
		e.logger.Info("cinemovida_source_end_summary", "attempted", summary.EndsAttempted, "recovered", summary.EndsRecovered, "unresolved", summary.EndsUnresolved, "budget_skipped", summary.EndsBudgetSkipped)
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetCineode:
		if e.newCineode == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newCineode()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary cineode.SyncSummary
		fetchCtx, cancel := context.WithTimeout(ctx, e.operationTimeout)
		data, summary, err = e.syncCineode(fetchCtx, client, cineode.SyncOptions{From: window.From, Now: e.now()})
		cancel()
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: max(summary.Requests, client.RequestCount()), Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetMK2:
		if e.newMK2 == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newMK2()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary mk2.SyncSummary
		data, summary, err = e.syncMK2(ctx, client, mk2.SyncOptions{From: window.From, Now: e.now()})
		requests := summary.Requests
		if counter, ok := client.(interface{ RequestCount() int }); ok {
			requests = max(requests, counter.RequestCount())
		}
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: requests, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	case TargetCineville:
		if e.newCineville == nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureInternal, nil)
		}
		client, clientErr := e.newCineville()
		if clientErr != nil {
			return data, outcome, newProviderRunError(provider, StageClientCreation, FailureClientCreation, clientErr)
		}
		*lines = append(*lines, lifecycleLog(e.now().UTC(), provider, eventClientReady), lifecycleLog(e.now().UTC(), provider, eventFetchStarted))
		var summary cineville.SyncSummary
		data, summary, err = e.syncCineville(ctx, client, cineville.SyncOptions{From: window.From, Now: e.now()})
		requests := summary.Requests
		if counter, ok := client.(interface{ RequestCount() int }); ok {
			requests = max(requests, counter.RequestCount())
		}
		outcome = SyncOutcome{Cinemas: summary.Cinemas, Movies: summary.Movies, Requests: requests, Showtimes: summary.Showtimes, GeneratedAt: summary.GeneratedAt}
	default:
		return data, outcome, newProviderRunError(provider, StageOrchestration, FailureInternal, ErrInvalidTarget)
	}
	if err != nil {
		if provider == TargetKinepolis {
			var requestErr *kinepolis.RequestError
			if errors.As(err, &requestErr) {
				return data, outcome, stageError(ctx, provider, StageProviderFetch, FailureProviderSync, err)
			}
		}
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
	if provider == TargetVeo && details.Category == categoryRedirect {
		var rejected *veo.RequestError
		if errors.As(err, &rejected) && rejected.Operation == veo.OperationCheckout && rejected.Kind == syncproxy.FailureRedirect {
			if facts, ok := rejected.RedirectFacts(); ok {
				args = append(args, safeVeoRedirectLogArgs(facts)...)
			}
		}
	}
	if provider == TargetVeo && details.Category == categoryInvalidPayload {
		var rejected *veo.CheckoutParseError
		if errors.As(err, &rejected) {
			// Revalidate at the logging boundary, including errors constructed by
			// callers. No provider strings or error/cause rendering is permitted.
			switch rejected.Reason {
			case veo.RejectRoom, veo.RejectBookingForm, veo.RejectClock, veo.RejectEndResolution:
				args = append(args, "veo_checkout_reason", string(rejected.Reason))
				if rejected.Reason == veo.RejectBookingForm {
					if subreason := safeVeoFormSubreason(rejected.FormSubreason()); subreason != "" {
						args = append(args, "veo_form_subreason", subreason)
					}
				}
				if schedule.ValidVeoIdentity("theater", rejected.CinemaID) && schedule.ValidVeoIdentity("showing", rejected.ShowingID) && strings.HasPrefix(rejected.ShowingID, rejected.CinemaID+"-") {
					args = append(args, "veo_cinema_id", rejected.CinemaID, "veo_showing_id", rejected.ShowingID)
					if rejected.Reason == veo.RejectBookingForm && rejected.FormSubreason() == "action_mismatch" {
						if names := safeVeoActionDifferences(rejected.ActionDifferences()); names != "" {
							args = append(args, "veo_action_difference_bits", rejected.ActionDifferences(), "veo_action_differences", names)
						}
						if facts, ok := rejected.ActionPathFacts(); ok {
							args = append(args, safeVeoActionPathLogArgs(facts)...)
						}
					}
					index, count := rejected.ClientPoolPosition()
					if validVeoClientPoolPosition(index, count) {
						args = append(args, "veo_client_pool_position", index, "veo_client_pool_size", count)
					}
				}
			}
		}
	}
	return args
}

func validVeoClientPoolPosition(index, count int) bool {
	return index >= 1 && index <= count && count <= 10000
}

func safeVeoRedirectLogArgs(d veo.CheckoutRedirectFacts) []any {
	// Independent logger whitelist. Never render an error, supplied struct or
	// unknown string, including when a getter constructs a forged typed error.
	if !schedule.ValidVeoIdentity("theater", d.CinemaID) || !schedule.ValidVeoIdentity("showing", d.ShowingID) || !strings.HasPrefix(d.ShowingID, d.CinemaID+"-") || !validVeoClientPoolPosition(d.PoolPosition, d.PoolSize) {
		return nil
	}
	switch d.Branch {
	case "regular", "gueret":
	default:
		return nil
	}
	if d.Branch == "gueret" && d.CinemaID != "G06DB" {
		return nil
	}
	switch d.Phase {
	case "request", "response", "callback", "unknown":
	default:
		return nil
	}
	switch d.Reason {
	case "unknown", "request_binding", "raw_location_parse", "unsafe_authority", "route", "query", "source_binding", "date_binding", "time_binding", "epoch_binding", "cinema_binding", "status", "location_count", "hop", "method", "response_binding", "representation", "response_identity_unknown":
	default:
		return nil
	}
	if d.Hop < 0 || d.Hop > 3 || !d.HopKnown && d.Hop != 0 || d.Branch == "regular" && d.Hop > 1 || d.HTTPStatus != 0 && (d.HTTPStatus < 100 || d.HTTPStatus > 599) || d.LocationCount < 0 || d.LocationCount > 4 || d.LocationCountTruncated && d.LocationCount != 4 {
		return nil
	}
	for _, host := range []string{d.ExpectedHost, d.ObservedHost} {
		switch host {
		case "unknown", "regular", "relay", "dedicated", "relative", "other":
		default:
			return nil
		}
	}
	for _, route := range []string{d.ExpectedRoute, d.ObservedRoute} {
		switch route {
		case "unknown", "source", "relay_source", "relay_root", "alias", "canonical", "other":
		default:
			return nil
		}
	}
	return []any{
		"veo_redirect_branch", d.Branch, "veo_redirect_phase", d.Phase, "veo_redirect_reason", d.Reason,
		"veo_redirect_hop", d.Hop, "veo_redirect_hop_known", d.HopKnown,
		"veo_redirect_http_status", d.HTTPStatus, "veo_redirect_location_count", d.LocationCount, "veo_redirect_location_count_truncated", d.LocationCountTruncated,
		"veo_redirect_expected_host", d.ExpectedHost, "veo_redirect_expected_route", d.ExpectedRoute,
		"veo_redirect_observed_host", d.ObservedHost, "veo_redirect_observed_route", d.ObservedRoute,
		"veo_cinema_id", d.CinemaID, "veo_showing_id", d.ShowingID,
		"veo_client_pool_position", d.PoolPosition, "veo_client_pool_size", d.PoolSize,
	}
}

func safeVeoActionPathLogArgs(d veo.CheckoutActionPathFacts) []any {
	// This independent boundary validates every provider-local enum, mask and
	// collection. Never serialize the supplied struct or any unrecognized value.
	switch d.Shape {
	case "canonical_unavailable", "unparseable", "canonical_tokens", "canonical_prefix_extra", "generic_entry", "source_alias", "empty_segments", "missing_segments", "reordered_tokens", "different_shape":
	default:
		return nil
	}
	if len(d.Classes) > 32 || d.ExpectedPresentMask&^uint8(63) != 0 || d.ExpectedPositionMask&^d.ExpectedPresentMask != 0 {
		return nil
	}
	for _, count := range []int{d.SegmentCount, d.EmptySegments, d.ExtraCount, d.MissingCount} {
		if count < 0 || count > 1024 {
			return nil
		}
	}
	for _, class := range d.Classes {
		switch class {
		case "expected_slug", "route_reserver", "expected_film", "expected_epoch", "expected_version", "expected_tail", "route_source_r", "film_like", "epoch_like", "version_enum", "decimal", "empty", "other":
		default:
			return nil
		}
	}
	for i, position := range d.ExpectedPositions {
		if position < -1 || position > 1024 || (position >= 0) != (d.ExpectedPresentMask&(1<<i) != 0) || d.ExpectedAtPosition[i] != (d.ExpectedPositionMask&(1<<i) != 0) {
			return nil
		}
	}
	if !d.Parsed && (d.ExpectedPresentMask != 0 || d.ExpectedPositionMask != 0 || d.SegmentCount != 0 || len(d.Classes) != 0 || d.CanonicalPrefix || d.ExpectedTokensInOrder) {
		return nil
	}
	return []any{
		"veo_action_path_shape", d.Shape, "veo_action_path_parsed", d.Parsed, "veo_action_path_canonical_valid", d.CanonicalValid,
		"veo_action_path_segments", d.SegmentCount, "veo_action_path_empty", d.EmptySegments,
		"veo_action_path_extra", d.ExtraCount, "veo_action_path_missing", d.MissingCount,
		"veo_action_path_counts_truncated", d.CountsTruncated, "veo_action_path_classes", strings.Join(d.Classes, ","),
		"veo_action_path_classes_truncated", d.ClassesTruncated,
		"veo_action_path_leading_slash", d.LeadingSlash, "veo_action_path_trailing_slash", d.TrailingSlash,
		"veo_action_path_canonical_prefix", d.CanonicalPrefix, "veo_action_path_tokens_in_order", d.ExpectedTokensInOrder,
		"veo_action_path_present_mask", d.ExpectedPresentMask, "veo_action_path_position_mask", d.ExpectedPositionMask,
		"veo_action_path_expected_positions", d.ExpectedPositions,
		"veo_action_path_absolute", d.Absolute, "veo_action_path_scheme_equal", d.SchemeEqual, "veo_action_path_authority_equal", d.AuthorityEqual,
		"veo_action_path_query", d.QueryPresent, "veo_action_path_fragment", d.FragmentPresent,
		"veo_action_path_userinfo", d.UserinfoPresent, "veo_action_path_escaping", d.EscapingPresent,
	}
}

func veoCheckoutRetryLogArgs(summary veo.SyncSummary) []any {
	if summary.CheckoutRetryAttempts < 0 || summary.CheckoutRetryAttempts > 8 || summary.CheckoutRecoveries < 0 || summary.CheckoutRecoveries > summary.CheckoutRetryAttempts || summary.ToulouseUnavailableCheckouts < 0 || summary.GueretUnavailableCheckouts < 0 {
		return nil
	}
	args := []any{"component", "sync", "provider", "veo", "checkout_retry_attempts", summary.CheckoutRetryAttempts, "checkout_recoveries", summary.CheckoutRecoveries}
	args = append(args, "toulouse_unavailable_checkouts", summary.ToulouseUnavailableCheckouts)
	args = append(args, "gueret_unavailable_checkouts", summary.GueretUnavailableCheckouts)
	if summary.CheckoutRetryAttempts > 0 && summary.FirstCheckoutRetry != nil {
		args = append(args, "first_retry_recovered", summary.FirstCheckoutRetryRecovered)
		// Use exactly the existing defensive reason/identity/bit/index whitelist.
		args = append(args, providerFetchLogArgs(TargetVeo, newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, summary.FirstCheckoutRetry))...)
	}
	return args
}

func safeVeoActionDifferences(bits uint32) string {
	// Independent logging whitelist. Unknown/forged bits suppress the entire set.
	names := [...]string{"scheme", "authority", "path_shape", "slug", "film", "epoch", "version", "tail", "trailing_slash", "query", "fragment", "relative", "whitespace", "escaping", "userinfo", "unparseable", "representation"}
	if bits == 0 || bits&^uint32((1<<len(names))-1) != 0 {
		return ""
	}
	selected := []string{}
	for i, name := range names {
		if bits&(1<<i) != 0 {
			selected = append(selected, name)
		}
	}
	return strings.Join(selected, ",")
}

func safeVeoFormSubreason(reason string) string {
	switch reason {
	case "body_encoding", "html_parse", "canonical_missing", "canonical_invalid", "raw_form_count", "dom_form_count", "template_inert", "action_count", "action_mismatch", "method_count", "method_non_post", "login_password", "fronton_scope":
		return reason
	default:
		return ""
	}
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
	case pathe.CategoryInvalidJSON, pathe.CategoryInvalidPayload:
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

func safeKinepolisOperation(operation kinepolis.Operation) logOperation {
	switch operation {
	case kinepolis.OperationSchedule:
		return operationCinemas
	case kinepolis.OperationCinema:
		return operationCinema
	default:
		return operationUnknown
	}
}

func safeKinepolisCategory(category kinepolis.ErrorCategory) logCategory {
	switch category {
	case kinepolis.CategoryCanceled:
		return categoryCanceled
	case kinepolis.CategoryInvalidURL:
		return categoryInvalidURL
	case kinepolis.CategoryNoProxy:
		return categoryTransportUnavailable
	case kinepolis.CategoryTransport:
		return categoryTransport
	case kinepolis.CategoryRedirect:
		return categoryRedirect
	case kinepolis.CategoryResponseRead:
		return categoryResponseRead
	case kinepolis.CategoryResponseLarge:
		return categoryResponseTooLarge
	case kinepolis.CategoryChallenge:
		return categoryChallenge
	case kinepolis.CategoryServer, kinepolis.CategoryStatus:
		return categoryHTTPStatus
	case kinepolis.CategoryContentType:
		return categoryContentType
	case kinepolis.CategoryInvalidPayload:
		return categoryInvalidPayload
	case kinepolis.CategoryEmptyResponse:
		return categoryEmptyResponse
	default:
		return categoryUnknown
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
	var kinepolisErr *kinepolis.RequestError
	hasTypedKinepolisFetchError := provider == TargetKinepolis && stage == StageProviderFetch && errors.As(err, &kinepolisErr)
	if !hasTypedKinepolisFetchError && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
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
			if details.Operation == operationShowings && details.Category == categoryInvalidPayload {
				details.ParseReason = safeUGCParseReason(requestErr.ParseReason)
			}
			details.HTTPStatus = requestErr.StatusCode
			details.Attempt = requestErr.Attempt
			details.AttemptLimit = requestErr.AttemptLimit
		}
	case TargetKinepolis:
		if kinepolisErr != nil {
			details.Operation = safeKinepolisOperation(kinepolisErr.Operation)
			details.Category = safeKinepolisCategory(kinepolisErr.Category)
			details.HTTPStatus = kinepolisErr.StatusCode
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
	case TargetCinewest:
		var requestErr *cinewest.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case "cinemas":
				details.Operation = operationCinemas
			case "movies":
				details.Operation = operationMovies
			case "showtimes":
				details.Operation = operationShowings
			case "program":
				details.Operation = operationProgram
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetGrandEcran:
		var requestErr *grandecran.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case grandecran.OperationCinemas, grandecran.OperationDiscovery:
				details.Operation = operationCinemas
			case grandecran.OperationProgram:
				details.Operation = operationProgram
			case grandecran.OperationMovies:
				details.Operation = operationMovies
			case grandecran.OperationSchedule, grandecran.OperationRoom:
				details.Operation = operationShowings
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetNoeCinemas:
		var requestErr *noecinemas.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case noecinemas.OperationCinemas:
				details.Operation = operationCinemas
			case noecinemas.OperationProgram:
				details.Operation = operationProgram
			case noecinemas.OperationMovies:
				details.Operation = operationMovies
			case noecinemas.OperationSchedule, noecinemas.OperationRoom:
				details.Operation = operationShowings
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetCinemovida:
		var requestErr *cinemovida.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case cinemovida.OperationCinemas:
				details.Operation = operationCinemas
			case cinemovida.OperationProgram:
				details.Operation = operationProgram
			case cinemovida.OperationMovies:
				details.Operation = operationMovies
			case cinemovida.OperationSchedule, cinemovida.OperationCheckout:
				details.Operation = operationShowings
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetVeo:
		var requestErr *veo.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case veo.OperationCinemas:
				details.Operation = operationCinemas
			case veo.OperationProgram, veo.OperationCastelia:
				details.Operation = operationProgram
			case veo.OperationMovies:
				details.Operation = operationMovies
			case veo.OperationSchedule, veo.OperationCheckout:
				details.Operation = operationShowings
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		} else {
			var rejected *veo.CheckoutParseError
			if errors.As(err, &rejected) {
				details.Operation, details.Category = operationShowings, categoryInvalidPayload
			}
		}
	case TargetMK2:
		var requestErr *mk2.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case mk2.OperationCinemas:
				details.Operation = operationCinemas
			case mk2.OperationFilms:
				details.Operation = operationMovies
			case mk2.OperationComplex:
				details.Operation = operationProgram
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetCineville:
		var requestErr *cineville.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case cineville.OperationBootstrap:
				details.Operation = operationCinemas
			case cineville.OperationCinema:
				details.Operation = operationProgram
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetMajestic:
		var requestErr *majestic.RequestError
		if errors.As(err, &requestErr) {
			details.Operation = operationProgram
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		}
	case TargetCineode:
		var requestErr *cineode.RequestError
		details.Operation = operationProgram
		if errors.As(err, &requestErr) {
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		} else {
			details.Category = categoryInvalidPayload
		}
	case TargetMegarama:
		var requestErr *megarama.RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Operation {
			case megarama.OperationConfig:
				details.Operation = operationCinemas
			case megarama.OperationProgram:
				details.Operation = operationProgram
			case megarama.OperationPoster:
				details.Operation = operationMovies
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
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

func safeUGCParseReason(reason ugc.ParseReason) logParseReason {
	switch reason {
	case ugc.ParseReasonDocumentParse:
		return parseReasonDocumentParse
	case ugc.ParseReasonTimezoneUnavailable:
		return parseReasonTimezoneUnavailable
	case ugc.ParseReasonInvalidServiceDate:
		return parseReasonInvalidServiceDate
	case ugc.ParseReasonShowingAttributesMissingOrConflicting:
		return parseReasonShowingAttributesMissingOrConflicting
	case ugc.ParseReasonConflictingDuplicateShowing:
		return parseReasonConflictingDuplicateShowing
	case ugc.ParseReasonUnrecognizedShowingsDocument:
		return parseReasonUnrecognizedShowingsDocument
	case ugc.ParseReasonFilmIdentityConflict:
		return parseReasonFilmIdentityConflict
	case ugc.ParseReasonFilmTitleMissing:
		return parseReasonFilmTitleMissing
	case ugc.ParseReasonFilmTitleConflicting:
		return parseReasonFilmTitleConflicting
	case ugc.ParseReasonFilmRuntimeMissing:
		return parseReasonFilmRuntimeMissing
	case ugc.ParseReasonInvalidFilmRuntime:
		return parseReasonInvalidFilmRuntime
	case ugc.ParseReasonUnrecognizedShowingOwnership:
		return parseReasonUnrecognizedShowingOwnership
	case ugc.ParseReasonInvalidFilmDetailLink:
		return parseReasonInvalidFilmDetailLink
	case ugc.ParseReasonUnknownShowingVersion:
		return parseReasonUnknownShowingVersion
	case ugc.ParseReasonInvalidShowingHour:
		return parseReasonInvalidShowingHour
	case ugc.ParseReasonShowingOutsideCinemaDay:
		return parseReasonShowingOutsideCinemaDay
	case ugc.ParseReasonInvalidShowingDate:
		return parseReasonInvalidShowingDate
	case ugc.ParseReasonUnknownShowingFormat:
		return parseReasonUnknownShowingFormat
	case ugc.ParseReasonShowingEndMissingOrConflicting:
		return parseReasonShowingEndMissingOrConflicting
	case ugc.ParseReasonInvalidShowingEnd:
		return parseReasonInvalidShowingEnd
	default:
		return parseReasonUnknown
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
		if showing.Movie.RuntimeMinutes == 0 && provider != TargetMegarama && provider != TargetMajestic && provider != TargetCineode && provider != TargetCinemovida && provider != TargetCineville && provider != TargetMK2 && provider != TargetCinewest && provider != TargetGrandEcran && provider != TargetNoeCinemas && provider != TargetVeo {
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

func safeMegaramaCategory(kind syncproxy.FailureKind) logCategory {
	switch kind {
	case syncproxy.FailureCanceled:
		return categoryCanceled
	case syncproxy.FailureInvalidURL:
		return categoryInvalidURL
	case syncproxy.FailureNoClient:
		return categoryTransportUnavailable
	case syncproxy.FailureTransport:
		return categoryTransport
	case syncproxy.FailureRedirect:
		return categoryRedirect
	case syncproxy.FailureResponseRead:
		return categoryResponseRead
	case syncproxy.FailureResponseLarge:
		return categoryResponseTooLarge
	case syncproxy.FailureChallenge:
		return categoryChallenge
	case syncproxy.FailureServer, syncproxy.FailureStatus:
		return categoryHTTPStatus
	case syncproxy.FailureContentType:
		return categoryContentType
	case syncproxy.FailureInvalidJSON:
		return categoryInvalidPayload
	case syncproxy.FailureEmptyResponse:
		return categoryEmptyResponse
	default:
		return categoryUnknown
	}
}
