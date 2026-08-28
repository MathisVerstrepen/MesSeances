package enrichment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/publicmoviepg"
)

func (s *PostgresStore) MatchedTMDBIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT metadata_movie_id
FROM movie_matches
WHERE metadata_provider='tmdb' AND status='matched'
ORDER BY metadata_movie_id`)
	if err != nil {
		return nil, fmt.Errorf("read matched TMDB IDs failed")
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil || id <= 0 {
			return nil, fmt.Errorf("read matched TMDB IDs failed")
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read matched TMDB IDs failed")
	}
	return ids, nil
}

func (s *PostgresStore) RefreshMetadata(ctx context.Context, metadata []Metadata) error {
	if len(metadata) == 0 {
		return nil
	}
	for _, item := range metadata {
		if err := validateMetadata(item); err != nil {
			return err
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin metadata refresh failed")
	}
	defer rollback(tx)
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	for _, item := range metadata {
		if err := writeMetadata(ctx, tx, item); err != nil {
			return err
		}
	}
	if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
		return fmt.Errorf("reconcile public movies after metadata refresh: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit metadata refresh failed")
	}
	return nil
}
