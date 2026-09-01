package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/cgr"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/httpapi"
	"messeances/api/internal/ign"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/observability"
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/shortlink"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/syncschedule"
	"messeances/api/internal/tmdb"
	"messeances/api/internal/ugc"
)

func main() {
	logger := observability.NewLogger(os.Stderr)
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		logDotEnvFailure(logger)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		logProcessFailure(logger, err)
		os.Exit(1)
	}
}

type processFailureDetail struct {
	stage  string
	reason string
}

type processFailureError struct {
	detail processFailureDetail
}

func (err *processFailureError) Error() string {
	return err.detail.reason
}

var migrationHistoryIncompatibleProcessFailure = &processFailureError{detail: processFailureDetail{
	stage:  "migration",
	reason: "database migration history is incompatible",
}}

func logDotEnvFailure(logger *slog.Logger) {
	logger.Error(
		"process_start_failed",
		"component", "api",
		"error_code", "configuration_error",
		"failure_stage", "configuration",
		"failure_reason", "dotenv load failed",
	)
}

func logProcessFailure(logger *slog.Logger, err error) {
	detail := safeProcessFailureDetail(err)
	logger.Error(
		"process_stopped",
		"component", "api",
		"error_code", "process_failure",
		"failure_stage", detail.stage,
		"failure_reason", detail.reason,
	)
}

func safeProcessFailureDetail(err error) processFailureDetail {
	if err == nil {
		return processFailureDetail{stage: "unknown", reason: "process failure"}
	}
	//nolint:errorlint // Exact identity intentionally rejects wrapped errors at the logging boundary.
	if err == migrationHistoryIncompatibleProcessFailure {
		return migrationHistoryIncompatibleProcessFailure.detail
	}

	// Known direct process failure values and exact fixed messages are the
	// logging trust boundary. Do not unwrap or log unrecognized errors because
	// either may contain sensitive runtime values.
	switch err.Error() {
	case "configuration error":
		return processFailureDetail{stage: "configuration", reason: "configuration error"}
	case "sync configuration is invalid":
		return processFailureDetail{stage: "configuration", reason: "sync configuration is invalid"}
	case "database startup failed":
		return processFailureDetail{stage: "database", reason: "database startup failed"}
	case "database migration failed":
		return processFailureDetail{stage: "migration", reason: "database migration failed"}
	case "shortlink retention startup failed":
		return processFailureDetail{stage: "retention", reason: "shortlink retention startup failed"}
	case "sync run retention startup failed":
		return processFailureDetail{stage: "retention", reason: "sync run retention startup failed"}
	case "schedule snapshot startup failed":
		return processFailureDetail{stage: "schedule", reason: "schedule snapshot startup failed"}
	case "schedule service startup failed":
		return processFailureDetail{stage: "schedule", reason: "schedule service startup failed"}
	case "TMDB configuration is invalid":
		return processFailureDetail{stage: "configuration", reason: "TMDB configuration is invalid"}
	case "TMDB metadata refresh configuration is invalid":
		return processFailureDetail{stage: "configuration", reason: "TMDB metadata refresh configuration is invalid"}
	case "geocoding configuration is invalid":
		return processFailureDetail{stage: "configuration", reason: "geocoding configuration is invalid"}
	case "sync schedule configuration is invalid":
		return processFailureDetail{stage: "configuration", reason: "sync schedule configuration is invalid"}
	case "API server failed":
		return processFailureDetail{stage: "server", reason: "API server failed"}
	case "API server shutdown failed":
		return processFailureDetail{stage: "server", reason: "API server shutdown failed"}
	default:
		return processFailureDetail{stage: "unknown", reason: "process failure"}
	}
}

type httpServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

const (
	shutdownTimeout         = 10 * time.Second
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 15 * time.Second
	serverWriteTimeout      = 3 * time.Minute
	serverIdleTimeout       = 120 * time.Second
	serverMaxHeaderBytes    = 1 << 20
)

