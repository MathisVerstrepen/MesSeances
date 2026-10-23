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
	BackgroundHistoryStatistics(context.Context, schedule.StatisticsQuery) (schedule.HistoryStatistics, error)
}

func runHistoryCache(ctx context.Context, reader historyCacheReader, cache *httpapi.HistoryCache, logger *slog.Logger, updates <-chan time.Time) {
	ticker := time.NewTicker(historyCacheRefreshInterval)
	defer ticker.Stop()
	runHistoryCacheEvents(ctx, reader, cache, logger, ticker.C, updates)
}

func runHistoryCacheTicks(ctx context.Context, reader historyCacheReader, cache *httpapi.HistoryCache, logger *slog.Logger, ticks <-chan time.Time) {
	runHistoryCacheEvents(ctx, reader, cache, logger, ticks, nil)
}

func runHistoryCacheEvents(ctx context.Context, reader historyCacheReader, cache *httpapi.HistoryCache, logger *slog.Logger, ticks, updates <-chan time.Time) {
	for ctx.Err() == nil {
		for _, period := range schedule.HistoryPeriods() {
			refreshCtx, cancel := context.WithTimeout(ctx, schedulepg.HistoryCacheTimeout)
			err := cache.Refresh(refreshCtx, period, func(ctx context.Context) (schedule.HistoryStatistics, error) {
				query, err := schedule.HistoryPeriodQuery(period, time.Now())
				if err != nil {
					return schedule.HistoryStatistics{}, err
				}
				return reader.BackgroundHistoryStatistics(ctx, query)
			})
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				logger.Warn("history_cache_refresh_failed", "component", "history", "period", period)
			} else {
				logger.Info("history_cache_refreshed", "component", "history", "period", period)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-updates:
		case _, ok := <-ticks:
			if !ok {
				return
			}
		}
	}
}
