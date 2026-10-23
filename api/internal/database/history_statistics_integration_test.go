package database

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

var historyStatisticsTables = []string{
	"screening_statistics_state", "screening_statistics_pending", "screening_statistics_hourly", "screening_statistics_daily", "screening_statistics_all_time", "screening_statistics_memberships",
}

// Additive migrations are excluded from old-row preservation only after proving
// their exact initial state. In particular, migration 071 must not scan/backfill
// history inside the startup migration transaction.
func assertHistoryStatisticsBaseline(t *testing.T, ctx context.Context, pool *pgxpool.Pool, facts map[string]string) {
	t.Helper()
	for _, table := range historyStatisticsTables[1:] {
		if facts[table] != "[]" {
			t.Fatalf("statistics migration did not create empty %s", table)
		}
		delete(facts, table)
	}
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(singleton AND NOT ready AND revision=0 AND applied_revision=0 AND processed_at IS NULL AND coverage='{}'::jsonb) FROM screening_statistics_state`).Scan(&valid); err != nil || !valid {
		t.Fatal("invalid statistics migration baseline", err)
	}
	delete(facts, "screening_statistics_state")
}

func TestHistoryStatisticsMigrationUpgradeIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "statistics_upgrade_")
	installMigrationPrefix(t, ctx, pool, 70, "070_ocine_provider.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('ugc','200','Retained film',90);
 INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'ugc','200',id,'ugc-film-200',title,runtime_minutes FROM public_movies;
 INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at) VALUES('ugc-25','ugc','25','ugc-25','Cinema','1 rue du test','Lille','59000','lille','Lille','{}',now());
 INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,first_seen_at,last_seen_at,source_generated_at,last_generation)
 VALUES('ugc-showing-100','ugc','100','ugc-25','200','2026-10-15','2026-10-15 18:00Z','2026-10-15 19:30Z',0,'VF','VF','2D','1','https://www.ugc.fr/reservationSeances.html?id=100',now(),now(),now(),1);
 INSERT INTO screening_history_providers(provider,collection_started_at,last_publication_at,last_generation,source_generated_at) VALUES('ugc',now(),now(),1,now());`)
	before := capitoleFacts(t, ctx, pool, nil)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	after := capitoleFacts(t, ctx, pool, nil)
	assertHistoryStatisticsBaseline(t, ctx, pool, after)
	for _, table := range []string{"screening_history_showtimes", "screening_history_theaters", "screening_history_providers"} {
		if before[table] != after[table] {
			t.Fatal("migration changed retained history", table)
		}
	}
	store := schedulepg.NewStore(pool)
	if changed, err := store.ProcessHistoryStatistics(ctx); err != nil || !changed {
		t.Fatal("existing history backfill failed", changed, err)
	}
	got, err := store.HistoryStatistics(ctx, schedule.StatisticsQuery{})
	if err != nil || got.Totals.Showtimes != 1 || got.Coverage.RecordedWindow == nil || got.StatisticsUpdatedAt == nil {
		t.Fatal("existing history not represented", got.Totals, err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.ProcessHistoryStatistics(ctx); err != nil || changed {
		t.Fatal("migration rerun changed counters", changed, err)
	}
}
