package main

import (
	"context"
	"log/slog"
	"time"

	"messeances/api/internal/httpapi"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

const historyCacheRefreshInterval = 15 * time.Minute

type historyCacheReader interface {
	AllTimeHistoryStatistics(context.Context) (schedule.HistoryStatistics, error)
}

func runHistoryCache(ctx context.Context, reader historyCacheReader, cache *httpapi.HistoryCache, logger *slog.Logger) {
	ticker := time.NewTicker(historyCacheRefreshInterval)
	defer ticker.Stop()
	runHistoryCacheTicks(ctx, reader, cache, logger, ticker.C)
}

func runHistoryCacheTicks(ctx context.Context, reader historyCacheReader, cache *httpapi.HistoryCache, logger *slog.Logger, ticks <-chan time.Time) {
	for ctx.Err() == nil {
		refreshCtx, cancel := context.WithTimeout(ctx, schedulepg.HistoryCacheTimeout)
		err := cache.Refresh(refreshCtx, reader.AllTimeHistoryStatistics)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("history_cache_refresh_failed", "component", "history")
		} else {
			logger.Info("history_cache_refreshed", "component", "history")
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
		}
	}
}
