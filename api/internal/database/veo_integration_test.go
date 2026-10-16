package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/schedule"
)

// Exact expected 063 CHECK deltas for legacy snapshot fixtures. Capture the
// original expression, then have PostgreSQL parse/deparse the expected amended
// CHECK on an empty temporary table. This compares full predicates (including
// their legacy branch), not just a provider name or a broad exclusion pattern.
type veoConstraintChange struct {
	table, name, addition, previous string
	newCheck, timing                bool
}

func captureVeoConstraintChanges(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []veoConstraintChange {
	t.Helper()
	changes := []veoConstraintChange{}
	add := func(table, name, addition string) {
		changes = append(changes, veoConstraintChange{table: table, name: name, addition: addition})
	}
	for _, table := range []string{"schedule_snapshot", "provider_snapshots", "theaters", "movies", "showtimes", "screening_history_providers", "theater_images", "cinema_activity_coverage"} {
		add(table, table+"_provider_check", "provider = 'veo'")
	}
	for _, item := range []struct{ table, name, column string }{
		{"movie_matches", "movie_matches_source_provider_check", "source_provider"},
		{"local_movie_groups", "local_movie_groups_primary_source_provider_check", "primary_source_provider"},
		{"local_movie_group_members", "local_movie_group_members_source_provider_check", "source_provider"},
		{"public_movies", "public_movies_identity_anchor_provider_check", "identity_anchor_provider"},
		{"public_movie_sources", "public_movie_sources_source_provider_check", "source_provider"},
		{"theater_locations", "theater_locations_provider_check", "provider"},
		{"sync_runs", "sync_runs_target_check", "target"},
		{"sync_schedules", "sync_schedules_target_check", "target"},
	} {
		add(item.table, item.name, item.column+" = 'veo'")
	}
	add("theaters", "theaters_provider_identity_check", "provider = 'veo' AND veo_identity_valid('theater', provider_id)")
	add("showtimes", "showtimes_provider_identity_check", "provider = 'veo' AND veo_identity_valid('showing', provider_showing_id) AND theater_id = 'veo-' || split_part(provider_showing_id, '-', 1)")
	add("showtimes", "showtimes_check", "provider = 'veo' AND theater_id = 'veo-EMS1361' AND first_part_duration_minutes >= 0")
	add("movie_slug_aliases", "movie_slug_aliases_check", "alias_kind = 'source' AND source_provider = 'veo' AND source_movie_id IS NOT NULL")
	add("sync_runs", "sync_runs_trigger_occurrence_check", "trigger_source = 'scheduled' AND target = 'veo' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2")
	add("cinema_activity_coverage", "cinema_activity_coverage_check", "provider = 'veo' AND theater_id <> 'veo-EMS1361' AND status = 'complete' AND basis = 'date_response'")
	// Migration 039 copied these CHECKs with their original names.
	for _, change := range append([]veoConstraintChange(nil), changes...) {
		if change.table == "theaters" || change.table == "showtimes" {
			add("screening_history_"+change.table, change.name, change.addition)
		}
	}
	for _, item := range []struct{ table, name, provider, id string }{
		{"movies", "movies_provider_identity_check", "provider", "provider_id"},
		{"movie_matches", "movie_matches_source_movie_id_check", "source_provider", "source_movie_id"},
		{"local_movie_groups", "local_movie_groups_check", "primary_source_provider", "primary_source_movie_id"},
		{"local_movie_group_members", "local_movie_group_members_check", "source_provider", "source_movie_id"},
		{"public_movies", "public_movies_check1", "identity_anchor_provider", "identity_anchor_source_movie_id"},
		{"public_movie_sources", "public_movie_sources_check", "source_provider", "source_movie_id"},
		{"movie_slug_aliases", "movie_slug_aliases_check1", "source_provider", "source_movie_id"},
	} {
		add(item.table, item.name, fmt.Sprintf("%s = 'veo' AND veo_identity_valid('movie', %s)", item.provider, item.id))
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		changes = append(changes, veoConstraintChange{table: table, name: "showtimes_time_check", timing: true})
		changes = append(changes, veoConstraintChange{table: table, name: "showtimes_veo_shape_check", newCheck: true, addition: `provider <> 'veo' OR (
theater_id = 'veo-' || split_part(provider_showing_id, '-', 1)
AND veo_identity_valid('movie', movie_provider_id)
AND ((theater_id = 'veo-EMS1361') = (movie_provider_id LIKE 'ticketingcine-%'))
AND (theater_id = 'veo-EMS1361' OR first_part_duration_minutes = 0)
AND veo_booking_valid(booking_url, split_part(provider_showing_id, '-', 1), provider_showing_id))`})
	}
	for _, item := range []struct{ table, name, expression string }{
		{"movie_slug_aliases", "movie_slug_aliases_veo_shape_check", "source_provider IS DISTINCT FROM 'veo' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'veo-film-' || source_movie_id)"},
		{"public_movie_sources", "public_movie_sources_veo_slug_check", "source_provider <> 'veo' OR source_slug = 'veo-film-' || source_movie_id"},
		{"theater_locations", "theater_locations_veo_identity_check", "provider <> 'veo' OR veo_identity_valid('theater', provider_theater_id)"},
		{"theater_images", "theater_images_veo_identity_check", "provider <> 'veo' OR veo_identity_valid('theater', provider_theater_id)"},
	} {
		changes = append(changes, veoConstraintChange{table: item.table, name: item.name, addition: item.expression, newCheck: true})
	}
	baseline := []veoConstraintChange{}
	for _, change := range changes {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, change.table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			continue // Later-added relations are outside the baseline snapshot.
		}
		if change.newCheck {
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, change.table, change.name).Scan(&count); err != nil || count != 0 {
				t.Fatal("unexpected baseline Véo CHECK", change.table, change.name, err)
			}
		} else if err := pool.QueryRow(ctx, `SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2 AND contype='c' AND convalidated`, change.table, change.name).Scan(&change.previous); err != nil {
			t.Fatal("missing legacy CHECK", change.table, change.name, err)
		}
		baseline = append(baseline, change)
	}
	return baseline
}

