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

	"messeances/api/internal/accountavatar"
	"messeances/api/internal/accountmail"
	"messeances/api/internal/accounts"
	"messeances/api/internal/cgr"
	"messeances/api/internal/cineville"
	"messeances/api/internal/cinewest"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/grandecran"
	"messeances/api/internal/httpapi"
	"messeances/api/internal/ign"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/megarama"
	"messeances/api/internal/mk2"
	"messeances/api/internal/noecinemas"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := dispatch(ctx, os.Args[1:], logger); err != nil {
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
	case "account avatar conversion required", "account avatar conversion failed":
		return processFailureDetail{stage: "migration", reason: err.Error()}
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
	// Own the media root before any schema change. The deferred close runs after
	// HTTP draining and worker cleanup, including every partial-startup failure.
	var avatars *accountavatar.Store
	if cfg.Accounts.Enabled {
		avatars, err = accountavatar.Open(cfg.Accounts.AvatarDir)
		if err != nil {
			return fmt.Errorf("configuration error")
		}
		defer func() {
			if err := avatars.Close(); err != nil {
				logger.Warn("account_avatar_close_failed")
			}
		}()
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
	if avatars != nil {
		if err := accounts.NewPostgresStore(pool).AvatarConversionReady(startupCtx); err != nil {
			if errors.Is(err, accounts.ErrAvatarConversionRequired) {
				return accounts.ErrAvatarConversionRequired
			}
			return fmt.Errorf("database migration failed")
		}
	}
	shortlinkStore := shortlink.NewPostgresStore(pool)
	if err := purgeShortlinksAtStartup(startupCtx, shortlinkStore, time.Now); err != nil {
		return err
	}
	runStore := synccontrol.NewPostgresRunStore(pool)
	if err := runStore.PurgeTerminalBefore(startupCtx, time.Now().UTC().Add(-synccontrol.TerminalRunRetentionPeriod)); err != nil {
		return fmt.Errorf("sync run retention startup failed")
	}
	schedules, err := newScheduleRuntime(startupCtx, pool, logger, metrics)
	if err != nil {
		return err
	}
	workerCtx, stopWorkers := context.WithCancel(ctx)
	var polling sync.WaitGroup
	var admin adminRuntime
	var syncs syncRuntime
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			shutdownWorkers(stopWorkers, syncs.scheduler, syncs.manager, admin.geocodingManager, admin.metadataRefreshManager, &polling)
			admin.upcomingManager.Close()
		})
	}
	defer cleanup()

	admin, err = newAdminRuntime(workerCtx, pool, cfg, logger, metrics)
	if err != nil {
		return err
	}
	syncs, err = newSyncRuntime(workerCtx, pool, schedules.store, runStore, proxies, syncConfig, admin, logger, metrics)
	if err != nil {
		return err
	}
	polling.Add(3)
	go func() {
		defer polling.Done()
		schedules.source.Run(workerCtx)
	}()
	go func() {
		defer polling.Done()
		runSyncRunRetention(workerCtx, runStore, logger, time.Now)
	}()
	go func() {
		defer polling.Done()
		runShortlinkRetention(workerCtx, shortlinkStore, logger, time.Now)
	}()
	admin.options.Syncs = syncs.controller
	admin.options.SyncSchedules = syncs.scheduler
	shortlinkService := shortlink.NewService(shortlinkStore, shortlink.ServiceOptions{})
	accountService, err := newAccountService(pool, cfg, avatars)
	if err != nil {
		return err
	}
	if accountService != nil {
		polling.Add(1)
		go func() { defer polling.Done(); runAccountAvatarCleanup(workerCtx, accountService, logger) }()
		polling.Add(1)
		go func() { defer polling.Done(); runAccountCleanup(workerCtx, accountService, logger) }()
		polling.Add(1)
		go func() { defer polling.Done(); runAccountMail(workerCtx, pool, cfg.Accounts, logger) }()
	}
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: newAPIHandler(schedules.service, cfg, admin.options, shortlinkService, schedules.store, httpapi.ReadinessOptions{
			Schedule:  schedules.source,
			Database:  pool,
			Revisions: schedules.store,
		}, accountService),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
	// Drain admitted HTTP work even when graceful shutdown times out. Closing the
	// listener/body transport alone does not wait for bounded synchronous decoders.
	requests := &accountRequestDrain{next: server.Handler}
	server.Handler = requests
	defer func() { requests.stop(); _ = server.Close(); requests.wait() }()
	logger.Info("api_listening", "component", "api")
	return serve(ctx, server, cleanup)
}

type scheduleRuntime struct {
	store   *schedulepg.Store
	source  *schedule.PostgresSource
	service *schedule.Service
}

