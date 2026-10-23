package main

import (
	"context"
	"log/slog"
	"time"
)

type historyStatisticsProcessor interface {
	ProcessHistoryStatistics(context.Context) (bool, error)
}

// Counter maintenance has its own budget and never runs in an HTTP request.
// The first pass backfills existing history; subsequent passes consume deltas.
func runHistoryStatistics(ctx context.Context, processor historyStatisticsProcessor, updates chan<- time.Time, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	runHistoryStatisticsTicks(ctx, processor, updates, logger, ticker.C)
}

func runHistoryStatisticsTicks(ctx context.Context, processor historyStatisticsProcessor, updates chan<- time.Time, logger *slog.Logger, ticks <-chan time.Time) {
	for ctx.Err() == nil {
		processCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		changed, err := processor.ProcessHistoryStatistics(processCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("history_statistics_processing_failed", "component", "history")
		} else if changed {
			logger.Info("history_statistics_processed", "component", "history")
			select {
			case updates <- time.Now():
			default:
			}
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
