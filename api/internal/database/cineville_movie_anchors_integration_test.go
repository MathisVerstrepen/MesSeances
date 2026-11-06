package database

import (
	"testing"
	"time"
)

func TestCinevilleMovieAnchorsMigrationIntegration(t *testing.T) {
	ctx := t.Context()
	pool, _ := newMigrationTestPool(t, ctx, "cineville_movie_anchors_")
	installMigrationPrefix(t, ctx, pool, 76, "076_source_movie_pages.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes,source_page_url) VALUES(1,'cgr','123','cgr-film-123','Existing',90,'https://www.cgrcinemas.fr/films-a-l-affiche/123-source-title/'),(1,'cineville','168608','cineville-film-168608','Film',90,NULL),(1,'cineville','-40370611261','cineville-film--40370611261','Event',0,NULL)`)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"168608", "-40370611261"} {
		valid := "https://www.cineville.fr/programmes/bruz#" + id
		mustCinevilleSQL(t, ctx, pool, `UPDATE movies SET source_page_url=$1 WHERE provider='cineville' AND provider_id=$2`, valid, id)
		for _, invalid := range []string{valid + "?token=x", valid + "#extra", "https://www.cineville.fr/programmes/bruz#1", "https://www.cineville.fr.evil.example/programmes/bruz#" + id, "https://user@www.cineville.fr/programmes/bruz#" + id} {
			if _, err := pool.Exec(ctx, `UPDATE movies SET source_page_url=$1 WHERE provider='cineville' AND provider_id=$2`, invalid, id); err == nil {
				t.Fatalf("unsafe or mismatched anchor accepted: %q", invalid)
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE movies SET source_page_url=$1 WHERE provider='cgr'`, valid); err == nil {
			t.Fatal("Cineville fragment exception applied to another provider")
		}
	}
	var before, after time.Time
	if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=77`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT applied_at FROM movieflow_schema_migrations WHERE version=77`).Scan(&after); err != nil || !before.Equal(after) {
		t.Fatal("migration reapplied", err)
	}
	var preserved, validated bool
	if err := pool.QueryRow(ctx, `SELECT source_page_url='https://www.cgrcinemas.fr/films-a-l-affiche/123-source-title/' FROM movies WHERE provider='cgr'`).Scan(&preserved); err != nil || !preserved {
		t.Fatal("existing source page changed", err)
	}
	if err := pool.QueryRow(ctx, `SELECT convalidated FROM pg_constraint WHERE conrelid='movies'::regclass AND conname='movies_source_page_url_check'`).Scan(&validated); err != nil || !validated {
		t.Fatal("source URL constraint not validated", err)
	}
}
