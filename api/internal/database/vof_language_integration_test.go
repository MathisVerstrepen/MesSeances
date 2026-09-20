package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVOFLanguageMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade041"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := historyMigrationPool(t, ctx)
			var before string
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 41, "041_movie_original_language.sql")
				seedVOFLanguageMigration(t, ctx, pool)
				before = vofLanguageMigrationState(t, ctx, pool)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if upgrade {
				if got := vofLanguageMigrationState(t, ctx, pool); got != before {
					t.Fatal("migration changed existing metadata, rows, identities, timestamps, or unrelated constraints")
				}
			} else {
				seedVOFLanguageMigration(t, ctx, pool)
			}
			for _, table := range []string{"showtimes", "screening_history_showtimes"} {
				var valid bool
				if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(convalidated) FROM pg_constraint WHERE conrelid=$1::regclass AND contype='c' AND conname=$2`, table, table+"_language_vof_check").Scan(&valid); err != nil || !valid {
					t.Fatalf("%s VOF CHECK missing, duplicated or unvalidated: %v", table, err)
				}
				for _, language := range []string{"VOF", "ORIGINAL", "ALL"} {
					_, err := pool.Exec(ctx, `UPDATE `+table+` SET language=$1 WHERE provider='ugc'`, language)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
						t.Fatalf("%s accepted query-only %s or unexpected error: %v", table, language, err)
					}
					if language == "VOF" && pgErr.ConstraintName != table+"_language_vof_check" || language == "ORIGINAL" && pgErr.ConstraintName != table+"_language_original_check" {
						t.Fatalf("%s wrong check for %s: %s", table, language, pgErr.ConstraintName)
					}
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
			if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=42`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			before = vofLanguageMigrationState(t, ctx, pool)
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			var once bool
			if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(applied_at=$1) FROM movieflow_schema_migrations WHERE version=42`, applied).Scan(&once); err != nil || !once {
				t.Fatal("migration 042 reapplied", err)
			}
			if got := vofLanguageMigrationState(t, ctx, pool); got != before {
				t.Fatal("migration rerun changed existing state")
			}
		})
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		t.Run("reject_existing_"+table, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := historyMigrationPool(t, ctx)
			migrations := installMigrationPrefix(t, ctx, pool, 41, "041_movie_original_language.sql")
			seedVOFLanguageMigration(t, ctx, pool)
			if _, err := pool.Exec(ctx, `UPDATE `+table+` SET language='VOF' WHERE provider='ugc'`); err != nil {
				t.Fatal("prefix 041 fixture rejected VOF", err)
			}
			before := vofLanguageMigrationState(t, ctx, pool)
			if err := RunMigrations(ctx, pool); err == nil || err.Error() != "database migration 042 failed" {
				t.Fatalf("expected transactional 042 failure: %v", err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, requireMigrationPrefix(t, migrations, 41, "041_movie_original_language.sql"))
			var checks int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid IN ('showtimes'::regclass,'screening_history_showtimes'::regclass) AND conname IN ('showtimes_language_vof_check','screening_history_showtimes_language_vof_check')`).Scan(&checks); err != nil || checks != 0 {
				t.Fatalf("partial 042 checks survived rollback: count=%d err=%v", checks, err)
			}
			if got := vofLanguageMigrationState(t, ctx, pool); got != before {
				t.Fatal("failed migration changed existing state")
			}
		})
	}
}

func seedVOFLanguageMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedOriginalLanguageMigration(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE public_movies SET original_language='fr'; UPDATE movie_metadata_cache SET original_language='en'`); err != nil {
		t.Fatal(err)
	}
}

func vofLanguageMigrationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	// Preserve original_language too: unlike the 040-to-041 snapshot, no metadata changes are expected.
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
    (SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM public_movies m),
    (SELECT jsonb_agg(to_jsonb(m) ORDER BY provider,provider_movie_id,locale) FROM movie_metadata_cache m),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY generation_id,id) FROM showtimes s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY provider,provider_showing_id,theater_id,service_date) FROM screening_history_showtimes s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY source_provider,source_movie_id) FROM public_movie_sources s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY generation_id,provider,provider_id) FROM movies s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY generation_id,id) FROM theaters s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY generation_id,theater_id,service_date) FROM theater_dates s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM screening_history_theaters s),
    (SELECT jsonb_agg(to_jsonb(s) ORDER BY version) FROM movieflow_schema_migrations s WHERE version < 42),
    (SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid),convalidated) ORDER BY conrelid,conname)
     FROM pg_constraint WHERE connamespace=current_schema()::regnamespace
     AND conname NOT IN ('showtimes_language_vof_check','screening_history_showtimes_language_vof_check'))
)::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}
