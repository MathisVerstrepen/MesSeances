package publicmoviepg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Catalog evidence is independent of provider components, including after corrections.
func addCatalogEvidence(ctx context.Context, tx pgx.Tx, components []*component) ([]*component, error) {
	byTMDB := map[int64]*component{}
	for _, item := range components {
		if item.tmdbID > 0 {
			byTMDB[item.tmdbID] = item
		}
	}
	rows, err := tx.Query(ctx, "SELECT tmdb_id,public_movie_id FROM tmdb_upcoming_movies ORDER BY tmdb_id")
	if err != nil {
		return nil, fmt.Errorf("read catalog identity evidence failed")
	}
	defer rows.Close()
	for rows.Next() {
		var id, owner int64
		if err := rows.Scan(&id, &owner); err != nil {
			return nil, fmt.Errorf("read catalog identity evidence failed")
		}
		item := byTMDB[id]
		if item == nil {
			item = &component{tmdbID: id}
			components = append(components, item)
			byTMDB[id] = item
		}
		item.catalogOwner = owner
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read catalog identity evidence failed")
	}
	return components, nil
}
