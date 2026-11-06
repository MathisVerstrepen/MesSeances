package database

import (
	"testing"
	"time"
)

func TestSourceMoviePagesMigrationIntegration(t *testing.T) {
	ctx := t.Context()
	pool, _ := newMigrationTestPool(t, ctx, "source_movie_pages_")
	installMigrationPrefix(t, ctx, pool, 75, "075_cineode_poster_urls.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'cgr','123','cgr-film-123','Existing',90)`)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var empty bool
	if err := pool.QueryRow(ctx, `SELECT source_page_url IS NULL AND title='Existing' AND runtime_minutes=90 FROM movies WHERE provider='cgr'`).Scan(&empty); err != nil || !empty {
		t.Fatal("existing movie changed or source link fabricated", err)
	}
	const valid = "https://www.cgrcinemas.fr/films-a-l-affiche/123-source-title/"
	mustCinevilleSQL(t, ctx, pool, `UPDATE movies SET source_page_url=$1 WHERE provider='cgr'`, valid)
	for _, raw := range []string{"", "http://www.cgrcinemas.fr/film/123", "https://user@www.cgrcinemas.fr/film/123", valid + "?token=secret", valid + "#fragment", valid + "\n", valid + "%20", valid + `\escape`} {
		if _, err := pool.Exec(ctx, `UPDATE movies SET source_page_url=$1 WHERE provider='cgr'`, raw); err == nil {
			t.Fatalf("unsafe source URL accepted: %q", raw)
		}
	}
	var before, after time.Time
	if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=76`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=76`).Scan(&after); err != nil || !before.Equal(after) {
		t.Fatal("migration reapplied", err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT source_page_url FROM movies WHERE provider='cgr'`).Scan(&got); err != nil || got != valid {
		t.Fatal("source URL changed after rejected writes or reapply", err)
	}
	var checked bool
	if err := pool.QueryRow(ctx, `SELECT convalidated FROM pg_constraint WHERE conrelid='movies'::regclass AND conname='movies_source_page_url_check'`).Scan(&checked); err != nil || !checked {
		t.Fatal("source URL constraint not validated", err)
	}
}
