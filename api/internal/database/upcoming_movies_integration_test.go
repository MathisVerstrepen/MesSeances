package database

import (
	"context"
	"testing"
	"time"
)

func TestUpcomingMoviesUpgradeFrom031Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "upcoming_upgrade_")
	migrations := installMigrationPrefix(t, ctx, pool, 31, "031_megarama_provider.sql")
	seed := []string{
		`INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,confirmed_tmdb_id) VALUES('ugc','10','Existing',90,42)`,
		`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES('tmdb-film-42',1,'tmdb')`,
		`INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title) VALUES(1,true,'Manual')`,
		`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'ugc-1','ugc','1','ugc-1','Cinema','Address','Paris','75001')`,
		`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'ugc-1','2026-09-14')`,
		`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90)`,
		`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'ugc-showing-100','ugc','100','2026-09-14','ugc-1','10','2026-09-14T18:00:00Z','2026-09-14T19:30:00Z','VF','VF','2D','1','https://example.com/booking')`,
	}
	for _, statement := range seed {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	var before string
	const existing = `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s)) FROM showtimes s),(SELECT jsonb_agg(to_jsonb(a)) FROM movie_slug_aliases a),(SELECT jsonb_agg(to_jsonb(o)) FROM public_movie_metadata_overrides o),(SELECT jsonb_agg(to_jsonb(m)) FROM public_movies m))::text`
	if err := pool.QueryRow(ctx, existing).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertCompleteMigrationHistory(t, ctx, pool, migrations)
	var after string
	// The only new public-row field is a null anchor; all preexisting bytes retain their values.
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s)) FROM showtimes s),(SELECT jsonb_agg(to_jsonb(a)) FROM movie_slug_aliases a),(SELECT jsonb_agg(to_jsonb(o)) FROM public_movie_metadata_overrides o),(SELECT jsonb_agg(to_jsonb(m)-'identity_anchor_tmdb_id') FROM public_movies m))::text`).Scan(&after); err != nil || after != before {
		t.Fatalf("existing data changed: err=%v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tmdb_upcoming_state").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration fabricated a successful publication")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public_movies(identity_anchor_tmdb_id,title,runtime_minutes,confirmed_tmdb_id) VALUES(99,'Upcoming',0,99)`); err != nil {
		t.Fatal(err)
	}
	for _, values := range []string{
		`NULL,NULL,NULL`, `NULL,'10',NULL`, `'ugc',NULL,NULL`, `'ugc','10',99`, `NULL,NULL,0`, `NULL,NULL,-1`, `NULL,NULL,99`, `'tmdb','100',NULL`, `'megarama','invalid',NULL`,
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,identity_anchor_tmdb_id,title,runtime_minutes) VALUES(`+values+`,'Invalid',0)`); err == nil {
			t.Fatalf("invalid anchor accepted: %s", values)
		}
	}
	for _, target := range []string{"ugc", "kinepolis", "pathe", "cgr", "megarama", "tmdb_metadata_refresh", "tmdb_upcoming_movies"} {
		if _, err := pool.Exec(ctx, `INSERT INTO sync_schedules(target,enabled,schedule_kind,local_time) VALUES($1,false,'daily','10:00')`, target); err != nil {
			t.Fatalf("valid target %s rejected: %v", target, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sync_runs(target,state,started_at,window_from,window_through,providers) VALUES('tmdb_upcoming_movies','running',now(),'2026-09-14','2026-09-14','{}')`); err == nil {
		t.Fatal("upcoming accepted as provider run")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,active,verified_at) VALUES(99,2,true,now())`); err == nil {
		t.Fatal("active release without date accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,active,verified_at) VALUES(99,2,false,now())`); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("migration not idempotent")
	}
}
