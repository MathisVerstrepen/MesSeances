package schedulepg

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ProcessHistoryStatistics publishes one complete read-model revision. Queue
// consumption and counters commit together: cancellation/crashes/retries cannot
// lose or double-apply changes. Writers take the same singleton lock before
// touching the queue, so coverage and counters describe one database snapshot.
// The only raw-history scan is the initial backfill (or explicit rebuild).
func (s *Store) ProcessHistoryStatistics(ctx context.Context) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return false, err
	}
	defer rollbackScheduleTx(tx)
	var ready bool
	var revision, applied int64
	err = tx.QueryRow(ctx, `SELECT ready,revision,applied_revision FROM screening_statistics_state WHERE singleton FOR UPDATE`).Scan(&ready, &revision, &applied)
	if err != nil {
		return false, err
	}
	if ready && revision == applied {
		return false, tx.Commit(ctx)
	}
	if !ready {
		err = backfillHistoryStatistics(ctx, tx)
	} else {
		err = applyHistoryStatistics(ctx, tx)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM screening_statistics_pending`); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE screening_statistics_state SET ready=true,applied_revision=revision,processed_at=clock_timestamp(),coverage=(`+historySummaryCoverageSQL+`) WHERE singleton`); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

type historyRollup struct {
	table string
	keys  string
	group string
}

func historyRollups() []historyRollup {
	const dimensions = "provider,movie_provider_id,theater_id,language,format"
	return []historyRollup{
		{"screening_statistics_hourly", dimensions + ",service_date,hour", dimensions + ",service_date,hour"},
		{"screening_statistics_daily", dimensions + ",service_date", dimensions + ",service_date"},
		{"screening_statistics_all_time", dimensions + ",weekday,hour", dimensions + ",extract(isodow FROM service_date)::smallint,hour"},
		{"screening_statistics_memberships", dimensions, dimensions},
	}
}

func backfillHistoryStatistics(ctx context.Context, tx pgx.Tx) error {
	for _, rollup := range historyRollups() {
		if _, err := tx.Exec(ctx, "DELETE FROM "+rollup.table); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO screening_statistics_hourly(provider,movie_provider_id,theater_id,language,format,service_date,hour,showtime_count)
 SELECT provider,movie_provider_id,theater_id,language,format,service_date,extract(hour FROM start_time AT TIME ZONE 'Europe/Paris')::smallint,count(*)
 FROM screening_history_showtimes GROUP BY 1,2,3,4,5,6,7`)
	if err != nil {
		return err
	}
	for _, rollup := range historyRollups()[1:] {
		_, err = tx.Exec(ctx, "INSERT INTO "+rollup.table+"("+rollup.keys+",showtime_count) SELECT "+rollup.group+",sum(showtime_count) FROM screening_statistics_hourly GROUP BY "+rollup.group)
		if err != nil {
			return err
		}
	}
	return nil
}

func applyHistoryStatistics(ctx context.Context, tx pgx.Tx) error {
	for _, rollup := range historyRollups() {
		// These fragments are repository-owned identifiers, never request input.
		keys := strings.Split(rollup.keys, ",")
		var matches []string
		for _, key := range keys {
			matches = append(matches, "f."+key+"=d."+key)
		}
		grouped := "SELECT " + rollup.group + ",sum(delta)::bigint delta FROM screening_statistics_pending GROUP BY " + rollup.group
		// Alias derived weekday expressions through the explicit CTE column list.
		cte := "WITH d(" + rollup.keys + ",delta) AS MATERIALIZED (" + grouped + ") "
		statements := []string{
			cte + "INSERT INTO " + rollup.table + "(" + rollup.keys + ",showtime_count) SELECT " + rollup.keys + ",0 FROM d WHERE delta<>0 ON CONFLICT DO NOTHING",
			cte + "UPDATE " + rollup.table + " f SET showtime_count=f.showtime_count+d.delta FROM d WHERE " + strings.Join(matches, " AND "),
			cte + "DELETE FROM " + rollup.table + " f USING d WHERE f.showtime_count=0 AND " + strings.Join(matches, " AND "),
		}
		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return fmt.Errorf("apply history counters: %w", err)
			}
		}
	}
	return nil
}

// Coverage reads maintained date keys and the small provider inventory, never
// individual screenings. The worker stores it with the applied revision.
const historySummaryCoverageSQL = `SELECT jsonb_build_object(
 'collection_started_at',(SELECT min(collection_started_at) FROM screening_history_providers),
 'last_publication_at',(SELECT max(last_publication_at) FROM screening_history_providers),
 'recorded_window',(SELECT CASE WHEN first_date IS NULL THEN NULL ELSE jsonb_build_object('from',first_date::text,'through',last_date::text) END
 FROM (SELECT (SELECT min(service_date) FROM screening_statistics_daily) first_date,(SELECT max(service_date) FROM screening_statistics_daily) last_date) bounds),
 'completeness','unknown','bootstrap','none','providers',coalesce((SELECT jsonb_agg(to_jsonb(p) ORDER BY provider) FROM
 (SELECT provider,collection_started_at,last_publication_at,source_generated_at FROM screening_history_providers ORDER BY provider LIMIT 10) p),'[]'))`
