package database

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

const capitoleOld = "cinewest-webediamovies-W8400"
const capitoleNew = "cinewest-ticketingcine-EMS1378"
const capitoleLegacyShowing = "webediamovies-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func seedCapitole059(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	installMigrationPrefix(t, ctx, pool, 59, "059_account_theater_follows.sql")
	mustCinevilleSQL(t, ctx, pool, `
        INSERT INTO schedule_snapshot(singleton,version,schema_version,provider,scope,generated_at,timezone,window_from,window_through)
            VALUES(true,2,1,'cinewest','all_cinemas','2026-09-14T12:00:00Z','Europe/Paris','2026-09-14','2026-09-14');
        UPDATE movie_enrichment_state SET version=7;
        UPDATE theater_location_state SET version=11;
        INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,updated_at)
            VALUES('cinewest','webediamovies-1','Historical film',90,'2026-09-14T12:00:00Z');
        INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes)
            SELECT 'cinewest','webediamovies-1',id,'cinewest-film-webediamovies-1','Historical film',90 FROM public_movies;
        INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id)
            SELECT 'cinewest-film-webediamovies-1',id,'source','cinewest','webediamovies-1' FROM public_movies;
        INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at)
            VALUES('cinewest','webediamovies-1','tmdb','unmatched','historical film',90,'[]','2026-09-14T12:00:00Z','2026-09-14T13:00:00Z','2026-09-14T12:00:00Z');
        WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('cinewest','webediamovies-1') RETURNING id)
            INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'cinewest','webediamovies-1' FROM inserted;
        INSERT INTO passes(code) VALUES('UGC_ILLIMITE');
        INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at)
            VALUES('cinewest-webediamovies-W8400','cinewest','webediamovies-W8400','cinewest-webediamovies-W8400','Capitole Studios','1 Avenue','Le Pontet','84130','le-pontet','Le Pontet','{}','2026-09-14T12:00:00Z');
        INSERT INTO screening_history_providers(provider,collection_started_at,last_publication_at,last_generation,source_generated_at)
            VALUES('cinewest','2026-09-01T12:00:00Z','2026-09-14T12:00:00Z',2,'2026-09-14T12:00:00Z');
        INSERT INTO cinema_activity_state(theater_id,history_started_at,last_publication_at,source_generated_at)
            VALUES('cinewest-webediamovies-W8400','2026-09-01T12:00:00Z','2026-09-14T12:00:00Z','2026-09-14T12:00:00Z');
        INSERT INTO cinema_activity_coverage(provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at)
            SELECT 'cinewest',g,'cinewest-webediamovies-W8400','2026-09-14','unknown','unproven','2026-09-14T12:00:00Z'::timestamptz,'2026-09-14T12:00:00Z'::timestamptz FROM generate_series(1,2) g;
        INSERT INTO cinema_activity_episodes(theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,observed_from,observed_through,detecting_generation)
            SELECT 'cinewest-webediamovies-W8400','cinewest','webediamovies-1',kind,'2026-09-14T12:00:00Z'::timestamptz,'2026-09-14','2026-09-14','2026-09-14',2
            FROM (VALUES('baseline'),('added_to_program'),('added_to_program')) kinds(kind);
        UPDATE cinema_activity_episodes SET superseded_by_id=2 WHERE id=3;
        INSERT INTO cinema_activity_episode_sources(theater_id,episode_id,source_provider,source_movie_id,observed_from,observed_through,first_seen_at,last_seen_at)
            SELECT theater_id,id,anchor_provider,anchor_source_movie_id,observed_from,observed_through,detected_at,detected_at FROM cinema_activity_episodes;
        INSERT INTO theater_locations(provider,provider_theater_id,source,status,matched_label,match_score,address_hash,updated_at,candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type)
            VALUES('cinewest','webediamovies-W8400','ign','ambiguous','Capitole candidate',0.6,repeat('a',64),'2026-09-14T12:00:00Z',43.98,4.86,'84130','Le Pontet','street');
        INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at)
            VALUES('ugc','25','manual','manual',50.6,3.1,'2026-09-14T12:00:00Z');
        INSERT INTO theater_images(provider,provider_theater_id,image_revision,file_key,width,height,size_bytes)
            VALUES('cinewest','webediamovies-W8400',4,repeat('a',32)||'.webp',800,600,12345),('ugc','25',0,NULL,NULL,NULL,NULL);
        INSERT INTO accounts(email,created_at,pending_kind) SELECT 'fixture'||g||'@example.test','2026-09-01T12:00:00Z','email' FROM generate_series(1,5) g;
        INSERT INTO account_theater_preferences(account_id,revision,theater_ids) VALUES
            (1,4,ARRAY['ugc-25','cinewest-webediamovies-W8400']),
            (2,9,ARRAY['cinewest-webediamovies-W8400','ugc-25','cinewest-ticketingcine-EMS1378']),
            (3,2,ARRAY['ugc-25']),(4,6,'{}');
        INSERT INTO account_theater_follows(account_id,revision,theater_ids) VALUES
            (1,7,ARRAY['cinewest-webediamovies-W8400']),
            (2,12,ARRAY['cinewest-ticketingcine-EMS1378','cinewest-webediamovies-W8400','ugc-25']),
            (3,5,ARRAY['ugc-25']),(4,8,'{}');
        INSERT INTO account_watchlist_state(account_id,revision) VALUES(1,3);
        INSERT INTO account_watchlist_items(account_id,public_movie_id,added_at) SELECT 1,id,'2026-09-14T12:00:00Z' FROM public_movies;
        INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('cinewest',false,'daily','10:00');
    `)
	for generation := 1; generation <= 2; generation++ {
		mustCinevilleSQL(t, ctx, pool, `INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES($1,'cinewest',1,'all_cinemas','2026-09-14T12:00:00Z','Europe/Paris','2026-09-14','2026-09-14')`, generation)
		mustCinevilleSQL(t, ctx, pool, `INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES($1,$2,'cinewest','webediamovies-W8400',$2,'Capitole Studios','1 Avenue','Le Pontet','84130')`, generation, capitoleOld)
		mustCinevilleSQL(t, ctx, pool, `INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES($1,$2,'2026-09-14')`, generation, capitoleOld)
		// Retained SQL rows may contain old pass metadata. The active Cinewest
		// snapshot must remain valid under its existing no-pass Go contract.
		if generation == 1 {
			mustCinevilleSQL(t, ctx, pool, `INSERT INTO theater_passes(generation_id,theater_id,pass_code) VALUES($1,$2,'UGC_ILLIMITE')`, generation, capitoleOld)
		}
		mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes,poster_url) VALUES($1,'cinewest','webediamovies-1','cinewest-film-webediamovies-1','Historical film',90,'https://all.web.img.acsta.net/img/6f/af/6faf7d9aa879bd9374e773f31db44956.jpg')`, generation)
		mustCinevilleSQL(t, ctx, pool, `INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes)
            VALUES($1,'cinewest-showing-'||$2::text,'cinewest',$2,'2026-09-14',$3,'webediamovies-1','2026-09-14T18:00:00.123456Z','2026-09-14T18:00:00.123456Z','VF','VF','INFINITY_VISION','Salle 1','https://www.capitolestudios-reserver.cotecine.fr/reserver/r/123',0)`, generation, capitoleLegacyShowing, capitoleOld)
	}
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes,first_seen_at,last_seen_at,source_generated_at,last_generation)
        SELECT id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes,'2026-09-01T12:00:00Z','2026-09-14T12:00:00Z','2026-09-14T12:00:00Z',generation_id FROM showtimes WHERE generation_id=2`)
}