func run(ctx context.Context) error {
	logger := observability.NewLogger(os.Stderr)
	metrics := observability.NewMetrics()
	cfg, syncConfig, err := loadAPIConfiguration(os.Getenv)
	if err != nil {
		return err
	}
	proxies, err := loadSyncProxies(cfg.Proxy.Path, func(path string) (io.ReadCloser, error) { return os.Open(path) })
	if err != nil {
		return err
	}
	startupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := database.OpenPool(startupCtx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("database startup failed")
	}
	defer pool.Close()
	if err := database.RunMigrations(startupCtx, pool); err != nil {
		if errors.Is(err, database.ErrMigrationHistoryIncompatible) {
			return migrationHistoryIncompatibleProcessFailure
		}
		return fmt.Errorf("database migration failed")
	}
	shortlinkStore := shortlink.NewPostgresStore(pool)
	if err := purgeShortlinksAtStartup(startupCtx, shortlinkStore, time.Now); err != nil {
		return err
	}
	runStore := synccontrol.NewPostgresRunStore(pool)
	if err := runStore.PurgeTerminalBefore(startupCtx, time.Now().UTC().Add(-synccontrol.TerminalRunRetentionPeriod)); err != nil {
		return fmt.Errorf("sync run retention startup failed")
	}
	store := schedulepg.NewStore(pool)
	source, err := schedule.NewPostgresSource(startupCtx, store, schedule.SourceOptions{Logger: logger, Observer: metrics})
	if err != nil {
		return fmt.Errorf("schedule snapshot startup failed")
	}
	service, err := schedule.NewService(source, newProductionScheduleOptions())
	if err != nil {
		return fmt.Errorf("schedule service startup failed")
	}
	enrichmentStore := enrichment.NewPostgresStore(pool)
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	var enrichmentProvider enrichment.Provider
	if cfg.TMDB.Token != "" {
		tmdbClient, err := tmdb.NewClient(cfg.TMDB.Token)
		if err != nil {
			return fmt.Errorf("TMDB configuration is invalid")
		}
		enrichmentProvider = tmdbClient
	}
	adminOptions, metadataRefreshManager, err := newAdminOptions(workerCtx, cfg.Admin.Password, cfg.Admin.SessionSecret, enrichmentStore, enrichmentProvider)
	if err != nil {
		return fmt.Errorf("TMDB metadata refresh configuration is invalid")
	}
	adminOptions.TheaterLocations = newTheaterLocationController(pool, time.Now)
	adminOptions.Logger = logger
	adminOptions.Metrics = metrics
	geocodingManager, err := newTheaterGeocodingManager(workerCtx, pool, time.Now)
	if err != nil {
		return fmt.Errorf("geocoding configuration is invalid")
	}
	defer geocodingManager.Close()
	adminOptions.TheaterGeocoding = geocodingManager
	var syncManager httpapi.SyncController
	var concreteSyncManager *synccontrol.Manager
	var syncScheduler *syncschedule.Service
	if len(proxies) != 0 {
		var enrich synccontrol.EnrichFunc
		if enrichmentProvider != nil {
			enrich = func(ctx context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
				summary, err := enrichment.NewMatcher(enrichmentStore, enrichmentProvider, time.Now).Run(ctx, movies)
				return &summary, err
			}
		}
		executor, err := synccontrol.NewProductionExecutor(newSyncExecutorOptions(store, proxies, syncConfig, enrich, time.Now, logger, metrics))
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		manager, err := synccontrol.NewManager(workerCtx, time.Now, executor, runStore, synccontrol.NewPostgresRunLocker(pool))
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		concreteSyncManager = manager
		syncManager = manager
	}
	if concreteSyncManager != nil || metadataRefreshManager != nil {
		scheduleStore := syncschedule.NewPostgresStore(pool)
		starter := syncScheduleStarter{providers: concreteSyncManager, metadata: metadataRefreshManager, claimer: scheduleStore}
		scheduler, err := syncschedule.NewService(scheduleStore, starter)
		if err != nil {
			if concreteSyncManager != nil {
				concreteSyncManager.Close()
			}
			return fmt.Errorf("sync schedule configuration is invalid")
		}
		if err := scheduler.Start(workerCtx); err != nil {
			scheduler.Close()
			if concreteSyncManager != nil {
				concreteSyncManager.Close()
			}
			return fmt.Errorf("sync schedule configuration is invalid")
		}
		syncScheduler = scheduler
	}
	var polling sync.WaitGroup
	polling.Add(3)
	go func() {
		defer polling.Done()
		source.Run(workerCtx)
	}()
	go func() {
		defer polling.Done()
		runSyncRunRetention(workerCtx, runStore, logger, time.Now)
	}()
	go func() {
		defer polling.Done()
		runShortlinkRetention(workerCtx, shortlinkStore, logger, time.Now)
	}()
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			shutdownWorkers(stopWorkers, syncScheduler, concreteSyncManager, geocodingManager, metadataRefreshManager, &polling)
		})
	}
	defer cleanup()
	adminOptions.Syncs = syncManager
	adminOptions.SyncSchedules = syncScheduler
	shortlinkService := shortlink.NewService(shortlinkStore, shortlink.ServiceOptions{})
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: newAPIHandler(service, cfg, adminOptions, shortlinkService, httpapi.ReadinessOptions{
			Schedule:  source,
			Database:  pool,
			Revisions: store,
		}),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
	logger.Info("api_listening", "component", "api")
	return serve(ctx, server, cleanup)
}

