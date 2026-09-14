package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCinevilleMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade033"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "cineville_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 33, "033_upcoming_movie_reviews.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90)`)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if upgrade {
				var title string
				if err := pool.QueryRow(ctx, `SELECT title FROM movies WHERE provider='ugc'`).Scan(&title); err != nil || title != "Existing" {
					t.Fatal("legacy data changed")
				}
			}
			for _, statement := range []string{
				`INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES(1,'cineville',1,'all_cinemas',now(),'Europe/Paris','2026-09-14','2027-07-01')`,
				`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'cineville-639','cineville','639','cineville-639','Katorza','','Quimper','29000')`,
				`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'cineville-639','2026-09-14')`,
				`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'cineville','-693091020261','cineville-film--693091020261','Event',0)`,
				`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'cineville-showing-639-1','cineville','639-1','2026-09-14','cineville-639','-693091020261','2026-09-13T22:15:00Z','2026-09-13T22:15:00Z','VF','VF','2D','4','https://www.cineville.fr/vad/639/1/9')`,
				`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('cineville','-693091020261','Event',0)`,
				`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) VALUES('cineville','-693091020261',1,'cineville-film--693091020261','Event',0)`,
				`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id) VALUES('cineville-film--693091020261',1,'source','cineville','-693091020261')`,
				`INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES('cineville','-693091020261','tmdb','unmatched','event',0,'[]',now(),now(),now())`,
				`WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('cineville','-693091020261') RETURNING id) INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'cineville','-693091020261' FROM inserted`,
				`INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at) VALUES('cineville','639','manual','manual',47.99,-4.1,now())`,
				`INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('cineville',false,'daily','10:00')`,
				`INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('cineville','running',now(),'2026-09-14','2027-07-01','{}')`,
			} {
				mustCinevilleSQL(t, ctx, pool, statement)
			}
			mustCinevilleSQL(t, ctx, pool, `UPDATE movies SET runtime_minutes=93 WHERE provider='cineville'`)
			mustCinevilleSQL(t, ctx, pool, `UPDATE sync_runs SET trigger_source='scheduled',schedule_id=1,schedule_revision=1,scheduled_for=now(),schedule_attempt=0 WHERE target='cineville'`)
			for _, statement := range []string{
				`UPDATE showtimes SET end_time=start_time+interval '93 minutes' WHERE provider='cineville'`,
				`UPDATE showtimes SET end_time=start_time-interval '1 minute' WHERE provider='cineville'`,
				`UPDATE showtimes SET first_part_duration_minutes=1 WHERE provider='cineville'`,
				`UPDATE showtimes SET provider_showing_id='707-1',id='cineville-showing-707-1' WHERE provider='cineville'`,
				`UPDATE showtimes SET provider_showing_id='639-01',id='cineville-showing-639-01' WHERE provider='cineville'`,
				`UPDATE theaters SET postal_code='' WHERE provider='cineville'`,
				`UPDATE theaters SET city='' WHERE provider='cineville'`,
				`UPDATE movie_slug_aliases SET slug='cineville-film-693091020261' WHERE source_provider='cineville'`,
				`UPDATE theater_locations SET provider_theater_id='01' WHERE provider='cineville'`,
				`UPDATE sync_runs SET schedule_id=NULL WHERE target='cineville'`,
				`UPDATE sync_runs SET schedule_revision=NULL WHERE target='cineville'`,
				`UPDATE sync_runs SET schedule_attempt=NULL WHERE target='cineville'`,
				`UPDATE sync_runs SET schedule_attempt=3 WHERE target='cineville'`,
				`UPDATE sync_runs SET target='tmdb_upcoming_movies' WHERE target='cineville'`,
			} {
				rejectCinevilleSQL(t, ctx, pool, statement)
			}
			for _, id := range []string{"0", "-0", "01", "+1", "1e3", "9223372036854775808", "-9223372036854775809"} {
				for _, statement := range []string{
					`UPDATE movies SET provider_id=$1::text,slug='cineville-film-'||$1::text WHERE provider='cineville'`,
					`UPDATE movie_matches SET source_movie_id=$1 WHERE source_provider='cineville'`,
					`UPDATE local_movie_groups SET primary_source_movie_id=$1 WHERE primary_source_provider='cineville'`,
					`UPDATE local_movie_group_members SET source_movie_id=$1 WHERE source_provider='cineville'`,
					`UPDATE public_movies SET identity_anchor_source_movie_id=$1 WHERE identity_anchor_provider='cineville'`,
					`UPDATE public_movie_sources SET source_movie_id=$1::text,source_slug='cineville-film-'||$1::text WHERE source_provider='cineville'`,
					`UPDATE movie_slug_aliases SET source_movie_id=$1::text,slug='cineville-film-'||$1::text WHERE source_provider='cineville'`,
				} {
					rejectCinevilleSQL(t, ctx, pool, statement, id)
				}
			}
			for _, id := range []string{"9223372036854775807", "-9223372036854775808", "2714300920262"} {
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'cineville',$1::text,'cineville-film-'||$1::text,'Boundary',93)`, id)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal("migration not idempotent")
			}
		})
	}
}

func mustCinevilleSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, statement, args...); err != nil {
		t.Fatalf("fixture statement %s: %v", statement, err)
	}
}
func rejectCinevilleSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) {
	t.Helper()
	_, err := pool.Exec(ctx, statement, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("expected check rejection for %s: %v", statement, err)
	}
}
