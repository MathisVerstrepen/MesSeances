package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"movieflow/api/internal/database"
)

func TestPostgresStoreIntegration(t *testing.T) {
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
	schema := "movieflow_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create integration schema %s failed", schema)
	}
	t.Cleanup(func() {
		if schema == "" || !strings.HasPrefix(schema, "movieflow_test_") {
			t.Errorf("unsafe integration schema cleanup rejected")
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop integration schema %s failed", schema)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	var currentSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("isolated schema assertion failed for %s", schema)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("first migration run failed")
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("repeat migration run failed")
	}
	var migrationCount int
	var migrationName string
	if err := pool.QueryRow(ctx, "SELECT count(*), min(name) FROM movieflow_schema_migrations").Scan(&migrationCount, &migrationName); err != nil || migrationCount != 1 || migrationName != "001_initial.sql" {
		t.Fatalf("migration history count=%d name=%q", migrationCount, migrationName)
	}
	store := NewPostgresStore(pool)
	if _, err := store.CurrentVersion(ctx); !errors.Is(err, ErrNoCompleteSnapshot) {
		t.Fatalf("missing current version error=%v", err)
	}
	if _, _, err := store.Load(ctx); !errors.Is(err, ErrNoCompleteSnapshot) {
		t.Fatalf("missing load error=%v", err)
	}

	t.Run("initial insert and load", func(t *testing.T) {
		version, err := store.Replace(ctx, testDataset())
		if err != nil || version != 1 {
			t.Fatalf("replace version=%d error=%v", version, err)
		}
		loaded, loadedVersion, err := store.Load(ctx)
		if err != nil || loadedVersion != 1 {
			t.Fatalf("load version=%d error=%v", loadedVersion, err)
		}
		if !loaded.GeneratedAt.Equal(testDataset().GeneratedAt) || loaded.GeneratedAt.Location() != time.UTC {
			t.Fatal("generated timestamp did not round trip in UTC")
		}
		if loaded.Showtimes[0].StartTime.Location().String() != Timezone || loaded.Showtimes[0].Movie.PosterURL != "" {
			t.Fatal("Paris timestamp or NULL poster did not round trip")
		}
	})

	var source *PostgresSource
	t.Run("source and complete replacement", func(t *testing.T) {
		var err error
		source, err = NewPostgresSource(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		replacement := testDataset()
		replacement.GeneratedAt = replacement.GeneratedAt.Add(time.Minute)
		replacement.Theaters = append([]TheaterRecord(nil), replacement.Theaters[0])
		replacement.Theaters[0].Name = "UGC Lille remplacé"
		replacement.Showtimes = append([]ShowtimeRecord(nil), replacement.Showtimes[0])
		version, err := store.Replace(ctx, replacement)
		if err != nil || version != 2 {
			t.Fatalf("replace version=%d error=%v", version, err)
		}
		loaded, loadedVersion, err := store.Load(ctx)
		if err != nil || loadedVersion != 2 || len(loaded.Theaters) != 1 || len(loaded.Showtimes) != 1 {
			t.Fatalf("replacement load version=%d theaters=%d showtimes=%d error=%v", loadedVersion, len(loaded.Theaters), len(loaded.Showtimes), err)
		}
		var oldRows int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM theaters WHERE id IN ('ugc-26', 'ugc-99')").Scan(&oldRows); err != nil || oldRows != 0 {
			t.Fatalf("old rows=%d", oldRows)
		}
		if got := source.Snapshot().Theaters[0].Name; got != "UGC Lille remplacé" {
			t.Fatalf("refreshed name=%q", got)
		}
		service, err := NewService(source, ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
		if err != nil {
			t.Fatal(err)
		}
		timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll})
		if err != nil || len(timeline.Theaters) != 1 || timeline.Theaters[0].Name != "UGC Lille remplacé" {
			t.Fatalf("timeline=%+v error=%v", timeline, err)
		}
	})

	t.Run("pre SQL rejection and rollback", func(t *testing.T) {
		single := testDataset()
		single.Scope = ScopeSingle
		if _, err := store.Replace(ctx, single); err == nil {
			t.Fatal("single scope replacement accepted")
		}
		conflict := testDataset()
		conflict.Showtimes[1].Movie.ProviderID = conflict.Showtimes[0].Movie.ProviderID
		conflict.Showtimes[1].Movie.Slug = conflict.Showtimes[0].Movie.Slug
		if _, err := store.Replace(ctx, conflict); err == nil {
			t.Fatal("conflicting movie replacement accepted")
		}
		invalidSQL := testDataset()
		invalidSQL.Theaters[0].Name = " "
		if _, err := store.Replace(ctx, invalidSQL); err == nil {
			t.Fatal("constraint-breaking replacement accepted")
		}
		version, err := store.CurrentVersion(ctx)
		if err != nil || version != 2 {
			t.Fatalf("version after rollback=%d error=%v", version, err)
		}
		loaded, _, err := store.Load(ctx)
		if err != nil || len(loaded.Theaters) != 1 || loaded.Theaters[0].Name != "UGC Lille remplacé" {
			t.Fatalf("last good after rollback=%+v error=%v", loaded.Theaters, err)
		}
	})
}
