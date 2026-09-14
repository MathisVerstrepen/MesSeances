package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMK2MigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade034"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "mk2_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 34, "034_cineville_provider.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90),(1,'cineville','-1','cineville-film--1','Existing event',0)`)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if upgrade {
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE (provider='ugc' AND title='Existing' AND runtime_minutes=90) OR (provider='cineville' AND title='Existing event' AND runtime_minutes=0)`).Scan(&count); err != nil || count != 2 {
					t.Fatal("legacy records changed", err)
				}
			}
			var schedules int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='mk2'`).Scan(&schedules); err != nil || schedules != 0 {
				t.Fatal("unexpected schedule seed")
			}
			for _, statement := range []string{
				`INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES(1,'mk2',1,'all_cinemas',now(),'Europe/Paris','2026-09-14','2027-07-01')`,
				`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'mk2-0004','mk2','0004','mk2-0004','MK2 Bibliothèque','128 avenue de France','Paris','75013')`,
				`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'mk2-0004','2026-09-14')`,
				`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'mk2','HO00006568','mk2-film-HO00006568','Silent',0)`,
				`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'mk2-showing-0004-140350','mk2','0004-140350','2026-09-14','mk2-0004','HO00006568','2026-09-13T22:15:00Z','2026-09-13T22:15:00Z','','Muet','2D','','https://www.mk2.com/panier/seance/tickets?cinemaId=0004&sessionId=140350')`,
				`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('mk2','HO00006568','Silent',0)`,
				`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'mk2','HO00006568',id,'mk2-film-HO00006568','Silent',0 FROM public_movies WHERE identity_anchor_provider='mk2'`,
				`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id) SELECT 'mk2-film-HO00006568',id,'source','mk2','HO00006568' FROM public_movies WHERE identity_anchor_provider='mk2'`,
				`INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES('mk2','HO00006568','tmdb','unmatched','silent',0,'[]',now(),now(),now())`,
				`WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('mk2','HO00006568') RETURNING id) INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'mk2','HO00006568' FROM inserted`,
				`INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at) VALUES('mk2','0004','manual','manual',48.83,2.37,now())`,
				`INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('mk2',false,'daily','10:00')`,
				`INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('mk2','running',now(),'2026-09-14','2027-07-01','{}')`,
				`UPDATE movies SET runtime_minutes=93 WHERE provider='mk2'`,
				`UPDATE sync_runs SET trigger_source='scheduled',schedule_id=1,schedule_revision=1,scheduled_for=now(),schedule_attempt=0 WHERE target='mk2'`,
			} {
				mustCinevilleSQL(t, ctx, pool, statement)
			}
			for _, statement := range []string{
				`UPDATE showtimes SET end_time=start_time+interval '93 minutes' WHERE provider='mk2'`,
				`UPDATE showtimes SET end_time=start_time-interval '1 minute' WHERE provider='mk2'`,
				`UPDATE showtimes SET first_part_duration_minutes=1 WHERE provider='mk2'`,
				`UPDATE showtimes SET room='1' WHERE provider='mk2'`,
				`UPDATE showtimes SET language='VF' WHERE provider='mk2'`,
				`UPDATE showtimes SET provider_version='VF' WHERE provider='mk2'`,
				`UPDATE showtimes SET provider_showing_id='0005-140350',id='mk2-showing-0005-140350' WHERE provider='mk2'`,
				`UPDATE showtimes SET provider_showing_id='0004-01',id='mk2-showing-0004-01' WHERE provider='mk2'`,
				`UPDATE theaters SET address='' WHERE provider='mk2'`,
				`UPDATE theaters SET postal_code='' WHERE provider='mk2'`,
				`UPDATE theaters SET city='' WHERE provider='mk2'`,
				`UPDATE movie_slug_aliases SET slug='mk2-film-HO9' WHERE source_provider='mk2'`,
				`UPDATE theater_locations SET provider_theater_id='0000' WHERE provider='mk2'`,
				`UPDATE sync_runs SET schedule_id=NULL WHERE target='mk2'`,
				`UPDATE sync_runs SET schedule_revision=NULL WHERE target='mk2'`,
				`UPDATE sync_runs SET scheduled_for=NULL WHERE target='mk2'`,
				`UPDATE sync_runs SET schedule_attempt=NULL WHERE target='mk2'`,
				`UPDATE sync_runs SET schedule_attempt=3 WHERE target='mk2'`,
			} {
				rejectCinevilleSQL(t, ctx, pool, statement)
			}
			for _, id := range []string{"HO", "ho1", "1", "HO-1"} {
				for _, statement := range []string{
					`UPDATE movies SET provider_id=$1::text,slug='mk2-film-'||$1::text WHERE provider='mk2'`,
					`UPDATE movie_matches SET source_movie_id=$1 WHERE source_provider='mk2'`,
					`UPDATE local_movie_groups SET primary_source_movie_id=$1 WHERE primary_source_provider='mk2'`,
					`UPDATE local_movie_group_members SET source_movie_id=$1 WHERE source_provider='mk2'`,
					`UPDATE public_movies SET identity_anchor_source_movie_id=$1 WHERE identity_anchor_provider='mk2'`,
					`UPDATE public_movie_sources SET source_movie_id=$1::text,source_slug='mk2-film-'||$1::text WHERE source_provider='mk2'`,
					`UPDATE movie_slug_aliases SET source_movie_id=$1::text,slug='mk2-film-'||$1::text WHERE source_provider='mk2'`,
				} {
					rejectCinevilleSQL(t, ctx, pool, statement, id)
				}
			}
			mustCinevilleSQL(t, ctx, pool, `UPDATE showtimes SET language='VF',provider_version='VF' WHERE provider='mk2'`)
			mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'mk2',$1::text,'mk2-film-'||$1::text,'Boundary',93)`, "HO"+strings.Repeat("1", 117))
			var valid bool
			if err := pool.QueryRow(ctx, `SELECT mk2_identity_valid('movie',$1) OR mk2_identity_valid('theater',$2) OR mk2_identity_valid('showing',$3)`, "HO"+strings.Repeat("1", 118), strings.Repeat("1", 125), "1-"+strings.Repeat("1", 115)).Scan(&valid); err != nil || valid {
				t.Fatal("overlong derived identities accepted", err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal("idempotence", err)
			}
		})
	}
}
