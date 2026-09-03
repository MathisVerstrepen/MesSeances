package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) PendingMatches(ctx context.Context, filter PendingMatchFilter, search string, limit, offset int) ([]PendingMatch, error) {
	if (filter != PendingMatchFilterUnresolved && filter != PendingMatchFilterRejected && filter != PendingMatchFilterMatched) || limit < 1 || limit > 100 || offset < 0 || search != "" && (filter != PendingMatchFilterMatched || strings.TrimSpace(search) != search || !utf8.ValidString(search) || utf8.RuneCountInString(search) > 1024) {
		return nil, fmt.Errorf("invalid review pagination")
	}
	var exactTMDBID *int64
	if id, ok := exactTMDBSearchID(search); ok {
		exactTMDBID = &id
	}
	rows, err := s.pool.Query(ctx, `SELECT m.provider, m.provider_id, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), COALESCE(mm.status, 'review_required'), COALESCE(mm.candidates, '[]'::jsonb), COALESCE(mm.evaluated_at, CURRENT_TIMESTAMP),
       mm.updated_at, mm.metadata_movie_id, mm.score, cache.localized_title, cache.provider_title, cache.runtime_minutes, cache.poster_url
FROM movies m JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
LEFT JOIN movie_matches mm ON mm.source_provider=m.provider AND mm.source_movie_id=m.provider_id AND mm.metadata_provider='tmdb'
LEFT JOIN movie_metadata_cache cache ON cache.provider='tmdb' AND cache.provider_movie_id=mm.metadata_movie_id AND cache.locale='fr-FR'
WHERE (($1='unresolved' AND (mm.status IS NULL OR mm.status IN ('review_required', 'unmatched')))
    OR ($1='rejected' AND mm.status='rejected')
    OR ($1='matched' AND mm.status='matched'))
  AND NOT EXISTS (SELECT 1 FROM local_movie_group_members lmgm WHERE lmgm.source_provider=m.provider AND lmgm.source_movie_id=m.provider_id)
  AND ($4::text='' OR strpos(lower(m.title), lower($4)) > 0
       OR strpos(lower(COALESCE(cache.localized_title, '')), lower($4)) > 0
       OR strpos(lower(COALESCE(cache.provider_title, '')), lower($4)) > 0
       OR strpos(lower(m.provider_id), lower($4)) > 0
       OR ($5::bigint IS NOT NULL AND mm.metadata_movie_id=$5))
ORDER BY LOWER(m.title), m.provider, m.provider_id LIMIT $2 OFFSET $3`, string(filter), limit, offset, search, exactTMDBID)
	if err != nil {
		return nil, fmt.Errorf("read pending movie matches failed")
	}
	defer rows.Close()
	items := make([]PendingMatch, 0)
	for rows.Next() {
		var item PendingMatch
		var candidates []byte
		var updatedAt *time.Time
		var metadataMovieID *int64
		var score *float64
		var localizedTitle, providerTitle, posterURL *string
		var runtime *int
		if err := rows.Scan(&item.SourceProvider, &item.SourceMovieID, &item.SourceTitle, &item.SourceRuntimeMinutes, &item.SourcePosterURL, &item.Status, &candidates, &item.EvaluatedAt, &updatedAt, &metadataMovieID, &score, &localizedTitle, &providerTitle, &runtime, &posterURL); err != nil || json.Unmarshal(candidates, &item.Candidates) != nil {
			return nil, fmt.Errorf("read pending movie matches failed")
		}
		if item.Status == StatusMatched {
			if updatedAt == nil || metadataMovieID == nil || score == nil {
				return nil, fmt.Errorf("read pending movie matches failed")
			}
			utc := updatedAt.UTC()
			item.UpdatedAt = &utc
			current := Candidate{ID: *metadataMovieID, Title: fmt.Sprintf("TMDB #%d", *metadataMovieID)}
			current.Score = *score
			if localizedTitle != nil {
				current.Title = *localizedTitle
			}
			if providerTitle != nil {
				current.OriginalTitle = *providerTitle
			}
			if runtime != nil {
				current.Runtime = *runtime
			}
			if posterURL != nil {
				current.PosterURL = *posterURL
			}
			item.CurrentMatch = &current
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read pending movie matches failed")
	}
	return items, nil
}

func exactTMDBSearchID(search string) (int64, bool) {
	if search == "" || search[0] < '1' || search[0] > '9' {
		return 0, false
	}
	id, err := strconv.ParseInt(search, 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != search {
		return 0, false
	}
	return id, true
}

func (s *PostgresStore) CorrectionSource(ctx context.Context, sourceProvider, sourceMovieID string, replacementID int64, expectedUpdatedAt time.Time) (int, error) {
	if replacementID <= 0 || expectedUpdatedAt.IsZero() {
		return 0, ErrReviewConflict
	}
	merged, err := isLocallyMerged(ctx, s.pool, sourceProvider, sourceMovieID)
	if err != nil {
		return 0, err
	}
	if merged {
		return 0, ErrReviewConflict
	}
	var status, normalizedTitle, currentTitle string
	var metadataMovieID int64
	var sourceRuntime, currentRuntime int
	var updatedAt time.Time
	err = s.pool.QueryRow(ctx, `SELECT mm.status, mm.metadata_movie_id, mm.normalized_source_title, mm.source_runtime_minutes, mm.updated_at, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider=mm.source_provider AND m.provider_id=mm.source_movie_id
JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
WHERE mm.source_provider=$1 AND mm.source_movie_id=$2 AND mm.metadata_provider='tmdb'`, sourceProvider, sourceMovieID).Scan(&status, &metadataMovieID, &normalizedTitle, &sourceRuntime, &updatedAt, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrReviewNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read correction source failed")
	}
	if !validCorrectionSource(status, metadataMovieID, replacementID, normalizedTitle, sourceRuntime, updatedAt, currentTitle, currentRuntime, expectedUpdatedAt, 0) {
		return 0, ErrReviewConflict
	}
	return sourceRuntime, nil
}

func (s *PostgresStore) CorrectReview(ctx context.Context, sourceProvider, sourceMovieID string, replacementID int64, expectedUpdatedAt time.Time, metadata Metadata, fallbackRuntime int, now time.Time) error {
	if !validCorrectionRequest(replacementID, expectedUpdatedAt, metadata, fallbackRuntime, now) {
		return fmt.Errorf("invalid review correction")
	}
	return s.withWriteTransaction(ctx, "begin review correction failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		if merged, err := isLocallyMerged(ctx, tx, sourceProvider, sourceMovieID); err != nil {
			return nil, err
		} else if merged {
			return nil, ErrReviewConflict
		}
		var status, normalizedTitle, currentTitle string
		var metadataMovieID int64
		var sourceRuntime, currentRuntime int
		var updatedAt time.Time
		err := tx.QueryRow(ctx, `SELECT mm.status, mm.metadata_movie_id, mm.normalized_source_title, mm.source_runtime_minutes, mm.updated_at, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider=mm.source_provider AND m.provider_id=mm.source_movie_id
JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version
WHERE mm.source_provider=$1 AND mm.source_movie_id=$2 AND mm.metadata_provider='tmdb' FOR UPDATE OF mm, m`, sourceProvider, sourceMovieID).Scan(&status, &metadataMovieID, &normalizedTitle, &sourceRuntime, &updatedAt, &currentTitle, &currentRuntime)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrReviewNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("lock correction source failed")
		}
		if !validCorrectionSource(status, metadataMovieID, replacementID, normalizedTitle, sourceRuntime, updatedAt, currentTitle, currentRuntime, expectedUpdatedAt, fallbackRuntime) {
			return nil, ErrReviewConflict
		}
		if err := writeMetadata(ctx, tx, metadata); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE movie_matches SET metadata_movie_id=$3, score=1, evaluated_at=$4, retry_after=$5, updated_at=$4
WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb'`, sourceProvider, sourceMovieID, replacementID, now, metadata.RefreshAfter); err != nil {
			return nil, fmt.Errorf("write corrected match failed")
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after review correction",
			advanceVersion: true,
			mapCommitError: func(error) error { return fmt.Errorf("commit reviewed match failed") },
		}, nil
	})
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
	if !validApprovalRequest(candidateID, metadata, fallbackRuntime, now) {
		return fmt.Errorf("invalid review approval")
	}
	return s.withWriteTransaction(ctx, "begin review approval failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		if merged, err := isLocallyMerged(ctx, tx, sourceProvider, sourceMovieID); err != nil {
			return nil, err
		} else if merged {
			return nil, ErrReviewConflict
		}
		status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, persisted, err := lockReview(ctx, tx, sourceProvider, sourceMovieID)
		if err != nil {
			return nil, err
		}
		candidate, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, candidateID)
		if !ok {
			return nil, ErrReviewConflict
		}
		if fallbackRuntime != 0 && sourceRuntime != fallbackRuntime {
			return nil, ErrReviewConflict
		}
		if err := writeMetadata(ctx, tx, metadata); err != nil {
			return nil, err
		}
		if persisted {
			if _, err := tx.Exec(ctx, `UPDATE movie_matches SET status='matched', metadata_movie_id=$3, score=$4, evaluated_at=$5, retry_after=$6, updated_at=$5
WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb'`, sourceProvider, sourceMovieID, candidateID, candidate.Score, now, metadata.RefreshAfter); err != nil {
				return nil, fmt.Errorf("write reviewed match failed")
			}
		} else {
			command, err := tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,'tmdb','matched',$3,$4,$5,$6,$7,$8,$9,$8) ON CONFLICT DO NOTHING`, sourceProvider, sourceMovieID, candidateID, candidate.Score, normalizedTitle, sourceRuntime, raw, now, metadata.RefreshAfter)
			if err != nil {
				return nil, fmt.Errorf("write reviewed match failed")
			}
			if command.RowsAffected() == 0 {
				return nil, ErrReviewConflict
			}
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after review approval",
			advanceVersion: true,
			mapCommitError: func(error) error { return fmt.Errorf("commit reviewed match failed") },
		}, nil
	})
}

func validCorrectionRequest(replacementID int64, expectedUpdatedAt time.Time, metadata Metadata, fallbackRuntime int, now time.Time) bool {
	return replacementID > 0 &&
		!expectedUpdatedAt.IsZero() &&
		metadata.ProviderMovieID == replacementID &&
		validReviewRuntime(metadata.RuntimeMinutes, fallbackRuntime) &&
		!now.IsZero() &&
		validateMetadata(metadata) == nil
}

func validApprovalRequest(candidateID int64, metadata Metadata, fallbackRuntime int, now time.Time) bool {
	return metadata.ProviderMovieID == candidateID &&
		validReviewRuntime(metadata.RuntimeMinutes, fallbackRuntime) &&
		!now.IsZero() &&
		validateMetadata(metadata) == nil
}

func validReviewRuntime(metadataRuntime, fallbackRuntime int) bool {
	return fallbackRuntime >= 0 &&
		(fallbackRuntime == 0 || validRuntimeMinutes(fallbackRuntime) && metadataRuntime == fallbackRuntime)
}

func validCorrectionSource(status string, metadataMovieID, replacementID int64, normalizedTitle string, sourceRuntime int, updatedAt time.Time, currentTitle string, currentRuntime int, expectedUpdatedAt time.Time, fallbackRuntime int) bool {
	return status == StatusMatched &&
		metadataMovieID != replacementID &&
		NormalizeTitle(currentTitle) == normalizedTitle &&
		currentRuntime == sourceRuntime &&
		updatedAt.Equal(expectedUpdatedAt) &&
		(fallbackRuntime == 0 || sourceRuntime == fallbackRuntime)
}

func (s *PostgresStore) RejectReview(ctx context.Context, sourceProvider, sourceMovieID string, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("invalid review rejection")
	}
	return s.withWriteTransaction(ctx, "begin review rejection failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		if merged, err := isLocallyMerged(ctx, tx, sourceProvider, sourceMovieID); err != nil {
			return nil, err
		} else if merged {
			return nil, ErrReviewConflict
		}
		status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, persisted, err := lockReview(ctx, tx, sourceProvider, sourceMovieID)
		if err != nil {
			return nil, err
		}
		if _, ok := validReviewCandidate(status, normalizedTitle, sourceRuntime, raw, currentTitle, currentRuntime, 0); !ok {
			return nil, ErrReviewConflict
		}
		if persisted {
			if _, err := tx.Exec(ctx, `UPDATE movie_matches SET status='rejected', metadata_movie_id=NULL, score=NULL, evaluated_at=$3, retry_after=$3, updated_at=$3
WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb'`, sourceProvider, sourceMovieID, now); err != nil {
				return nil, fmt.Errorf("write rejected match failed")
			}
		} else {
			command, err := tx.Exec(ctx, `INSERT INTO movie_matches (source_provider, source_movie_id, metadata_provider, status, metadata_movie_id, score, normalized_source_title, source_runtime_minutes, candidates, evaluated_at, retry_after, updated_at)
VALUES ($1,$2,'tmdb','rejected',NULL,NULL,$3,$4,$5,$6,$6,$6) ON CONFLICT DO NOTHING`, sourceProvider, sourceMovieID, normalizedTitle, sourceRuntime, raw, now)
			if err != nil {
				return nil, fmt.Errorf("write rejected match failed")
			}
			if command.RowsAffected() == 0 {
				return nil, ErrReviewConflict
			}
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after review rejection",
			advanceVersion: true,
			mapCommitError: func(error) error { return fmt.Errorf("commit reviewed match failed") },
		}, nil
	})
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
