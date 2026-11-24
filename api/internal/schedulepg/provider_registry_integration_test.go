package schedulepg

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
	"messeances/api/internal/schedule"
)

func registryFixtureDatasets() []Dataset {
	return []Dataset{
		testDataset(), kinepolisTestDataset(), patheTestDataset(), cgrTestDataset(), megaramaTestDataset(),
		cinevilleTestDataset(), mk2TestDataset(), cinewestTestDataset(), grandecranTestDataset(), noecinemasTestDataset(),
		veoTestDataset(), majesticTestDataset(), cineodeTestDataset(), cinemovidaTestDataset(), ocineTestDataset(),
	}
}

// Snapshots published under the provider-specific SQL checks (082) must load
// unchanged after the registry migration replaces them.
func TestProviderRegistryMigrationPreservesEveryProviderIntegration(t *testing.T) {
	ctx := t.Context()
	pool := newHistoryPoolWith(t, func(ctx context.Context, pool *pgxpool.Pool) error {
		return database.RunMigrationsThrough(ctx, pool, 82)
	})
	store := NewStore(pool)
	datasets := registryFixtureDatasets()
	covered := map[schedule.Provider]bool{}
	for _, d := range datasets {
		covered[d.Provider] = true
	}
	// Providers registered by 083 existed under the 082 checks; later ones
	// (TicketingCiné, 084) cannot be published at 082.
	for _, provider := range schedule.SourceProviders() {
		if !covered[provider] && provider != schedule.ProviderTicketingcine {
			t.Fatalf("no upgrade fixture for %s", provider)
		}
	}
	for _, d := range datasets {
		if _, err := store.Replace(ctx, []Dataset{d}); err != nil {
			t.Fatalf("publish %s at 082: %v", d.Provider, err)
		}
	}
	before, _, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := func() map[string]int {
		result := map[string]int{}
		for _, table := range []string{"theaters", "movies", "showtimes", "screening_history_theaters", "screening_history_showtimes", "public_movie_sources", "movie_slug_aliases", "cinema_activity_coverage"} {
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
				t.Fatal(table, err)
			}
			result[table] = n
		}
		return result
	}
	beforeCounts := counts()

	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("registry migration on populated schema", err)
	}
	after, _, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("published snapshot changed across the registry migration")
	}
	if afterCounts := counts(); !reflect.DeepEqual(beforeCounts, afterCounts) {
		t.Fatalf("row counts changed: before=%v after=%v", beforeCounts, afterCounts)
	}

	var keys []string
	rows, err := pool.Query(ctx, `SELECT key FROM providers ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	rows.Close()
	var want []string
	for _, provider := range schedule.SourceProviders() {
		want = append(want, string(provider))
	}
	// The registry must cover every application provider after all migrations.
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Fatalf("providers=%v want=%v", keys, want)
	}

	// Re-publishing every provider still works on the new schema.
	for _, d := range datasets {
		if _, err := store.Replace(ctx, []Dataset{d}); err != nil {
			t.Fatalf("publish %s after 083: %v", d.Provider, err)
		}
	}
}

// A new provider needs only a registry row; unknown providers are rejected by
// foreign key, and generic invariants still apply to every provider.
func TestProviderRegistryAdmitsRegisteredProvidersOnlyIntegration(t *testing.T) {
	ctx := t.Context()
	pool := newHistoryPool(t)
	insertTheater := func(provider, id string) error {
		_, err := pool.Exec(ctx, `INSERT INTO theaters (generation_id,id,provider,provider_id,slug,name,address,city,postal_code)
VALUES (999,$1||'-'||$2,$1,$2,$1||'-'||$2,'Cinéma','1 rue','Ville','75001')`, provider, id)
		return err
	}
	if err := insertTheater("scare", "1234"); err == nil {
		t.Fatal("unregistered provider accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO providers (key) VALUES ('scare')`); err != nil {
		t.Fatal(err)
	}
	if err := insertTheater("scare", "1234"); err != nil {
		t.Fatal("registered provider rejected", err)
	}
	if err := insertTheater("scare", "bad id"); err == nil {
		t.Fatal("identity outside the shared character set accepted")
	}
	for _, key := range []string{"Bad", "a", "has-dash", strings.Repeat("x", 40)} {
		if _, err := pool.Exec(ctx, `INSERT INTO providers (key) VALUES ($1)`, key); err == nil {
			t.Fatalf("invalid provider key %q accepted", key)
		}
	}
	if _, err := NewStore(pool).Replace(ctx, []Dataset{testDataset()}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE showtimes SET end_time=start_time-interval '1 minute'`,
		`UPDATE showtimes SET first_part_duration_minutes=-1`,
		`UPDATE showtimes SET language='ALL'`,
		`UPDATE showtimes SET provider_showing_id='bad id'`,
		`UPDATE showtimes SET provider='scare'`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatal("generic invariant not enforced", statement)
		}
	}
}
