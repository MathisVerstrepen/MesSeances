package schedulepg

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Called only by the history worker, after its counters have been updated and
// before its pending queue is acknowledged. Both read models publish atomically.
func refreshTheaterTechnologies(ctx context.Context, tx pgx.Tx, today time.Time, rebuild, pending bool) (bool, error) {
	var ready bool
	var evaluated *time.Time
	if err := tx.QueryRow(ctx, `SELECT ready,evaluated_on FROM theater_technology_state WHERE singleton FOR UPDATE`).Scan(&ready, &evaluated); err != nil {
		return false, err
	}
	full := !ready || evaluated == nil || evaluated.Format("2006-01-02") != today.Format("2006-01-02") || rebuild
	if !full && !pending {
		return false, nil
	}
	var changed int64
	err := tx.QueryRow(ctx, `WITH affected AS MATERIALIZED (
 SELECT id FROM screening_history_theaters
 WHERE $1 OR id IN (SELECT theater_id FROM screening_statistics_pending)
), desired AS MATERIALIZED (
 SELECT d.theater_id,d.format,max(d.service_date) AS latest_advertised_date
 FROM affected a JOIN screening_statistics_daily d ON d.theater_id=a.id
 WHERE d.showtime_count>0 AND d.service_date BETWEEN ($2::date - interval '12 months')::date AND $2::date
 AND d.format IN ('3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE','INFINITY_VISION')
 GROUP BY d.theater_id,d.format
), removed AS (
 DELETE FROM theater_technology_summary s USING affected a
 WHERE s.theater_id=a.id AND NOT EXISTS (
 SELECT 1 FROM desired d WHERE d.theater_id=s.theater_id AND d.format=s.format)
 RETURNING 1
), updated AS (
 INSERT INTO theater_technology_summary(theater_id,format,latest_advertised_date)
 SELECT theater_id,format,latest_advertised_date FROM desired
 ON CONFLICT(theater_id,format) DO UPDATE SET latest_advertised_date=excluded.latest_advertised_date
 WHERE theater_technology_summary.latest_advertised_date IS DISTINCT FROM excluded.latest_advertised_date
 RETURNING 1
)
SELECT (SELECT count(*) FROM removed)+(SELECT count(*) FROM updated)`, full, today).Scan(&changed)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE theater_technology_state SET ready=true,evaluated_on=$1,
 version=version+CASE WHEN NOT ready OR $2 THEN 1 ELSE 0 END WHERE singleton`, today, changed > 0)
	return changed > 0 || !ready, err
}
