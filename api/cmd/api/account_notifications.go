package main

import (
	"context"
	"log/slog"
	"time"

	"messeances/api/internal/accounts"
)

type accountNotificationSweeper interface {
	SweepNotifications(context.Context) (accounts.NotificationSweepResult, error)
}

// Runtime owns cancellation and joins this loop before closing the pool. A
// timer after each cycle prevents overlapping work or queued catch-up ticks.
func runAccountNotifications(ctx context.Context, service accountNotificationSweeper, logger *slog.Logger) {
	runAccountNotificationCycles(ctx, service, logger, 15*time.Second)
}

func runAccountNotificationCycles(ctx context.Context, service accountNotificationSweeper, logger *slog.Logger, interval time.Duration) {
	for ctx.Err() == nil {
		result, err := service.SweepNotifications(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("account_notifications_failed", "component", "accounts")
		} else if result.Events > 0 || result.Expired > 0 {
			logger.Info("account_notifications_completed", "component", "accounts", "events", result.Events, "notifications", result.Notifications, "expired", result.Expired)
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
