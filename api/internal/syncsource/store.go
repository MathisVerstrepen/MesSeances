// Package syncsource reads the cinemas configured for platform connectors
// (table sync_sources). Rows are managed by operators; connectors only read
// enabled ones.
package syncsource

import (
	"context"
	"encoding/json"
	"fmt"

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
