package enrichment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
)

func TestAdminMovieShowtimeCountIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate test schema nonce failed")
	}
	schema := pgx.Identifier{"movieflow_admin_showtimes_test_" + hex.EncodeToString(nonce)}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("drop integration schema failed")
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run integration migrations failed")
	}

	// Keep past and staged generations alongside the published generation. Source
	// IDs deliberately collide across providers, and one public movie has two sources.
	if _, err := pool.Exec(ctx, `
INSERT INTO schedule_snapshot (version,schema_version,provider,scope,generated_at,timezone,window_from,window_through)
VALUES (2,1,'combined','all_cinemas',CURRENT_TIMESTAMP,'Europe/Paris',CURRENT_DATE,CURRENT_DATE+1);
INSERT INTO public_movies (id,identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes)
OVERRIDING SYSTEM VALUE VALUES
    (1,'ugc','1','Zero',90), (2,'ugc','10','Combined',90),
    (3,'kinepolis','10','Other provider',90), (4,'ugc','12','Inactive only',90),
    (5,'ugc','13','Tie',90), (6,'ugc','14','Expired only',90);
INSERT INTO public_movies (id,redirect_to_id,identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes)
OVERRIDING SYSTEM VALUE VALUES (7,2,'ugc','15','Redirect',90);
INSERT INTO public_movie_sources (source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) VALUES
    ('ugc','10',2,'ugc-film-10','Combined',90), ('ugc','11',2,'ugc-film-11','Combined peer',90),
    ('kinepolis','10',3,'kinepolis-film-10','Other provider',90), ('ugc','12',4,'ugc-film-12','Inactive only',90),
    ('ugc','13',5,'ugc-film-13','Tie',90), ('ugc','14',6,'ugc-film-14','Expired only',90),
    ('ugc','15',7,'ugc-film-15','Redirect',90);
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes)
SELECT generation,source_provider,source_movie_id,source_slug,title,runtime_minutes
FROM public_movie_sources CROSS JOIN generate_series(1,3) generation;
INSERT INTO movies (generation_id,provider,provider_id,slug,title,runtime_minutes)
VALUES (2,'ugc','99','ugc-film-99','Unmapped',90);
INSERT INTO theaters (generation_id,id,provider_id,slug,name,address,city,postal_code)
SELECT generation,'ugc-'||theater,theater::text,'ugc-'||theater,'Test theater','Test','Lille','59000'
FROM generate_series(1,3) generation CROSS JOIN generate_series(1,2) theater;
INSERT INTO theater_dates (generation_id,theater_id,service_date)
SELECT generation_id,id,CURRENT_DATE+day_offset
FROM theaters CROSS JOIN generate_series(-1,41) day_offset;
INSERT INTO showtimes (generation_id,id,provider_showing_id,service_date,theater_id,movie_provider_id,
    start_time,end_time,language,provider_version,format,room,booking_url,provider)
SELECT generation,provider||'-showing-'||showing_id,showing_id,(CURRENT_TIMESTAMP+starts_in)::date,
    theater,movie_id,CURRENT_TIMESTAMP+starts_in,CURRENT_TIMESTAMP+starts_in+interval '90 minutes',
    language,language,format,'1','https://example.test/booking',provider
FROM (VALUES
    (2,'101','ugc','10',interval '1 hour','VF','2D','ugc-1'),
    (2,'102','ugc','10',interval '40 days','VO','3D','ugc-2'),
    (2,'103','ugc','10',interval '-1 minute','VF','2D','ugc-1'),
    (2,'104','ugc','10',interval '-1 day','VF','2D','ugc-1'),
    (2,'105','ugc','11',interval '1 hour','VOSTFR','IMAX','ugc-1'),
    (2,'201','kinepolis','10',interval '1 hour','VF','2D','ugc-2'),
    (2,'501','ugc','13',interval '1 hour','VF','2D','ugc-1'),
    (2,'601','ugc','14',interval '-1 minute','VF','2D','ugc-1'),
    (2,'701','ugc','15',interval '1 hour','VF','2D','ugc-1'),
    (2,'901','ugc','99',interval '1 hour','VF','2D','ugc-1'),
    (1,'101','ugc','10',interval '1 hour','VF','2D','ugc-1'),
    (3,'101','ugc','10',interval '1 hour','VF','2D','ugc-1'),
    (1,'401','ugc','12',interval '1 hour','VF','2D','ugc-1'),
    (3,'401','ugc','12',interval '1 hour','VF','2D','ugc-1')
) fixture(generation,showing_id,provider,movie_id,starts_in,language,format,theater);
`); err != nil {
		t.Fatalf("seed admin showtime fixtures failed: %v", err)
	}
	store := NewPostgresStore(pool)
	query := AdminMovieQuery{Limit: 100, OverrideStatus: "all", Sort: "showtime_count", Direction: "asc"}
	list, err := store.AdminMovies(ctx, query)
	if err != nil || list.Total != 6 || len(list.Items) != 6 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	counts := make(map[string]int)
	for _, item := range list.Items {
		counts[item.ID] = item.ShowtimeCount
	}
	if want := map[string]int{"1": 0, "2": 3, "3": 1, "4": 0, "5": 1, "6": 0}; !reflect.DeepEqual(counts, want) {
		t.Fatalf("counts=%v want=%v", counts, want)
	}

	t.Run("global sort before pagination with stable ties", func(t *testing.T) {
		for direction, want := range map[string][]string{
			"asc": {"1", "4", "6", "3", "5", "2"}, "desc": {"2", "3", "5", "1", "4", "6"},
		} {
			paged := query
			paged.Direction, paged.Limit = direction, 2
			var got []string
			for paged.Offset = 0; paged.Offset < len(want); paged.Offset += paged.Limit {
				page, err := store.AdminMovies(ctx, paged)
				if err != nil || page.Total != 6 || page.Limit != 2 || page.Offset != paged.Offset || len(page.Items) != 2 {
					t.Fatalf("query=%+v page=%+v err=%v", paged, page, err)
				}
				for _, item := range page.Items {
					got = append(got, item.ID)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("direction=%s ids=%v want=%v", direction, got, want)
			}
		}
	})

	t.Run("strict cutoff independent of timezone", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin cutoff test failed")
		}
		defer rollback(tx)
		if _, err := tx.Exec(ctx, "SET LOCAL TIME ZONE 'Pacific/Honolulu'"); err != nil {
			t.Fatal("set cutoff test timezone failed")
		}
		// CURRENT_TIMESTAMP is fixed throughout this transaction, so equality and
		// adjacent microseconds exercise the production SQL without a wall-clock race.
		for _, test := range []struct{ microseconds, want int }{{-1, 0}, {0, 0}, {1, 1}} {
			if _, err := tx.Exec(ctx, `UPDATE showtimes
SET start_time=CURRENT_TIMESTAMP+($1::bigint * interval '1 microsecond'), end_time=CURRENT_TIMESTAMP+interval '1 hour'
WHERE generation_id=2 AND id='ugc-showing-601'`, test.microseconds); err != nil {
				t.Fatal("set cutoff fixture failed")
			}
			var got int
			if err := tx.QueryRow(ctx, adminMovieEffectiveCTE+"SELECT showtime_count FROM effective WHERE id=6").Scan(&got); err != nil || got != test.want {
				t.Fatalf("microseconds=%d count=%d want=%d err=%v", test.microseconds, got, test.want, err)
			}
		}
	})

	t.Run("filtered and patch responses retain count", func(t *testing.T) {
		filtered := query
		filtered.Search = "Combined"
		list, err := store.AdminMovies(ctx, filtered)
		if err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].ShowtimeCount != 3 {
			t.Fatalf("filtered list=%+v err=%v", list, err)
		}
		expected, err := time.Parse(time.RFC3339Nano, list.Items[0].UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		patch := AdminMoviePatch{ExpectedUpdatedAt: expected, Overrides: AdminMovieOverrides{
			Title: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer("Combined manual")},
		}}
		for range 2 {
			item, err := store.UpdateAdminMovie(ctx, 2, patch)
			if err != nil || item.ShowtimeCount != 3 || item.Values.Title != "Combined manual" {
				t.Fatalf("updated item=%+v err=%v", item, err)
			}
			patch.ExpectedUpdatedAt, err = time.Parse(time.RFC3339Nano, item.UpdatedAt)
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("no published generation retains movies with zero counts", func(t *testing.T) {
		if _, err := pool.Exec(ctx, "DELETE FROM schedule_snapshot"); err != nil {
			t.Fatal("remove test snapshot failed")
		}
		list, err := store.AdminMovies(ctx, query)
		if err != nil || list.Total != 6 || len(list.Items) != 6 {
			t.Fatalf("list=%+v err=%v", list, err)
		}
		for _, item := range list.Items {
			if item.ShowtimeCount != 0 {
				t.Fatalf("movie=%s count=%d without published generation", item.ID, item.ShowtimeCount)
			}
		}
	})
}
