package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOriginalLanguageMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade040"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := historyMigrationPool(t, ctx)
			var before string
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 40, "040_infinity_vision_format.sql")
				seedOriginalLanguageMigration(t, ctx, pool)
				before = originalLanguageMigrationState(t, ctx, pool)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if upgrade {
				if got := originalLanguageMigrationState(t, ctx, pool); got != before {
					t.Fatal("migration changed existing data, identities, or timestamps")
				}
			} else {
				seedOriginalLanguageMigration(t, ctx, pool)
			}
			for _, table := range []string{"movie_metadata_cache", "public_movies"} {
				var unknown bool
				if err := pool.QueryRow(ctx, `SELECT bool_and(original_language IS NULL) FROM `+table).Scan(&unknown); err != nil || !unknown {
					t.Fatalf("%s did not start unknown: %v", table, err)
				}
				var nullable, dataType string
				var length int
				if err := pool.QueryRow(ctx, `SELECT is_nullable, data_type, character_maximum_length FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='original_language'`, table).Scan(&nullable, &dataType, &length); err != nil || nullable != "YES" || dataType != "character varying" || length != 2 {
					t.Fatalf("%s incorrect column type: %s %s %d: %v", table, nullable, dataType, length, err)
				}
				for _, language := range []any{"fr", "en", nil} {
					if _, err := pool.Exec(ctx, `UPDATE `+table+` SET original_language=$1`, language); err != nil {
						t.Fatalf("%s rejected %v: %v", table, language, err)
					}
				}
				for _, language := range []string{"", "FR", "Fr", "f", "f1", "é", "fra", " fr"} {
					_, err := pool.Exec(ctx, `UPDATE `+table+` SET original_language=$1`, language)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || (pgErr.Code != "23514" && pgErr.Code != "22001") {
						t.Fatalf("%s accepted malformed %q or unexpected error: %v", table, language, err)
					}
				}
			}
			_, err := pool.Exec(ctx, `UPDATE public_movies SET confirmed_tmdb_id=NULL, original_language='fr'`)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.ConstraintName != "public_movies_original_language_tmdb_check" {
				t.Fatalf("missing association check: %v", err)
			}
			for _, table := range []string{"showtimes", "screening_history_showtimes"} {
				_, err := pool.Exec(ctx, `UPDATE `+table+` SET language='ORIGINAL' WHERE provider='ugc'`)
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != table+"_language_original_check" {
					t.Fatalf("%s accepted ORIGINAL or unexpected error: %v", table, err)
				}
				for _, language := range []string{"VF", "VF_SME", "VFSTF", "VO", "VOSTFR", "OTHER"} {
					if _, err := pool.Exec(ctx, `UPDATE `+table+` SET language=$1 WHERE provider='ugc'`, language); err != nil {
						t.Fatalf("%s rejected existing token %q: %v", table, language, err)
					}
				}
				var silent int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE language='' AND provider_version IN ('Muet','VERSION_MUET')`).Scan(&silent); err != nil || silent != 2 {
					t.Fatalf("%s lost silent exceptions: count=%d err=%v", table, silent, err)
				}
			}
			var applied time.Time
			if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=41`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var once bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(applied_at=$1) FROM movieflow_schema_migrations WHERE version=41`, applied).Scan(&once); err != nil || !once {
				t.Fatal("migration 041 reapplied", err)
			}
		})
	}
}

func seedOriginalLanguageMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedInfinityMigration(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
UPDATE public_movies SET confirmed_tmdb_id=42;
INSERT INTO movie_metadata_cache(provider,provider_movie_id,locale,provider_title,localized_title,runtime_minutes,fetched_at,refresh_after)
VALUES('tmdb',42,'fr-FR','Fixture','Fixture',90,'2026-09-01','2026-10-01');
INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES
(1,'mk2','HO1','mk2-film-HO1','Silent',0),(1,'cinewest','cineoffice-1','cinewest-film-cineoffice-1','Silent',0);
INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES
(1,'mk2-0004','mk2','0004','mk2-0004','MK2','1 Rue','Paris','75013'),
(1,'cinewest-cineoffice-royanlelido','cinewest','cineoffice-royanlelido','cinewest-cineoffice-royanlelido','Cinema','1 Rue','Royan','17200');
INSERT INTO theater_dates(generation_id,theater_id,service_date) SELECT 1,id,'2026-09-15' FROM theaters WHERE provider<>'ugc';
INSERT INTO showtimes(generation_id,id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,language,provider_version,format,room,booking_url) VALUES
(1,'mk2-showing-0004-1','mk2','0004-1','mk2-0004','HO1','2026-09-15','2026-09-15 18:00Z','2026-09-15 18:00Z','','Muet','2D','','https://example.test'),
(1,'cinewest-showing-cineoffice-'||repeat('a',64),'cinewest','cineoffice-'||repeat('a',64),'cinewest-cineoffice-royanlelido','cineoffice-1','2026-09-15','2026-09-15 18:00Z','2026-09-15 19:30Z','','VERSION_MUET','2D','1','https://example.test');
INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes)
SELECT provider,provider_id,(SELECT id FROM public_movies),slug,title,runtime_minutes FROM movies WHERE provider<>'ugc';
INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at)
SELECT id,provider,provider_id,slug,name,address,city,postal_code,lower(city),city,'{}',now() FROM theaters WHERE provider<>'ugc';
INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,first_seen_at,last_seen_at,source_generated_at,last_generation)
SELECT id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,now(),now(),now(),1 FROM showtimes WHERE provider<>'ugc';`); err != nil {
		t.Fatal(err)
	}
}

func originalLanguageMigrationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
    (SELECT jsonb_agg(to_jsonb(m)-'original_language' ORDER BY id) FROM public_movies m),
    (SELECT jsonb_agg(to_jsonb(m)-'original_language' ORDER BY provider_movie_id) FROM movie_metadata_cache m),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM showtimes s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM screening_history_showtimes s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY source_provider,source_movie_id) FROM public_movie_sources s)
)::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}