func newProductionScheduleOptions() schedule.ServiceOptions {
	return schedule.ServiceOptions{
		DefaultCity: "Paris",
		CityAliases: map[string][]string{
			"Lille": {"Lille", "Villeneuve d'Ascq"},
		},
	}
}

func runSyncRunRetention(ctx context.Context, store *synccontrol.PostgresRunStore, logger *slog.Logger, now func() time.Time) {
	ticker := time.NewTicker(synccontrol.TerminalRunRetentionPurgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purgeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := store.PurgeTerminalBefore(purgeCtx, now().UTC().Add(-synccontrol.TerminalRunRetentionPeriod))
			cancel()
			if err != nil {
				logger.Error("sync_run_retention_failed", "component", "sync", "error_code", "database_error")
			}
		}
	}
}

type shortlinkRetentionStore interface {
	PurgeCreatedBefore(context.Context, time.Time) error
}

func purgeShortlinksAtStartup(ctx context.Context, store shortlinkRetentionStore, now func() time.Time) error {
	if err := store.PurgeCreatedBefore(ctx, now().UTC().Add(-shortlink.RetentionPeriod)); err != nil {
		return fmt.Errorf("shortlink retention startup failed")
	}
	return nil
}

func runShortlinkRetention(ctx context.Context, store shortlinkRetentionStore, logger *slog.Logger, now func() time.Time) {
	ticker := time.NewTicker(shortlink.RetentionPurgeInterval)
	defer ticker.Stop()
	runShortlinkRetentionTicks(ctx, store, logger, now, ticker.C)
}

func runShortlinkRetentionTicks(ctx context.Context, store shortlinkRetentionStore, logger *slog.Logger, now func() time.Time, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			purgeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := store.PurgeCreatedBefore(purgeCtx, now().UTC().Add(-shortlink.RetentionPeriod))
			cancel()
			if err != nil {
				logger.Error("shortlink_retention_failed", "component", "shortlink", "error_code", "database_error")
			}
		}
	}
}

