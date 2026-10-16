package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestVeoGueretMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "veo_gueret_")
	installMigrationPrefix(t, ctx, pool, 64, "064_veo_checkout_source_paths.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'veo-G06DB','veo','G06DB','veo-G06DB','Le Sénéchal','Canonical address','Guéret','23000');
INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'veo-G06DB','2026-10-16');
INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'veo','123','veo-film-123','Synthetic',90);
INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('veo','123','Synthetic',90);
INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'veo','123',id,'veo-film-123','Synthetic',90 FROM public_movies WHERE identity_anchor_provider='veo';
INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'veo-showing-G06DB-'||repeat('a',64),'veo','G06DB-'||repeat('a',64),'2026-10-16','veo-G06DB','123','2026-10-16T13:00:00Z','2026-10-16T13:00:00Z','VF','VF','2D','SENECHAL5','https://achat.veocinemas.fr/gueret/r/1');
INSERT INTO screening_history_providers VALUES('veo',now(),now(),1,now());
INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at) SELECT id,provider,provider_id,slug,name,address,city,postal_code,'gueret','Guéret','{}',now() FROM theaters;
INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,first_part_duration_minutes,first_seen_at,last_seen_at,source_generated_at,last_generation) SELECT id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,0,now(),now(),now(),1 FROM showtimes;`)
	snapshot := func() string {
		var result string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM showtimes t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM screening_history_showtimes t),
 (SELECT jsonb_agg(to_jsonb(t) ORDER BY version) FROM movieflow_schema_migrations t WHERE version<=64),
 (SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid),convalidated) ORDER BY conrelid::regclass::text,conname) FROM pg_constraint WHERE connamespace='public'::regnamespace)
)::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("065 rewrote data, CHECKs or published migration ledger")
	}
	assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
	const raw = "https://relay.mvtx.us/ticketing/dbz?code_theater=P1394&gid_entity=fixture_123.opaque&tags=Localization.Language.French,Format.Projection.Digital&perfd=20261016&perft=15:00"
	values := []string{raw, strings.ReplaceAll(strings.ReplaceAll(raw, ",", "%2C"), ":00", "%3a00"), strings.ReplaceAll(raw, "20261016", "20000229"), strings.ReplaceAll(raw, "fixture_123.opaque", strings.Repeat("%41", 1024)), raw + "&tags=x", raw + "&x=1", raw + "&", raw + "#", raw + "\n", raw + " ", strings.ReplaceAll(raw, "code_theater", "code%5ftheater"), strings.ReplaceAll(raw, "P1394", "G06DB"), strings.ReplaceAll(raw, "20261016", "20260229"), strings.ReplaceAll(raw, "20261016", "19000229"), strings.ReplaceAll(raw, "20261016", "00001016"), strings.ReplaceAll(raw, "15:00", "24:00"), strings.ReplaceAll(raw, "15:00", "15:60"), strings.ReplaceAll(raw, "https:", "http:"), strings.ReplaceAll(raw, "relay.mvtx.us", "relay.mvtx.us.evil.test"), strings.ReplaceAll(raw, "relay.mvtx.us", "relay.mvtx.us:443"), strings.ReplaceAll(raw, "https://", "https://user@"), strings.ReplaceAll(raw, "/ticketing/dbz", "/ticketing/%64bz"), strings.ReplaceAll(raw, "/ticketing/dbz", "/x/../ticketing/dbz"), "https://achat.cinema-senechal.com/r/1"}
	for _, value := range []string{"", "%", "%GG", "%252C", "%26x%3D1", "%3B", "%23", "%3F", "%2F", "%5C", "%00", "%0A", "+", "%20", "é", "%C3%A9", "a=b", strings.Repeat("a", 1025)} {
		values = append(values, strings.ReplaceAll(raw, "fixture_123.opaque", value))
	}
	for _, theater := range []string{"G06DB", "P1394", "P1028", "EMS1361"} {
		showing := theater + "-" + strings.Repeat("a", 64)
		for i, value := range values {
			var got bool
			if err := pool.QueryRow(ctx, `SELECT veo_booking_valid($1,$2,$3)`, value, theater, showing).Scan(&got); err != nil || got != schedule.ValidVeoBookingURL(value, theater, showing) {
				t.Fatalf("Go/SQL decoder parity case=%d theater=%s err=%v", i, theater, err)
			}
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		if _, err := pool.Exec(ctx, `UPDATE `+table+` SET booking_url=$1`, raw); err != nil {
			t.Fatal("original relay not persistable", err)
		}
		for _, change := range []string{`booking_url=booking_url||'&tags=x'`, `booking_url=replace(booking_url,'P1394','G06DB')`, `end_time=start_time-interval '1 minute'`, `first_part_duration_minutes=1`, `movie_provider_id='ticketingcine-A12B3'`} {
			rejectCinevilleSQL(t, ctx, pool, `UPDATE `+table+` SET `+change)
		}
		var saved string
		if err := pool.QueryRow(ctx, `SELECT booking_url FROM `+table).Scan(&saved); err != nil || saved != raw {
			t.Fatal("original bytes changed", err)
		}
	}
}
