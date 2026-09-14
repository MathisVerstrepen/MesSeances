package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestCinewestMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade035"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "cinewest_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 35, "035_mk2_provider.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90),(1,'mk2','HO1','mk2-film-HO1','Existing MK2',0)`)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if upgrade {
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE (provider='ugc' AND title='Existing' AND runtime_minutes=90) OR (provider='mk2' AND title='Existing MK2' AND runtime_minutes=0)`).Scan(&count); err != nil || count != 2 {
					t.Fatal("legacy data changed", err)
				}
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='cinewest'`).Scan(&count); err != nil || count != 0 {
				t.Fatal("schedule seeded", err)
			}
			for _, sql := range []string{
				`INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES(1,'cinewest',1,'all_cinemas',now(),'Europe/Paris','2026-09-14','2027-07-01')`,
				`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('cinewest','cineoffice-1','Event',0)`,
				`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'cinewest','cineoffice-1',id,'cinewest-film-cineoffice-1','Event',0 FROM public_movies WHERE identity_anchor_provider='cinewest'`,
				`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id) SELECT 'cinewest-film-cineoffice-1',id,'source','cinewest','cineoffice-1' FROM public_movies WHERE identity_anchor_provider='cinewest'`,
				`INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES('cinewest','cineoffice-1','tmdb','unmatched','event',0,'[]',now(),now(),now())`,
				`WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('cinewest','cineoffice-1') RETURNING id) INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'cinewest','cineoffice-1' FROM inserted`,
				`INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at) VALUES('cinewest','cineoffice-royanlelido','manual','manual',45.62,-1.02,now())`,
				`INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('cinewest',false,'daily','10:00')`,
				`INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('cinewest','running',now(),'2026-09-14','2027-07-01','{}')`,
				`UPDATE sync_runs SET trigger_source='scheduled',schedule_id=1,schedule_revision=1,scheduled_for=now(),schedule_attempt=0 WHERE target='cinewest'`,
			} {
				mustCinevilleSQL(t, ctx, pool, sql)
			}
			for _, v := range []struct {
				theater, movie, raw, end, language, version string
				first                                       int
			}{
				{"cineoffice-royanlelido", "cineoffice-1", "1", "2026-09-14T21:07:00.123456Z", "", "VERSION_MUET", 0},
				{"ticketingcine-EMS0042", "ticketingcine-ABCDE", "emsx004200000001", "2026-09-14T18:00:00.123456Z", "VF", "VF", 15},
				{"webediamovies-W8400", "webediamovies-1", "1", "2026-09-14T18:00:00.123456Z", "VF", "VF", 0},
			} {
				id, _ := schedule.CinewestShowingID(v.theater, v.raw)
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'cinewest-'||$1::text,'cinewest',$1,'cinewest-'||$1::text,'Cinema','1 Rue','Royan','17200')`, v.theater)
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'cinewest-'||$1::text,'2026-09-14')`, v.theater)
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'cinewest',$1::varchar,'cinewest-film-'||$1::text,'Event',0)`, v.movie)
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes) VALUES(1,'cinewest-showing-'||$1::text,'cinewest',$1,'2026-09-14','cinewest-'||$2::text,$3,'2026-09-14T18:00:00.123456Z',$4,$5,$6,'2D','Salle 1',$7,$8)`, id, v.theater, v.movie, v.end, v.language, v.version, schedule.CinewestWebsite(v.theater), v.first)
			}
			for _, sql := range []string{
				`UPDATE showtimes SET end_time=start_time WHERE provider_showing_id LIKE 'cineoffice-%'`,
				`UPDATE showtimes SET end_time=start_time+interval '1 minute' WHERE provider_showing_id LIKE 'webediamovies-%'`,
				`UPDATE showtimes SET end_time=start_time-interval '1 minute' WHERE provider='cinewest'`,
				`UPDATE showtimes SET first_part_duration_minutes=1 WHERE provider_showing_id LIKE 'cineoffice-%'`,
				`UPDATE showtimes SET first_part_duration_minutes=-1 WHERE provider='cinewest'`,
				`UPDATE showtimes SET language='VF' WHERE provider_showing_id LIKE 'cineoffice-%'`,
				`UPDATE showtimes SET language='',provider_version='VERSION_MUET' WHERE provider_showing_id LIKE 'ticketingcine-%'`,
				`UPDATE showtimes SET room='' WHERE provider='cinewest'`,
				`UPDATE movie_slug_aliases SET slug='cinewest-film-cineoffice-2' WHERE source_provider='cinewest'`,
				`UPDATE theater_locations SET provider_theater_id='cineoffice-cinewest' WHERE provider='cinewest'`,
				`UPDATE sync_runs SET schedule_id=NULL WHERE target='cinewest'`,
				`UPDATE sync_runs SET schedule_revision=NULL WHERE target='cinewest'`,
				`UPDATE sync_runs SET schedule_attempt=3 WHERE target='cinewest'`,
			} {
				rejectCinevilleSQL(t, ctx, pool, sql)
			}
			for _, kind := range []string{"movie", "showing", "theater"} {
				for _, id := range append(schedule.CinewestTheaterIDs(), "cineoffice-1", "ticketingcine-ABCDE", "ticketingcine-EMS0042-emsx0042HC1", "ticketingcine-EMS0042-emsx1185HC1", "cineoffice-"+strings.Repeat("1", 103), "cineoffice-"+strings.Repeat("1", 104), "cineoffice-"+strings.Repeat("a", 64), "cineoffice-0", "1") {
					var valid bool
					if err := pool.QueryRow(ctx, `SELECT cinewest_identity_valid($1,$2)`, kind, id).Scan(&valid); err != nil || valid != schedule.ValidCinewestIdentity(kind, id) {
						t.Fatalf("SQL/Go identity parity kind=%s err=%v", kind, err)
					}
				}
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal("idempotence", err)
			}
		})
	}
}
