package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInfinityVisionPrefixConstraintsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := historyMigrationPool(t, ctx)
	installMigrationPrefix(t, ctx, pool, 39, "039_screening_history.sql")
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		var name, definition string
		if err := pool.QueryRow(ctx, `SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%format%'`, table).Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		t.Logf("migration 039 %s: %s %s", table, name, definition)
		if name != "showtimes_format_check" {
			t.Fatalf("unexpected copied CHECK name: %q", name)
		}
	}
}

func TestInfinityVisionMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade039"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := historyMigrationPool(t, ctx)
			var beforeRows, beforeConstraints string
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 39, "039_screening_history.sql")
				seedInfinityMigration(t, ctx, pool)
				beforeRows, beforeConstraints = infinityMigrationState(t, ctx, pool)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if upgrade {
				rows, constraints := infinityMigrationState(t, ctx, pool)
				if rows != beforeRows || constraints != beforeConstraints {
					t.Fatal("migration changed existing rows or unrelated constraints")
				}
			} else {
				seedInfinityMigration(t, ctx, pool)
			}
			var applied time.Time
			if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=40`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var once bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(applied_at=$1) FROM movieflow_schema_migrations WHERE version=40`, applied).Scan(&once); err != nil || !once {
				t.Fatal("migration runner reapplied 040", err)
			}
			for _, table := range []string{"showtimes", "screening_history_showtimes"} {
				var valid bool
				if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(convalidated) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%format%'`, table).Scan(&valid); err != nil || !valid {
					t.Fatal("format CHECK missing, duplicated or unvalidated", table, err)
				}
				for _, format := range []string{"2D", "3D", "IMAX", "DOLBY", "SCREENX", "LASER_ULTRA", "4DX", "ICE", "INFINITY_VISION"} {
					if _, err := pool.Exec(ctx, `UPDATE `+table+` SET format=$1`, format); err != nil {
						t.Fatalf("%s rejected %s: %v", table, format, err)
					}
				}
				for _, format := range []string{"ALL", "invented", "infinity_vision", "Infinity Vision", ""} {
					_, err := pool.Exec(ctx, `UPDATE `+table+` SET format=$1`, format)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "showtimes_format_check" {
						t.Fatalf("%s invalid %q: expected format CHECK violation, got %v", table, format, err)
					}
				}
			}
		})
	}
}

func seedInfinityMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Fixture',90);
INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'ugc-25','ugc','25','ugc-25','UGC Lille','Fixture address','Lille','59000');
INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'ugc-25','2026-09-15');
INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url)
 SELECT 1,'ugc-showing-'||n,'ugc',n::text,'ugc-25','10','2026-09-15','2026-09-15 18:00Z','2026-09-15 19:30Z',0,'VF','VF',CASE WHEN n=100 THEN '2D' ELSE 'ICE' END,'1','https://www.ugc.fr/reservationSeances.html?id='||n FROM generate_series(100,101) n;
INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('ugc','10','Fixture',90);
INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'ugc','10',id,'ugc-film-10','Fixture',90 FROM public_movies;
INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at) VALUES('ugc-25','ugc','25','ugc-25','UGC Lille','Fixture address','Lille','59000','lille','Lille','{}',now());
INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,first_seen_at,last_seen_at,source_generated_at,last_generation)
 SELECT id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,now(),now(),now(),1 FROM showtimes;`); err != nil {
		t.Fatal(err)
	}
}

func infinityMigrationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	var rows, constraints string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM showtimes s),(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM screening_history_showtimes h))::text`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid),convalidated) ORDER BY conrelid,conname)::text FROM pg_constraint WHERE conrelid IN ('showtimes'::regclass,'screening_history_showtimes'::regclass) AND conname NOT IN ('showtimes_format_check', 'showtimes_language_original_check', 'screening_history_showtimes_language_original_check', 'showtimes_language_vof_check', 'screening_history_showtimes_language_vof_check')`).Scan(&constraints); err != nil {
		t.Fatal(err)
	}
	return rows, constraints
}
