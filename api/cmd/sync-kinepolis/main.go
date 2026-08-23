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
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/tmdb"
)

type dependencies struct {
	getenv       func(string) string
	newClient    func(kinepolis.ClientConfig) (kinepolis.Fetcher, error)
	openDatabase func(context.Context, string) (databaseServices, func(), error)
	newTMDB      func(string) (enrichment.Provider, error)
	enrich       func(context.Context, enrichment.Store, enrichment.Provider, []enrichment.Movie) (enrichment.Summary, error)
	newExecutor  func(synccontrol.ProductionExecutorOptions) (fullExecutor, error)
}

type databaseServices struct {
	writer     schedule.SnapshotWriter
	enrichment enrichment.Store
}

type fullExecutor interface {
	Run(context.Context, synccontrol.Target, synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error)
}

func productionDependencies() dependencies {
	return dependencies{
		getenv:    os.Getenv,
		newClient: func(config kinepolis.ClientConfig) (kinepolis.Fetcher, error) { return kinepolis.NewClient(config) },
		openDatabase: func(ctx context.Context, databaseURL string) (databaseServices, func(), error) {
			pool, err := database.OpenPool(ctx, databaseURL)
			if err != nil {
				return databaseServices{}, nil, fmt.Errorf("database open failed")
			}
			if err := database.RunMigrations(ctx, pool); err != nil {
				pool.Close()
				return databaseServices{}, nil, fmt.Errorf("database migration failed")
			}
			return databaseServices{writer: schedulepg.NewStore(pool), enrichment: enrichment.NewPostgresStore(pool)}, pool.Close, nil
		},
		newTMDB: func(token string) (enrichment.Provider, error) { return tmdb.NewClient(token) },
		enrich: func(ctx context.Context, store enrichment.Store, provider enrichment.Provider, movies []enrichment.Movie) (enrichment.Summary, error) {
			return enrichment.NewMatcher(store, provider, time.Now).Run(ctx, movies)
		},
		newExecutor: func(options synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
			return synccontrol.NewProductionExecutor(options)
		},
	}
}

func main() {
	logger := observability.NewLogger(os.Stderr)
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		logger.Error("process_start_failed", "component", "sync_kinepolis", "error_code", "configuration_error")
		os.Exit(2)
	}
	os.Exit(run(context.Background(), os.Args[1:], time.Now))
}

func run(ctx context.Context, args []string, now func() time.Time) int {
	return runWithIO(ctx, args, now, os.Stdout, os.Stderr)
}

func runWithIO(ctx context.Context, args []string, now func() time.Time, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, args, now, stdout, stderr, productionDependencies())
}

func runWithDependencies(ctx context.Context, args []string, now func() time.Time, stdout, stderr io.Writer, deps dependencies) int {
	logger := observability.NewLogger(stderr)
	if deps.getenv == nil {
		deps.getenv = func(string) string { return "" }
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	today := now().In(location)
	flags := flag.NewFlagSet("sync-kinepolis", flag.ContinueOnError)
	flags.SetOutput(stderr)
	from, proxyFile := "", ""
	timeout, requestInterval := 20*time.Second, 2*time.Second
	flags.StringVar(&from, "from", today.Format("2006-01-02"), "first service date")
	flags.DurationVar(&timeout, "timeout", 20*time.Second, "request timeout")
	flags.DurationVar(&requestInterval, "request-interval", 2*time.Second, "delay between request starts")
	flags.StringVar(&proxyFile, "proxy-file", "", "required proxy file")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	fromDate, e1 := time.ParseInLocation("2006-01-02", from, location)
	if e1 != nil || fromDate.Format("2006-01-02") != from {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	if proxyFile == "" {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	overrides := &runtimeconfig.Overrides{}
	flags.Visit(func(flag *flag.Flag) {
		switch flag.Name {
		case "timeout":
			overrides.RequestTimeout = &timeout
		case "request-interval":
			overrides.KinepolisRequestInterval = &requestInterval
		}
	})
	timing, err := runtimeconfig.Load(runtimeconfig.KinepolisTiming, deps.getenv, overrides)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	timeout, requestInterval = timing.Sync.RequestTimeout, timing.Sync.KinepolisRequestInterval
	file, err := os.Open(proxyFile)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	proxies, parseErr := syncproxy.Parse(file)
	closeErr := file.Close()
	if parseErr != nil || closeErr != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	client, err := deps.newClient(kinepolis.ClientConfig{Proxies: proxies, RequestInterval: requestInterval, Timeout: timeout})
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	fullConfig, err := runtimeconfig.Load(runtimeconfig.SyncFull, deps.getenv, nil)
	if err != nil {
		logCLIError(logger, "configuration_failed", "configuration_error")
		return 2
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	services, closeDatabase, err := deps.openDatabase(startup, fullConfig.Database.URL)
	cancel()
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
	executor, err := deps.newExecutor(synccontrol.ProductionExecutorOptions{
		Writer: services.writer, NewKinepolis: func() (kinepolis.Fetcher, error) { return client, nil }, Enrich: enrich, Now: now, Logger: logger, OperationTimeout: fullConfig.Sync.OperationTimeout,
	})
	if err != nil {
		logCLIError(logger, "sync_command_failed", "configuration_error")
		return 1
	}
	outcomes, err := executor.Run(ctx, synccontrol.TargetKinepolis, synccontrol.Window{From: from})
	if err != nil {
		logCLIError(logger, "sync_command_failed", syncFailureCode(err))
		return 1
	}
	outcome := outcomes[synccontrol.TargetKinepolis]
	summary := outcome.Sync
	fmt.Fprintf(stdout, "sync complete provider=kinepolis version=%d cinemas=%d showtimes=%d generated_at=%s\n", summary.Version, summary.Cinemas, summary.Showtimes, summary.GeneratedAt.Format(time.RFC3339))
	renderEnrichment(stdout, outcome.Enrichment)
	return 0
}

func syncFailureCode(err error) string {
	var runError *synccontrol.RunError
	if errors.As(err, &runError) {
		return string(runError.Code)
	}
	return string(synccontrol.FailureInternal)
}

func logCLIError(logger *slog.Logger, event, code string) {
	logger.Error(event, "component", "sync_kinepolis", "error_code", code)
}

func renderEnrichment(stdout io.Writer, outcome synccontrol.EnrichmentOutcome) {
	if outcome.Counts == nil {
		fmt.Fprintf(stdout, "enrichment=%s\n", outcome.Status)
		return
	}
	c := outcome.Counts
	fmt.Fprintf(stdout, "enrichment=%s reused=%d matched=%d review_required=%d unmatched=%d failed=%d\n", outcome.Status, c.Reused, c.Matched, c.ReviewRequired, c.Unmatched, c.Failed)
}
