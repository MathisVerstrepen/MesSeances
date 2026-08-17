package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	runtimeconfig "movieflow/api/internal/config"
	"movieflow/api/internal/database"
	"movieflow/api/internal/enrichment"
	"movieflow/api/internal/schedule"
	"movieflow/api/internal/tmdb"
	"movieflow/api/internal/ugc"
)

type config struct {
	proxyFile, from, through, cinemaID string
	proxyLimit                         int
	requestInterval, timeout           time.Duration
}

type dependencies struct {
	getenv       func(string) string
	sync         func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	openDatabase func(context.Context, string) (databaseServices, func(), error)
	newTMDB      func(string) (enrichment.Provider, error)
	enrich       func(context.Context, enrichment.Store, enrichment.Provider, []enrichment.Movie) (enrichment.Summary, error)
}

type databaseServices struct {
	writer     schedule.SnapshotWriter
	enrichment enrichment.Store
}

func productionDependencies() dependencies {
	return dependencies{getenv: os.Getenv, sync: ugc.Sync, openDatabase: func(ctx context.Context, databaseURL string) (databaseServices, func(), error) {
		pool, err := database.OpenPool(ctx, databaseURL)
		if err != nil {
			return databaseServices{}, nil, fmt.Errorf("database open failed")
		}
		if err := database.RunMigrations(ctx, pool); err != nil {
			pool.Close()
			return databaseServices{}, nil, fmt.Errorf("database migration failed")
		}
		return databaseServices{writer: schedule.NewPostgresStore(pool), enrichment: enrichment.NewPostgresStore(pool)}, pool.Close, nil
	}, newTMDB: func(token string) (enrichment.Provider, error) { return tmdb.NewClient(token) }, enrich: func(ctx context.Context, store enrichment.Store, provider enrichment.Provider, movies []enrichment.Movie) (enrichment.Summary, error) {
		return enrichment.NewMatcher(store, provider, time.Now).Run(ctx, movies)
	}}
}

func main() {
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "configuration error")
		os.Exit(2)
	}
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runWithDependencies(ctx, args, stdout, stderr, now, productionDependencies())
}

