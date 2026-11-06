package trailer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Fetcher interface {
	Fetch(context.Context, string) (*Media, error)
}

// Run observes committed publications, including metadata-only refreshes. It
// owns no schedule publication and never propagates provider errors into one.
func Run(ctx context.Context, store *Store, fetcher Fetcher, logger *slog.Logger) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		cycleCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		summary, err := RunCycle(cycleCtx, store, fetcher, time.Now)
		if summary.Attempted > 0 {
			logger.Info("provider_trailer_lookup", "attempted", summary.Attempted, "found", summary.Found, "missing", summary.Missing, "failed", summary.Failed)
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.Warn("provider_trailer_cycle_failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Summary counts network outcomes, not playback or distinct film coverage.
type Summary struct{ Attempted, Found, Missing, Failed int }

func RunCycle(ctx context.Context, store *Store, fetcher Fetcher, now func() time.Time) (Summary, error) {
	var summary Summary
	err := store.withLease(ctx, func(leased *Store) error {
		if err := leased.expire(ctx, now().UTC()); err != nil {
			return err
		}
		if fetcher == nil {
			return nil
		}
		ids, err := leased.due(ctx, now().UTC())
		if err != nil {
			return err
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			summary.Attempted++
			requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			media, fetchErr := fetcher.Fetch(requestCtx, id)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if media != nil && (!Valid(media) || media.Source != "ugc") {
				media = nil
				fetchErr = fmt.Errorf("invalid trailer result")
			}
			if err := leased.saveLookup(ctx, id, media, fetchErr != nil, now().UTC()); err != nil {
				return err
			}
			if fetchErr != nil {
				summary.Failed++
				if errors.Is(fetchErr, ErrUGCBlocked) {
					return nil
				}
			} else if media == nil {
				summary.Missing++
			} else {
				summary.Found++
			}
		}
		return nil
	})
	return summary, err
}
