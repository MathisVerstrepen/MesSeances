package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) PendingMatches(ctx context.Context, limit, offset int) ([]PendingMatch, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, fmt.Errorf("invalid review pagination")
	}
	rows, err := s.pool.Query(ctx, `SELECT m.provider, m.provider_id, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), COALESCE(mm.status, 'review_required'), COALESCE(mm.candidates, '[]'::jsonb), COALESCE(mm.evaluated_at, CURRENT_TIMESTAMP)
FROM movies m LEFT JOIN movie_matches mm ON mm.source_provider=m.provider AND mm.source_movie_id=m.provider_id AND mm.metadata_provider='tmdb'
WHERE (mm.status IS NULL OR mm.status IN ('review_required', 'unmatched', 'rejected'))
  AND NOT EXISTS (SELECT 1 FROM local_movie_group_members lmgm WHERE lmgm.source_provider=m.provider AND lmgm.source_movie_id=m.provider_id)
ORDER BY (mm.evaluated_at IS NULL), mm.evaluated_at, m.provider, m.provider_id LIMIT $1 OFFSET $2`, limit, offset)
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
WHERE mm.source_provider=$1 AND mm.source_movie_id=$2 AND mm.metadata_provider='tmdb'`, sourceProvider, sourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime, &raw, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		if candidateID <= 0 {
			return Candidate{}, 0, ErrReviewConflict
		}
		if movieErr := s.pool.QueryRow(ctx, `SELECT title, runtime_minutes FROM movies WHERE provider=$1 AND provider_id=$2`, sourceProvider, sourceMovieID).Scan(&currentTitle, &currentRuntime); errors.Is(movieErr, pgx.ErrNoRows) {
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

func lockEnrichmentVersion(ctx context.Context, tx pgx.Tx) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true FOR UPDATE").Scan(&version); err != nil || version < 0 || version == math.MaxInt64 {
		return 0, fmt.Errorf("read enrichment version failed")
	}
	return version, nil
}

func lockReview(ctx context.Context, tx pgx.Tx, sourceProvider, sourceMovieID string) (string, string, int, []byte, string, int, bool, error) {
	var status, normalizedTitle, currentTitle string
	var sourceRuntime, currentRuntime int
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT mm.status, mm.normalized_source_title, mm.source_runtime_minutes, mm.candidates, m.title, m.runtime_minutes
FROM movie_matches mm JOIN movies m ON m.provider=mm.source_provider AND m.provider_id=mm.source_movie_id
WHERE mm.source_provider=$1 AND mm.source_movie_id=$2 AND mm.metadata_provider='tmdb' FOR UPDATE OF mm, m`, sourceProvider, sourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime, &raw, &currentTitle, &currentRuntime)
	if errors.Is(err, pgx.ErrNoRows) {
		if movieErr := tx.QueryRow(ctx, `SELECT title, runtime_minutes FROM movies WHERE provider=$1 AND provider_id=$2 FOR SHARE`, sourceProvider, sourceMovieID).Scan(&currentTitle, &currentRuntime); errors.Is(movieErr, pgx.ErrNoRows) {
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

func (s *PostgresStore) LocalMovieGroups(ctx context.Context, limit, offset int) ([]LocalMovieGroup, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrLocalMovieInvalid
	}
	rows, err := s.pool.Query(ctx, `WITH selected_groups AS (
    SELECT id, primary_source_provider, primary_source_movie_id
    FROM local_movie_groups ORDER BY id LIMIT $1 OFFSET $2
)
SELECT g.id, g.primary_source_provider, g.primary_source_movie_id,
       member.source_provider, member.source_movie_id,
       movie.provider IS NOT NULL, movie.title, movie.runtime_minutes, movie.poster_url
FROM selected_groups g
JOIN local_movie_group_members member ON member.local_movie_id=g.id
LEFT JOIN movies movie ON movie.provider=member.source_provider AND movie.provider_id=member.source_movie_id
ORDER BY g.id, member.source_provider, member.source_movie_id`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read local movie groups failed")
	}
	defer rows.Close()
	groups := make([]LocalMovieGroup, 0)
	for rows.Next() {
		var id int64
		var primary, source LocalMovieSource
		var member LocalMovieMember
		if err := rows.Scan(&id, &primary.SourceProvider, &primary.SourceMovieID, &source.SourceProvider, &source.SourceMovieID, &member.Available, &member.SourceTitle, &member.SourceRuntimeMinutes, &member.SourcePosterURL); err != nil {
			return nil, fmt.Errorf("read local movie groups failed")
		}
		if len(groups) == 0 || groups[len(groups)-1].ID != id {
			groups = append(groups, LocalMovieGroup{ID: id, LocalMovieID: LocalMovieID(id), Primary: primary, Members: []LocalMovieMember{}})
		}
		member.LocalMovieSource = source
		groups[len(groups)-1].Members = append(groups[len(groups)-1].Members, member)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read local movie groups failed")
	}
	for index := range groups {
		prepareLocalMovieGroup(&groups[index])
	}
	return groups, nil
}

func (s *PostgresStore) MergeLocalMovies(ctx context.Context, members []LocalMovieSource, primary LocalMovieSource) (LocalMovieGroup, error) {
	if err := validateLocalMovieMerge(members, primary); err != nil {
		return LocalMovieGroup{}, err
	}
	members = append([]LocalMovieSource(nil), members...)
	sort.Slice(members, func(i, j int) bool { return localMovieSourceKey(members[i]) < localMovieSourceKey(members[j]) })
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LocalMovieGroup{}, fmt.Errorf("begin local movie merge failed")
	}
	defer rollback(tx)
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return LocalMovieGroup{}, err
	}
	group := LocalMovieGroup{Primary: primary, Members: make([]LocalMovieMember, 0, len(members))}
	for _, source := range members {
		member := LocalMovieMember{LocalMovieSource: source}
		if err := tx.QueryRow(ctx, `SELECT title, runtime_minutes, poster_url FROM movies WHERE provider=$1 AND provider_id=$2 FOR UPDATE`, source.SourceProvider, source.SourceMovieID).Scan(&member.SourceTitle, &member.SourceRuntimeMinutes, &member.SourcePosterURL); errors.Is(err, pgx.ErrNoRows) {
			return LocalMovieGroup{}, ErrLocalMovieConflict
		} else if err != nil {
			return LocalMovieGroup{}, fmt.Errorf("lock local movie source failed")
		}
		member.Available = true
		var status, normalizedTitle string
		var sourceRuntime int
		err := tx.QueryRow(ctx, `SELECT status, normalized_source_title, source_runtime_minutes FROM movie_matches WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb' FOR UPDATE`, source.SourceProvider, source.SourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return LocalMovieGroup{}, fmt.Errorf("lock local movie decision failed")
		}
		if err == nil && (status == StatusMatched || status != StatusReviewRequired && status != StatusUnmatched && status != StatusRejected || normalizedTitle != NormalizeTitle(*member.SourceTitle) || sourceRuntime != *member.SourceRuntimeMinutes) {
			return LocalMovieGroup{}, ErrLocalMovieConflict
		}
		if merged, err := isLocallyMerged(ctx, tx, source.SourceProvider, source.SourceMovieID); err != nil {
			return LocalMovieGroup{}, err
		} else if merged {
			return LocalMovieGroup{}, ErrLocalMovieConflict
		}
		group.Members = append(group.Members, member)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO local_movie_groups (primary_source_provider, primary_source_movie_id) VALUES ($1,$2) RETURNING id`, primary.SourceProvider, primary.SourceMovieID).Scan(&group.ID); err != nil {
		return LocalMovieGroup{}, localMovieWriteError("create local movie group failed", err)
	}
	for _, source := range members {
		if _, err := tx.Exec(ctx, `INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,$2,$3)`, group.ID, source.SourceProvider, source.SourceMovieID); err != nil {
			return LocalMovieGroup{}, localMovieWriteError("write local movie member failed", err)
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return LocalMovieGroup{}, fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return LocalMovieGroup{}, localMovieWriteError("commit local movie merge failed", err)
	}
	prepareLocalMovieGroup(&group)
	return group, nil
}

func (s *PostgresStore) UnmergeLocalMovie(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrLocalMovieInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin local movie unmerge failed")
	}
	defer rollback(tx)
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	var lockedID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM local_movie_groups WHERE id=$1 FOR UPDATE", id).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
		return ErrLocalMovieNotFound
	} else if err != nil {
		return fmt.Errorf("lock local movie group failed")
	}
	if _, err := tx.Exec(ctx, "DELETE FROM local_movie_groups WHERE id=$1", lockedID); err != nil {
		return fmt.Errorf("delete local movie group failed")
	}
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit local movie unmerge failed")
	}
	return nil
}

func (s *PostgresStore) IsLocallyMerged(ctx context.Context, sourceProvider, sourceMovieID string) (bool, error) {
	if !validSourceIdentity(sourceProvider, sourceMovieID) {
		return false, ErrLocalMovieInvalid
	}
	return isLocallyMerged(ctx, s.pool, sourceProvider, sourceMovieID)
}

type localMovieQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func isLocallyMerged(ctx context.Context, query localMovieQueryRow, sourceProvider, sourceMovieID string) (bool, error) {
	var exists bool
	if err := query.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM local_movie_group_members WHERE source_provider=$1 AND source_movie_id=$2)`, sourceProvider, sourceMovieID).Scan(&exists); err != nil {
		return false, fmt.Errorf("read local movie membership failed")
	}
	return exists, nil
}

func localMovieWriteError(message string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "23503" || postgresError.Code == "23514") {
		return ErrLocalMovieConflict
	}
	return fmt.Errorf("%s", message)
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
