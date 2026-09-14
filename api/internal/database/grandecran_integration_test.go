package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"messeances/api/internal/schedule"
)

func TestGrandEcranMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade036"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "grandecran_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 36, "036_cinewest_provider.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90),(1,'mk2','HO1','mk2-film-HO1','Existing event',0),(1,'cinewest','cineoffice-1','cinewest-film-cineoffice-1','Existing Cinewest',90)`)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if upgrade {
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE title IN ('Existing','Existing event','Existing Cinewest')`).Scan(&count); err != nil || count != 3 {
					t.Fatal("legacy records changed", err)
				}
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='grandecran'`).Scan(&count); err != nil || count != 0 {
				t.Fatal("seeded schedule")
			}
			for _, statement := range []string{
				`UPDATE schedule_snapshot SET provider='grandecran'`,
				`INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES(1,'grandecran',1,'all_cinemas',now(),'Europe/Paris','2026-09-14','2029-07-01')`,
				`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'grandecran-G028P','grandecran','G028P','grandecran-G028P','Grand Ecran Test','1 rue Test','Paris','75001')`,
				`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'grandecran-G028P','2026-09-14')`,
				`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'grandecran','cEvent_1','grandecran-film-cEvent_1','Event',0),(1,'grandecran','123','grandecran-film-123','Film',120)`,
				`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'grandecran-showing-G028P-'||repeat('a',64),'grandecran','G028P-'||repeat('a',64),'2026-09-14','grandecran-G028P','cEvent_1','2026-09-14T18:00:00Z','2026-09-14T18:00:00Z','VOSTFR','VOSTFR','2D','','https://achat.grandecran.fr/test/r/123')`,
				`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('grandecran','cEvent_1','Event',0)`,
				`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'grandecran','cEvent_1',id,'grandecran-film-cEvent_1','Event',0 FROM public_movies WHERE identity_anchor_provider='grandecran'`,
				`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id) SELECT 'grandecran-film-cEvent_1',id,'source','grandecran','cEvent_1' FROM public_movies WHERE identity_anchor_provider='grandecran'`,
				`INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES('grandecran','cEvent_1','tmdb','unmatched','event',0,'[]',now(),now(),now())`,
				`WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('grandecran','cEvent_1') RETURNING id) INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'grandecran','cEvent_1' FROM inserted`,
				`INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at) VALUES('grandecran','G028P','manual','manual',48.83,2.37,now())`,
				`INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('grandecran',false,'daily','10:00')`,
				`INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('grandecran','running',now(),'2026-09-14','2029-07-01','{}')`,
				`UPDATE sync_runs SET trigger_source='scheduled',schedule_id=1,schedule_revision=1,scheduled_for=now(),schedule_attempt=0 WHERE target='grandecran'`,
				`UPDATE movies SET runtime_minutes=93 WHERE provider='grandecran'`,
			} {
				mustCinevilleSQL(t, ctx, pool, statement)
			}
			for _, statement := range []string{
				`UPDATE showtimes SET end_time=start_time+interval '93 minutes' WHERE provider='grandecran'`,
				`UPDATE showtimes SET end_time=start_time-interval '1 minute' WHERE provider='grandecran'`,
				`UPDATE showtimes SET first_part_duration_minutes=1 WHERE provider='grandecran'`,
				`UPDATE showtimes SET provider_showing_id='P9488-'||repeat('a',64),id='grandecran-showing-P9488-'||repeat('a',64) WHERE provider='grandecran'`,
				`UPDATE theaters SET provider_id='g028p',id='grandecran-g028p',slug='grandecran-g028p' WHERE provider='grandecran'`,
				`UPDATE movie_slug_aliases SET slug='grandecran-film-123' WHERE source_provider='grandecran'`,
				`UPDATE public_movie_sources SET source_slug='grandecran-film-123' WHERE source_provider='grandecran'`,
				`UPDATE theater_locations SET provider_theater_id='g028p' WHERE provider='grandecran'`,
				`UPDATE sync_runs SET schedule_id=NULL WHERE target='grandecran'`,
				`UPDATE sync_runs SET schedule_revision=NULL WHERE target='grandecran'`,
				`UPDATE sync_runs SET scheduled_for=NULL WHERE target='grandecran'`,
				`UPDATE sync_runs SET schedule_attempt=NULL WHERE target='grandecran'`,
				`UPDATE sync_runs SET schedule_attempt=3 WHERE target='grandecran'`,
			} {
				rejectCinevilleSQL(t, ctx, pool, statement)
			}
			for _, id := range []string{"0", "01", "c", "Cevent", "c/x", "1\n", strings.Repeat("1", 113)} {
				for _, statement := range []string{
					`UPDATE movies SET provider_id=$1::text,slug='grandecran-film-'||$1::text WHERE provider='grandecran' AND provider_id='cEvent_1'`,
					`UPDATE movie_matches SET source_movie_id=$1 WHERE source_provider='grandecran'`,
					`UPDATE local_movie_groups SET primary_source_movie_id=$1 WHERE primary_source_provider='grandecran'`,
					`UPDATE local_movie_group_members SET source_movie_id=$1 WHERE source_provider='grandecran'`,
					`UPDATE public_movies SET identity_anchor_source_movie_id=$1 WHERE identity_anchor_provider='grandecran'`,
					`UPDATE public_movie_sources SET source_movie_id=$1::text,source_slug='grandecran-film-'||$1::text WHERE source_provider='grandecran'`,
					`UPDATE movie_slug_aliases SET source_movie_id=$1::text,slug='grandecran-film-'||$1::text WHERE source_provider='grandecran'`,
				} {
					// Derived slugs exceed varchar(128) before CHECK evaluation at this boundary.
					if len(id) == 113 && strings.Contains(statement, "'grandecran-film-'||") {
						_, err := pool.Exec(ctx, statement, id)
						var pgErr *pgconn.PgError
						if !errors.As(err, &pgErr) || pgErr.Code != "22001" {
							t.Fatalf("expected derived slug length rejection: %v", err)
						}
						continue
					}
					rejectCinevilleSQL(t, ctx, pool, statement, id)
				}
			}
			for _, kind := range []string{"movie", "theater", "showing"} {
				for _, id := range []string{"G028P", "P9488", "g028p", "1", "01", "cEvent_1", strings.Repeat("1", 112), strings.Repeat("1", 113), "c" + strings.Repeat("a", 111), "c" + strings.Repeat("a", 112), "G028P-" + strings.Repeat("a", 64), "G028P-" + strings.Repeat("A", 64), "G028P\n"} {
					var valid bool
					if err := pool.QueryRow(ctx, `SELECT grandecran_identity_valid($1,$2)`, kind, id).Scan(&valid); err != nil || valid != schedule.ValidGrandEcranIdentity(kind, id) {
						t.Fatalf("Go/SQL parity kind=%s err=%v", kind, err)
					}
				}
			}
			mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'grandecran',$1::text,'grandecran-film-'||$1::text,'Boundary',0)`, "c"+strings.Repeat("a", 111))
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal("runner repeat", err)
			}
		})
	}
}
