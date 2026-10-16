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

func TestVeoCheckoutSourceMigrationIntegration(t *testing.T) {
	for _, baseline := range []int64{0, 63} {
		t.Run(fmt.Sprintf("baseline%03d", baseline), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "veo_checkout_")
			var beforeRows, beforeConstraints, beforeLedger string
			if baseline != 0 {
				installMigrationPrefix(t, ctx, pool, 63, "063_veo_provider.sql")
				seedVeoCheckoutRows(t, ctx, pool)
				insertVeoCheckoutRow(t, ctx, pool, "P0448", "old", "https://achat.veocinemas.fr/chateaurenard/r/22309")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90);
INSERT INTO accounts(id,email,created_at,email_verified_at,verification_source) OVERRIDING SYSTEM VALUE VALUES(1,'watched@example.com','2026-09-01T12:00:00Z','2026-09-01T12:01:00Z','email');
INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES(10,10,'Watched film',90);
INSERT INTO account_watched_state(account_id,revision,default_visibility,auto_remove_watchlist) VALUES(1,17,'show',true);
INSERT INTO account_watched_items(account_id,public_movie_id,watched_at) VALUES(1,10,'2026-09-15T18:00:00.123456Z')`)
				beforeRows = veoCheckoutRows(t, ctx, pool)
				beforeConstraints = veoCheckoutConstraints(t, ctx, pool)
				beforeLedger = veoCheckoutLedger(t, ctx, pool, 63)
				var accepted bool
				if err := pool.QueryRow(ctx, `SELECT veo_booking_valid($1,'P0448',$2)`, "https://achat.veocinemas.fr/chateaurenard/reserver/r/22309", schedule.VeoShowingID("P0448", "old")).Scan(&accepted); err != nil || accepted {
					t.Fatal("063 unexpectedly accepts alternate source", err)
				}
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if baseline != 0 {
				if beforeRows != veoCheckoutRows(t, ctx, pool) || beforeConstraints != veoCheckoutConstraints(t, ctx, pool) || beforeLedger != veoCheckoutLedger(t, ctx, pool, 63) {
					t.Fatal("064 changed existing rows/timestamps, constraints or published ledger")
				}
			} else {
				seedVeoCheckoutRows(t, ctx, pool)
			}
			for _, cinema := range []struct{ id, slug, session string }{{"P0448", "chateaurenard", "22309"}, {"W3187", "labarthe-sur-leze", "5093"}} {
				for index, segment := range []string{"", "reserver/"} {
					raw := "https://achat.veocinemas.fr/" + cinema.slug + "/" + segment + "r/" + cinema.session
					showing := schedule.VeoShowingID(cinema.id, fmt.Sprintf("new%d", index))
					insertVeoCheckoutRow(t, ctx, pool, cinema.id, fmt.Sprintf("new%d", index), raw)
					assertVeoBookingParity(t, ctx, pool, raw, cinema.id, showing, true)
					assertVeoBookingParity(t, ctx, pool, raw, "W0067", showing, false)
					assertVeoBookingParity(t, ctx, pool, raw, "EMS1361", schedule.VeoShowingID("EMS1361", "emsx136100000001"), false)
					for _, bad := range []string{
						raw + "?", raw + "?x=1", raw + "#", raw + "/", raw + "/extra", raw + "\n", raw + "\r", " " + raw,
						strings.Replace(raw, "https:", "http:", 1), strings.Replace(raw, ".fr/", ".fr:443/", 1), strings.Replace(raw, "https://", "https://user@", 1), strings.Replace(raw, "achat.", "evil.achat.", 1),
						strings.Replace(raw, "/"+cinema.session, "/0", 1), strings.Replace(raw, "/"+cinema.session, "/01", 1), strings.Replace(raw, "/r/", "/R/", 1), strings.Replace(raw, "/r/", "/%72/", 1),
						strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/../"+cinema.slug+"/", 1),
						strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/reserver/reserver/", 1),
						strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/Reserver/", 1),
						strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/reserver//", 1),
						strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/%72eserver/", 1),
						strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/reserver/F1/D1792065000/VF/", 1),
						raw + strings.Repeat("1", 4097),
					} {
						assertVeoBookingParity(t, ctx, pool, bad, cinema.id, showing, false)
						for _, table := range []string{"showtimes", "screening_history_showtimes"} {
							if _, err := pool.Exec(ctx, `UPDATE `+table+` SET booking_url=$1 WHERE provider_showing_id=$2`, bad, showing); err == nil {
								t.Fatal("unsafe live/history source accepted", table)
							}
						}
					}
				}
			}
			castShowing := schedule.VeoShowingID("EMS1361", "emsx136100000001")
			for _, raw := range []string{"https://castelginest.miracinemas.fr", "https://castelginest.miracinemas.fr/", "https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001"} {
				assertVeoBookingParity(t, ctx, pool, raw, "EMS1361", castShowing, true)
			}
			for _, raw := range []string{"https://castelginest.miracinemas.fr/#showsession?id=emsx136100000002", "https://castelginest.miracinemas.fr:443/", "https://castelginest.miracinemas.fr/?", "https://castelginest.miracinemas.fr/reserver/r/1"} {
				assertVeoBookingParity(t, ctx, pool, raw, "EMS1361", castShowing, false)
			}
			rows, constraints, ledger := veoCheckoutRows(t, ctx, pool), veoCheckoutConstraints(t, ctx, pool), veoCheckoutLedger(t, ctx, pool, 64)
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if rows != veoCheckoutRows(t, ctx, pool) || constraints != veoCheckoutConstraints(t, ctx, pool) || ledger != veoCheckoutLedger(t, ctx, pool, 64) {
				t.Fatal("migration rerun changed data/schema/ledger timestamps")
			}
		})
	}
}

func seedVeoCheckoutRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	for _, sql := range []string{
		`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) SELECT 1,'veo-'||id,'veo',id,'veo-'||id,'Véo','1 rue','Fixture','75001' FROM unnest(ARRAY['P0448','W3187']) id`,
		`INSERT INTO theater_dates(generation_id,theater_id,service_date) SELECT 1,id,DATE '2026-10-15' FROM theaters WHERE provider='veo'`,
		`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'veo','123','veo-film-123','Film',90)`,
		`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('veo','123','Film',90)`,
		`INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'veo','123',id,'veo-film-123','Film',90 FROM public_movies WHERE identity_anchor_provider='veo'`,
		`INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at) SELECT id,provider,provider_id,slug,name,address,city,postal_code,'fixture','Fixture','{}','2026-10-15T10:00:00Z' FROM theaters WHERE provider='veo'`,
	} {
		mustCinevilleSQL(t, ctx, pool, sql)
	}
}

func insertVeoCheckoutRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, theater, source, booking string) {
	t.Helper()
	showing := schedule.VeoShowingID(theater, source)
	_, err := pool.Exec(ctx, `INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,$1,'veo',$2,'2026-10-15',$3,'123','2026-10-15T18:00:00Z','2026-10-15T19:41:00Z','VF','VF','2D','API room',$4)`, "veo-showing-"+showing, showing, "veo-"+theater, booking)
	if err != nil {
		t.Fatal(err)
	}
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes,first_seen_at,last_seen_at,source_generated_at,last_generation) SELECT id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,0,'2026-10-15T10:00:00Z','2026-10-15T10:00:00Z','2026-10-15T10:00:00Z',1 FROM showtimes ON CONFLICT DO NOTHING`)
}

func assertVeoBookingParity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, raw, theater, showing string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `SELECT veo_booking_valid($1,$2,$3)`, raw, theater, showing).Scan(&got); err != nil || got != want || got != schedule.ValidVeoBookingURL(raw, theater, showing) {
		t.Fatal("Go/SQL source policy mismatch", err)
	}
}

func veoCheckoutRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename <> 'movieflow_schema_migrations' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	var result strings.Builder
	for _, table := range tables {
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]')::text FROM `+pgx.Identifier{table}.Sanitize()+` r`).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&result, "%s:%s\n", table, snapshot)
	}
	return result.String()
}

func veoCheckoutConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(c.conrelid::regclass::text,c.conname,c.contype,c.convalidated,pg_get_constraintdef(c.oid)) ORDER BY c.conrelid::regclass::text,c.conname),'[]')::text FROM pg_constraint c WHERE c.connamespace='public'::regnamespace`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func veoCheckoutLedger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, through int) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY version),'[]')::text FROM movieflow_schema_migrations m WHERE version<=$1`, through).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
