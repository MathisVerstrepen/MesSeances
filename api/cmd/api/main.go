package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncproxy"
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
	serverWriteTimeout      = 30 * time.Second
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
	if len(proxies) != 0 {
		var enrich synccontrol.EnrichFunc
		if enrichmentProvider != nil {
			enrich = func(ctx context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
				summary, err := enrichment.NewMatcher(enrichmentStore, enrichmentProvider, time.Now).Run(ctx, movies)
				return &summary, err
			}
		}
		executor, err := synccontrol.NewProductionExecutor(synccontrol.ProductionExecutorOptions{
			Writer: store, Now: time.Now, Logger: logger, Observer: metrics, Enrich: enrich, OperationTimeout: syncConfig.Sync.OperationTimeout,
			NewUGC: func() (ugc.Getter, error) {
				return ugc.NewClient(ugc.ClientConfig{Proxies: proxies, Timeout: syncConfig.Sync.RequestTimeout})
			},
			NewKinepolis: func() (kinepolis.Fetcher, error) {
				return kinepolis.NewClient(kinepolis.ClientConfig{Proxies: proxies, RequestInterval: syncConfig.Sync.KinepolisRequestInterval, Timeout: syncConfig.Sync.RequestTimeout})
			},
		})
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		manager, err := synccontrol.NewManager(workerCtx, time.Now, executor)
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		concreteSyncManager = manager
		syncManager = manager
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
			stopWorkers()
			if concreteSyncManager != nil {
				concreteSyncManager.Close()
			}
			polling.Wait()
		})
	}
	defer cleanup()
	adminOptions.Syncs = syncManager
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           newAPIHandler(service, cfg, adminOptions),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
	logger.Info("api_listening", "component", "api")
	return serve(ctx, server, cleanup)
}

func newAPIHandler(service *schedule.Service, cfg runtimeconfig.Config, adminOptions httpapi.AdminOptions) http.Handler {
	return httpapi.NewHandlerWithAdmin(service, cfg.Server.Origin, adminOptions)
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
	return httpapi.AdminOptions{
		Password:      password,
		SessionSecret: sessionSecret,
		Reviews:       enrichment.NewReviewService(store, provider, nil),
		LocalMovies:   enrichment.NewLocalMovieService(store),
	}
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
