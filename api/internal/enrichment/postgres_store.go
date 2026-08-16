package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) PendingMatches(ctx context.Context, limit, offset int) ([]PendingMatch, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, fmt.Errorf("invalid review pagination")
	}
	rows, err := s.pool.Query(ctx, `SELECT mm.source_movie_id, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), mm.status, mm.candidates, mm.evaluated_at
FROM movie_matches mm JOIN movies m ON m.provider_id=mm.source_movie_id
WHERE mm.source_provider='ugc' AND mm.metadata_provider='tmdb' AND mm.status IN ('review_required', 'unmatched')
ORDER BY mm.evaluated_at, mm.source_movie_id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read pending movie matches failed")
	}
	defer rows.Close()
	items := make([]PendingMatch, 0)
	for rows.Next() {
		var item PendingMatch
		var candidates []byte
		if err := rows.Scan(&item.SourceMovieID, &item.SourceTitle, &item.SourceRuntimeMinutes, &item.SourcePosterURL, &item.Status, &candidates, &item.EvaluatedAt); err != nil || json.Unmarshal(candidates, &item.Candidates) != nil {
			return nil, fmt.Errorf("read pending movie matches failed")
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read pending movie matches failed")
	}
	return items, nil
}

func (s *PostgresStore) ReviewCandidate(ctx context.Context, sourceMovieID string, candidateID int64) (Candidate, error) {
	var status, normalizedTitle, currentTitle string
	var sourceRuntime, currentRuntime int
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT mm.status, mm.normalized_source_title, mm.source_runtime_minutes, mm.candidates, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider_id=mm.source_movie_id
WHERE mm.source_provider='ugc' AND mm.source_movie_id=$1 AND mm.metadata_provider='tmdb'`, sourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime, &raw, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrReviewNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read review candidate failed")
	}
	candidate, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, candidateID)
	if !ok {
		return Candidate{}, ErrReviewConflict
	}
	return candidate, nil
}

func (s *PostgresStore) ApproveReview(ctx context.Context, sourceMovieID string, candidateID int64, metadata Metadata, now time.Time) error {
	if metadata.ProviderMovieID != candidateID || now.IsZero() {
		return fmt.Errorf("invalid review approval")
	}
	if err := validateMetadata(metadata); err != nil {
		return fmt.Errorf("invalid review approval")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin review approval failed")
	}
	defer rollback(tx)
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, err := lockReview(ctx, tx, sourceMovieID)
	if err != nil {
		return err
	}
	candidate, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, candidateID)
	if !ok {
		return ErrReviewConflict
	}
	if err := writeMetadata(ctx, tx, metadata); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE movie_matches SET status='matched', metadata_movie_id=$2, score=$3, evaluated_at=$4, retry_after=$5, updated_at=$4
WHERE source_provider='ugc' AND source_movie_id=$1 AND metadata_provider='tmdb'`, sourceMovieID, candidateID, candidate.Score, now, metadata.RefreshAfter); err != nil {
		return fmt.Errorf("write reviewed match failed")
	}
	return commitReview(ctx, tx, version)
}

func (s *PostgresStore) RejectReview(ctx context.Context, sourceMovieID string, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("invalid review rejection")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin review rejection failed")
	}
	defer rollback(tx)
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, err := lockReview(ctx, tx, sourceMovieID)
	if err != nil {
		return err
	}
	if _, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, 0); !ok {
		return ErrReviewConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE movie_matches SET status='rejected', metadata_movie_id=NULL, score=NULL, evaluated_at=$2, retry_after=$2, updated_at=$2
WHERE source_provider='ugc' AND source_movie_id=$1 AND metadata_provider='tmdb'`, sourceMovieID, now); err != nil {
		return fmt.Errorf("write rejected match failed")
	}
	return commitReview(ctx, tx, version)
}

func validReviewCandidate(status, normalizedTitle string, sourceRuntime int, raw []byte, currentTitle string, currentRuntime int, candidateID int64) (Candidate, bool) {
	assignable := status == StatusReviewRequired || status == StatusUnmatched
	if !assignable || NormalizeTitle(currentTitle) != normalizedTitle || currentRuntime != sourceRuntime {
		return Candidate{}, false
	}
	var candidates []Candidate
	if json.Unmarshal(raw, &candidates) != nil {
		return Candidate{}, false
	}
	if candidateID == 0 {
		return Candidate{}, status == StatusReviewRequired
	}
	if candidateID < 0 {
		return Candidate{}, false
	}
	for _, candidate := range candidates {
		if candidate.ID == candidateID {
			return candidate, true
		}
	}
	return Candidate{ID: candidateID, Score: 1}, true
}

func lockEnrichmentVersion(ctx context.Context, tx pgx.Tx) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true FOR UPDATE").Scan(&version); err != nil || version < 0 || version == math.MaxInt64 {
		return 0, fmt.Errorf("read enrichment version failed")
	}
	return version, nil
}

