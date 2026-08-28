package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/publicmoviepg"
)

func (s *PostgresStore) Match(ctx context.Context, sourceProvider, sourceMovieID, metadataProvider string) (Match, bool, error) {
	var match Match
	var metadataID *int64
	var score *float64
	var candidates []byte
	err := s.pool.QueryRow(ctx, `SELECT source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after
FROM movie_matches WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider=$3`, sourceProvider, sourceMovieID, metadataProvider).Scan(&match.SourceProvider, &match.SourceMovieID, &match.MetadataProvider, &match.Status, &metadataID, &score, &match.NormalizedSourceTitle, &match.SourceRuntimeMinutes, &candidates, &match.EvaluatedAt, &match.RetryAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return Match{}, false, nil
	}
	if err != nil || json.Unmarshal(candidates, &match.Candidates) != nil {
		return Match{}, false, fmt.Errorf("read movie match failed")
	}
	if metadataID != nil {
		match.MetadataMovieID = *metadataID
	}
	if score != nil {
		match.Score = *score
	}
	return match, true, nil
}

func (s *PostgresStore) ConfirmedMatches(ctx context.Context, excludeSourceProvider, metadataProvider string, minRuntimeMinutes, maxRuntimeMinutes int) ([]ReusableMetadataMatch, error) {
	rows, err := s.pool.Query(ctx, `SELECT source_provider, normalized_source_title, source_runtime_minutes, metadata_movie_id, score
FROM movie_matches
WHERE source_provider<>$1 AND metadata_provider=$2 AND status='matched'
AND source_runtime_minutes BETWEEN $3 AND $4
ORDER BY source_provider, source_movie_id`, excludeSourceProvider, metadataProvider, minRuntimeMinutes, maxRuntimeMinutes)
	if err != nil {
		return nil, fmt.Errorf("read confirmed movie matches failed")
	}
	defer rows.Close()
	matches := []ReusableMetadataMatch{}
	for rows.Next() {
		var match ReusableMetadataMatch
		if err := rows.Scan(&match.SourceProvider, &match.NormalizedSourceTitle, &match.SourceRuntimeMinutes, &match.MetadataMovieID, &match.Score); err != nil {
			return nil, fmt.Errorf("read confirmed movie matches failed")
		}
		matches = append(matches, match)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read confirmed movie matches failed")
	}
	return matches, nil
}

func (s *PostgresStore) Metadata(ctx context.Context, provider string, movieID int64, locale string) (Metadata, bool, error) {
	var metadata Metadata
	var imdbID, overview, releaseDate, poster, backdrop, trailerVFYouTubeKey, trailerVOYouTubeKey *string
	err := s.pool.QueryRow(ctx, `SELECT provider, provider_movie_id, imdb_id, locale, provider_title, localized_title, overview, release_date::text, poster_url, backdrop_url, trailer_vf_youtube_key, trailer_vo_youtube_key, runtime_minutes, genres, fetched_at, refresh_after
FROM movie_metadata_cache WHERE provider=$1 AND provider_movie_id=$2 AND locale=$3`, provider, movieID, locale).Scan(&metadata.Provider, &metadata.ProviderMovieID, &imdbID, &metadata.Locale, &metadata.ProviderTitle, &metadata.LocalizedTitle, &overview, &releaseDate, &poster, &backdrop, &trailerVFYouTubeKey, &trailerVOYouTubeKey, &metadata.RuntimeMinutes, &metadata.Genres, &metadata.FetchedAt, &metadata.RefreshAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, fmt.Errorf("read movie metadata failed")
	}
	if overview != nil {
		metadata.Overview = *overview
	}
	if imdbID != nil {
		metadata.IMDBID = *imdbID
	}
	if releaseDate != nil {
		metadata.ReleaseDate = *releaseDate
	}
	if poster != nil {
		metadata.PosterURL = *poster
	}
	if backdrop != nil {
		metadata.BackdropURL = *backdrop
	}
	if trailerVFYouTubeKey != nil {
		metadata.TrailerVFYouTubeKey = *trailerVFYouTubeKey
	}
	if trailerVOYouTubeKey != nil {
		metadata.TrailerVOYouTubeKey = *trailerVOYouTubeKey
	}
	if metadata.Genres == nil {
		metadata.Genres = []string{}
	}
	return metadata, true, nil
}

