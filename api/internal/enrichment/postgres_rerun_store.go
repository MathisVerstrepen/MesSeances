package enrichment

import (
	"context"
	"fmt"
)

func (s *PostgresStore) UnresolvedMovies(ctx context.Context) ([]Movie, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.provider, m.provider_id, m.title, m.runtime_minutes, MIN(st.start_time)
FROM movies m
JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
LEFT JOIN movie_matches mm ON mm.source_provider=m.provider AND mm.source_movie_id=m.provider_id AND mm.metadata_provider='tmdb'
JOIN showtimes st ON st.generation_id=m.generation_id AND st.provider=m.provider AND st.movie_provider_id=m.provider_id
WHERE (mm.status IS NULL OR mm.status IN ('unmatched', 'review_required'))
  AND NOT EXISTS (SELECT 1 FROM local_movie_group_members lmgm WHERE lmgm.source_provider=m.provider AND lmgm.source_movie_id=m.provider_id)
GROUP BY m.provider, m.provider_id, m.title, m.runtime_minutes
ORDER BY m.provider, m.provider_id`)
	if err != nil {
		return nil, fmt.Errorf("read unresolved movies failed")
	}
	defer rows.Close()
	movies := make([]Movie, 0)
	for rows.Next() {
		var movie Movie
		if err := rows.Scan(&movie.SourceProvider, &movie.ProviderID, &movie.Title, &movie.RuntimeMinutes, &movie.FirstShowingAt); err != nil {
			return nil, fmt.Errorf("read unresolved movies failed")
		}
		movies = append(movies, movie)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read unresolved movies failed")
	}
	return movies, nil
}
