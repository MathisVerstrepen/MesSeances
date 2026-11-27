package main

import (
	"context"
	"log/slog"
	"time"

	"messeances/api/internal/accounts"
)

type accountPushDeliverer interface {
	DeliverPush(context.Context) (accounts.PushDeliveryResult, error)
}

// Logs carry counters only: never endpoints, titles or account identifiers.
func runAccountPush(ctx context.Context, service accountPushDeliverer, logger *slog.Logger) {
	runAccountPushCycles(ctx, service, logger, 5*time.Second)
}

func runAccountPushCycles(ctx context.Context, service accountPushDeliverer, logger *slog.Logger, interval time.Duration) {
	for ctx.Err() == nil {
		result, err := service.DeliverPush(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("account_push_failed", "component", "accounts")
		} else if result != (accounts.PushDeliveryResult{}) {
			logger.Info("account_push_completed", "component", "accounts", "sent", result.Sent, "retried", result.Retried, "dropped", result.Dropped, "removed", result.Removed)
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
