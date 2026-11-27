package main

import (
	"context"
	"log/slog"
	"time"

	"messeances/api/internal/accounts"
)

type accountDigestSweeper interface {
	SweepDigests(context.Context) (accounts.DigestResult, error)
}

// Logs carry counters only: never addresses, titles or tokens.
func runAccountDigests(ctx context.Context, service accountDigestSweeper, logger *slog.Logger) {
	runAccountDigestCycles(ctx, service, logger, time.Minute)
}

func runAccountDigestCycles(ctx context.Context, service accountDigestSweeper, logger *slog.Logger, interval time.Duration) {
	for ctx.Err() == nil {
		result, err := service.SweepDigests(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("account_digests_failed", "component", "accounts")
		} else if result != (accounts.DigestResult{}) {
			logger.Info("account_digests_completed", "component", "accounts", "sent", result.Sent, "empty", result.Empty, "deferred", result.Deferred)
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
