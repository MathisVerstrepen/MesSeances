package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/httpapi"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/tmdb"
)

func main() {
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		log.Fatal("configuration error")
	}
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("database configuration is missing")
	}
	proxies, err := loadSyncProxies(strings.TrimSpace(os.Getenv("PROXY_FILE")), func(path string) (io.ReadCloser, error) { return os.Open(path) })
	if err != nil {
		return err
	}
	startupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := database.OpenPool(startupCtx, databaseURL)
	if err != nil {
		return fmt.Errorf("database startup failed")
	}
	defer pool.Close()
	if err := database.RunMigrations(startupCtx, pool); err != nil {
		return fmt.Errorf("database migration failed")
	}
	source, err := schedule.NewPostgresSource(startupCtx, schedule.NewPostgresStore(pool))
	if err != nil {
		return fmt.Errorf("schedule snapshot startup failed")
	}
	service, err := schedule.NewService(source, schedule.ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
	if err != nil {
		return fmt.Errorf("schedule service startup failed")
	}
	enrichmentStore := enrichment.NewPostgresStore(pool)
	var enrichmentProvider enrichment.Provider
	if token := strings.TrimSpace(os.Getenv("TMDB_API_READ_ACCESS_TOKEN")); token != "" {
		tmdbClient, err := tmdb.NewClient(token)
		if err != nil {
			return fmt.Errorf("TMDB configuration is invalid")
		}
		enrichmentProvider = tmdbClient
	}
	adminOptions := newAdminOptions(os.Getenv("ADMIN_PASSWORD"), enrichmentStore, enrichmentProvider)
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	var syncManager httpapi.SyncController
	if len(proxies) != 0 {
		executor, err := synccontrol.NewProductionExecutor(proxies, schedule.NewPostgresStore(pool), enrichmentStore, enrichmentProvider, time.Now)
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		manager, err := synccontrol.NewManager(workerCtx, time.Now, executor)
		if err != nil {
			return fmt.Errorf("sync configuration is invalid")
		}
		syncManager = manager
	}
	port := envOrDefault("PORT", "8080")
	adminOptions.Syncs = syncManager
	server := &http.Server{Addr: ":" + port, Handler: httpapi.NewHandlerWithAdmin(service, envOrDefault("WEB_ORIGIN", "http://localhost:3000"), adminOptions), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("API MesSeances à l'écoute sur http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("API server failed")
	}
	return nil
}

func newAdminOptions(password string, store *enrichment.PostgresStore, provider enrichment.Provider) httpapi.AdminOptions {
	return httpapi.AdminOptions{
		Password:    password,
		Reviews:     enrichment.NewReviewService(store, provider, nil),
		LocalMovies: enrichment.NewLocalMovieService(store),
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

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
