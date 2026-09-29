package main

import (
	"context"
	"log/slog"
	"time"

	"messeances/api/internal/accounts"
)

type accountWatchlistReleaseSweeper interface {
	SweepWatchlistReleases(context.Context) (accounts.WatchlistReleaseResult, error)
}

// Runtime owns cancellation and joins this loop before closing the pool. A
// timer after each cycle prevents overlapping work or queued catch-up ticks.
func runAccountWatchlistReleases(ctx context.Context, service accountWatchlistReleaseSweeper, logger *slog.Logger) {
	runAccountWatchlistReleaseCycles(ctx, service, logger, 10*time.Second)
}

func runAccountWatchlistReleaseCycles(ctx context.Context, service accountWatchlistReleaseSweeper, logger *slog.Logger, interval time.Duration) {
	for ctx.Err() == nil {
		result, err := service.SweepWatchlistReleases(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("account_watchlist_releases_failed", "component", "accounts")
		} else if result.Attempted > 0 {
			logger.Info("account_watchlist_releases_completed", "component", "accounts", "attempted", result.Attempted, "verified", result.Verified, "unavailable", result.Unavailable)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
