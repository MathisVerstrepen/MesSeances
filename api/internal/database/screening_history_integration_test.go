package database

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func historyMigrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool, schema := newMigrationTestPool(t, ctx, "history_migration_")
	assert := func() {
		var actual string
		if err := pool.QueryRow(context.Background(), `SELECT current_schema()`).Scan(&actual); err != nil || actual != schema {
			t.Fatalf("history schema guard failed: %v", err)
		}
	}
	assert()
	t.Cleanup(assert)
	return pool
}

func TestHistoryMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade038"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := historyMigrationPool(t, ctx)
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 38, "038_noecinemas_provider.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Previous',90),(2,'ugc','11','ugc-film-11','Current',90)`)
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code)
 SELECT n,'ugc-25','ugc','25','ugc-25','UGC Lille','Fixture address','Lille','59000' FROM generate_series(1,2) n;
 INSERT INTO theater_dates(generation_id,theater_id,service_date) SELECT n,'ugc-25','2026-09-15' FROM generate_series(1,2) n;
 INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url)
 SELECT generation_id,'ugc-showing-100','ugc','100','ugc-25',provider_id,'2026-09-15','2026-09-15 18:00Z','2026-09-15 19:30Z',0,'VF','VF','2D','1','https://www.ugc.fr/reservationSeances.html?id=100' FROM movies;`)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if upgrade {
				var rows int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM showtimes WHERE generation_id IN (1,2)`).Scan(&rows); err != nil || rows != 2 {
					t.Fatal("existing generations changed during history upgrade", rows, err)
				}
			}
			var applied time.Time
			if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=39`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			var same bool
			if err := pool.QueryRow(ctx, `SELECT applied_at=$1 FROM movieflow_schema_migrations WHERE version=39`, applied).Scan(&same); err != nil || !same {
				t.Fatal("migration timestamp changed", err)
			}
			for _, table := range []string{"screening_history_providers", "screening_history_theaters", "screening_history_showtimes"} {
				var n int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
					t.Fatal("history seeded", table, n, err)
				}
			}
			var fks, indexes int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='screening_history_showtimes'::regclass AND contype='f' AND confdeltype='a' AND confrelid IN ('screening_history_theaters'::regclass,'public_movie_sources'::regclass)`).Scan(&fks); err != nil || fks != 2 {
				t.Fatal("durable foreign keys", fks, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename IN ('screening_history_showtimes','screening_history_theaters')`).Scan(&indexes); err != nil || indexes != 7 {
				t.Fatal("indexes", indexes, err)
			}
		})
	}
}

func TestHistoryMigrationRollbackIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := historyMigrationPool(t, ctx)
	installMigrationPrefix(t, ctx, pool, 38, "038_noecinemas_provider.sql")
	// Fail after the first two history tables were created.
	mustCinevilleSQL(t, ctx, pool, `CREATE TABLE screening_history_showtimes(blocker boolean)`)
	if err := RunMigrations(ctx, pool); err == nil {
		t.Fatal("expected migration failure")
	}
	var clean bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('screening_history_providers') IS NULL AND to_regclass('screening_history_theaters') IS NULL AND NOT EXISTS(SELECT 1 FROM movieflow_schema_migrations WHERE version=39)`).Scan(&clean); err != nil || !clean {
		t.Fatal("DDL/history did not rollback", err)
	}
	mustCinevilleSQL(t, ctx, pool, `DROP TABLE screening_history_showtimes`)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
