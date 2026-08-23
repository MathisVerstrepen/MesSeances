package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/tmdb"
	"messeances/api/internal/ugc"
)

type config struct {
	proxyFile, from, cinemaID string
	proxyLimit                int
	timeout                   time.Duration
}

type dependencies struct {
	getenv       func(string) string
	newClient    func(ugc.ClientConfig) (ugc.Getter, error)
	sync         func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	openDatabase func(context.Context, string) (databaseServices, func(), error)
	newTMDB      func(string) (enrichment.Provider, error)
	enrich       func(context.Context, enrichment.Store, enrichment.Provider, []enrichment.Movie) (enrichment.Summary, error)
	newExecutor  func(synccontrol.ProductionExecutorOptions) (fullExecutor, error)
}

type fullExecutor interface {
	Run(context.Context, synccontrol.Target, synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error)
}

type databaseServices struct {
	writer     schedule.SnapshotWriter
	enrichment enrichment.Store
}

func productionDependencies() dependencies {
	return dependencies{getenv: os.Getenv, newClient: func(config ugc.ClientConfig) (ugc.Getter, error) { return ugc.NewClient(config) }, sync: ugc.Sync, openDatabase: func(ctx context.Context, databaseURL string) (databaseServices, func(), error) {
		pool, err := database.OpenPool(ctx, databaseURL)
		if err != nil {
			return databaseServices{}, nil, fmt.Errorf("database open failed")
		}
		if err := database.RunMigrations(ctx, pool); err != nil {
			pool.Close()
			return databaseServices{}, nil, fmt.Errorf("database migration failed")
		}
		return databaseServices{writer: schedulepg.NewStore(pool), enrichment: enrichment.NewPostgresStore(pool)}, pool.Close, nil
	}, newTMDB: func(token string) (enrichment.Provider, error) { return tmdb.NewClient(token) }, enrich: func(ctx context.Context, store enrichment.Store, provider enrichment.Provider, movies []enrichment.Movie) (enrichment.Summary, error) {
		return enrichment.NewMatcher(store, provider, time.Now).Run(ctx, movies)
	}, newExecutor: func(options synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
		return synccontrol.NewProductionExecutor(options)
	}}
}

func main() {
	logger := observability.NewLogger(os.Stderr)
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		logger.Error("process_start_failed", "component", "sync_ugc", "error_code", "configuration_error")
		os.Exit(2)
	}
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runWithDependencies(ctx, args, stdout, stderr, now, productionDependencies())
}

