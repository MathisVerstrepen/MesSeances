package shortlink

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

	"messeances/api/internal/database"
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
		t.Fatal("generate schema nonce failed")
	}
	schema := "movieflow_shortlink_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE")
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run migrations failed")
	}

	first := NewPostgresStore(pool)
	link := Link{Code: "AAAAAAAAAAAAAAAAAAAAAA", Target: "/films?sort=title&q=Am%C3%A9lie"}
	if err := first.Create(ctx, link); err != nil {
		t.Fatal("create link failed")
	}
	resolved, err := NewPostgresStore(pool).Resolve(ctx, link.Code)
	if err != nil || resolved != link {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	if err := first.Create(ctx, Link{Code: link.Code, Target: "/"}); !errors.Is(err, ErrCollision) {
		t.Fatalf("duplicate err=%v", err)
	}
	if _, err := first.Resolve(ctx, "BBBBBBBBBBBBBBBBBBBBBB"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found err=%v", err)
	}
	cutoff := time.Date(2026, 8, 28, 12, 0, 0, 0, time.FixedZone("test", 2*60*60))
	retentionLinks := []struct {
		link      Link
		createdAt time.Time
		retained  bool
	}{
		{link: Link{Code: "CCCCCCCCCCCCCCCCCCCCCC", Target: "/films"}, createdAt: cutoff.Add(-time.Nanosecond)},
		{link: Link{Code: "DDDDDDDDDDDDDDDDDDDDDD", Target: "/films"}, createdAt: cutoff, retained: true},
		{link: Link{Code: "EEEEEEEEEEEEEEEEEEEEEE", Target: "/films"}, createdAt: cutoff.Add(time.Nanosecond), retained: true},
	}
	for _, item := range retentionLinks {
		if _, err := pool.Exec(ctx, "INSERT INTO short_links (code, target, created_at) VALUES ($1, $2, $3)", item.link.Code, item.link.Target, item.createdAt); err != nil {
			t.Fatal("insert retention fixture failed")
		}
	}
	if err := first.PurgeCreatedBefore(ctx, cutoff); err != nil {
		t.Fatal("purge short links failed")
	}
	for _, item := range retentionLinks {
		resolved, err := first.Resolve(ctx, item.link.Code)
		if item.retained && (err != nil || resolved != item.link) {
			t.Fatalf("retained link=%+v resolved=%+v err=%v", item.link, resolved, err)
		}
		if !item.retained && !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired link err=%v", err)
		}
	}
	var retentionIndex bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_indexes
		WHERE schemaname=current_schema() AND tablename='short_links'
		  AND indexname='short_links_retention_idx'
		  AND indexdef LIKE '%(created_at)%'
	)`).Scan(&retentionIndex); err != nil || !retentionIndex {
		t.Fatalf("retention index exists=%t err=%v", retentionIndex, err)
	}
	for _, invalid := range []Link{
		{Code: "short", Target: "/"},
		{Code: "BBBBBBBBBBBBBBBBBBBBBB", Target: ""},
		{Code: "BBBBBBBBBBBBBBBBBBBBBB", Target: "//evil.example"},
		{Code: "BBBBBBBBBBBBBBBBBBBBBB", Target: "/films\nInjected"},
		{Code: "BBBBBBBBBBBBBBBBBBBBBB", Target: "/" + strings.Repeat("x", 2048)},
	} {
		if _, err := pool.Exec(ctx, "INSERT INTO short_links (code, target) VALUES ($1,$2)", invalid.Code, invalid.Target); err == nil {
			t.Fatalf("database accepted invalid link: %+v", invalid)
		}
	}
}
