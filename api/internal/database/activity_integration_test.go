package database

import "testing"

func TestActivityMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade055"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "activity_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 55, "055_account_watchlist_preferences.sql")
			}
			for range 2 {
				if err := RunMigrations(ctx, pool); err != nil {
					t.Fatal(err)
				}
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			for _, table := range []string{"cinema_activity_state", "cinema_activity_coverage", "cinema_activity_episodes", "cinema_activity_episode_sources"} {
				var count int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatal(table, count, err)
				}
			}
			var refs int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid IN ('cinema_activity_state'::regclass,'cinema_activity_coverage'::regclass,'cinema_activity_episodes'::regclass,'cinema_activity_episode_sources'::regclass) AND contype='f' AND confdeltype='a'`).Scan(&refs); err != nil || refs != 7 {
				t.Fatal("durable foreign keys", refs, err)
			}
			var indexes int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname IN ('cinema_activity_coverage_date_idx','cinema_activity_events_idx','cinema_activity_event_upper_idx','cinema_activity_source_idx')`).Scan(&indexes); err != nil || indexes != 4 {
				t.Fatal("bounded read indexes", indexes, err)
			}
			exec := func(sql string) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES(10,10,'Activity fixture',100);
 INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) VALUES('ugc','200',10,'ugc-film-200','Activity fixture',100);
 INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at)
 VALUES('ugc-25','ugc','25','ugc-25','Cinema','Address','Lille','59000','lille','Lille','{}',now()),('ugc-26','ugc','26','ugc-26','Cinema','Address','Lille','59000','lille','Lille','{}',now());
 INSERT INTO cinema_activity_state VALUES('ugc-25',now(),now(),now());
 INSERT INTO cinema_activity_episodes(theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,observed_from,observed_through,detecting_generation)
 VALUES('ugc-25','ugc','200','baseline',now(),'2026-10-01','2026-10-01','2026-10-01',1),('ugc-26','ugc','200','baseline',now(),'2026-10-01','2026-10-01','2026-10-01',1)`)
			for _, sql := range []string{
				`INSERT INTO cinema_activity_state VALUES('ugc-999',now(),now(),now())`,
				`INSERT INTO cinema_activity_coverage VALUES('kinepolis',1,'ugc-25','2026-10-01','complete','date_response',now(),now())`,
				`INSERT INTO cinema_activity_coverage VALUES('ugc',0,'ugc-25','2026-10-01','complete','date_response',now(),now())`,
				`INSERT INTO cinema_activity_coverage VALUES('ugc',1,'ugc-25','2026-10-01','unknown','date_response',now(),now())`,
				`UPDATE cinema_activity_episodes SET kind='invented' WHERE id=1`,
				`UPDATE cinema_activity_episodes SET kind='return_to_program' WHERE id=1`,
				`UPDATE cinema_activity_episodes SET observed_through='2026-09-01' WHERE id=1`,
				`UPDATE cinema_activity_episodes SET superseded_by_id=1 WHERE id=1`,
				`UPDATE cinema_activity_episodes SET superseded_by_id=1 WHERE id=2`,
				`INSERT INTO cinema_activity_episode_sources VALUES('ugc-26',1,'ugc','200','2026-10-01','2026-10-01',now(),now())`,
				`DELETE FROM public_movie_sources`,
				`DELETE FROM screening_history_theaters`,
			} {
				if _, err := pool.Exec(ctx, sql); err == nil {
					t.Fatal("invalid journal state accepted", sql)
				}
			}
		})
	}
}

func TestActivityMigrationRollbackIntegration(t *testing.T) {
	ctx := t.Context()
	pool, _ := newMigrationTestPool(t, ctx, "activity_rollback_")
	installMigrationPrefix(t, ctx, pool, 55, "055_account_watchlist_preferences.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE cinema_activity_episodes(blocker boolean)`); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err == nil {
		t.Fatal("expected DDL failure")
	}
	var clean bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('cinema_activity_state') IS NULL AND to_regclass('cinema_activity_coverage') IS NULL AND NOT EXISTS(SELECT 1 FROM movieflow_schema_migrations WHERE version=56)`).Scan(&clean); err != nil || !clean {
		t.Fatal("migration did not rollback", err)
	}
}
