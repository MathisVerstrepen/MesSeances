package database

import (
	"context"
	"testing"
	"time"
)

func TestUpcomingReviewsUpgradeFrom032Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "upcoming_review_upgrade_")
	migrations := installMigrationPrefix(t, ctx, pool, 32, "032_upcoming_movies.sql")
	for _, sql := range []string{
		`INSERT INTO public_movies(identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) VALUES(42,42,'Existing',90)`,
		`INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES('tmdb-film-42',1,'tmdb')`,
		`INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title) VALUES(1,true,'Manual')`,
		`INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,french_release_date,active,verified_at) VALUES(42,1,'2026-10-07',true,'2026-09-13T12:00:00Z')`,
		`INSERT INTO tmdb_upcoming_state VALUES(true,'2026-09-13T12:00:00Z','2026-09-14','2027-09-13')`,
		`INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES(1,'ugc-1','ugc','1','ugc-1','Cinema','Address','Paris','75001')`,
		`INSERT INTO theater_dates(generation_id,theater_id,service_date) VALUES(1,'ugc-1','2026-09-14')`,
		`INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Existing',90)`,
		`INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url) VALUES(1,'ugc-showing-100','ugc','100','2026-09-14','ugc-1','10','2026-09-14T18:00:00Z','2026-09-14T19:30:00Z','VF','VF','2D','1','https://example.com/booking')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	const snapshot = `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(m)-'original_language') FROM public_movies m),(SELECT jsonb_agg(to_jsonb(a)) FROM movie_slug_aliases a),(SELECT jsonb_agg(to_jsonb(o)) FROM public_movie_metadata_overrides o),(SELECT jsonb_agg(to_jsonb(s)) FROM tmdb_upcoming_state s),(SELECT jsonb_agg(to_jsonb(s)) FROM showtimes s))::text`
	var before, after string
	if err := pool.QueryRow(ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertCompleteMigrationHistory(t, ctx, pool, migrations)
	if err := pool.QueryRow(ctx, snapshot).Scan(&after); err != nil || before != after {
		t.Fatalf("prior data changed: %v", err)
	}
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT active AND french_release_date='2026-10-07' AND verified_at='2026-09-13T12:00:00Z' AND assessed_at IS NULL AND french_releases='[]' AND reason_codes='{}' AND decision='unreviewed' AND review_revision=1 FROM tmdb_upcoming_movies WHERE tmdb_id=42`).Scan(&valid); err != nil || !valid {
		t.Fatalf("pending defaults: %v", err)
	}
	for _, set := range []string{`french_releases='{}'`, `reason_codes=ARRAY['unknown']`, `reason_codes=ARRAY[NULL]`, `reason_codes=ARRAY['limited_only']`, `decision='hidden'`, `review_revision=0`, `review_revision=9007199254740992`, `french_releases='[{"type":3}]'`} {
		if _, err := pool.Exec(ctx, "UPDATE tmdb_upcoming_movies SET "+set); err == nil {
			t.Fatalf("accepted invalid %s", set)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE tmdb_upcoming_movies SET decision='excluded',review_revision=2"); err != nil {
		t.Fatal(err)
	}
}
