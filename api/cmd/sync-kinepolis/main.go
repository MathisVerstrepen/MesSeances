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
	"movieflow/api/internal/kinepolis"
	"movieflow/api/internal/schedule"
	"movieflow/api/internal/syncproxy"
	"movieflow/api/internal/tmdb"
)

func main() {
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		fmt.Fprintln(os.Stderr, "configuration error")
		os.Exit(2)
	}
	os.Exit(run(context.Background(), os.Args[1:], time.Now))
}

func run(ctx context.Context, args []string, now func() time.Time) int {
	return runWithIO(ctx, args, now, os.Stdout, os.Stderr)
}

func runWithIO(ctx context.Context, args []string, now func() time.Time, stdout, stderr io.Writer) int {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		fmt.Fprintln(stderr, "configuration error: schedule timezone unavailable")
		return 2
	}
	today := now().In(location)
	flags := flag.NewFlagSet("sync-kinepolis", flag.ContinueOnError)
	flags.SetOutput(stderr)
	from, through, proxyFile := "", "", ""
	timeout, requestInterval := 20*time.Second, 2*time.Second
	flags.StringVar(&from, "from", today.Format("2006-01-02"), "first service date")
	flags.StringVar(&through, "through", today.AddDate(0, 0, 7).Format("2006-01-02"), "last service date (inclusive)")
	flags.DurationVar(&timeout, "timeout", 20*time.Second, "request timeout")
	flags.DurationVar(&requestInterval, "request-interval", 2*time.Second, "delay between request starts")
	flags.StringVar(&proxyFile, "proxy-file", "", "required proxy file")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	fromDate, e1 := time.ParseInLocation("2006-01-02", from, location)
	throughDate, e2 := time.ParseInLocation("2006-01-02", through, location)
	if e1 != nil || e2 != nil || fromDate.Format("2006-01-02") != from || throughDate.Format("2006-01-02") != through || !schedule.ValidInclusiveDateWindow(fromDate, throughDate) {
		fmt.Fprintln(stderr, "configuration error: date window must contain 1 to 14 inclusive days")
		return 2
	}
	if proxyFile == "" {
		fmt.Fprintln(stderr, "configuration error: proxy-file is required")
		return 2
	}
	file, err := os.Open(proxyFile)
	if err != nil {
		fmt.Fprintln(stderr, "configuration error: proxy file unavailable")
		return 2
	}
	proxies, parseErr := syncproxy.Parse(file)
	closeErr := file.Close()
	if parseErr != nil || closeErr != nil {
		fmt.Fprintln(stderr, "configuration error: proxy file is invalid")
		return 2
	}
	client, err := kinepolis.NewClient(kinepolis.ClientConfig{Proxies: proxies, RequestInterval: requestInterval, Timeout: timeout})
	if err != nil {
		fmt.Fprintln(stderr, "configuration error: invalid transport settings")
		return 2
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		fmt.Fprintln(stderr, "configuration error: DATABASE_URL is required")
		return 2
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	pool, err := database.OpenPool(startup, databaseURL)
	if err == nil {
		err = database.RunMigrations(startup, pool)
	}
	cancel()
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		fmt.Fprintln(stderr, "sync failed: database startup failed")
		return 1
	}
	defer pool.Close()
	data, summary, err := kinepolis.Sync(ctx, client, kinepolis.SyncOptions{From: from, Through: through, Now: now()})
	if err != nil {
		fmt.Fprintf(stderr, "sync failed: %v\n", err)
		return 1
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, 2*time.Minute)
	version, err := schedule.NewPostgresStore(pool).Replace(writeCtx, data)
	writeCancel()
	if err != nil {
		fmt.Fprintln(stderr, "sync failed: database replacement failed")
		return 1
	}
	fmt.Fprintf(stdout, "sync complete provider=kinepolis version=%d cinemas=%d showtimes=%d generated_at=%s\n", version, summary.Cinemas, summary.Showtimes, summary.GeneratedAt.Format(time.RFC3339))
	token := strings.TrimSpace(os.Getenv("TMDB_API_READ_ACCESS_TOKEN"))
	if token == "" {
		fmt.Fprintln(stdout, "enrichment=skipped")
		return 0
	}
	provider, err := tmdb.NewClient(token)
	if err != nil {
		fmt.Fprintln(stdout, "enrichment=degraded")
		return 0
	}
	unique := map[string]enrichment.Movie{}
	for _, showing := range data.Showtimes {
		unique[showing.Movie.ProviderID] = enrichment.Movie{SourceProvider: enrichment.SourceKinepolis, ProviderID: showing.Movie.ProviderID, Title: showing.Movie.Title, RuntimeMinutes: showing.Movie.RuntimeMinutes}
	}
	movies := make([]enrichment.Movie, 0, len(unique))
	for _, movie := range unique {
		movies = append(movies, movie)
	}
	enrichCtx, enrichCancel := context.WithTimeout(ctx, 2*time.Minute)
	result, err := enrichment.NewMatcher(enrichment.NewPostgresStore(pool), provider, time.Now).Run(enrichCtx, movies)
	enrichCancel()
	status := "complete"
	if err != nil {
		status = "degraded"
	}
	fmt.Fprintf(stdout, "enrichment=%s reused=%d matched=%d review_required=%d unmatched=%d failed=%d\n", status, result.Reused, result.Matched, result.ReviewRequired, result.Unmatched, result.Failed)
	return 0
}