func runWithDependencies(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, deps dependencies) int {
	logger := observability.NewLogger(stderr)
	if deps.getenv == nil {
		deps.getenv = func(string) string { return "" }
	}
	if deps.newClient == nil {
		deps.newClient = func(config ugc.ClientConfig) (ugc.Getter, error) { return ugc.NewClient(config) }
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	today := now().In(location)
	flags := flag.NewFlagSet("sync-ugc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cfg := config{}
	flags.StringVar(&cfg.proxyFile, "proxy-file", "../tmp/proxies.txt", "required proxy file")
	flags.StringVar(&cfg.from, "from", today.Format("2006-01-02"), "first service date")
	flags.StringVar(&cfg.cinemaID, "cinema-id", "", "diagnostic cinema ID")
	flags.IntVar(&cfg.proxyLimit, "proxy-limit", 0, "maximum proxies to use")
	flags.DurationVar(&cfg.timeout, "timeout", 20*time.Second, "per-request timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	from, err := time.ParseInLocation("2006-01-02", cfg.from, location)
	if err != nil || from.Format("2006-01-02") != cfg.from {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	if err := ugc.ValidateCinemaID(cfg.cinemaID); err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	if cfg.proxyLimit < 0 {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	overrides := &runtimeconfig.Overrides{}
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "timeout" {
			overrides.RequestTimeout = &cfg.timeout
		}
	})
	timing, err := runtimeconfig.Load(runtimeconfig.UGCTiming, deps.getenv, overrides)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	cfg.timeout = timing.Sync.RequestTimeout
	proxyFile, err := os.Open(cfg.proxyFile)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	proxies, parseErr := ugc.ParseProxies(proxyFile)
	closeErr := proxyFile.Close()
	if parseErr != nil || closeErr != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	if cfg.proxyLimit > 0 {
		if cfg.proxyLimit > len(proxies) {
			logCLIError(logger, "configuration_failed", "configuration_error")
			return 2
		}
		proxies = proxies[:cfg.proxyLimit]
	}
	client, err := deps.newClient(ugc.ClientConfig{Proxies: proxies, Timeout: cfg.timeout})
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	options := ugc.SyncOptions{From: cfg.from, CinemaID: cfg.cinemaID, Now: now()}
	if cfg.cinemaID != "" {
		data, summary, err := deps.sync(ctx, client, options)
		if err != nil {
			logCLIError(logger, "sync_command_failed", "provider_sync_failed")
			return 1
		}
		if err := schedule.ValidateDataset(data, false); err != nil || data.Scope != schedule.ScopeSingle {
			logCLIError(logger, "sync_command_failed", "dataset_rejected")
			return 1
		}
		fmt.Fprintf(stdout, "sync complete mode=single_cinema persisted=false cinemas=%d skipped=%d dates=%d requests=%d showtimes=%d proxies=%d generated_at=%s\n", summary.Cinemas, summary.Skipped, summary.Dates, summary.Requests, summary.Showtimes, len(proxies), summary.GeneratedAt.Format(time.RFC3339))
		return 0
	}
	fullConfig, err := runtimeconfig.Load(runtimeconfig.SyncFull, deps.getenv, nil)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	openCtx, openCancel := context.WithTimeout(ctx, 30*time.Second)
	services, closeDatabase, err := deps.openDatabase(openCtx, fullConfig.Database.URL)
	openCancel()
	if err != nil {
		logCLIError(logger, "sync_command_failed", "database_startup_failed")
		return 1
	}
	defer closeDatabase()
	enrich := func(enrichCtx context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
		token := fullConfig.TMDB.Token
		if token == "" {
			return nil, nil
		}
		provider, err := deps.newTMDB(token)
		if err != nil || services.enrichment == nil {
			return nil, fmt.Errorf("enrichment setup failed")
		}
		summary, err := deps.enrich(enrichCtx, services.enrichment, provider, movies)
		return &summary, err
	}
	executor, err := deps.newExecutor(synccontrol.ProductionExecutorOptions{Writer: services.writer, NewUGC: func() (ugc.Getter, error) { return client, nil }, Enrich: enrich, Now: now, Logger: logger, OperationTimeout: fullConfig.Sync.OperationTimeout})
	if err != nil {
		logCLIError(logger, "sync_command_failed", "configuration_error")
		return 1
	}
	outcomes, err := executor.Run(ctx, synccontrol.TargetUGC, synccontrol.Window{From: cfg.from})
	if err != nil {
		logCLIError(logger, "sync_command_failed", syncFailureCode(err))
		return 1
	}
	outcome := outcomes[synccontrol.TargetUGC]
	summary := outcome.Sync
	fmt.Fprintf(stdout, "sync complete mode=all_cinemas persisted=true version=%d cinemas=%d skipped=%d dates=%d requests=%d showtimes=%d proxies=%d generated_at=%s\n", summary.Version, summary.Cinemas, summary.Skipped, summary.Dates, summary.Requests, summary.Showtimes, len(proxies), summary.GeneratedAt.Format(time.RFC3339))
	renderEnrichment(stdout, logger, outcome.Enrichment)
	return 0
}

func syncFailureCode(err error) string {
	var runError *synccontrol.RunError
	if errors.As(err, &runError) {
		return string(runError.Code)
	}
	return string(synccontrol.FailureInternal)
}

func renderEnrichment(stdout io.Writer, logger *slog.Logger, outcome synccontrol.EnrichmentOutcome) {
	if outcome.Status == "degraded" {
		logger.Warn("sync_enrichment_degraded", "component", "sync_ugc", "error_code", "enrichment_failed")
	}
	if outcome.Counts == nil {
		fmt.Fprintf(stdout, "enrichment=%s\n", outcome.Status)
		return
	}
	c := outcome.Counts
	fmt.Fprintf(stdout, "enrichment=%s reused=%d matched=%d review_required=%d unmatched=%d failed=%d\n", outcome.Status, c.Reused, c.Matched, c.ReviewRequired, c.Unmatched, c.Failed)
}

func logCLIError(logger *slog.Logger, event, code string) {
	logger.Error(event, "component", "sync_ugc", "error_code", code)
}
