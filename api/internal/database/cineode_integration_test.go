package database

import (
	"context"
	"testing"
	"time"
)

func TestCineodeMigrationIdentityPosterAndUpgradeIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "cineode_migration_")
	installMigrationPrefix(t, ctx, pool, 66, "066_majestic_provider.sql")
	mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Retained',90)`)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE provider='ugc' AND title='Retained' AND runtime_minutes=90`).Scan(&n); err != nil || n != 1 {
		t.Fatal("legacy changed", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='cineode'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("schedule seeded", err)
	}
	for _, tc := range []struct {
		kind, value string
		want        bool
	}{
		{"theater", "4640", true}, {"theater", "991", true}, {"theater", "04640", false}, {"theater", "9999", false},
		{"movie", "atelier-local", true}, {"movie", "123", true}, {"movie", "01", false}, {"movie", "A-local", false},
		{"showing", "4640-atelier-local-1792175400", true}, {"showing", "4640-atelier-local-01792175400", false}, {"showing", "4640-atelier-local-9223372036854775808", false},
	} {
		var got bool
		if err := pool.QueryRow(ctx, `SELECT cineode_identity_valid($1,$2)`, tc.kind, tc.value).Scan(&got); err != nil || got != tc.want {
			t.Fatal("identity policy", tc, err)
		}
	}
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"", true}, {"https://fr.web.img6.acsta.net/img/aa/poster.jpg", true},
		{"https://static.cotecine.fr/tb/Affiches/180x240/LE+BON_4332.JPG", true},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a%2Bb.JPG", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a.JPG?x=1", false},
		{"https://static.cotecine.fr/tb/Affiches/180x240/a/b.JPG", false},
		{"https://static.cotecine.fr:443/tb/Affiches/180x240/a.JPG", false},
		{"https://fr.web.img6.acsta.net/img/../poster.jpg", false},
	} {
		var got bool
		if err := pool.QueryRow(ctx, `SELECT cineode_poster_valid($1)`, tc.url).Scan(&got); err != nil || got != tc.want {
			t.Fatal("poster policy", tc, err)
		}
	}
}