// Compare every persisted row, omitting only the explicitly mutable columns.
func capitoleFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, omit map[string][]string) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	facts := map[string]string{}
	for _, table := range tables {
		columns := omit[table]
		if columns == nil {
			columns = []string{}
		}
		var value string
		sql := fmt.Sprintf(`SELECT coalesce(jsonb_agg(value ORDER BY value::text),'[]')::text FROM (SELECT to_jsonb(t)-$1::text[] AS value FROM %q t) projected`, table)
		if err := pool.QueryRow(ctx, sql, columns).Scan(&value); err != nil {
			t.Fatal(table, err)
		}
		facts[table] = value
	}
	return facts
}

func capitoleFKFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid),condeferrable,condeferred,convalidated) ORDER BY conrelid::regclass::text,conname)::text FROM pg_constraint WHERE contype='f' AND connamespace=(SELECT oid FROM pg_namespace WHERE nspname=current_schema())`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCapitoleContinuityMigrationIntegration(t *testing.T) {
	for _, tombstone := range []bool{false, true} {
		t.Run(fmt.Sprintf("tombstone_%t", tombstone), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "capitole_continuity_")
			seedCapitole059(t, ctx, pool)
			if tombstone {
				mustCinevilleSQL(t, ctx, pool, `UPDATE theater_images SET file_key=NULL,width=NULL,height=NULL,size_bytes=NULL WHERE provider='cinewest'`)
			}
			omit := map[string][]string{
				"movieflow_schema_migrations": {"version", "name", "applied_at"},
				"theaters":                    {"id", "slug", "provider_id"}, "screening_history_theaters": {"id", "slug", "provider_id"},
				"theater_dates": {"theater_id"}, "theater_passes": {"theater_id"}, "showtimes": {"theater_id"}, "screening_history_showtimes": {"theater_id"},
				"cinema_activity_state": {"theater_id"}, "cinema_activity_coverage": {"theater_id"}, "cinema_activity_episodes": {"theater_id"}, "cinema_activity_episode_sources": {"theater_id"},
				"theater_locations": {"provider_theater_id"}, "theater_images": {"provider_theater_id", "image_revision"}, "theater_location_state": {"version"},
				"account_theater_preferences": {"theater_ids", "revision"}, "account_theater_follows": {"theater_ids", "revision"},
			}
			before := capitoleFacts(t, ctx, pool, omit)
			delete(before, "movieflow_schema_migrations")
			fks := capitoleFKFacts(t, ctx, pool)
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			after := capitoleFacts(t, ctx, pool, omit)
			delete(after, "movieflow_schema_migrations")
			if !reflect.DeepEqual(before, after) {
				for table, value := range before {
					if value != after[table] {
						t.Errorf("preserved facts changed in %s", table)
					}
				}
			}
			if fks != capitoleFKFacts(t, ctx, pool) {
				t.Fatal("FK properties changed")
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			assertCapitoleRekey(t, ctx, pool)
			assertCapitoleConstraints(t, ctx, pool)
			migrated := capitoleFacts(t, ctx, pool, nil)
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(migrated, capitoleFacts(t, ctx, pool, nil)) {
				t.Fatal("applied-ledger rerun mutated data")
			}
			assertCapitolePublicationContinuity(t, ctx, pool)
		})
	}
}

func assertCapitoleRekey(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{"theaters", "screening_history_theaters", "theater_dates", "theater_passes", "showtimes", "screening_history_showtimes", "cinema_activity_state", "cinema_activity_coverage", "cinema_activity_episodes", "cinema_activity_episode_sources"} {
		column := "theater_id"
		if table == "theaters" || table == "screening_history_theaters" {
			column = "id"
		}
		var old, new int
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FILTER(WHERE %s=$1),count(*) FILTER(WHERE %s=$2) FROM %s`, column, column, table), capitoleOld, capitoleNew).Scan(&old, &new); err != nil || old != 0 || new == 0 {
			t.Fatal("missing rekey", table, err)
		}
	}
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT version=12 FROM theater_location_state) AND (SELECT image_revision=5 FROM theater_images WHERE provider='cinewest' AND provider_theater_id='ticketingcine-EMS1378') AND NOT EXISTS(SELECT 1 FROM theater_locations WHERE provider='cinewest' AND provider_theater_id='webediamovies-W8400') AND EXISTS(SELECT 1 FROM theater_locations WHERE provider='cinewest' AND provider_theater_id='ticketingcine-EMS1378')`).Scan(&ok); err != nil || !ok {
		t.Fatal("media revision/key", err)
	}
	for _, tc := range []struct {
		table     string
		revisions []int64
		ids       [][]string
	}{
		{"account_theater_preferences", []int64{5, 10, 2, 6}, [][]string{{capitoleNew, "ugc-25"}, {capitoleNew, "ugc-25"}, {"ugc-25"}, {}}},
		{"account_theater_follows", []int64{8, 13, 5, 8}, [][]string{{capitoleNew}, {capitoleNew, "ugc-25"}, {"ugc-25"}, {}}},
	} {
		for i, want := range tc.ids {
			var ids []string
			var revision int64
			if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT revision,theater_ids FROM %s WHERE account_id=$1`, tc.table), i+1).Scan(&revision, &ids); err != nil || revision != tc.revisions[i] || !reflect.DeepEqual(ids, want) {
				t.Fatal("account rekey/revision", tc.table, i, err)
			}
		}
		tag, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET revision=revision+1 WHERE account_id=1 AND revision=$1`, tc.table), tc.revisions[0]-1)
		if err != nil || tag.RowsAffected() != 0 {
			t.Fatal("pre-migration CAS accepted", tc.table, err)
		}
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT NOT EXISTS(SELECT 1 FROM %s WHERE account_id=5)`, tc.table)).Scan(&ok); err != nil || !ok {
			t.Fatal("absent state fabricated", err)
		}
	}
}

func assertCapitoleConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT bool_and(convalidated) FROM pg_constraint WHERE contype IN ('c','f') AND connamespace=(SELECT oid FROM pg_namespace WHERE nspname=current_schema())`).Scan(&ok); err != nil || !ok {
		t.Fatal("unvalidated integrity", err)
	}
	for _, kind := range []string{"theater", "movie", "showing"} {
		for _, id := range []string{"webediamovies-W8400", "ticketingcine-EMS1378", "webediamovies-1", capitoleLegacyShowing, "ticketingcine-EMS1378-emsx1378HC1", "ticketingcine-EMS1378-emsx1185HC1"} {
			if err := pool.QueryRow(ctx, `SELECT cinewest_identity_valid($1,$2)`, kind, id).Scan(&ok); err != nil || ok != schedule.ValidCinewestIdentity(kind, id) {
				t.Fatal("identity parity", kind, id, err)
			}
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		for _, change := range []string{`end_time=start_time+interval '1 minute'`, `first_part_duration_minutes=1`, `room=''`, `movie_provider_id='ticketingcine-ABCDE'`, `theater_id='cinewest-ticketingcine-EMS1185'`} {
			rejectCinevilleSQL(t, ctx, pool, fmt.Sprintf(`UPDATE %s SET %s WHERE provider='cinewest'`, table, change))
		}
	}
	rejectCinevilleSQL(t, ctx, pool, `UPDATE screening_history_theaters SET id='cinewest-webediamovies-W8400',slug='cinewest-webediamovies-W8400',provider_id='webediamovies-W8400' WHERE provider='cinewest'`)
	rejectCinevilleSQL(t, ctx, pool, `UPDATE theater_locations SET provider_theater_id='webediamovies-W8400' WHERE provider='cinewest'`)
	// Restored FKs fail immediately, not only when a transaction commits.
	for _, sql := range []string{`UPDATE cinema_activity_episode_sources SET episode_id=999999 WHERE episode_id=1`, `UPDATE cinema_activity_episodes SET anchor_source_movie_id='webediamovies-999' WHERE id=1`} {
		_, err := pool.Exec(ctx, sql)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatal("immediate FK rejection missing", err)
		}
	}
}

func assertCapitolePublicationContinuity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	store := schedulepg.NewStore(pool)
	loaded, revision, err := store.Load(ctx)
	if err != nil || revision.ScheduleVersion != 2 || revision.EnrichmentVersion != 7 || revision.TheaterLocationVersion != 12 || len(loaded.Showtimes) != 1 {
		t.Fatal("pre-publication migrated load", err)
	}
	if loaded.Theaters[0].ID != capitoleNew || loaded.Showtimes[0].ProviderShowingID != capitoleLegacyShowing || !loaded.Showtimes[0].EndTime.Equal(loaded.Showtimes[0].StartTime) {
		t.Fatal("historical interpretation changed")
	}
	ugc := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: schedule.ScopeAll, GeneratedAt: loaded.GeneratedAt.Add(time.Minute), Timezone: schedule.Timezone, Window: loaded.Window, Theaters: []schedule.TheaterRecord{{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "Other cinema", Address: "1 Rue", City: "Lille", PostalCode: "59000", AvailableDates: []string{}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []schedule.ShowtimeRecord{}}
	if _, err := store.Replace(ctx, []schedule.Dataset{ugc}); err != nil {
		t.Fatal("publication copying legacy snapshot", err)
	}
	loaded, _, err = store.Load(ctx)
	if err != nil || len(loaded.Showtimes) != 1 || loaded.Showtimes[0].ProviderShowingID != capitoleLegacyShowing {
		t.Fatal("copied legacy load", err)
	}
	next := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCinewest, Scope: schedule.ScopeAll, GeneratedAt: ugc.GeneratedAt.Add(time.Minute), Timezone: schedule.Timezone, Window: ugc.Window, Theaters: []schedule.TheaterRecord{{Provider: schedule.ProviderCinewest, ID: capitoleNew, ProviderID: "ticketingcine-EMS1378", Slug: capitoleNew, Name: "Capitole Studios", Address: "1 Avenue", City: "Le Pontet", PostalCode: "84130", AvailableDates: []string{"2026-09-14"}, AcceptedPasses: []string{}}}}
	showing := loaded.Showtimes[0]
	id, _ := schedule.CinewestShowingID("ticketingcine-EMS1378", "emsx137800000001")
	showing.ID, showing.ProviderShowingID = "cinewest-showing-"+id, id
	showing.Movie = schedule.MovieRecord{Provider: schedule.ProviderCinewest, ProviderID: "ticketingcine-ABCDE", Slug: "cinewest-film-ticketingcine-ABCDE", Title: "New film", RuntimeMinutes: 100}
	showing.FirstPartDurationMinutes = 15
	showing.EndTime = showing.StartTime.Add(115 * time.Minute)
	showing.BookingURL = "https://www.capitolestudios.com/#showsession?id=emsx137800000001"
	next.Showtimes = []schedule.ShowtimeRecord{showing}
	if _, err := store.Replace(ctx, []schedule.Dataset{next}); err != nil {
		t.Fatal("new Capitole publication", err)
	}
	loaded, _, err = store.Load(ctx)
	if err != nil || len(loaded.Showtimes) != 1 || loaded.Showtimes[0].ProviderShowingID != id {
		t.Fatal("new Capitole load", err)
	}
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening_history_showtimes WHERE theater_id=$1 AND provider_showing_id=$2 AND movie_provider_id='webediamovies-1' AND end_time=start_time AND last_generation=2 AND first_seen_at='2026-09-01T12:00:00Z') AND (SELECT count(*)=3 FROM cinema_activity_episodes WHERE id<=3 AND theater_id=$1 AND anchor_source_movie_id='webediamovies-1') AND (SELECT count(*)=3 FROM cinema_activity_episode_sources WHERE episode_id<=3 AND theater_id=$1 AND source_movie_id='webediamovies-1')`, capitoleNew, capitoleLegacyShowing).Scan(&ok); err != nil || !ok {
		t.Fatal("history/activity anchor continuity", err)
	}
}

