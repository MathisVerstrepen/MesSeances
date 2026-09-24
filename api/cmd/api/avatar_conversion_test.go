package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/accountavatar"
	"messeances/api/internal/accounts"
	"messeances/api/internal/database"
)

func TestAvatarConversionDispatchAndEnvironment(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	for _, args := range [][]string{{"unknown"}, {"migrate-account-avatars", "extra"}} {
		if err := dispatch(t.Context(), args, logger); err == nil || err.Error() != "configuration error" {
			t.Fatal("invalid args accepted")
		}
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	media, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = media.Close() }()
	getenv := func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "not a database URL"
		case "ACCOUNT_AVATAR_DIR":
			return root
		default:
			t.Fatalf("maintenance accessed unrelated configuration %s", key)
			return ""
		}
	}
	if err := migrateAccountAvatars(t.Context(), getenv, logger); err == nil || err.Error() != "configuration error" {
		t.Fatal("root lock not checked before database", err)
	}
	for _, reason := range []string{"account avatar conversion required", "account avatar conversion failed"} {
		log.Reset()
		logProcessFailure(logger, errors.New(reason))
		if !strings.Contains(log.String(), reason) || !strings.Contains(log.String(), `"failure_stage":"migration"`) {
			t.Fatal("unsafe or unclassified conversion failure")
		}
	}
}

// Command uses public schema by contract, so test owns a whole generated DB,
// never changes TEST_DATABASE_URL's database or any existing media directory.
func conversionCommandDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid isolated database URL")
	}
	hosts := []string{cfg.ConnConfig.Host}
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			t.Fatal("command tests require loopback")
		}
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("command tests require dedicated *_test database")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || u.RawQuery != "sslmode=disable" {
		t.Fatal("command tests require explicit simple postgres URL")
	}
	admin, err := pgx.Connect(t.Context(), raw)
	if err != nil {
		t.Fatal("connect test database")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	_, digest, err := accounts.NewToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "avatar_command_" + fmt.Sprintf("%x", digest[:8])
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal("create owned database", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error("cleanup owned command database", err)
		}
	})
	u.Path = "/" + name
	pool, err := database.OpenPool(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return u.String(), pool
}

func seedCommandHistoricalPNG(t *testing.T, pool *pgxpool.Pool, root string) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `CREATE TABLE movieflow_schema_migrations(version bigint PRIMARY KEY,name text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../internal/database/migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		name := filepath.Base(path)
		version, err := strconv.Atoi(name[:3])
		if err != nil {
			t.Fatal(err)
		}
		if version > 46 {
			break
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(t.Context(), string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO movieflow_schema_migrations(version,name) VALUES($1,$2)`, version, name); err != nil {
			t.Fatal(err)
		}
	}
	name := strings.Repeat("a", 32) + ".png"
	if _, err := tx.Exec(t.Context(), `INSERT INTO accounts(email,pending_kind,created_at,avatar_path,avatar_source,avatar_revision) VALUES('conversion@example.com','google',now()-interval '90 days',$1,'google',6)`, name); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAvatarConversionCommandAndStartupIntegration(t *testing.T) {
	db, pool := conversionCommandDatabase(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	seedCommandHistoricalPNG(t, pool, root)
	for key, value := range map[string]string{
		"DATABASE_URL": db, "ACCOUNT_AVATAR_DIR": root, "ACCOUNTS_ENABLED": "true", "WEB_ORIGIN": "https://messeances.fr",
		"GOOGLE_CLIENT_ID": "synthetic.apps.googleusercontent.com", "GOOGLE_CLIENT_SECRET": "synthetic-client-secret",
		"AWS_REGION": "eu-west-3", "SES_FROM_EMAIL": "comptes@example.com", "SES_CONFIGURATION_SET": "accounts",
		"SES_FEEDBACK_TOPIC_ARN": "arn:aws:sns:eu-west-3:123456789012:accounts", "SES_IDENTITY_ARN": "arn:aws:ses:eu-west-3:123456789012:identity/example.com",
		"SES_FEEDBACK_QUEUE_URL": "https://sqs.eu-west-3.amazonaws.com/123456789012/accounts",
		"ACCOUNT_OUTBOX_KEY_ID":  "key-1", "ACCOUNT_OUTBOX_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		"ACCOUNT_ADDRESS_HMAC_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), "PROXY_FILE": "", "TMDB_API_READ_ACCESS_TOKEN": "",
		"ADMIN_PASSWORD": "", "ADMIN_SESSION_SECRET": "",
	} {
		t.Setenv(key, value)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	lock, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context()); err == nil || err.Error() != "configuration error" {
		t.Fatal("API ignored live converter root lock", err)
	}
	if err := dispatch(t.Context(), []string{"migrate-account-avatars"}, logger); err == nil || err.Error() != "configuration error" {
		t.Fatal("converter ignored live API root lock", err)
	}
	var version int
	if err := pool.QueryRow(t.Context(), `SELECT max(version) FROM movieflow_schema_migrations`).Scan(&version); err != nil || version != 46 {
		t.Fatal("schema changed despite lock contention")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context()); !errors.Is(err, accounts.ErrAvatarConversionRequired) {
		t.Fatal("enabled API did not stop at pending marker", err)
	}
	var source, path string
	var rev int
	if err := pool.QueryRow(t.Context(), `SELECT avatar_source,avatar_path,avatar_revision FROM accounts`).Scan(&source, &path, &rev); err != nil || source != "google" || rev != 6 || !strings.HasSuffix(path, ".png") {
		t.Fatal("startup purged or converted pending account")
	}
	// Clear all OAuth/mail config and put malformed dotenv in a test-only cwd.
	for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "ACCOUNT_OUTBOX_KEY", "ACCOUNT_ADDRESS_HMAC_KEY", "AWS_REGION"} {
		t.Setenv(key, "")
	}
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "deploy"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "deploy/.env"), []byte("BROKEN=\"unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	if err := dispatch(t.Context(), []string{"migrate-account-avatars"}, logger); err != nil {
		t.Fatal("maintenance loaded dotenv/providers or failed", err)
	}
	if err := accounts.NewPostgresStore(pool).AvatarConversionReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT avatar_source,avatar_path,avatar_revision FROM accounts`).Scan(&source, &path, &rev); err != nil || source != "google" || rev != 7 || !strings.HasSuffix(path, ".webp") {
		t.Fatal("command state mismatch")
	}
	if err := dispatch(t.Context(), []string{"migrate-account-avatars"}, logger); err != nil {
		t.Fatal("command rerun failed", err)
	}
	for _, secret := range []string{db, root, path, "conversion@example.com"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("command leaked private state")
		}
	}
	if !strings.Contains(logs.String(), `"already_webp":1`) {
		t.Fatal("missing aggregate progress")
	}
	if err := dispatch(t.Context(), nil, logger); err == nil || !strings.Contains(logs.String(), "dotenv load failed") {
		t.Fatal("ordinary startup stopped loading dotenv")
	}
}
