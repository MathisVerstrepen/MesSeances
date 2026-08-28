package shortlink

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	RetentionPeriod        = 90 * 24 * time.Hour
	RetentionPurgeInterval = 24 * time.Hour
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, link Link) error {
	_, err := s.pool.Exec(ctx, "INSERT INTO short_links (code, target) VALUES ($1, $2)", link.Code, link.Target)
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "short_links_pkey" {
		return ErrCollision
	}
	return fmt.Errorf("create shortlink failed: %w", ErrUnavailable)
}

func (s *PostgresStore) Resolve(ctx context.Context, code string) (Link, error) {
	var link Link
	err := s.pool.QueryRow(ctx, "SELECT code, target FROM short_links WHERE code=$1", code).Scan(&link.Code, &link.Target)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("resolve shortlink failed: %w", ErrUnavailable)
	}
	return link, nil
}

func (s *PostgresStore) PurgeCreatedBefore(ctx context.Context, cutoff time.Time) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM short_links WHERE created_at < $1", cutoff.UTC()); err != nil {
		return fmt.Errorf("purge shortlinks failed: %w", ErrUnavailable)
	}
	return nil
}
