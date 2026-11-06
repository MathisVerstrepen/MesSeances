package database

import (
	"context"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestCineodePosterMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "cineode_poster_")
	installMigrationPrefix(t, ctx, pool, 74, "074_veo_saint_affrique_relay.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes,poster_url) VALUES(1,'cineode','atelier-local','cineode-film-atelier-local','Retained',0,'https://static.cotecine.fr/tb/Affiches/180x240/LE+BON.JPG');
INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('cineode','atelier-local','Retained',0);
INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes) SELECT 'cineode','atelier-local',id,'cineode-film-atelier-local','Retained',0 FROM public_movies WHERE identity_anchor_provider='cineode';`)
	const acsta = "https://fr.web.img6.acsta.net/img/86/7c/867cc81bac4d9a831c12cac0b7e0ff85.JPG"
	const cotecine = "https://static.cotecine.fr/tb/Affiches/180x240/COMPETITION%20DE%20COURTS%20METRAGES.JPG"
	for _, poster := range []string{acsta, cotecine} {
		var accepted bool
		if err := pool.QueryRow(ctx, `SELECT cineode_poster_valid($1)`, poster).Scan(&accepted); err != nil || accepted {
			t.Fatal("074 unexpectedly accepts new poster", err)
		}
	}
	for range 2 {
		if err := RunMigrations(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT title='Retained' AND runtime_minutes=0 AND poster_url='https://static.cotecine.fr/tb/Affiches/180x240/LE+BON.JPG' FROM movies WHERE provider='cineode'`).Scan(&retained); err != nil || !retained {
		t.Fatal("migration changed retained movie", err)
	}
	for _, tc := range []struct {
		poster string
		want   bool
	}{
		{acsta, true}, {cotecine, true},
		{"https://static.cotecine.fr/tb/Affiches/180x240/LE+BON.JPG", true},
		{"https://fr.web.img6.acsta.net/img/aa/poster.jpg", true},
		{"https://fr.web.img6.acsta.net/img/../poster.JPG", false},
		{"https://fr.web.img6.acsta.net/img//poster.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a%2Fb.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a%2520b.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a%5Cb.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a%00b.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a%0Ab.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a b.JPG", false},
		{cotecine + "?x=1", false}, {cotecine + "#x", false},
		{"https://static.cotecine.fr.evil.test/tb/Affiches/180x240/a.JPG", false},
	} {
		var got bool
		if err := pool.QueryRow(ctx, `SELECT cineode_poster_valid($1)`, tc.poster).Scan(&got); err != nil || got != tc.want || got != schedule.ValidCineodePosterURL(tc.poster) {
			t.Fatal("Go/SQL poster policy mismatch", tc, err)
		}
		for _, statement := range []string{`UPDATE movies SET poster_url=$1 WHERE provider='cineode'`, `UPDATE public_movie_sources SET poster_url=$1 WHERE source_provider='cineode'`} {
			_, err := pool.Exec(ctx, statement, tc.poster)
			if (err == nil) != tc.want {
				t.Fatal("poster persistence policy mismatch", tc, err)
			}
		}
	}
}