// Test fixtures can represent independently populated new-platform state before
// upgrade, without weakening final schema or using the rejected legacy helper.
func allowCapitoleDestinationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var definition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('cinewest_identity_valid(text,text)'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	definition = strings.Replace(definition, "'webediamovies-W8400')", "'webediamovies-W8400', 'ticketingcine-EMS1378')", 1)
	mustCinevilleSQL(t, ctx, pool, definition)
}

func TestCapitoleContinuityRollbackIntegration(t *testing.T) {
	cases := map[string]string{
		"cross_namespace_collision": `DELETE FROM theater_images WHERE provider='cinewest'; INSERT INTO theater_images(provider,provider_theater_id,image_revision) VALUES('cinewest','ticketingcine-EMS1378',0)`,
		"image_collision":           `INSERT INTO theater_images(provider,provider_theater_id,image_revision) VALUES('cinewest','ticketingcine-EMS1378',0)`,
		"location_collision":        `INSERT INTO theater_locations SELECT provider,'ticketingcine-EMS1378',latitude,longitude,source,matched_label,match_score,address_hash,status,updated_at,candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type FROM theater_locations WHERE provider='cinewest'`,
		"generation_collision":      `INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) SELECT generation_id,'cinewest-ticketingcine-EMS1378',provider,'ticketingcine-EMS1378','cinewest-ticketingcine-EMS1378',name,address,city,postal_code FROM theaters WHERE generation_id=1`,
		"history_collision":         `INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at) SELECT 'cinewest-ticketingcine-EMS1378',provider,'ticketingcine-EMS1378','cinewest-ticketingcine-EMS1378',name,address,city,postal_code,city_slug,city_name,passes,last_observed_at FROM screening_history_theaters`,
		"image_exhausted":           `UPDATE theater_images SET image_revision=9007199254740991 WHERE provider='cinewest'`,
		"preferences_exhausted":     `UPDATE account_theater_preferences SET revision=9007199254740991 WHERE account_id=1`,
		"follows_exhausted":         `UPDATE account_theater_follows SET revision=9007199254740991 WHERE account_id=2`,
		"aggregate_exhausted":       `UPDATE theater_location_state SET version=9223372036854775807`,
		"invalid_existing_location": `ALTER TABLE theater_locations DROP CONSTRAINT theater_locations_cinewest_identity_check; UPDATE theater_locations SET provider_theater_id='webediamovies-W0000' WHERE provider='cinewest'; ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_cinewest_identity_check CHECK(provider<>'cinewest' OR cinewest_identity_valid('theater',provider_theater_id)) NOT VALID`,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "capitole_rollback_")
			seedCapitole059(t, ctx, pool)
			allowCapitoleDestinationFixture(t, ctx, pool)
			mustCinevilleSQL(t, ctx, pool, sql)
			before := capitoleFacts(t, ctx, pool, nil)
			fks := capitoleFKFacts(t, ctx, pool)
			if err := RunMigrations(ctx, pool); err == nil {
				t.Fatal("unsafe continuity migration committed")
			}
			if !reflect.DeepEqual(before, capitoleFacts(t, ctx, pool, nil)) || fks != capitoleFKFacts(t, ctx, pool) {
				t.Fatal("rollback changed rows, revisions, ledger or FKs")
			}
			var oldValid, newValid bool
			if err := pool.QueryRow(ctx, `SELECT cinewest_identity_valid('theater','webediamovies-W8400'),cinewest_identity_valid('theater','ticketingcine-EMS1378')`).Scan(&oldValid, &newValid); err != nil || !oldValid || !newValid {
				t.Fatal("helper replacement did not roll back", err)
			}
		})
	}
}

func TestCapitoleDestinationOnlyMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "capitole_destination_")
	installMigrationPrefix(t, ctx, pool, 59, "059_account_theater_follows.sql")
	allowCapitoleDestinationFixture(t, ctx, pool)
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO accounts(email,created_at,pending_kind) VALUES('fixture@example.test',now(),'email'); INSERT INTO account_theater_preferences VALUES(1,9007199254740991,ARRAY['cinewest-ticketingcine-EMS1378']); INSERT INTO account_theater_follows VALUES(1,9007199254740991,ARRAY['cinewest-ticketingcine-EMS1378']); INSERT INTO theater_images(provider,provider_theater_id,image_revision) VALUES('cinewest','ticketingcine-EMS1378',9007199254740991); UPDATE theater_location_state SET version=9223372036854775807;`)
	before := capitoleFacts(t, ctx, pool, nil)
	delete(before, "movieflow_schema_migrations")
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("unaffected exhausted destination", err)
	}
	after := capitoleFacts(t, ctx, pool, nil)
	delete(after, "movieflow_schema_migrations")
	// Later additive migrations introduce derived sitemap baseline storage, not
	// a mutation of Capitole's existing destination-only state.
	delete(after, "public_page_content_state")
	delete(after, "public_page_content")
	var baseline bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*)=1 FROM public_page_content_state WHERE singleton AND observed_at IS NULL) AND NOT EXISTS(SELECT 1 FROM public_page_content)`).Scan(&baseline); err != nil || !baseline {
		t.Fatal("sitemap migration did not create an unknown empty baseline", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("destination-only state mutated")
	}
}

func TestCapitoleArrayOnlyMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "capitole_arrays_")
	installMigrationPrefix(t, ctx, pool, 59, "059_account_theater_follows.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO accounts(email,created_at,pending_kind) VALUES('fixture@example.test',now(),'email'); INSERT INTO account_theater_preferences VALUES(1,4,ARRAY['cinewest-webediamovies-W8400']); INSERT INTO account_theater_follows VALUES(1,8,ARRAY['cinewest-webediamovies-W8400']);`)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT version=0 FROM theater_location_state) AND (SELECT revision=5 AND theater_ids=ARRAY['cinewest-ticketingcine-EMS1378'] FROM account_theater_preferences) AND (SELECT revision=9 AND theater_ids=ARRAY['cinewest-ticketingcine-EMS1378'] FROM account_theater_follows) AND NOT EXISTS(SELECT 1 FROM theaters) AND NOT EXISTS(SELECT 1 FROM screening_history_theaters) AND NOT EXISTS(SELECT 1 FROM theater_images) AND NOT EXISTS(SELECT 1 FROM schedule_snapshot)`).Scan(&ok); err != nil || !ok {
		t.Fatal("array-only upgrade fabricated publication/state", err)
	}
}
