// Package syncsource reads the cinemas configured for platform connectors
// (table sync_sources). Rows are managed by operators; connectors only read
// enabled ones.
package syncsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/schedule"
)

// Source is one configured cinema of a provider.
type Source struct {
	Provider schedule.Provider
	// Key is the provider's native cinema identifier (for TicketingCiné, the
	// EMSdddd site).
	Key     string
	Name    string
	Website string
	CNCID   string
	Params  json.RawMessage
}

// PostgresStore reads sync sources.
type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Enabled returns provider's enabled sources ordered by key.
func (s *PostgresStore) Enabled(ctx context.Context, provider schedule.Provider) ([]Source, error) {
	rows, err := s.pool.Query(ctx, `SELECT source_key,name,coalesce(website,''),coalesce(cnc_id,''),params
FROM sync_sources WHERE provider=$1 AND enabled ORDER BY source_key`, string(provider))
	if err != nil {
		return nil, fmt.Errorf("list sync sources failed")
	}
	defer rows.Close()
	sources := []Source{}
	for rows.Next() {
		source := Source{Provider: provider}
		if err := rows.Scan(&source.Key, &source.Name, &source.Website, &source.CNCID, &source.Params); err != nil {
			return nil, fmt.Errorf("read sync source failed")
		}
		sources = append(sources, source)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read sync sources failed")
	}
	return sources, nil
}

// ErrNotFound reports an unknown provider/key pair.
var ErrNotFound = errors.New("sync source not found")

// AdminSource is a configured source with its operator state.
type AdminSource struct {
	Source
	Enabled   bool
	UpdatedAt time.Time
}

// List returns every configured source ordered by provider, name and key.
func (s *PostgresStore) List(ctx context.Context) ([]AdminSource, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider,source_key,name,coalesce(website,''),coalesce(cnc_id,''),params,enabled,updated_at
FROM sync_sources ORDER BY provider,lower(name),source_key`)
	if err != nil {
		return nil, fmt.Errorf("list sync sources failed")
	}
	defer rows.Close()
	sources := []AdminSource{}
	for rows.Next() {
		var source AdminSource
		var provider string
		if err := rows.Scan(&provider, &source.Key, &source.Name, &source.Website, &source.CNCID, &source.Params, &source.Enabled, &source.UpdatedAt); err != nil {
			return nil, fmt.Errorf("read sync source failed")
		}
		source.Provider = schedule.Provider(provider)
		sources = append(sources, source)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read sync sources failed")
	}
	return sources, nil
}

// SetEnabled enables or disables one source.
func (s *PostgresStore) SetEnabled(ctx context.Context, provider schedule.Provider, key string, enabled bool) (AdminSource, error) {
	source := AdminSource{Source: Source{Provider: provider}}
	err := s.pool.QueryRow(ctx, `UPDATE sync_sources SET enabled=$3,updated_at=CASE WHEN enabled=$3 THEN updated_at ELSE now() END
WHERE provider=$1 AND source_key=$2
RETURNING source_key,name,coalesce(website,''),coalesce(cnc_id,''),params,enabled,updated_at`, string(provider), key, enabled).
		Scan(&source.Key, &source.Name, &source.Website, &source.CNCID, &source.Params, &source.Enabled, &source.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminSource{}, ErrNotFound
	}
	if err != nil {
		return AdminSource{}, fmt.Errorf("update sync source failed")
	}
	return source, nil
}

// SetAllEnabled enables or disables every source of provider, or of every
// provider when provider is empty. It returns the number of changed rows.
func (s *PostgresStore) SetAllEnabled(ctx context.Context, provider schedule.Provider, enabled bool) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE sync_sources SET enabled=$2,updated_at=now()
WHERE ($1='' OR provider=$1) AND enabled<>$2`, string(provider), enabled)
	if err != nil {
		return 0, fmt.Errorf("update sync sources failed")
	}
	return tag.RowsAffected(), nil
}
