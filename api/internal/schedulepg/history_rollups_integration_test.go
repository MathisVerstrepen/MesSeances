package schedulepg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"messeances/api/internal/schedule"
)

func TestHistoryIncrementalQueueIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	data := testDataset()
	if _, err := s.Replace(t.Context(), []Dataset{data}); err != nil {
		t.Fatal(err)
	}
	// HTTP reads must not consume pending work or expose partial publications.
	before := historyGet(t, s, schedule.StatisticsQuery{})
	if before.Totals.Showtimes != 0 || before.Coverage.LastPublicationAt != nil {
		t.Fatal("unprocessed publication leaked", before)
	}
	historyProcess(t, s)
	first := historyGet(t, s, schedule.StatisticsQuery{})
	if first.Totals.Showtimes != len(data.Showtimes) || first.StatisticsRevision <= before.StatisticsRevision || first.StatisticsUpdatedAt == nil || first.Coverage.LastPublicationAt == nil {
		t.Fatal("publication not atomically applied", first)
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_statistics_pending`, 0)
	// Receipt/room-only updates must not enqueue work or change the applied revision.
	if _, err := pool.Exec(t.Context(), `UPDATE screening_history_showtimes SET last_seen_at=last_seen_at+interval '1 second',room='Receipt only'`); err != nil {
		t.Fatal(err)
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_statistics_pending`, 0)
	if changed, err := s.ProcessHistoryStatistics(t.Context()); err != nil || changed {
		t.Fatal("receipt update caused aggregation", changed, err)
	}
	// A source/date/hour/version/format correction must remove its old contribution.
	if _, err := pool.Exec(t.Context(), `UPDATE screening_history_showtimes SET movie_provider_id='201',start_time=start_time+interval '1 hour',language='VF',format='IMAX' WHERE provider='ugc' AND provider_showing_id='100'`); err != nil {
		t.Fatal(err)
	}
	unchanged := historyGet(t, s, schedule.StatisticsQuery{})
	if unchanged.StatisticsRevision != first.StatisticsRevision || !reflect.DeepEqual(unchanged.TopMovies, first.TopMovies) {
		t.Fatal("pending correction leaked")
	}
	// Fail after earlier summary tables were updated. Everything, including queue
	// acknowledgement, must roll back, and the next attempt must apply it once.
	if _, err := pool.Exec(t.Context(), `ALTER TABLE screening_statistics_all_time ADD CONSTRAINT reject_counter_update CHECK(showtime_count=0) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessHistoryStatistics(t.Context()); err == nil {
		t.Fatal("fault injection did not fail")
	}
	failed := historyGet(t, s, schedule.StatisticsQuery{})
	if failed.StatisticsRevision != first.StatisticsRevision || !reflect.DeepEqual(failed.TopMovies, first.TopMovies) {
		t.Fatal("failed batch exposed partial counters")
	}
	var pending int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM screening_statistics_pending`).Scan(&pending); err != nil || pending == 0 {
		t.Fatal("failed batch lost queue", err)
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE screening_statistics_all_time DROP CONSTRAINT reject_counter_update`); err != nil {
		t.Fatal(err)
	}
	historyProcess(t, s)
	corrected := historyGet(t, s, schedule.StatisticsQuery{Format: "IMAX", Language: "VF"})
	if corrected.Totals.Showtimes != 1 || corrected.StatisticsRevision <= first.StatisticsRevision {
		t.Fatal("correction not applied", corrected.Totals)
	}
	assertHistorySums(t, historyGet(t, s, schedule.StatisticsQuery{}))
	if changed, err := s.ProcessHistoryStatistics(t.Context()); err != nil || changed {
		t.Fatal("retry double applied", changed, err)
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_statistics_pending`, 0)
	// Rolled-back writes must leave neither queue changes nor counter changes.
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), `DELETE FROM screening_history_showtimes`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_statistics_pending`, 0)
	if got := historyGet(t, s, schedule.StatisticsQuery{}); got.Totals.Showtimes != len(data.Showtimes) {
		t.Fatal("rolled-back deletion changed counts", got.Totals)
	}
	// Real deletion removes zero buckets/memberships; missing provider observations
	// are handled separately by the existing retention tests and never enqueue it.
	if _, err = pool.Exec(t.Context(), `DELETE FROM screening_history_showtimes`); err != nil {
		t.Fatal(err)
	}
	historyProcess(t, s)
	for _, r := range historyRollups() {
		historyCount(t, pool, "SELECT count(*) FROM "+r.table, 0)
	}
	if got := historyGet(t, s, schedule.StatisticsQuery{}); got.Totals.Showtimes != 0 || got.Coverage.RecordedWindow != nil || len(got.Options.Languages) != 0 {
		t.Fatal("last membership retained", got)
	}
}

func TestHistoryBackfillAndConcurrentWorkersIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	data := testDataset()
	if _, err := s.Replace(t.Context(), []Dataset{data}); err != nil {
		t.Fatal(err)
	}
	// Mimic an upgraded existing database, including queued writes captured before
	// its initial backfill. Those writes must not be applied a second time.
	if _, err := pool.Exec(t.Context(), `UPDATE screening_statistics_state SET ready=false`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HistoryStatistics(t.Context(), schedule.StatisticsQuery{}); !errors.Is(err, schedule.ErrHistoryUnavailable) {
		t.Fatal("cold read model served incomplete data", err)
	}
	start := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := s.ProcessHistoryStatistics(t.Context()); done <- err }()
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "40001" {
				t.Fatal(err)
			}
		}
	}
	historyProcess(t, s)
	got := historyGet(t, s, schedule.StatisticsQuery{})
	if got.Totals.Showtimes != len(data.Showtimes) {
		t.Fatal("backfill counted queue twice", got.Totals)
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_statistics_pending`, 0)
	// TRUNCATE explicitly invalidates the read model and is repaired by backfill.
	if _, err := pool.Exec(t.Context(), `TRUNCATE screening_history_showtimes`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HistoryStatistics(t.Context(), schedule.StatisticsQuery{}); !errors.Is(err, schedule.ErrHistoryUnavailable) {
		t.Fatal("truncate did not invalidate", err)
	}
	historyProcess(t, s)
	if got := historyGet(t, s, schedule.StatisticsQuery{}); got.Totals.Showtimes != 0 {
		t.Fatal("truncate repair", got.Totals)
	}
}

func TestHistorySummarySnapshotIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, testDataset())
	before := historyGet(t, s, schedule.StatisticsQuery{})
	err := s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT applied_revision FROM screening_statistics_state WHERE singleton`).Scan(&revision); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `DELETE FROM screening_history_showtimes WHERE provider_showing_id='100'`); err != nil {
			return err
		}
		if _, err := s.ProcessHistoryStatistics(ctx); err != nil {
			return err
		}
		var again int64
		if err := tx.QueryRow(ctx, `SELECT applied_revision FROM screening_statistics_state WHERE singleton`).Scan(&again); err != nil {
			return err
		}
		var raw []byte
		if err := tx.QueryRow(ctx, historyStatisticsSQL, "", "", []string{}, []string{}, "", "", "", "", "", nil).Scan(&raw); err != nil {
			return err
		}
		var got schedule.HistoryStatistics
		if err := decodeHistoryJSON(raw, &got); err != nil {
			return err
		}
		if revision != again || got.Totals.Showtimes != before.Totals.Showtimes {
			return fmt.Errorf("read mixed summary revisions")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := historyGet(t, s, schedule.StatisticsQuery{}); got.Totals.Showtimes != before.Totals.Showtimes-1 || got.StatisticsRevision <= before.StatisticsRevision {
		t.Fatal("next snapshot did not advance", got.Totals)
	}
}

func TestHistoryScreeningGrowthIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	data := testDataset()
	data.Showtimes = data.Showtimes[:1]
	historyPublish(t, s, data)
	var previous []int
	for _, size := range []int{1000, 100000} {
		start := 1000000
		if size == 100000 {
			start += 1000
		}
		end := 1000000 + size - 1
		_, err := pool.Exec(t.Context(), `INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,first_seen_at,last_seen_at,source_generated_at,last_generation)
  SELECT 'ugc-showing-'||n,h.provider,n::text,h.theater_id,h.movie_provider_id,h.service_date,h.start_time,h.end_time,h.first_part_duration_minutes,h.language,h.provider_version,h.format,h.room,'https://www.ugc.fr/reservationSeances.html?id='||n,h.first_seen_at,h.last_seen_at,h.source_generated_at,h.last_generation
  FROM screening_history_showtimes h CROSS JOIN generate_series($1::int,$2::int) n WHERE h.provider='ugc' AND h.provider_showing_id='100'`, start, end)
		if err != nil {
			t.Fatal(err)
		}
		historyProcess(t, s)
		counts := []int{}
		for _, r := range historyRollups() {
			var count int
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+r.table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			counts = append(counts, count)
		}
		if previous != nil && !reflect.DeepEqual(previous, counts) {
			t.Fatal("screening growth expanded summary grains", previous, counts)
		}
		previous = counts
		begin := time.Now()
		got := historyGet(t, s, schedule.StatisticsQuery{})
		t.Logf("screenings=%d summary_rows=%v request_elapsed=%s", size+1, counts, time.Since(begin))
		if got.Totals.Showtimes != size+1 || got.Totals.Movies != 1 || got.Totals.Theaters != 1 || got.DailyShowtimes[0].ShowtimeCount != size+1 {
			t.Fatal("weighted counts or distincts incorrect", got.Totals)
		}
		assertHistorySums(t, got)
		// Inspect executed serving plans: no relation may read individual screenings.
		for _, query := range []struct {
			sql  string
			args []any
		}{
			{historyIntegritySQL, nil},
			{historyStatisticsSQL, []any{"", "", []string{}, []string{}, "", "", "", "", "", nil}},
			{historyStatisticsSQL, []any{data.Showtimes[0].ServiceDate, data.Showtimes[0].ServiceDate, []string{}, []string{}, "", "", "", "", "", nil}},
			{historyStatisticsOptionsSQL, []any{[]string{}, []string{}, "", ""}},
			{historyOptionsSQL, []any{"theater", "", []string{}}},
		} {
			var raw []byte
			if err := pool.QueryRow(t.Context(), "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+query.sql, query.args...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []map[string]any
			if err := json.Unmarshal(raw, &plans); err != nil {
				t.Fatal(err)
			}
			var inspect func(any)
			inspect = func(v any) {
				switch v := v.(type) {
				case map[string]any:
					if v["Relation Name"] == "screening_history_showtimes" {
						t.Fatal("request plan scanned raw history")
					}
					for _, child := range v {
						inspect(child)
					}
				case []any:
					for _, child := range v {
						inspect(child)
					}
				}
			}
			inspect(plans)
		}
	}
}
