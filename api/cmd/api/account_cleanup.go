package main

import (
	"context"
	"log/slog"
	"time"

	"messeances/api/internal/accounts"
)

type accountCleaner interface {
	Cleanup(context.Context) (accounts.CleanupResult, error)
}

// Worker is owned by the runtime worker context and WaitGroup. Failures affect
// account retention monitoring, not anonymous browsing or catalog readiness.
func runAccountCleanup(ctx context.Context, service accountCleaner, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		result, err := service.Cleanup(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("account_cleanup_failed", "component", "accounts")
		} else {
			logger.Info("account_cleanup_completed", "component", "accounts", "pending_accounts", result.PendingAccounts, "expired_rows", result.ExpiredRows)
			if result.Overdue {
				logger.Warn("account_cleanup_overdue", "component", "accounts")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
