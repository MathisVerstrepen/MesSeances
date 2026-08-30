package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationLockID int64 = 6211428337968314

// ErrMigrationHistoryIncompatible identifies recorded migration history that
// cannot be reconciled with the embedded migrations.
var ErrMigrationHistoryIncompatible = errors.New("database migration history is incompatible")

type migration struct {
	version int64
	name    string
	sql     string
}

func OpenPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("database configuration is missing")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database configuration is invalid")
	}
	config.ConnConfig.RuntimeParams["search_path"] = "public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("database pool initialization failed")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database connection failed")
	}
	return pool, nil
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return fmt.Errorf("database migration discovery failed")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("database migration transaction failed")
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("database migration lock failed")
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS movieflow_schema_migrations (
version bigint PRIMARY KEY,
name text NOT NULL,
applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("database migration bookkeeping failed")
	}
	rows, err := tx.Query(ctx, "SELECT version, name FROM movieflow_schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("database migration bookkeeping read failed")
	}
	var recorded []migration
	for rows.Next() {
		var item migration
		if err := rows.Scan(&item.version, &item.name); err != nil {
			rows.Close()
			return fmt.Errorf("database migration bookkeeping read failed")
		}
		recorded = append(recorded, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("database migration bookkeeping read failed")
	}
	rows.Close()
	if err := validateMigrationHistory(recorded, migrations); err != nil {
		return err
	}
	for _, item := range migrations[len(recorded):] {
		if _, err := tx.Exec(ctx, item.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("database migration %03d failed", item.version)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO movieflow_schema_migrations (version, name) VALUES ($1, $2)", item.version, item.name); err != nil {
			return fmt.Errorf("database migration bookkeeping write failed")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database migration commit failed")
	}
	return nil
}

func validateMigrationHistory(recorded, available []migration) error {
	if len(recorded) > len(available) {
		return ErrMigrationHistoryIncompatible
	}
	for i, item := range recorded {
		if item.version != available[i].version || item.name != available[i].name {
			return ErrMigrationHistoryIncompatible
		}
	}
	return nil
}

func embeddedMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	items := make([]migration, 0, len(entries))
	seen := map[int64]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("unexpected migration directory")
		}
		name := entry.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 || len(parts[0]) != 3 || !strings.HasSuffix(parts[1], ".sql") || strings.TrimSuffix(parts[1], ".sql") == "" {
			return nil, fmt.Errorf("malformed migration name")
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version <= 0 || seen[version] {
			return nil, fmt.Errorf("invalid migration version")
		}
		content, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		seen[version] = true
		items = append(items, migration{version: version, name: name, sql: string(content)})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no migrations")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	return items, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
