package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/schedule"
)

func TestHistoryCacheAggregationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, testDataset(), kinepolisTestDataset())
	live := historyGet(t, s, schedule.StatisticsQuery{})
	background, err := s.AllTimeHistoryStatistics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if background.GeneratedAt.IsZero() {
		t.Fatal("background response lacks generated_at")
	}
	background.GeneratedAt = live.GeneratedAt
	if !reflect.DeepEqual(background, live) {
		t.Fatal("background aggregation differs from live schema or semantics")
	}
	// Longer background settings remain transaction-local and read-only; live
	// calls immediately afterwards still get their original timeout limits.
	for _, tc := range []struct {
		name      string
		timeout   time.Duration
		statement string
		setting   string
	}{
		{"background", HistoryCacheTimeout, "120s", "2min"},
		{"live", 3 * time.Second, "2s", "2s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.historyReadWithTimeout(t.Context(), tc.timeout, tc.statement, func(ctx context.Context, tx pgx.Tx) error {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > tc.timeout {
					return fmt.Errorf("unexpected history deadline")
				}
				var timeout, isolation, readonly, timezone, plan string
				if err := tx.QueryRow(ctx, `SELECT current_setting('statement_timeout'),current_setting('transaction_isolation'),current_setting('transaction_read_only'),current_setting('timezone'),current_setting('plan_cache_mode')`).Scan(&timeout, &isolation, &readonly, &timezone, &plan); err != nil {
					return err
				}
				if timeout != tc.setting || isolation != "repeatable read" || readonly != "on" || timezone != "UTC" || plan != "force_custom_plan" {
					return fmt.Errorf("unexpected history transaction settings")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	var statement string
	if err := pool.QueryRow(t.Context(), `SHOW statement_timeout`).Scan(&statement); err != nil || statement != "0" {
		t.Fatal("background timeout leaked into pool", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.AllTimeHistoryStatistics(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("background cancellation", err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err = s.historyReadWithTimeout(ctx, HistoryCacheTimeout, "120s", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT pg_sleep(5)`)
		return err
	})
	if !errors.Is(err, schedule.ErrHistoryQueryTimeout) {
		t.Fatal("parent deadline did not bound background query", err)
	}
}