func newScheduleRuntime(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, metrics *observability.Metrics) (scheduleRuntime, error) {
	store := schedulepg.NewStore(pool)
	source, err := schedule.NewPostgresSource(ctx, store, schedule.SourceOptions{Logger: logger, Observer: metrics})
	if err != nil {
		return scheduleRuntime{}, fmt.Errorf("schedule snapshot startup failed")
	}
	service, err := schedule.NewService(source, newProductionScheduleOptions())
	if err != nil {
		return scheduleRuntime{}, fmt.Errorf("schedule service startup failed")
	}
	return scheduleRuntime{store: store, source: source, service: service}, nil
}

type adminRuntime struct {
	upcomingManager        *enrichment.UpcomingManager
	options                httpapi.AdminOptions
	enrichmentStore        *enrichment.PostgresStore
	enrichmentProvider     enrichment.Provider
	metadataRefreshManager *enrichment.MetadataRefreshManager
	geocodingManager       *geocoding.Manager
}

func newAdminRuntime(ctx context.Context, pool *pgxpool.Pool, cfg runtimeconfig.Config, logger *slog.Logger, metrics *observability.Metrics) (adminRuntime, error) {
	store := enrichment.NewPostgresStore(pool)
	var provider adminTMDBProvider
	var upcomingProvider enrichment.UpcomingProvider
	if cfg.TMDB.Token != "" {
		client, err := tmdb.NewClient(cfg.TMDB.Token)
		if err != nil {
			return adminRuntime{}, fmt.Errorf("TMDB configuration is invalid")
		}
		provider = client
		upcomingProvider = client
	}
	gate := enrichment.NewTMDBRunGate()
	options, metadataRefreshManager, err := newAdminOptions(ctx, cfg.Admin.Password, cfg.Admin.SessionSecret, store, provider, gate)
	if err != nil {
		return adminRuntime{}, fmt.Errorf("TMDB metadata refresh configuration is invalid")
	}
	geocodingManager, err := newTheaterGeocodingManager(ctx, pool, time.Now)
	if err != nil {
		if metadataRefreshManager != nil {
			metadataRefreshManager.Close()
		}
		return adminRuntime{}, fmt.Errorf("geocoding configuration is invalid")
	}
	options.TheaterLocations = newTheaterLocationController(pool, time.Now)
	options.UpcomingReviews = enrichment.NewUpcomingReviewService(store, time.Now)
	options.TheaterGeocoding = geocodingManager
	options.Logger = logger
	options.Metrics = metrics
	var upcomingManager *enrichment.UpcomingManager
	if upcomingProvider != nil && cfg.Admin.Password != "" {
		upcomingManager, err = enrichment.NewUpcomingManager(ctx, enrichment.NewUpcomingService(store, upcomingProvider, nil, gate), enrichment.NewPostgresUpcomingLocker(pool))
		if err != nil {
			metadataRefreshManager.Close()
			geocodingManager.Close()
			return adminRuntime{}, fmt.Errorf("upcoming configuration is invalid")
		}
		options.TMDBUpcoming = upcomingManager
	}
	return adminRuntime{
		upcomingManager:        upcomingManager,
		options:                options,
		enrichmentStore:        store,
		enrichmentProvider:     provider,
		metadataRefreshManager: metadataRefreshManager,
		geocodingManager:       geocodingManager,
	}, nil
}

type syncRuntime struct {
	controller httpapi.SyncController
	manager    *synccontrol.Manager
	scheduler  *syncschedule.Service
}

