package publicmoviepg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
)

func bulkTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(url) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	bootstrap, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"movieflow_bulk_test_" + hex.EncodeToString(nonce)}.Sanitize()
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = bootstrap.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	})
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("parse integration pool failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

type countedTx struct {
	pgx.Tx
	statements int
}

func (tx *countedTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.statements++
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx *countedTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.statements++
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *countedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.statements++
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func bulkReconcile(t testing.TB, pool *pgxpool.Pool) int {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	counted := &countedTx{Tx: tx}
	if err := Reconcile(t.Context(), counted); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return counted.statements
}

func seedBulkCatalog(t testing.TB, pool *pgxpool.Pool, size int) {
	t.Helper()
	// Retained, inactive sources allow a genuine no-op across transactions.
	// Active sources must still advance last_seen_at on every reconciliation.
	_, err := pool.Exec(t.Context(), `INSERT INTO public_movies
    (id,identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,last_seen_at)
OVERRIDING SYSTEM VALUE SELECT i,'ugc',i::text,'Fixture '||i,90,'2000-01-01'
FROM generate_series(1,$1::integer) i;
INSERT INTO public_movie_sources
    (source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes,last_seen_at)
SELECT 'ugc',i::text,i,'ugc-film-'||i,'Fixture '||i,90,'2020-01-01'
FROM generate_series(1,$1::integer) i;
INSERT INTO public_movies
    (id,identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,redirect_to_id)
OVERRIDING SYSTEM VALUE SELECT $1::integer+i,'kinepolis',i::text,'Tombstone',90,i FROM generate_series(1,$1::integer/3) i;
SELECT setval(pg_get_serial_sequence('public_movies','id'),(SELECT max(id) FROM public_movies));
INSERT INTO movie_metadata_cache
    (provider,provider_movie_id,locale,provider_title,localized_title,runtime_minutes,genres,fetched_at,refresh_after)
SELECT 'tmdb',i,'fr-FR','Fixture '||i,'Fixture '||i,90,'{}','2020-01-01','2030-01-01'
FROM generate_series(1,$1::integer/9) i;
INSERT INTO movie_matches
    (source_provider,source_movie_id,metadata_provider,status,metadata_movie_id,score,
     normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at)
SELECT 'ugc',i::text,'tmdb','matched',i,1,'fixture '||i,90,'[]','2020-01-01','2030-01-01','2020-01-01'
FROM generate_series(1,$1::integer/9) i;
INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,active,verified_at)
SELECT i,i,false,'2020-01-01' FROM generate_series(1,$1::integer/9) i;`, pgx.QueryExecModeSimpleProtocol, size)
	if err != nil {
		t.Fatal(err)
	}
	bulkReconcile(t, pool)
	_, err = pool.Exec(t.Context(), `INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id)
SELECT 'historical-'||source_movie_id,public_movie_id,'source',source_provider,source_movie_id FROM public_movie_sources`)
	if err != nil {
		t.Fatal(err)
	}
}

// Include ctid as well as every value: preserving updated_at alone does not
// prove that PostgreSQL skipped physical updates (xmin alone misses same-tx writes).
func bulkSnapshot(t testing.TB, tx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) []string {
	t.Helper()
	var result []string
	for _, table := range []string{"public_movies", "public_movie_sources", "movie_slug_aliases", "tmdb_upcoming_movies"} {
		var snapshot string
		err := tx.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(value ORDER BY value::text),'[]')::text
FROM (SELECT to_jsonb(row)||jsonb_build_object('physical_ctid',row.ctid::text) AS value FROM `+pgx.Identifier{table}.Sanitize()+` row) snapshots`).Scan(&snapshot)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, snapshot)
	}
	return result
}

func TestReconcileBoundedStatementsAndNoOpIntegration(t *testing.T) {
	var baseline int
	for _, size := range []int{10, 2751} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			pool := bulkTestPool(t)
			seedBulkCatalog(t, pool, size)
			before := bulkSnapshot(t, pool)
			start := time.Now()
			count := bulkReconcile(t, pool)
			t.Logf("steady-state sources=%d tombstones=%d catalog=%d statements=%d elapsed=%s", size, size/3, size/9, count, time.Since(start))
			if baseline == 0 {
				baseline = count
			}
			if count != baseline || count > 24 {
				t.Fatalf("statement count=%d baseline=%d, want constant <=24", count, baseline)
			}
			if !reflect.DeepEqual(before, bulkSnapshot(t, pool)) {
				t.Fatal("unchanged reconciliation physically rewrote catalog rows or changed values")
			}
			if _, err := pool.Exec(t.Context(), "UPDATE movie_metadata_cache SET localized_title='Changed title' WHERE provider_movie_id=1"); err != nil {
				t.Fatal(err)
			}
			if got := bulkReconcile(t, pool); got != baseline {
				t.Fatalf("metadata change statements=%d baseline=%d", got, baseline)
			}
			var title string
			if err := pool.QueryRow(t.Context(), "SELECT title FROM public_movies WHERE id=1").Scan(&title); err != nil || title != "Changed title" {
				t.Fatalf("title=%q err=%v", title, err)
			}
		})
	}
}

func TestReconcileBulkAliasCollisionRollbackIntegration(t *testing.T) {
	pool := bulkTestPool(t)
	seedBulkCatalog(t, pool, 10)
	for name, sql := range map[string]string{
		"source kind":     "UPDATE movie_slug_aliases SET alias_kind='local',source_provider=NULL,source_movie_id=NULL WHERE slug='ugc-film-1'",
		"source owner":    "UPDATE movie_slug_aliases SET source_movie_id='2' WHERE slug='ugc-film-1'",
		"source provider": "UPDATE movie_slug_aliases SET source_provider='kinepolis' WHERE slug='ugc-film-1'",
		"evidence kind":   "UPDATE movie_slug_aliases SET alias_kind='local' WHERE slug='tmdb-film-1'",
		"duplicate input": "UPDATE public_movie_sources SET source_slug='ugc-film-1' WHERE source_movie_id='2'",
	} {
		t.Run(name, func(t *testing.T) {
			before := bulkSnapshot(t, pool)
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				t.Fatal(err)
			}
			// Ensure a real metadata update precedes the rejected alias phase.
			if _, err := tx.Exec(t.Context(), "UPDATE public_movie_sources SET title='Must roll back' WHERE source_movie_id='3'"); err != nil {
				t.Fatal(err)
			}
			if err := Reconcile(t.Context(), tx); err == nil {
				t.Fatal("alias collision accepted, including collision with unchanged target")
			}
			if err := tx.Rollback(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, bulkSnapshot(t, pool)) {
				t.Fatal("collision transaction did not roll back atomically")
			}
		})
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	alias := aliasAssignment{Slug: "local-film-999", Kind: "local", Target: 1}
	if err := writeAliases(t.Context(), tx, []aliasAssignment{alias, alias}, []sourceAssignment{}); err != nil {
		t.Fatalf("identical evidence should be deduplicated: %v", err)
	}
}

func BenchmarkReconcileSteadyState(b *testing.B) {
	pool := bulkTestPool(b)
	seedBulkCatalog(b, pool, 2751)
	b.ResetTimer()
	for b.Loop() {
		bulkReconcile(b, pool)
	}
}

func TestBulkAssignmentsOrderingAndTimestampsIntegration(t *testing.T) {
	pool := bulkTestPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `INSERT INTO public_movies
    (id,identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,confirmed_tmdb_id,updated_at,last_seen_at)
OVERRIDING SYSTEM VALUE VALUES
    (1,'ugc','1','First',90,11,'2000-01-01','2000-01-01'),
    (2,'ugc','2','Second',90,22,'2000-01-01','2000-01-01'),
    (3,'ugc','3','Loser',90,NULL,'2000-01-01','2000-01-01');
INSERT INTO public_movies
    (id,identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,redirect_to_id,updated_at)
OVERRIDING SYSTEM VALUE VALUES (4,'kinepolis','A','Prior tombstone',90,3,'2000-01-01');
INSERT INTO public_movie_sources
    (source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes,last_seen_at)
VALUES ('ugc','1',1,'ugc-film-1','First',90,'2020-01-01'),
    ('ugc','2',1,'ugc-film-2','Second',90,'2030-01-01'),
    ('ugc','3',3,'ugc-film-3','Loser',90,'2025-01-01');
INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id,first_seen_at,retargeted_at)
SELECT source_slug,public_movie_id,'source',source_provider,source_movie_id,'2000-01-01','2000-01-01'
FROM public_movie_sources;
INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,source_provider,source_movie_id,first_seen_at)
VALUES ('old-source-2',1,'source','ugc','2','2000-01-01');
INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind,first_seen_at)
VALUES ('tmdb-film-11',1,'tmdb','2000-01-01'),('tmdb-film-22',3,'tmdb','2000-01-01'),('old-tombstone',4,'local','2000-01-01');
INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,active,verified_at) VALUES (22,3,false,'2000-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	sources, err := loadSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	date := time.Date(2024, 2, 29, 0, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	components := []*component{
		{publicID: 1, catalogOwner: 3, tmdbID: 22, localGroupID: 99,
			members:  []*source{sources[sourceKey{"ugc", "1"}], sources[sourceKey{"ugc", "3"}]},
			metadata: metadata{title: "L'été \"quoted\" \\ film", runtime: 0, overview: &empty, releaseDate: &date, genres: []string{}, tmdbID: 22}},
		{publicID: 2, tmdbID: 11, members: []*source{sources[sourceKey{"ugc", "2"}]},
			metadata: metadata{title: "Second", runtime: 90, genres: []string{"Drame", "Science-fiction"}, tmdbID: 11}},
	}
	persist := func() {
		t.Helper()
		movies, err := loadPublicMovies(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if err := persistAssignments(ctx, tx, components, movies, nil); err != nil {
			t.Fatal(err)
		}
		if err := persistAliases(ctx, tx, components); err != nil {
			t.Fatal(err)
		}
		if err := validateTargets(ctx, tx); err != nil {
			t.Fatal(err)
		}
	}
	persist()
	// Swapping unique TMDB IDs must clear both owners before either claim.
	// First component sees source 2 until its later transfer to component 2.
	var valid bool
	err = tx.QueryRow(ctx, `SELECT
    (SELECT confirmed_tmdb_id=22 AND title=$1 AND runtime_minutes=0 AND overview='' AND
        release_date='2024-02-29' AND genres='{}' AND poster_url IS NULL AND backdrop_url IS NULL AND
        imdb_id IS NULL AND trailer_vf_youtube_key IS NULL AND trailer_vo_youtube_key IS NULL AND
        updated_at=CURRENT_TIMESTAMP AND last_seen_at='2030-01-01' FROM public_movies WHERE id=1)
    AND (SELECT confirmed_tmdb_id=11 AND overview IS NULL AND release_date IS NULL AND
        genres=ARRAY['Drame','Science-fiction'] AND updated_at=CURRENT_TIMESTAMP AND last_seen_at='2030-01-01'
        FROM public_movies WHERE id=2)
    AND (SELECT redirect_to_id=1 AND updated_at=CURRENT_TIMESTAMP FROM public_movies WHERE id=3)
    AND (SELECT redirect_to_id=1 AND updated_at='2000-01-01' FROM public_movies WHERE id=4)
    AND (SELECT public_movie_id=1 FROM tmdb_upcoming_movies WHERE tmdb_id=22)
    AND (SELECT public_movie_id=2 AND last_seen_at='2030-01-01' FROM public_movie_sources WHERE source_movie_id='2')`, components[0].metadata.title).Scan(&valid)
	if err != nil || !valid {
		t.Fatalf("metadata, ownership, last-seen or redirect timestamps changed: valid=%v err=%v", valid, err)
	}
	err = tx.QueryRow(ctx, `SELECT
    (SELECT public_movie_id=1 AND retargeted_at='2000-01-01' AND first_seen_at='2000-01-01' FROM movie_slug_aliases WHERE slug='ugc-film-1')
    AND (SELECT bool_and(public_movie_id=2 AND retargeted_at=CURRENT_TIMESTAMP AND first_seen_at='2000-01-01')
        FROM movie_slug_aliases WHERE slug IN ('ugc-film-2','old-source-2','tmdb-film-11'))
    AND (SELECT bool_and(public_movie_id=1 AND retargeted_at=CURRENT_TIMESTAMP AND first_seen_at='2000-01-01')
        FROM movie_slug_aliases WHERE slug IN ('ugc-film-3','tmdb-film-22','old-tombstone'))
    AND (SELECT public_movie_id=1 AND retargeted_at IS NULL FROM movie_slug_aliases WHERE slug='local-film-99')`).Scan(&valid)
	if err != nil || !valid {
		t.Fatalf("alias retarget or first-seen timestamps changed: valid=%v err=%v", valid, err)
	}
	// Refresh in-memory ownership as Reconcile would on its next call.
	for _, item := range components {
		for _, member := range item.members {
			member.publicID = item.publicID
		}
		if item.catalogOwner > 0 {
			item.catalogOwner = item.publicID
		}
	}
	before := bulkSnapshot(t, tx)
	persist()
	if !reflect.DeepEqual(before, bulkSnapshot(t, tx)) {
		t.Fatal("repeated assignments physically rewrote unchanged metadata, tombstones, sources or aliases")
	}
	// Clearing only TMDB identity has never itself touched updated_at. Keep
	// that behavior separate from the metadata-change timestamp predicate.
	if _, err := tx.Exec(ctx, "UPDATE public_movies SET updated_at='2000-01-01' WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	components[1].tmdbID, components[1].metadata.tmdbID = 0, 0
	persist()
	err = tx.QueryRow(ctx, "SELECT confirmed_tmdb_id IS NULL AND updated_at='2000-01-01' FROM public_movies WHERE id=2").Scan(&valid)
	if err != nil || !valid {
		t.Fatalf("identity clear timestamp semantics changed: valid=%v err=%v", valid, err)
	}
}

func TestReconcileActiveLastSeenIntegration(t *testing.T) {
	pool := bulkTestPool(t)
	seedBulkCatalog(t, pool, 10)
	_, err := pool.Exec(t.Context(), `INSERT INTO schedule_snapshot
    (version,schema_version,provider,scope,generated_at,timezone,window_from,window_through)
VALUES (1,1,'combined','all_cinemas','2026-08-23','Europe/Paris','2026-08-23','2026-08-24');
INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes)
VALUES (1,'ugc','1','ugc-film-1','Fixture 1',90);
UPDATE public_movies SET updated_at='2000-01-01' WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := Reconcile(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	var valid bool
	err = tx.QueryRow(t.Context(), `SELECT movie.last_seen_at=CURRENT_TIMESTAMP AND source.last_seen_at=CURRENT_TIMESTAMP
    AND movie.updated_at='2000-01-01' FROM public_movies movie
JOIN public_movie_sources source ON source.public_movie_id=movie.id WHERE movie.id=1`).Scan(&valid)
	if err != nil || !valid {
		t.Fatalf("active last-seen refresh changed: valid=%v err=%v", valid, err)
	}
	before := bulkSnapshot(t, tx)
	if err := Reconcile(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, bulkSnapshot(t, tx)) {
		t.Fatal("same-transaction active refresh physically rewrote unchanged rows")
	}
}
