package enrichment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
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
	return s.withWriteTransaction(ctx, "begin metadata refresh failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		for _, item := range metadata {
			if err := writeMetadata(ctx, tx, item); err != nil {
				return nil, err
			}
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after metadata refresh",
			advanceVersion: true,
			mapCommitError: func(error) error { return fmt.Errorf("commit metadata refresh failed") },
		}, nil
	})
}
