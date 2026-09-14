package database

import (
	"context"
	"errors"
	"messeances/api/internal/schedule"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestNoeCinemasMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade037"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "noecinemas_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 37, "037_grandecran_provider.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90),(1,'grandecran','cEvent_1','grandecran-film-cEvent_1','Existing',0)`)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var n int
			if upgrade {
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE title='Existing'`).Scan(&n); err != nil || n != 2 {
					t.Fatal("legacy data changed")
				}
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='noecinemas'`).Scan(&n); err != nil || n != 0 {
				t.Fatal("schedule seeded")
			}
			for _, sql := range []string{
				`UPDATE schedule_snapshot SET provider='noecinemas'`,
				`INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES(1,'noecinemas',1,'all_cinemas',now(),'Europe/Paris','2026-09-14','2028-07-15')`,
				`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'noecinemas-P8088','noecinemas','P8088','noecinemas-P8088','Noé Test','1 rue Test','L''Aigle','61300')`,
				`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'noecinemas-P8088','2026-09-14')`,
				`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'noecinemas','cEvent_1','noecinemas-film-cEvent_1','Event',0),(1,'noecinemas','123','noecinemas-film-123','Film',120)`,
				`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'noecinemas-showing-P8088-'||repeat('a',64),'noecinemas','P8088-'||repeat('a',64),'2026-09-14','noecinemas-P8088','cEvent_1','2026-09-14T18:00:00Z','2026-09-14T18:00:00Z','VFSTF','VFSTF','2D','','https://achat.cinema-laigle.com/reserver/r/123')`,
				`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('noecinemas','cEvent_1','Event',0)`,
				`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'noecinemas','cEvent_1',id,'noecinemas-film-cEvent_1','Event',0 FROM public_movies WHERE identity_anchor_provider='noecinemas'`,
				`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id) SELECT 'noecinemas-film-cEvent_1',id,'source','noecinemas','cEvent_1' FROM public_movies WHERE identity_anchor_provider='noecinemas'`,
				`INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES('noecinemas','cEvent_1','tmdb','unmatched','event',0,'[]',now(),now(),now())`,
				`WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('noecinemas','cEvent_1') RETURNING id) INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'noecinemas','cEvent_1' FROM inserted`,
				`INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at) VALUES('noecinemas','P8088','manual','manual',48.83,2.37,now())`,
				`INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('noecinemas',false,'daily','10:00')`,
				`INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('noecinemas','running',now(),'2026-09-14','2028-07-15','{}')`,
				`UPDATE sync_runs SET trigger_source='scheduled',schedule_id=1,schedule_revision=1,scheduled_for=now(),schedule_attempt=0 WHERE target='noecinemas'`,
			} {
				mustCinevilleSQL(t, ctx, pool, sql)
			}
			for _, sql := range []string{
				`UPDATE showtimes SET end_time=start_time+interval '1 minute' WHERE provider='noecinemas'`,
				`UPDATE showtimes SET end_time=start_time-interval '1 minute' WHERE provider='noecinemas'`,
				`UPDATE showtimes SET first_part_duration_minutes=1 WHERE provider='noecinemas'`,
				`UPDATE showtimes SET provider_showing_id='B0181-'||repeat('a',64),id='noecinemas-showing-B0181-'||repeat('a',64) WHERE provider='noecinemas'`,
				`UPDATE theaters SET provider_id='p8088',id='noecinemas-p8088',slug='noecinemas-p8088' WHERE provider='noecinemas'`,
				`UPDATE movie_slug_aliases SET slug='noecinemas-film-123' WHERE source_provider='noecinemas'`,
				`UPDATE public_movie_sources SET source_slug='noecinemas-film-123' WHERE source_provider='noecinemas'`,
				`UPDATE theater_locations SET provider_theater_id='p8088' WHERE provider='noecinemas'`,
				`UPDATE sync_runs SET schedule_id=NULL WHERE target='noecinemas'`,
				`UPDATE sync_runs SET schedule_revision=NULL WHERE target='noecinemas'`,
				`UPDATE sync_runs SET scheduled_for=NULL WHERE target='noecinemas'`,
				`UPDATE sync_runs SET schedule_attempt=NULL WHERE target='noecinemas'`,
				`UPDATE sync_runs SET schedule_attempt=3 WHERE target='noecinemas'`,
			} {
				rejectCinevilleSQL(t, ctx, pool, sql)
			}
			for _, id := range []string{"0", "01", "c", "Cevent", "c/x", "1\n", strings.Repeat("1", 113)} {
				for _, sql := range []string{
					`UPDATE movies SET provider_id=$1::text,slug='noecinemas-film-'||$1::text WHERE provider='noecinemas' AND provider_id='cEvent_1'`,
					`UPDATE movie_matches SET source_movie_id=$1 WHERE source_provider='noecinemas'`,
					`UPDATE local_movie_groups SET primary_source_movie_id=$1 WHERE primary_source_provider='noecinemas'`,
					`UPDATE local_movie_group_members SET source_movie_id=$1 WHERE source_provider='noecinemas'`,
					`UPDATE public_movies SET identity_anchor_source_movie_id=$1 WHERE identity_anchor_provider='noecinemas'`,
					`UPDATE public_movie_sources SET source_movie_id=$1::text,source_slug='noecinemas-film-'||$1::text WHERE source_provider='noecinemas'`,
					`UPDATE movie_slug_aliases SET source_movie_id=$1::text,slug='noecinemas-film-'||$1::text WHERE source_provider='noecinemas'`,
				} {
					if len(id) == 113 && strings.Contains(sql, "'noecinemas-film-'||") {
						_, err := pool.Exec(ctx, sql, id)
						var pgErr *pgconn.PgError
						if !errors.As(err, &pgErr) || pgErr.Code != "22001" {
							t.Fatal("derived slug boundary", err)
						}
						continue
					}
					rejectCinevilleSQL(t, ctx, pool, sql, id)
				}
			}
			for _, kind := range []string{"movie", "theater", "showing"} {
				for _, id := range []string{"P8088", "W8391", "p8088", "1", "01", "cEvent_1", strings.Repeat("1", 112), strings.Repeat("1", 113), "c" + strings.Repeat("a", 111), "c" + strings.Repeat("a", 112), "P8088-" + strings.Repeat("a", 64), "P8088-" + strings.Repeat("A", 64), "P8088\n"} {
					var valid bool
					if err := pool.QueryRow(ctx, `SELECT noecinemas_identity_valid($1,$2)`, kind, id).Scan(&valid); err != nil || valid != schedule.ValidNoeCinemasIdentity(kind, id) {
						t.Fatalf("Go/SQL parity kind=%s err=%v", kind, err)
					}
				}
			}
			mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'noecinemas',$1::text,'noecinemas-film-'||$1::text,'Boundary',0)`, "c"+strings.Repeat("a", 111))
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
		})
	}
}