func (s *PostgresStore) SaveDecision(ctx context.Context, match Match) error {
	if err := validateMatch(match); err != nil {
		return err
	}
	candidates, err := json.Marshal(match.Candidates)
	if err != nil {
		return fmt.Errorf("encode movie candidates failed")
	}
	var metadataID, score any
	if match.Status == StatusMatched {
		metadataID, score = match.MetadataMovieID, match.Score
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin movie decision failed")
	}
	defer rollback(tx)
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	if merged, err := isLocallyMerged(ctx, tx, match.SourceProvider, match.SourceMovieID); err != nil {
		return err
	} else if merged {
		return ErrLocalMovieConflict
	}
	var priorStatus string
	priorErr := tx.QueryRow(ctx, `SELECT status FROM movie_matches
WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider=$3 FOR UPDATE`, match.SourceProvider, match.SourceMovieID, match.MetadataProvider).Scan(&priorStatus)
	if priorErr != nil && !errors.Is(priorErr, pgx.ErrNoRows) {
		return fmt.Errorf("lock prior movie decision failed")
	}
	command, err := tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$10)
ON CONFLICT (source_provider, source_movie_id, metadata_provider) DO UPDATE SET status=EXCLUDED.status, metadata_movie_id=EXCLUDED.metadata_movie_id, score=EXCLUDED.score, normalized_source_title=EXCLUDED.normalized_source_title, source_runtime_minutes=EXCLUDED.source_runtime_minutes, candidates=EXCLUDED.candidates, evaluated_at=EXCLUDED.evaluated_at, retry_after=EXCLUDED.retry_after, updated_at=EXCLUDED.updated_at
WHERE NOT (movie_matches.status='rejected' AND movie_matches.normalized_source_title=EXCLUDED.normalized_source_title AND movie_matches.source_runtime_minutes=EXCLUDED.source_runtime_minutes)`, match.SourceProvider, match.SourceMovieID, match.MetadataProvider, match.Status, metadataID, score, match.NormalizedSourceTitle, match.SourceRuntimeMinutes, candidates, match.EvaluatedAt, match.RetryAfter)
	if err != nil {
		return fmt.Errorf("write movie decision failed")
	}
	if command.RowsAffected() == 0 {
		return nil
	}
	if priorStatus == StatusMatched {
		if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
			return fmt.Errorf("reconcile public movies after matched decision change: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
			return fmt.Errorf("publish enrichment version failed")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit movie decision failed")
	}
	return nil
}

func (s *PostgresStore) Publish(ctx context.Context, match Match, metadata Metadata) error {
	if match.Status != StatusMatched || match.MetadataMovieID != metadata.ProviderMovieID {
		return fmt.Errorf("match and metadata disagree")
	}
	if err := validateMatch(match); err != nil {
		return err
	}
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin enrichment publication failed")
	}
	defer rollback(tx)
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	if merged, err := isLocallyMerged(ctx, tx, match.SourceProvider, match.SourceMovieID); err != nil {
		return err
	} else if merged {
		return ErrLocalMovieConflict
	}
	candidates, _ := json.Marshal(match.Candidates)
	command, err := tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$10)
ON CONFLICT (source_provider, source_movie_id, metadata_provider) DO UPDATE SET status=EXCLUDED.status, metadata_movie_id=EXCLUDED.metadata_movie_id, score=EXCLUDED.score, normalized_source_title=EXCLUDED.normalized_source_title, source_runtime_minutes=EXCLUDED.source_runtime_minutes, candidates=EXCLUDED.candidates, evaluated_at=EXCLUDED.evaluated_at, retry_after=EXCLUDED.retry_after, updated_at=EXCLUDED.updated_at
WHERE NOT (movie_matches.status='rejected' AND movie_matches.normalized_source_title=EXCLUDED.normalized_source_title AND movie_matches.source_runtime_minutes=EXCLUDED.source_runtime_minutes)`, match.SourceProvider, match.SourceMovieID, match.MetadataProvider, match.Status, match.MetadataMovieID, match.Score, match.NormalizedSourceTitle, match.SourceRuntimeMinutes, candidates, match.EvaluatedAt, match.RetryAfter)
	if err != nil {
		return fmt.Errorf("write matched decision failed")
	}
	if command.RowsAffected() == 0 {
		return nil
	}
	if err := writeMetadata(ctx, tx, metadata); err != nil {
		return err
	}
	if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
		return fmt.Errorf("reconcile public movies after enrichment publication: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit enrichment publication failed")
	}
	return nil
}
