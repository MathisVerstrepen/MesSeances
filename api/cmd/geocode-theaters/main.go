package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/ign"
	"messeances/api/internal/observability"
)

type dependencies struct {
	loadDotEnv  func() error
	getenv      func(string) string
	openStore   func(context.Context, string) (geocoding.Store, func(), error)
	newProvider func(time.Duration) (geocoding.Provider, error)
	execute     func(context.Context, geocoding.Store, geocoding.Provider, geocoding.RunOptions, func() time.Time) (geocoding.Summary, error)
}

func productionDependencies() dependencies {
	return dependencies{
		loadDotEnv: runtimeconfig.LoadDotEnv,
		getenv:     os.Getenv,
		openStore: func(ctx context.Context, databaseURL string) (geocoding.Store, func(), error) {
			pool, err := database.OpenPool(ctx, databaseURL)
			if err != nil {
				return nil, nil, fmt.Errorf("database open failed")
			}
			if err := database.RunMigrations(ctx, pool); err != nil {
				pool.Close()
				return nil, nil, fmt.Errorf("database migration failed")
			}
			return geocoding.NewPostgresStore(pool), pool.Close, nil
		},
		newProvider: func(timeout time.Duration) (geocoding.Provider, error) {
			return ign.NewClient(ign.Config{Timeout: timeout})
		},
		execute: func(ctx context.Context, store geocoding.Store, provider geocoding.Provider, options geocoding.RunOptions, now func() time.Time) (geocoding.Summary, error) {
			runner, err := geocoding.NewRunner(store, provider, now)
			if err != nil {
				return geocoding.Summary{DryRun: options.DryRun}, err
			}
			return runner.Run(ctx, options)
		},
	}
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], time.Now))
}

func run(ctx context.Context, args []string, now func() time.Time) int {
	return runWithIO(ctx, args, now, os.Stdout, os.Stderr)
}

func runWithIO(ctx context.Context, args []string, now func() time.Time, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, args, now, stdout, stderr, productionDependencies())
}

func runWithDependencies(ctx context.Context, args []string, now func() time.Time, stdout, stderr io.Writer, deps dependencies) int {
	flags := flag.NewFlagSet("geocode-theaters", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun, retryAmbiguous := false, false
	provider, theaterID := "", ""
	limit := 0
	timeout := runtimeconfig.DefaultRequestTimeout
	flags.BoolVar(&dryRun, "dry-run", false, "evaluate IGN results without database writes (still sends requests)")
	flags.StringVar(&provider, "provider", "", "limit to ugc, kinepolis, pathe, or cgr")
	flags.StringVar(&theaterID, "theater-id", "", "limit to one canonical theater ID")
	flags.IntVar(&limit, "limit", 0, "maximum processable theaters (0 means unlimited)")
	flags.BoolVar(&retryAmbiguous, "retry-ambiguous", false, "retry unchanged ambiguous locations")
	flags.DurationVar(&timeout, "timeout", runtimeconfig.DefaultRequestTimeout, "IGN request timeout between 5s and 60s")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	provider = strings.TrimSpace(provider)
	theaterID = strings.TrimSpace(theaterID)
	explicitTheaterID := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "theater-id" {
			explicitTheaterID = true
		}
	})
	logger := observability.NewLogger(stderr)
	if provider != "" && !geocoding.ValidProvider(provider) || explicitTheaterID && theaterID == "" || limit < 0 || timeout < 5*time.Second || timeout > 60*time.Second {
		logError(logger, "geocode_configuration_failed", "configuration_error")
		return 2
	}
	if deps.loadDotEnv != nil {
		if err := deps.loadDotEnv(); err != nil {
			logError(logger, "geocode_configuration_failed", "configuration_error")
			return 2
		}
	}
	if deps.getenv == nil {
		deps.getenv = func(string) string { return "" }
	}
	overrides := &runtimeconfig.Overrides{}
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "timeout" {
			overrides.RequestTimeout = &timeout
		}
	})
	config, err := runtimeconfig.Load(runtimeconfig.Geocoding, deps.getenv, overrides)
	if err != nil {
		logError(logger, "geocode_configuration_failed", "configuration_error")
		return 2
	}
	if deps.newProvider == nil || deps.openStore == nil || deps.execute == nil {
		logError(logger, "geocode_configuration_failed", "configuration_error")
		return 2
	}
	providerClient, err := deps.newProvider(config.Sync.RequestTimeout)
	if err != nil {
		logError(logger, "geocode_configuration_failed", "configuration_error")
		return 2
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	store, closeStore, err := deps.openStore(startup, config.Database.URL)
	cancel()
	if err != nil {
		logError(logger, "geocode_command_failed", "database_startup_failed")
		return 1
	}
	defer closeStore()
	options := geocoding.RunOptions{Filters: geocoding.Filters{Provider: provider, TheaterID: theaterID}, Limit: limit, RetryAmbiguous: retryAmbiguous, DryRun: dryRun}
	summary, runErr := deps.execute(ctx, store, providerClient, options, now)
	fmt.Fprintf(stdout, "geocode complete dry_run=%t selected=%d skipped=%d matched=%d ambiguous=%d not_found=%d failed=%d written=%d\n", summary.DryRun, summary.Selected, summary.Skipped, summary.Matched, summary.Ambiguous, summary.NotFound, summary.Failed, summary.Written)
	if runErr != nil {
		logError(logger, "geocode_command_failed", "geocoding_failed")
		return 1
	}
	return 0
}

func logError(logger *slog.Logger, event, code string) {
	logger.Error(event, "component", "geocode_theaters", "error_code", code)
}
