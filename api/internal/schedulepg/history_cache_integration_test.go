package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
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
	background, err := s.BackgroundHistoryStatistics(t.Context(), schedule.StatisticsQuery{})
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
	if _, err := s.BackgroundHistoryStatistics(ctx, schedule.StatisticsQuery{}); !errors.Is(err, context.Canceled) {
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

func TestHistoryCachePresetBoundariesIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	dates := []string{"2026-10-13", "2026-10-14", "2026-10-19", "2026-10-20", "2026-10-21", "2026-10-26", "2026-10-27", "2026-10-28", "2026-11-18", "2026-11-19"}
	for _, date := range dates {
		data := testDataset()
		data.Window = Window{From: date, Through: date}
		for i := range data.Theaters {
			data.Theaters[i].AvailableDates = []string{date}
		}
		showing := data.Showtimes[0]
		showing.ServiceDate = date
		showing.StartTime, err = time.ParseInLocation("2006-01-02 15:04", date+" 12:00", location)
		if err != nil {
			t.Fatal(err)
		}
		showing.EndTime = showing.StartTime.Add(100 * time.Minute)
		data.Showtimes = []ShowtimeRecord{showing}
		historyPublish(t, s, data)
	}
	for _, tc := range []struct {
		name, now string
		counts    [4]int
	}{
		{"Tuesday", "2026-10-20T12:00:00Z", [4]int{3, 6, 3, 10}},
		{"ParisWednesday", "2026-10-20T22:00:00Z", [4]int{3, 6, 3, 10}},
		{"emptyPeriods", "2026-12-01T12:00:00Z", [4]int{0, 0, 0, 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			for i, period := range schedule.HistoryPeriods() {
				t.Run(string(period), func(t *testing.T) {
					query, err := schedule.HistoryPeriodQuery(period, now)
					if err != nil {
						t.Fatal(err)
					}
					background, err := s.BackgroundHistoryStatistics(t.Context(), query)
					if err != nil {
						t.Fatal(err)
					}
					live := historyGet(t, s, query)
					if background.GeneratedAt.IsZero() || background.Totals.Showtimes != tc.counts[i] {
						t.Fatal("wrong preset count", background.Totals, tc.counts[i])
					}
					if query.Date != "" && (background.Range == nil || background.Range.From != query.Date || background.Range.Through != query.DateTo) {
						t.Fatal("preset response lost inclusive range", background.Range, query)
					}
					var gotDates, wantDates []string
					for _, day := range background.DailyShowtimes {
						if day.ShowtimeCount != 0 {
							gotDates = append(gotDates, day.Date)
						}
					}
					for _, date := range dates {
						if query.Date == "" || date >= query.Date && date <= query.DateTo {
							wantDates = append(wantDates, date)
						}
					}
					if !slices.Equal(gotDates, wantDates) {
						t.Fatal("included or excluded wrong boundary screening", gotDates, wantDates)
					}
					background.GeneratedAt = live.GeneratedAt
					if !reflect.DeepEqual(background, live) {
						t.Fatal("preset background differs from equivalent live aggregation")
					}
				})
			}
		})
	}
}