func newSyncRuntime(ctx context.Context, pool *pgxpool.Pool, store *schedulepg.Store, runStore *synccontrol.PostgresRunStore, proxies []syncproxy.Proxy, cfg runtimeconfig.Config, admin adminRuntime, logger *slog.Logger, metrics *observability.Metrics) (syncRuntime, error) {
	var runtime syncRuntime
	if len(proxies) != 0 {
		var enrich synccontrol.EnrichFunc
		if admin.enrichmentProvider != nil {
			enrich = func(ctx context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
				summary, err := enrichment.NewMatcher(admin.enrichmentStore, admin.enrichmentProvider, time.Now).Run(ctx, movies)
				return &summary, err
			}
		}
		executor, err := synccontrol.NewProductionExecutor(newSyncExecutorOptions(store, proxies, cfg, enrich, time.Now, logger, metrics))
		if err != nil {
			return syncRuntime{}, fmt.Errorf("sync configuration is invalid")
		}
		manager, err := synccontrol.NewManager(ctx, time.Now, executor, runStore, synccontrol.NewPostgresRunLocker(pool))
		if err != nil {
			return syncRuntime{}, fmt.Errorf("sync configuration is invalid")
		}
		runtime.controller = manager
		runtime.manager = manager
	}
	if runtime.manager == nil && admin.metadataRefreshManager == nil && admin.upcomingManager == nil {
		return runtime, nil
	}
	scheduleStore := syncschedule.NewPostgresStore(pool)
	starter := syncScheduleStarter{claimer: scheduleStore}
	// Do not put typed nil pointers into availability interfaces.
	if runtime.manager != nil {
		starter.providers = runtime.manager
	}
	if admin.metadataRefreshManager != nil {
		starter.metadata = admin.metadataRefreshManager
	}
	if admin.upcomingManager != nil {
		starter.upcoming = admin.upcomingManager
	}
	scheduler, err := syncschedule.NewService(scheduleStore, starter)
	if err != nil {
		if runtime.manager != nil {
			runtime.manager.Close()
		}
		return syncRuntime{}, fmt.Errorf("sync schedule configuration is invalid")
	}
	if err := scheduler.Start(ctx); err != nil {
		scheduler.Close()
		if runtime.manager != nil {
			runtime.manager.Close()
		}
		return syncRuntime{}, fmt.Errorf("sync schedule configuration is invalid")
	}
	runtime.scheduler = scheduler
	return runtime, nil
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
		NewMegarama: func() (megarama.Getter, error) {
			return megarama.NewClient(megarama.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
		NewCineville: func() (cineville.Fetcher, error) {
			return cineville.NewClient(cineville.ClientConfig{Proxies: proxies, RequestInterval: cfg.Sync.CinevilleRequestInterval, Timeout: cfg.Sync.RequestTimeout})
		},
		NewMK2: func() (mk2.Fetcher, error) {
			return mk2.NewClient(mk2.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
		NewCinewest: func() (cinewest.Fetcher, error) {
			return cinewest.NewClient(cinewest.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
		NewGrandEcran: func() (grandecran.Getter, error) {
			return grandecran.NewClient(grandecran.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
		},
		NewNoeCinemas: func() (noecinemas.Getter, error) {
			return noecinemas.NewClient(noecinemas.ClientConfig{Proxies: proxies, Timeout: cfg.Sync.RequestTimeout})
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

func newAPIHandler(service *schedule.Service, cfg runtimeconfig.Config, adminOptions httpapi.AdminOptions, shortlinks httpapi.ShortlinkService, history httpapi.HistoryReader, readiness httpapi.ReadinessOptions, accountService *accounts.Service) http.Handler {
	return httpapi.NewHandlerWithOptions(service, cfg.Server.Origin, httpapi.HandlerOptions{
		Accounts:             httpapi.AccountOptions{Enabled: cfg.Accounts.Enabled, Service: accountService, Origin: cfg.Server.Origin},
		Admin:                adminOptions,
		Readiness:            readiness,
		Shortlinks:           shortlinks,
		History:              history,
		TrustedProxyCIDRs:    cfg.Server.TrustedProxyCIDRs,
		InternalSharedSecret: cfg.Internal.SharedSecret,
	})
}

func newAccountService(pool *pgxpool.Pool, cfg runtimeconfig.Config, avatars *accountavatar.Store) (*accounts.Service, error) {
	if !cfg.Accounts.Enabled {
		return nil, nil
	}
	hasher, err := accounts.NewArgonHasher(nil)
	if err != nil {
		return nil, fmt.Errorf("configuration error")
	}
	google, err := accounts.NewGoogleProvider(cfg.Accounts.GoogleClientID, cfg.Accounts.GoogleClientSecret, cfg.Accounts.GoogleCallbackURL)
	if err != nil {
		return nil, fmt.Errorf("configuration error")
	}
	cipher, err := accountmail.NewCipher(cfg.Accounts.OutboxKeyID, cfg.Accounts.OutboxKey[:], nil)
	if err != nil {
		return nil, fmt.Errorf("configuration error")
	}
	if avatars == nil {
		return nil, fmt.Errorf("configuration error")
	}
	service, err := accounts.NewService(accounts.NewPostgresStore(pool), accounts.ServiceOptions{
		Hasher: hasher, Origin: cfg.Server.Origin, AddressHMACKey: cfg.Accounts.AddressHMACKey[:],
		Google: google, FlowCipher: cipher, Mail: &accountmail.Outbox{Cipher: cipher},
		Avatars: avatars,
	})
	if err != nil {
		return nil, fmt.Errorf("configuration error")
	}
	return service, nil
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

type adminTMDBProvider interface {
	enrichment.Provider
	enrichment.AdminMoviePosterProvider
}

func newAdminOptions(ctx context.Context, password, sessionSecret string, store *enrichment.PostgresStore, provider adminTMDBProvider, gate *enrichment.TMDBRunGate) (httpapi.AdminOptions, *enrichment.MetadataRefreshManager, error) {
	options := httpapi.AdminOptions{
		Password:      password,
		SessionSecret: sessionSecret,
		Reviews:       enrichment.NewReviewService(store, provider, nil),
		LocalMovies:   enrichment.NewLocalMovieService(store),
		Movies:        enrichment.NewAdminMovieService(store, provider),
	}
	if provider != nil {
		if gate == nil {
			gate = enrichment.NewTMDBRunGate()
		}
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
