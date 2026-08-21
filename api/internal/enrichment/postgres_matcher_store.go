package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

func (s *PostgresStore) Metadata(ctx context.Context, provider string, movieID int64, locale string) (Metadata, bool, error) {
	var metadata Metadata
	var overview, releaseDate, poster, backdrop *string
	err := s.pool.QueryRow(ctx, `SELECT provider, provider_movie_id, locale, provider_title, localized_title, overview, release_date::text, poster_url, backdrop_url, runtime_minutes, genres, fetched_at, refresh_after
FROM movie_metadata_cache WHERE provider=$1 AND provider_movie_id=$2 AND locale=$3`, provider, movieID, locale).Scan(&metadata.Provider, &metadata.ProviderMovieID, &metadata.Locale, &metadata.ProviderTitle, &metadata.LocalizedTitle, &overview, &releaseDate, &poster, &backdrop, &metadata.RuntimeMinutes, &metadata.Genres, &metadata.FetchedAt, &metadata.RefreshAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, fmt.Errorf("read movie metadata failed")
	}
	if overview != nil {
		metadata.Overview = *overview
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
	if _, err := lockEnrichmentVersion(ctx, tx); err != nil {
		return err
	}
	if merged, err := isLocallyMerged(ctx, tx, match.SourceProvider, match.SourceMovieID); err != nil {
		return err
	} else if merged {
		return ErrLocalMovieConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$10)
ON CONFLICT (source_provider, source_movie_id, metadata_provider) DO UPDATE SET status=EXCLUDED.status, metadata_movie_id=EXCLUDED.metadata_movie_id, score=EXCLUDED.score, normalized_source_title=EXCLUDED.normalized_source_title, source_runtime_minutes=EXCLUDED.source_runtime_minutes, candidates=EXCLUDED.candidates, evaluated_at=EXCLUDED.evaluated_at, retry_after=EXCLUDED.retry_after, updated_at=EXCLUDED.updated_at
WHERE NOT (movie_matches.status='rejected' AND movie_matches.normalized_source_title=EXCLUDED.normalized_source_title AND movie_matches.source_runtime_minutes=EXCLUDED.source_runtime_minutes)`, match.SourceProvider, match.SourceMovieID, match.MetadataProvider, match.Status, metadataID, score, match.NormalizedSourceTitle, match.SourceRuntimeMinutes, candidates, match.EvaluatedAt, match.RetryAfter)
	if err != nil {
		return fmt.Errorf("write movie decision failed")
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
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit enrichment publication failed")
	}
	return nil
}