func veoConstraintKeys(changes []veoConstraintChange) []string {
	keys := make([]string, 0, len(changes))
	for _, change := range changes {
		keys = append(keys, change.table+"."+change.name)
	}
	return keys
}

func assertVeoConstraintChanges(t *testing.T, ctx context.Context, pool *pgxpool.Pool, changes []veoConstraintChange) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, change := range changes {
		expression := change.addition
		if change.timing {
			expression = fmt.Sprintf("(provider <> 'veo' AND (%s)) OR (provider = 'veo' AND end_time >= start_time)", change.previous)
		} else if !change.newCheck {
			expression = fmt.Sprintf("(%s) OR (%s)", change.previous, change.addition)
		}
		// LIKE without INCLUDING CONSTRAINTS copies columns, not CHECKs/data.
		query := fmt.Sprintf("CREATE TEMP TABLE veo_expected_check (LIKE %s) ON COMMIT DROP; ALTER TABLE veo_expected_check ADD CONSTRAINT expected CHECK (%s)", pgx.Identifier{change.table}.Sanitize(), expression)
		if _, err := tx.Exec(ctx, query, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatal("construct expected CHECK", change.table, change.name, err)
		}
		var matches bool
		if err := tx.QueryRow(ctx, `SELECT actual.contype='c' AND actual.convalidated AND pg_get_constraintdef(actual.oid)=pg_get_constraintdef(expected.oid)
FROM pg_constraint actual CROSS JOIN pg_constraint expected
WHERE actual.conrelid=$1::regclass AND actual.conname=$2
AND expected.conrelid='pg_temp.veo_expected_check'::regclass AND expected.conname='expected'`, change.table, change.name).Scan(&matches); err != nil || !matches {
			t.Fatal("063 changed more than the expected Véo CHECK delta", change.table, change.name, err)
		}
		if _, err := tx.Exec(ctx, `DROP TABLE pg_temp.veo_expected_check`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVeoMigrationIntegration(t *testing.T) {
	for _, baseline := range []int64{0, 61, 62} {
		name := "fresh"
		if baseline != 0 {
			name = fmt.Sprintf("upgrade%03d", baseline)
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "veo_migration_")
			var changes []veoConstraintChange
			var watchedBefore string
			if baseline != 0 {
				throughName := "061_public_page_content.sql"
				if baseline == 62 {
					throughName = "062_account_watched.sql"
				}
				installMigrationPrefix(t, ctx, pool, baseline, throughName)
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90)`)
				changes = captureVeoConstraintChanges(t, ctx, pool)
				if baseline == 62 {
					mustCinevilleSQL(t, ctx, pool, `INSERT INTO accounts(id,email,created_at,email_verified_at,verification_source) OVERRIDING SYSTEM VALUE VALUES(1,'watched@example.com','2026-09-01T12:00:00Z','2026-09-01T12:01:00Z','email');
INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES(10,10,'Retained watched film',90);
INSERT INTO account_watched_state(account_id,revision,default_visibility,search_visibility,planning_visibility,films_visibility,upcoming_visibility,home_visibility,cinema_city_visibility,auto_remove_watchlist) VALUES(1,17,'show','hide','show','inherit','hide','show','hide',true);
INSERT INTO account_watched_items(account_id,public_movie_id,watched_at) VALUES(1,10,'2026-09-15T18:00:00.123456Z')`)
					watchedBefore = watchedVeoUpgradeSnapshot(t, ctx, pool)
				}
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var n int
			if baseline != 0 {
				assertVeoConstraintChanges(t, ctx, pool, changes)
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE provider='ugc' AND title='Existing' AND runtime_minutes=90`).Scan(&n); err != nil || n != 1 {
					t.Fatal("legacy changed", err)
				}
			}
			if baseline == 62 && watchedVeoUpgradeSnapshot(t, ctx, pool) != watchedBefore {
				t.Fatal("Véo063 changed published watched062 data, metadata or ledger timestamps")
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='veo'`).Scan(&n); err != nil || n != 0 {
				t.Fatal("seeded schedule", err)
			}
			for _, sql := range []string{
				`UPDATE schedule_snapshot SET provider='veo'`,
				`INSERT INTO provider_snapshots(generation_id,provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES(1,'veo',1,'all_cinemas',now(),'Europe/Paris','2026-10-15','2027-05-31')`,
				`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'veo-P1028','veo','P1028','veo-P1028','Véo','1 rue','Paris','75001'),(1,'veo-EMS1361','veo','EMS1361','veo-EMS1361','Castelia','1 rue','Castelginest','31780')`,
				`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'veo-P1028','2026-10-15'),(1,'veo-EMS1361','2026-10-15')`,
				`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'veo','123','veo-film-123','Box film',0),(1,'veo','ticketingcine-A12B3','veo-film-ticketingcine-A12B3','Ticket film',90)`,
				`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'veo-showing-P1028-'||repeat('a',64),'veo','P1028-'||repeat('a',64),'2026-10-15','veo-P1028','123','2026-10-15T18:00:00Z','2026-10-15T18:00:00Z','VF','VF','2D','','https://achat.veocinemas.fr/fixture/r/123')`,
				`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('veo','123','Box film',0)`,
				`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'veo','123',id,'veo-film-123','Box film',0 FROM public_movies WHERE identity_anchor_provider='veo'`,
				`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id) SELECT 'veo-film-123',id,'source','veo','123' FROM public_movies WHERE identity_anchor_provider='veo'`,
				`INSERT INTO movie_matches(source_provider,source_movie_id,metadata_provider,status,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES('veo','123','tmdb','unmatched','box film',0,'[]',now(),now(),now())`,
				`WITH inserted AS (INSERT INTO local_movie_groups(primary_source_provider,primary_source_movie_id) VALUES('veo','123') RETURNING id) INSERT INTO local_movie_group_members(local_movie_id,source_provider,source_movie_id) SELECT id,'veo','123' FROM inserted`,
				`INSERT INTO theater_locations(provider,provider_theater_id,source,status,latitude,longitude,updated_at) VALUES('veo','P1028','manual','manual',48.83,2.37,now())`,
				`INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES('veo',false,'daily','10:00')`,
				`INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('veo','running',now(),'2026-10-15','2027-05-31','{}')`,
				`UPDATE sync_runs SET trigger_source='scheduled',schedule_id=1,schedule_revision=1,scheduled_for=now(),schedule_attempt=0 WHERE target='veo'`,
				`INSERT INTO screening_history_providers VALUES('veo',now(),now(),1,now())`,
				`INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at) SELECT id,provider,provider_id,slug,name,address,city,postal_code,'fixture','Fixture','{}',now() FROM theaters WHERE provider='veo'`,
				`INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes,first_seen_at,last_seen_at,source_generated_at,last_generation) SELECT id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,0,now(),now(),now(),1 FROM showtimes WHERE provider='veo'`,
				`INSERT INTO theater_images(provider,provider_theater_id) VALUES('veo','P1028'),('veo','EMS1361')`,
				`INSERT INTO cinema_activity_coverage VALUES('veo',1,'veo-P1028','2026-10-15','complete','date_response',now(),now()),('veo',1,'veo-EMS1361','2026-10-15','unknown','unproven',now(),now())`,
			} {
				mustCinevilleSQL(t, ctx, pool, sql)
			}
			for _, table := range []string{"showtimes", "screening_history_showtimes"} {
				mustCinevilleSQL(t, ctx, pool, `UPDATE `+table+` SET end_time=start_time+interval '1 minute' WHERE provider='veo'`)
				for _, change := range []string{`end_time=start_time-interval '1 minute'`, `first_part_duration_minutes=1`, `movie_provider_id='ticketingcine-A12B3'`, `booking_url='https://achat.veocinemas.fr/fixture/r/123?evil=1'`, `provider_showing_id='W0067-'||repeat('a',64),id='veo-showing-W0067-'||repeat('a',64)`} {
					rejectCinevilleSQL(t, ctx, pool, `UPDATE `+table+` SET `+change+` WHERE provider='veo'`)
				}
			}
			for _, sql := range []string{`UPDATE cinema_activity_coverage SET status='complete',basis='date_response' WHERE theater_id='veo-EMS1361'`, `UPDATE sync_runs SET schedule_id=NULL WHERE target='veo'`, `UPDATE sync_runs SET schedule_revision=NULL WHERE target='veo'`, `UPDATE sync_runs SET scheduled_for=NULL WHERE target='veo'`, `UPDATE sync_runs SET schedule_attempt=NULL WHERE target='veo'`, `UPDATE sync_runs SET schedule_attempt=3 WHERE target='veo'`, `UPDATE movie_slug_aliases SET slug='veo-film-999' WHERE source_provider='veo'`, `UPDATE public_movie_sources SET source_slug='veo-film-999' WHERE source_provider='veo'`, `UPDATE theater_locations SET provider_theater_id='EMS9999' WHERE provider='veo'`} {
				rejectCinevilleSQL(t, ctx, pool, sql)
			}
			rejectCinevilleSQL(t, ctx, pool, `UPDATE theater_images SET provider_theater_id='EMS9999' WHERE provider='veo'`)
			for _, kind := range []string{"theater", "movie", "showing"} {
				for _, id := range []string{"P1028", "p1028", "EMS1361", "EMS9999", "123", "01", "cEvent_1", "ticketingcine-A12B3", "P1028-" + strings.Repeat("a", 64), strings.Repeat("1", 112), strings.Repeat("1", 113), "P1028\n"} {
					var valid bool
					if err := pool.QueryRow(ctx, `SELECT veo_identity_valid($1,$2)`, kind, id).Scan(&valid); err != nil || valid != schedule.ValidVeoIdentity(kind, id) {
						t.Fatal(kind, id, err)
					}
				}
			}
			showing := schedule.VeoShowingID("EMS1361", "emsx136100000001")
			for _, raw := range []string{"https://castelginest.miracinemas.fr/", "https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001", "https://castelginest.miracinemas.fr/#showsession?id=emsx136100000002", "https://castelginest.miracinemas.fr:443/", "https://castelginest.miracinemas.fr/?x=1"} {
				var valid bool
				if err := pool.QueryRow(ctx, `SELECT veo_booking_valid($1,'EMS1361',$2)`, raw, showing).Scan(&valid); err != nil || valid != schedule.ValidVeoBookingURL(raw, "EMS1361", showing) {
					t.Fatal(raw, err)
				}
			}
			var historyBefore string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM movieflow_schema_migrations m`).Scan(&historyBefore); err != nil {
				t.Fatal(err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			var historyAfter string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM movieflow_schema_migrations m`).Scan(&historyAfter); err != nil || historyAfter != historyBefore {
				t.Fatal("rerun changed migration records or application timestamps", err)
			}
			if baseline == 62 && watchedVeoUpgradeSnapshot(t, ctx, pool) != watchedBefore {
				t.Fatal("rerun changed watched data, metadata or original migration history")
			}
		})
	}
}

func watchedVeoUpgradeSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id) FROM account_watched_state t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id,public_movie_id) FROM account_watched_items t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM accounts t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM public_movies t WHERE identity_anchor_provider IS DISTINCT FROM 'veo'),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY version) FROM movieflow_schema_migrations t WHERE version <= 62),
 (SELECT jsonb_agg(jsonb_build_array(c.relname,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid)) ORDER BY c.relname,a.attnum)
  FROM pg_class c JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
  LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum
  WHERE c.oid IN ('account_watched_state'::regclass,'account_watched_items'::regclass)),
 (SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid),convalidated) ORDER BY conrelid::regclass::text,conname)
  FROM pg_constraint WHERE conrelid IN ('account_watched_state'::regclass,'account_watched_items'::regclass)),
 (SELECT jsonb_agg(jsonb_build_array(tablename,indexname,indexdef) ORDER BY tablename,indexname)
  FROM pg_indexes WHERE schemaname=current_schema() AND tablename IN ('account_watched_state','account_watched_items'))
)::text`).Scan(&snapshot); err != nil {
		t.Fatal("snapshot published watched062 state", err)
	}
	return snapshot
}
