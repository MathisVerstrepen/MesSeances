package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/publicmoviepg"
)

func (s *PostgresStore) PendingMatches(ctx context.Context, limit, offset int) ([]PendingMatch, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, fmt.Errorf("invalid review pagination")
	}
	rows, err := s.pool.Query(ctx, `SELECT m.provider, m.provider_id, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), COALESCE(mm.status, 'review_required'), COALESCE(mm.candidates, '[]'::jsonb), COALESCE(mm.evaluated_at, CURRENT_TIMESTAMP)
FROM movies m JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
LEFT JOIN movie_matches mm ON mm.source_provider=m.provider AND mm.source_movie_id=m.provider_id AND mm.metadata_provider='tmdb'
WHERE (mm.status IS NULL OR mm.status IN ('review_required', 'unmatched', 'rejected'))
  AND NOT EXISTS (SELECT 1 FROM local_movie_group_members lmgm WHERE lmgm.source_provider=m.provider AND lmgm.source_movie_id=m.provider_id)
ORDER BY LOWER(m.title), m.provider, m.provider_id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read pending movie matches failed")
	}
	defer rows.Close()
	items := make([]PendingMatch, 0)
	for rows.Next() {
		var item PendingMatch
		var candidates []byte
		if err := rows.Scan(&item.SourceProvider, &item.SourceMovieID, &item.SourceTitle, &item.SourceRuntimeMinutes, &item.SourcePosterURL, &item.Status, &candidates, &item.EvaluatedAt); err != nil || json.Unmarshal(candidates, &item.Candidates) != nil {
			return nil, fmt.Errorf("read pending movie matches failed")
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read pending movie matches failed")
	}
	return items, nil
}

func (s *PostgresStore) ReviewCandidate(ctx context.Context, sourceProvider, sourceMovieID string, candidateID int64) (Candidate, int, error) {
	merged, err := s.IsLocallyMerged(ctx, sourceProvider, sourceMovieID)
	if err != nil {
		return Candidate{}, 0, err
	}
	if merged {
		return Candidate{}, 0, ErrReviewConflict
	}
	var status, normalizedTitle, currentTitle string
	var sourceRuntime, currentRuntime int
	var raw []byte
	err = s.pool.QueryRow(ctx, `SELECT mm.status, mm.normalized_source_title, mm.source_runtime_minutes, mm.candidates, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider=mm.source_provider AND m.provider_id=mm.source_movie_id
JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
WHERE mm.source_provider=$1 AND mm.source_movie_id=$2 AND mm.metadata_provider='tmdb'`, sourceProvider, sourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime, &raw, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		if candidateID <= 0 {
			return Candidate{}, 0, ErrReviewConflict
		}
		if movieErr := s.pool.QueryRow(ctx, `SELECT m.title, m.runtime_minutes FROM movies m JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version WHERE m.provider=$1 AND m.provider_id=$2`, sourceProvider, sourceMovieID).Scan(&currentTitle, &currentRuntime); errors.Is(movieErr, pgx.ErrNoRows) {
			return Candidate{}, 0, ErrReviewNotFound
		} else if movieErr != nil {
			return Candidate{}, 0, fmt.Errorf("read review candidate failed")
		}
		return Candidate{ID: candidateID, Score: 1}, currentRuntime, nil
	}
	if err != nil {
		return Candidate{}, 0, fmt.Errorf("read review candidate failed")
	}
	candidate, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, candidateID)
	if !ok {
		return Candidate{}, 0, ErrReviewConflict
	}
	return candidate, sourceRuntime, nil
}

func (s *PostgresStore) ApproveReview(ctx context.Context, sourceProvider, sourceMovieID string, candidateID int64, metadata Metadata, fallbackRuntime int, now time.Time) error {
	if metadata.ProviderMovieID != candidateID || fallbackRuntime < 0 || fallbackRuntime != 0 && (!validRuntimeMinutes(fallbackRuntime) || metadata.RuntimeMinutes != fallbackRuntime) || now.IsZero() {
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
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	if merged, err := isLocallyMerged(ctx, tx, sourceProvider, sourceMovieID); err != nil {
		return err
	} else if merged {
		return ErrReviewConflict
	}
	status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, persisted, err := lockReview(ctx, tx, sourceProvider, sourceMovieID)
	if err != nil {
		return err
	}
	candidate, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, candidateID)
	if !ok {
		return ErrReviewConflict
	}
	if fallbackRuntime != 0 && sourceRuntime != fallbackRuntime {
		return ErrReviewConflict
	}
	if err := writeMetadata(ctx, tx, metadata); err != nil {
		return err
	}
	if persisted {
		if _, err := tx.Exec(ctx, `UPDATE movie_matches SET status='matched', metadata_movie_id=$3, score=$4, evaluated_at=$5, retry_after=$6, updated_at=$5
WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb'`, sourceProvider, sourceMovieID, candidateID, candidate.Score, now, metadata.RefreshAfter); err != nil {
			return fmt.Errorf("write reviewed match failed")
		}
	} else {
		command, err := tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,'tmdb','matched',$3,$4,$5,$6,$7,$8,$9,$8) ON CONFLICT DO NOTHING`, sourceProvider, sourceMovieID, candidateID, candidate.Score, normalizedTitle, sourceRuntime, raw, now, metadata.RefreshAfter)
		if err != nil {
			return fmt.Errorf("write reviewed match failed")
		}
		if command.RowsAffected() == 0 {
			return ErrReviewConflict
		}
	}
	if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
		return fmt.Errorf("reconcile public movies after review approval: %w", err)
	}
	return commitReview(ctx, tx, version)
}

func (s *PostgresStore) RejectReview(ctx context.Context, sourceProvider, sourceMovieID string, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("invalid review rejection")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin review rejection failed")
	}
	defer rollback(tx)
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	if merged, err := isLocallyMerged(ctx, tx, sourceProvider, sourceMovieID); err != nil {
		return err
	} else if merged {
		return ErrReviewConflict
	}
	status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, persisted, err := lockReview(ctx, tx, sourceProvider, sourceMovieID)
	if err != nil {
		return err
	}
	if _, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, 0); !ok {
		return ErrReviewConflict
	}
	if persisted {
		if _, err := tx.Exec(ctx, `UPDATE movie_matches SET status='rejected', metadata_movie_id=NULL, score=NULL, evaluated_at=$3, retry_after=$3, updated_at=$3
WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb'`, sourceProvider, sourceMovieID, now); err != nil {
			return fmt.Errorf("write rejected match failed")
		}
	} else {
		command, err := tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,'tmdb','rejected',NULL,NULL,$3,$4,$5,$6,$6,$6) ON CONFLICT DO NOTHING`, sourceProvider, sourceMovieID, normalizedTitle, sourceRuntime, raw, now)
		if err != nil {
			return fmt.Errorf("write rejected match failed")
		}
		if command.RowsAffected() == 0 {
			return ErrReviewConflict
		}
	}
	if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
		return fmt.Errorf("reconcile public movies after review rejection: %w", err)
	}
	return commitReview(ctx, tx, version)
}

func validReviewCandidate(status, normalizedTitle string, sourceRuntime int, raw []byte, currentTitle string, currentRuntime int, candidateID int64) (Candidate, bool) {
	assignable := status == StatusReviewRequired || status == StatusUnmatched
	if !assignable || !validRuntimeMinutes(sourceRuntime) || NormalizeTitle(currentTitle) != normalizedTitle || currentRuntime != sourceRuntime {
		return Candidate{}, false
	}
	var candidates []Candidate
	if json.Unmarshal(raw, &candidates) != nil {
		return Candidate{}, false
	}
	if candidateID == 0 {
		return Candidate{}, true
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

func lockReview(ctx context.Context, tx pgx.Tx, sourceProvider, sourceMovieID string) (string, string, int, []byte, string, int, bool, error) {
	var status, normalizedTitle, currentTitle string
	var sourceRuntime, currentRuntime int
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT mm.status, mm.normalized_source_title, mm.source_runtime_minutes, mm.candidates, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider=mm.source_provider AND m.provider_id=mm.source_movie_id
JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
WHERE mm.source_provider=$1 AND mm.source_movie_id=$2 AND mm.metadata_provider='tmdb' FOR UPDATE OF mm, m`, sourceProvider, sourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime, &raw, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		if movieErr := tx.QueryRow(ctx, `SELECT m.title, m.runtime_minutes FROM movies m JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version WHERE m.provider=$1 AND m.provider_id=$2 FOR SHARE OF m`, sourceProvider, sourceMovieID).Scan(&currentTitle, &currentRuntime); errors.Is(movieErr, pgx.ErrNoRows) {
			return "", "", 0, nil, "", 0, false, ErrReviewNotFound
		} else if movieErr != nil {
			return "", "", 0, nil, "", 0, false, fmt.Errorf("lock reviewed match failed")
		}
		return StatusReviewRequired, NormalizeTitle(currentTitle), currentRuntime, []byte("[]"), currentTitle, currentRuntime, false, nil
	}
	if err != nil {
		return "", "", 0, nil, "", 0, false, fmt.Errorf("lock reviewed match failed")
	}
	return status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, true, nil
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
