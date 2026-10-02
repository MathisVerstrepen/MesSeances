package database

import (
	"strings"
	"testing"
	"time"
)

func TestMetacriticMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "upgrade056"}[upgrade], func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "movieflow_metacritic_migration_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 56, "056_cinema_activity.sql")
				if _, err := pool.Exec(ctx, `INSERT INTO movie_metadata_cache(provider,provider_movie_id,locale,provider_title,localized_title,runtime_minutes,fetched_at,refresh_after) VALUES('tmdb',42,'fr-FR','Film','Film',90,now(),now()); INSERT INTO public_movies(identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) VALUES(42,42,'Film',90)`); err != nil {
					t.Fatal(err)
				}
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if upgrade {
				var absent bool
				if err := pool.QueryRow(ctx, `SELECT (SELECT bool_and(metacritic_id IS NULL) FROM movie_metadata_cache) AND (SELECT bool_and(metacritic_id IS NULL) FROM public_movies)`).Scan(&absent); err != nil || !absent {
					t.Fatalf("legacy data changed: %v", err)
				}
			} else if _, err := pool.Exec(ctx, `INSERT INTO movie_metadata_cache(provider,provider_movie_id,locale,provider_title,localized_title,runtime_minutes,fetched_at,refresh_after) VALUES('tmdb',42,'fr-FR','Film','Film',90,now(),now()); INSERT INTO public_movies(identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) VALUES(42,42,'Film',90)`); err != nil {
				t.Fatal(err)
			}
			var applied time.Time
			if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=57`).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			var repeated time.Time
			if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=57`).Scan(&repeated); err != nil || !repeated.Equal(applied) {
				t.Fatal("migration reapplied", err)
			}
			for _, id := range []string{"movie/a", "movie/mission-impossible_(2026)+!", "movie/" + strings.Repeat("a", 249)} {
				for _, table := range []string{"movie_metadata_cache", "public_movies"} {
					if _, err := pool.Exec(ctx, "UPDATE "+table+" SET metacritic_id=$1", id); err != nil {
						t.Fatalf("valid %s %q: %v", table, id, err)
					}
				}
			}
			for _, id := range []string{"", "movie/", "movie/A", "game/a", "https://www.metacritic.com/movie/a", "movie/a/b", "movie/..", "movie/a%2f", "movie/a?b", "movie/a#b", "movie/é", "movie/a\n", "movie/a\r\n", "movie/a\\b", " movie/a", "movie/" + strings.Repeat("a", 250)} {
				for _, table := range []string{"movie_metadata_cache", "public_movies"} {
					if _, err := pool.Exec(ctx, "UPDATE "+table+" SET metacritic_id=$1", id); err == nil {
						t.Fatalf("invalid %s %q accepted", table, id)
					}
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE public_movies SET confirmed_tmdb_id=NULL,metacritic_id='movie/a'`); err == nil {
				t.Fatal("unassociated ID accepted")
			}
			if _, err := pool.Exec(ctx, `UPDATE public_movies SET confirmed_tmdb_id=NULL,metacritic_id=NULL`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