func lockReview(ctx context.Context, tx pgx.Tx, sourceMovieID string) (string, string, int, []byte, string, int, error) {
	var status, normalizedTitle, currentTitle string
	var sourceRuntime, currentRuntime int
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT mm.status, mm.normalized_source_title, mm.source_runtime_minutes, mm.candidates, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider_id=mm.source_movie_id
WHERE mm.source_provider='ugc' AND mm.source_movie_id=$1 AND mm.metadata_provider='tmdb' FOR UPDATE OF mm`, sourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime, &raw, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", 0, nil, "", 0, ErrReviewNotFound
	}
	if err != nil {
		return "", "", 0, nil, "", 0, fmt.Errorf("lock reviewed match failed")
	}
	return status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, nil
}

func writeMetadata(ctx context.Context, tx pgx.Tx, metadata Metadata) error {
	var overview, releaseDate, poster, backdrop any
	if metadata.Overview != "" {
		overview = metadata.Overview
	}
	if metadata.ReleaseDate != "" {
		releaseDate = metadata.ReleaseDate
	}
	if metadata.PosterURL != "" {
		poster = metadata.PosterURL
	}
	if metadata.BackdropURL != "" {
		backdrop = metadata.BackdropURL
	}
	_, err := tx.Exec(ctx, `INSERT INTO movie_metadata_cache (provider, provider_movie_id, locale, provider_title, localized_title, overview, release_date, poster_url, backdrop_url, runtime_minutes, genres, fetched_at, refresh_after)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (provider, provider_movie_id, locale) DO UPDATE SET provider_title=EXCLUDED.provider_title, localized_title=EXCLUDED.localized_title, overview=EXCLUDED.overview, release_date=EXCLUDED.release_date, poster_url=EXCLUDED.poster_url, backdrop_url=EXCLUDED.backdrop_url, runtime_minutes=EXCLUDED.runtime_minutes, genres=EXCLUDED.genres, fetched_at=EXCLUDED.fetched_at, refresh_after=EXCLUDED.refresh_after`, metadata.Provider, metadata.ProviderMovieID, metadata.Locale, metadata.ProviderTitle, metadata.LocalizedTitle, overview, releaseDate, poster, backdrop, metadata.RuntimeMinutes, metadata.Genres, metadata.FetchedAt, metadata.RefreshAfter)
	if err != nil {
		return fmt.Errorf("write movie metadata failed")
	}
	return nil
}

func commitReview(ctx context.Context, tx pgx.Tx, version int64) error {
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reviewed match failed")
	}
	return nil
}

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
	_, err = s.pool.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$10)
ON CONFLICT (source_provider, source_movie_id, metadata_provider) DO UPDATE SET status=EXCLUDED.status, metadata_movie_id=EXCLUDED.metadata_movie_id, score=EXCLUDED.score, normalized_source_title=EXCLUDED.normalized_source_title, source_runtime_minutes=EXCLUDED.source_runtime_minutes, candidates=EXCLUDED.candidates, evaluated_at=EXCLUDED.evaluated_at, retry_after=EXCLUDED.retry_after, updated_at=EXCLUDED.updated_at
WHERE NOT (movie_matches.status='rejected' AND movie_matches.normalized_source_title=EXCLUDED.normalized_source_title AND movie_matches.source_runtime_minutes=EXCLUDED.source_runtime_minutes)`, match.SourceProvider, match.SourceMovieID, match.MetadataProvider, match.Status, metadataID, score, match.NormalizedSourceTitle, match.SourceRuntimeMinutes, candidates, match.EvaluatedAt, match.RetryAfter)
	if err != nil {
		return fmt.Errorf("write movie decision failed")
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
	var version int64
	if err := tx.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true FOR UPDATE").Scan(&version); err != nil || version < 0 || version == math.MaxInt64 {
		return fmt.Errorf("read enrichment version failed")
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
	var overview, releaseDate, poster, backdrop any
	if metadata.Overview != "" {
		overview = metadata.Overview
	}
	if metadata.ReleaseDate != "" {
		releaseDate = metadata.ReleaseDate
	}
	if metadata.PosterURL != "" {
		poster = metadata.PosterURL
	}
	if metadata.BackdropURL != "" {
		backdrop = metadata.BackdropURL
	}
	if _, err := tx.Exec(ctx, `INSERT INTO movie_metadata_cache (provider, provider_movie_id, locale, provider_title, localized_title, overview, release_date, poster_url, backdrop_url, runtime_minutes, genres, fetched_at, refresh_after)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (provider, provider_movie_id, locale) DO UPDATE SET provider_title=EXCLUDED.provider_title, localized_title=EXCLUDED.localized_title, overview=EXCLUDED.overview, release_date=EXCLUDED.release_date, poster_url=EXCLUDED.poster_url, backdrop_url=EXCLUDED.backdrop_url, runtime_minutes=EXCLUDED.runtime_minutes, genres=EXCLUDED.genres, fetched_at=EXCLUDED.fetched_at, refresh_after=EXCLUDED.refresh_after`, metadata.Provider, metadata.ProviderMovieID, metadata.Locale, metadata.ProviderTitle, metadata.LocalizedTitle, overview, releaseDate, poster, backdrop, metadata.RuntimeMinutes, metadata.Genres, metadata.FetchedAt, metadata.RefreshAfter); err != nil {
		return fmt.Errorf("write movie metadata failed")
	}
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit enrichment publication failed")
	}
	return nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