func runWithDependencies(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, deps dependencies) int {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		fmt.Fprintln(stderr, "configuration error: schedule timezone unavailable")
		return 2
	}
	today := now().In(location)
	flags := flag.NewFlagSet("sync-ugc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cfg := config{}
	flags.StringVar(&cfg.proxyFile, "proxy-file", "../tmp/proxies.txt", "required proxy file")
	flags.StringVar(&cfg.from, "from", today.Format("2006-01-02"), "first service date")
	flags.StringVar(&cfg.through, "through", today.AddDate(0, 0, 7).Format("2006-01-02"), "last service date (inclusive)")
	flags.StringVar(&cfg.cinemaID, "cinema-id", "", "diagnostic cinema ID")
	flags.IntVar(&cfg.proxyLimit, "proxy-limit", 0, "maximum proxies to use")
	flags.DurationVar(&cfg.requestInterval, "request-interval", 2*time.Second, "delay between request starts")
	flags.DurationVar(&cfg.timeout, "timeout", 20*time.Second, "per-request timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "configuration error: unexpected arguments")
		return 2
	}
	from, err := time.ParseInLocation("2006-01-02", cfg.from, location)
	if err != nil || from.Format("2006-01-02") != cfg.from {
		fmt.Fprintln(stderr, "configuration error: from must use YYYY-MM-DD")
		return 2
	}
	through, err := time.ParseInLocation("2006-01-02", cfg.through, location)
	if err != nil || through.Format("2006-01-02") != cfg.through || !schedule.ValidInclusiveDateWindow(from, through) {
		fmt.Fprintln(stderr, "configuration error: date window must contain 1 to 14 inclusive days")
		return 2
	}
	if err := ugc.ValidateCinemaID(cfg.cinemaID); err != nil {
		fmt.Fprintln(stderr, "configuration error: cinema-id must be a positive integer")
		return 2
	}
	if cfg.proxyLimit < 0 {
		fmt.Fprintln(stderr, "configuration error: proxy-limit cannot be negative")
		return 2
	}
	if cfg.requestInterval < time.Second {
		fmt.Fprintln(stderr, "configuration error: request-interval must be at least 1s")
		return 2
	}
	if cfg.timeout < 5*time.Second || cfg.timeout > 60*time.Second {
		fmt.Fprintln(stderr, "configuration error: timeout must be between 5s and 60s")
		return 2
	}
	proxyFile, err := os.Open(cfg.proxyFile)
	if err != nil {
		fmt.Fprintln(stderr, "configuration error: proxy file unavailable")
		return 2
	}
	proxies, parseErr := ugc.ParseProxies(proxyFile)
	closeErr := proxyFile.Close()
	if parseErr != nil || closeErr != nil {
		fmt.Fprintln(stderr, "configuration error: proxy file is invalid")
		return 2
	}
	if cfg.proxyLimit > 0 {
		if cfg.proxyLimit > len(proxies) {
			fmt.Fprintln(stderr, "configuration error: proxy-limit exceeds available entries")
			return 2
		}
		proxies = proxies[:cfg.proxyLimit]
	}
	client, err := ugc.NewClient(ugc.ClientConfig{Proxies: proxies, RequestInterval: cfg.requestInterval, Timeout: cfg.timeout})
	if err != nil {
		fmt.Fprintln(stderr, "configuration error: invalid transport settings")
		return 2
	}
	options := ugc.SyncOptions{From: cfg.from, Through: cfg.through, CinemaID: cfg.cinemaID, Now: now()}
	if cfg.cinemaID != "" {
		data, summary, err := deps.sync(ctx, client, options)
		if err != nil {
			fmt.Fprintf(stderr, "sync failed: %v\n", err)
			return 1
		}
		if err := schedule.ValidateDataset(data, false); err != nil || data.Scope != schedule.ScopeSingle {
			fmt.Fprintln(stderr, "sync failed: diagnostic dataset rejected")
			return 1
		}
		fmt.Fprintf(stdout, "sync complete mode=single_cinema persisted=false cinemas=%d skipped=%d dates=%d requests=%d showtimes=%d proxies=%d generated_at=%s\n", summary.Cinemas, summary.Skipped, summary.Dates, summary.Requests, summary.Showtimes, len(proxies), summary.GeneratedAt.Format(time.RFC3339))
		return 0
	}
	databaseURL := deps.getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		fmt.Fprintln(stderr, "configuration error: DATABASE_URL is required")
		return 2
	}
	openCtx, openCancel := context.WithTimeout(ctx, 30*time.Second)
	services, closeDatabase, err := deps.openDatabase(openCtx, databaseURL)
	openCancel()
	if err != nil {
		fmt.Fprintln(stderr, "sync failed: database startup failed")
		return 1
	}
	defer closeDatabase()
	data, summary, err := deps.sync(ctx, client, options)
	if err != nil {
		fmt.Fprintf(stderr, "sync failed: %v\n", err)
		return 1
	}
	if data.Scope != schedule.ScopeAll || schedule.ValidateDataset(data, true) != nil {
		fmt.Fprintln(stderr, "sync failed: complete dataset rejected")
		return 1
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, 2*time.Minute)
	version, err := services.writer.Replace(writeCtx, data)
	writeCancel()
	if err != nil {
		fmt.Fprintln(stderr, "sync failed: database replacement failed")
		return 1
	}
	fmt.Fprintf(stdout, "sync complete mode=all_cinemas persisted=true version=%d cinemas=%d skipped=%d dates=%d requests=%d showtimes=%d proxies=%d generated_at=%s\n", version, summary.Cinemas, summary.Skipped, summary.Dates, summary.Requests, summary.Showtimes, len(proxies), summary.GeneratedAt.Format(time.RFC3339))
	token := strings.TrimSpace(deps.getenv("TMDB_API_READ_ACCESS_TOKEN"))
	if token == "" {
		fmt.Fprintln(stdout, "enrichment=skipped")
		return 0
	}
	provider, err := deps.newTMDB(token)
	if err != nil || services.enrichment == nil {
		fmt.Fprintln(stderr, "warning: movie enrichment degraded")
		fmt.Fprintln(stdout, "enrichment=degraded")
		return 0
	}
	moviesByID := map[string]enrichment.Movie{}
	for _, showing := range data.Showtimes {
		moviesByID[showing.Movie.ProviderID] = enrichment.Movie{SourceProvider: enrichment.SourceUGC, ProviderID: showing.Movie.ProviderID, Title: showing.Movie.Title, RuntimeMinutes: showing.Movie.RuntimeMinutes}
	}
	movies := make([]enrichment.Movie, 0, len(moviesByID))
	for _, movie := range moviesByID {
		movies = append(movies, movie)
	}
	enrichmentCtx, enrichmentCancel := context.WithTimeout(ctx, 2*time.Minute)
	enrichmentSummary, err := deps.enrich(enrichmentCtx, services.enrichment, provider, movies)
	enrichmentCancel()
	if err != nil {
		fmt.Fprintln(stderr, "warning: movie enrichment degraded")
		fmt.Fprintf(stdout, "enrichment=degraded reused=%d matched=%d review_required=%d unmatched=%d failed=%d\n", enrichmentSummary.Reused, enrichmentSummary.Matched, enrichmentSummary.ReviewRequired, enrichmentSummary.Unmatched, enrichmentSummary.Failed)
		return 0
	}
	fmt.Fprintf(stdout, "enrichment=complete reused=%d matched=%d review_required=%d unmatched=%d failed=%d\n", enrichmentSummary.Reused, enrichmentSummary.Matched, enrichmentSummary.ReviewRequired, enrichmentSummary.Unmatched, enrichmentSummary.Failed)
	return 0
}