func newSyncExecutorOptions(writer schedule.SnapshotWriter, proxies []syncproxy.Proxy, cfg runtimeconfig.Config, enrich synccontrol.EnrichFunc, now func() time.Time, logger *slog.Logger, observer synccontrol.SyncObserver) synccontrol.ProductionExecutorOptions {
	return synccontrol.ProductionExecutorOptions{
		Writer: writer, Now: now, Logger: logger, Observer: observer, Enrich: enrich, OperationTimeout: cfg.Sync.OperationTimeout,
		NewUGC: func() (ugc.Getter, error) {
			return ugc.NewClient(ugc.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
		NewKinepolis: func() (kinepolis.Fetcher, error) {
			return kinepolis.NewClient(kinepolis.ClientConfig{Proxies: proxies, RequestInterval: cfg.Sync.KinepolisRequestInterval, Timeout: cfg.Sync.RequestTimeout})
		},
		NewPathe: func() (pathe.Getter, error) {
			return pathe.NewClient(pathe.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
		NewCGR: func() (cgr.Getter, error) {
			return cgr.NewClient(cgr.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
	}
}

type closeableWorker interface {
	Close()
}

func shutdownWorkers(stopWorkers context.CancelFunc, schedules, syncManager, geocodingManager, metadataRefreshManager closeableWorker, polling *sync.WaitGroup) {
	stopWorkers()
	if schedules != nil {
		schedules.Close()
	}
	if syncManager != nil {
		syncManager.Close()
	}
	if geocodingManager != nil {
		geocodingManager.Close()
	}
	if metadataRefreshManager != nil {
		metadataRefreshManager.Close()
	}
	polling.Wait()
}

func newAPIHandler(service *schedule.Service, cfg runtimeconfig.Config, adminOptions httpapi.AdminOptions, shortlinks httpapi.ShortlinkService, readiness httpapi.ReadinessOptions) http.Handler {
	return httpapi.NewHandlerWithOptions(service, cfg.Server.Origin, httpapi.HandlerOptions{
		Admin:                adminOptions,
		Readiness:            readiness,
		Shortlinks:           shortlinks,
		TrustedProxyCIDRs:    cfg.Server.TrustedProxyCIDRs,
		InternalSharedSecret: cfg.Internal.SharedSecret,
	})
}

func loadAPIConfiguration(getenv func(string) string) (runtimeconfig.Config, runtimeconfig.Config, error) {
	cfg, err := runtimeconfig.Load(runtimeconfig.APIBase, getenv)
	if err != nil {
		return runtimeconfig.Config{}, runtimeconfig.Config{}, err
	}
	if cfg.Proxy.Path == "" {
		return cfg, runtimeconfig.Config{}, nil
	}
	syncConfig, err := runtimeconfig.Load(runtimeconfig.APISync, getenv)
	if err != nil {
		return runtimeconfig.Config{}, runtimeconfig.Config{}, err
	}
	return cfg, syncConfig, nil
}

func serve(ctx context.Context, server httpServer, stopWorkers context.CancelFunc) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("API server failed")
		}
		return nil
	case <-ctx.Done():
		stopWorkers()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("API server shutdown failed")
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("API server failed")
		}
		return nil
	}
}

func newAdminOptions(ctx context.Context, password, sessionSecret string, store *enrichment.PostgresStore, provider enrichment.Provider) (httpapi.AdminOptions, *enrichment.MetadataRefreshManager, error) {
	options := httpapi.AdminOptions{
		Password:      password,
		SessionSecret: sessionSecret,
		Reviews:       enrichment.NewReviewService(store, provider, nil),
		LocalMovies:   enrichment.NewLocalMovieService(store),
		Movies:        enrichment.NewAdminMovieService(store),
	}
	if provider != nil {
		gate := enrichment.NewTMDBRunGate()
		options.TMDBReruns = enrichment.NewRerunService(store, enrichment.NewMatcher(store, provider, nil), gate)
		manager, err := enrichment.NewMetadataRefreshManager(ctx, enrichment.NewMetadataRefreshService(store, provider, nil, gate), nil)
		if err != nil {
			return httpapi.AdminOptions{}, nil, err
		}
		options.TMDBRefreshes = manager
		return options, manager, nil
	}
	return options, nil, nil
}

func newTheaterLocationController(pool *pgxpool.Pool, now func() time.Time) httpapi.TheaterLocationController {
	return geocoding.NewResolutionService(geocoding.NewPostgresResolutionStore(pool), now)
}

func newTheaterGeocodingManager(ctx context.Context, pool *pgxpool.Pool, now func() time.Time) (*geocoding.Manager, error) {
	runner, err := newTheaterGeocodingRunner(geocoding.NewPostgresStore(pool), runtimeconfig.DefaultRequestTimeout, now)
	if err != nil {
		return nil, err
	}
	return geocoding.NewManager(ctx, now, runner, geocoding.NewPostgresRunStore(pool), geocoding.NewPostgresRunLocker(pool))
}

func newTheaterGeocodingRunner(store geocoding.Store, timeout time.Duration, now func() time.Time) (*geocoding.Runner, error) {
	client, err := ign.NewClient(ign.Config{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	return geocoding.NewRunner(store, client, now)
}

func loadSyncProxies(path string, open func(string) (io.ReadCloser, error)) ([]syncproxy.Proxy, error) {
	if path == "" {
		return nil, nil
	}
	file, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("sync configuration is invalid")
	}
	proxies, parseErr := syncproxy.Parse(file)
	closeErr := file.Close()
	if parseErr != nil || closeErr != nil {
		return nil, fmt.Errorf("sync configuration is invalid")
	}
	return proxies, nil
}
