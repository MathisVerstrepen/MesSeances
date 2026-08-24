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

	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/httpapi"
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
		logger.Error("process_start_failed", "component", "api", "error_code", "configuration_error")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		logger.Error("process_stopped", "component", "api", "error_code", "process_failure")
		os.Exit(1)
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
		return fmt.Errorf("database migration failed")
	}
	store := schedulepg.NewStore(pool)
	source, err := schedule.NewPostgresSource(startupCtx, store, schedule.SourceOptions{Logger: logger, Observer: metrics})
	if err != nil {
		return fmt.Errorf("schedule snapshot startup failed")
	}
	service, err := schedule.NewService(source, schedule.ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
	if err != nil {
		return fmt.Errorf("schedule service startup failed")
	}
	enrichmentStore := enrichment.NewPostgresStore(pool)
	var enrichmentProvider enrichment.Provider
	if cfg.TMDB.Token != "" {
		tmdbClient, err := tmdb.NewClient(cfg.TMDB.Token)
		if err != nil {
			return fmt.Errorf("TMDB configuration is invalid")
		}
		enrichmentProvider = tmdbClient
	}
	adminOptions := newAdminOptions(cfg.Admin.Password, cfg.Admin.SessionSecret, enrichmentStore, enrichmentProvider)
	adminOptions.Logger = logger
	adminOptions.Metrics = metrics
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
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
		runStore := synccontrol.NewPostgresRunStore(pool)
		manager, err := synccontrol.NewManager(workerCtx, time.Now, executor, runStore, synccontrol.NewPostgresRunLocker(pool))
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		scheduler, err := syncschedule.NewService(syncschedule.NewPostgresStore(pool), manager)
		if err != nil {
			manager.Close()
			return fmt.Errorf("sync schedule configuration is invalid")
		}
		if err := scheduler.Start(workerCtx); err != nil {
			scheduler.Close()
			manager.Close()
			return fmt.Errorf("sync schedule configuration is invalid")
		}
		concreteSyncManager = manager
		syncManager = manager
		syncScheduler = scheduler
	}
	var polling sync.WaitGroup
	polling.Add(1)
	go func() {
		defer polling.Done()
		source.Run(workerCtx)
	}()
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			shutdownWorkers(stopWorkers, syncScheduler, concreteSyncManager, &polling)
		})
	}
	defer cleanup()
	adminOptions.Syncs = syncManager
	adminOptions.SyncSchedules = syncScheduler
	shortlinkService := shortlink.NewService(shortlink.NewPostgresStore(pool), shortlink.ServiceOptions{})
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           newAPIHandler(service, cfg, adminOptions, shortlinkService),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
	logger.Info("api_listening", "component", "api")
	return serve(ctx, server, cleanup)
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
	}
}

type closeableWorker interface {
	Close()
}

func shutdownWorkers(stopWorkers context.CancelFunc, schedules, manager closeableWorker, polling *sync.WaitGroup) {
	stopWorkers()
	if schedules != nil {
		schedules.Close()
	}
	if manager != nil {
		manager.Close()
	}
	polling.Wait()
}

func newAPIHandler(service *schedule.Service, cfg runtimeconfig.Config, adminOptions httpapi.AdminOptions, shortlinks httpapi.ShortlinkService) http.Handler {
	return httpapi.NewHandlerWithOptions(service, cfg.Server.Origin, httpapi.HandlerOptions{Admin: adminOptions, Shortlinks: shortlinks})
}

func loadAPIConfiguration(getenv func(string) string) (runtimeconfig.Config, runtimeconfig.Config, error) {
	cfg, err := runtimeconfig.Load(runtimeconfig.APIBase, getenv, nil)
	if err != nil {
		return runtimeconfig.Config{}, runtimeconfig.Config{}, err
	}
	if cfg.Proxy.Path == "" {
		return cfg, runtimeconfig.Config{}, nil
	}
	syncConfig, err := runtimeconfig.Load(runtimeconfig.APISync, getenv, nil)
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

func newAdminOptions(password, sessionSecret string, store *enrichment.PostgresStore, provider enrichment.Provider) httpapi.AdminOptions {
	options := httpapi.AdminOptions{
		Password:      password,
		SessionSecret: sessionSecret,
		Reviews:       enrichment.NewReviewService(store, provider, nil),
		LocalMovies:   enrichment.NewLocalMovieService(store),
	}
	if provider != nil {
		options.TMDBReruns = enrichment.NewRerunService(store, enrichment.NewMatcher(store, provider, nil))
	}
	return options
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
