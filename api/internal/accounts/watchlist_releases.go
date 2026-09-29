package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/tmdb"
)

type WatchlistReleaseProvider interface {
	FrenchReleaseEvidence(context.Context, int64) (tmdb.ReleaseEvidence, error)
}

// Select the observation before its nullable date. A newer verified negative
// must suppress an older positive; legacy upcoming nulls are not observations.
// p must be the current canonical identity, never its saved redirect tombstone.
const watchlistReleaseObservation = ` LEFT JOIN LATERAL (
 SELECT observation.french_release_date, observation.verified_at FROM (
  SELECT u.french_release_date,u.verified_at,1 priority FROM tmdb_upcoming_movies u
  WHERE u.tmdb_id=p.confirmed_tmdb_id AND (u.french_release_date IS NOT NULL OR u.assessed_at IS NOT NULL)
  UNION ALL
  SELECT c.french_release_date,c.verified_at,0 priority FROM tmdb_french_release_cache c
  WHERE c.tmdb_id=p.confirmed_tmdb_id AND c.verified_at IS NOT NULL
 ) observation ORDER BY observation.verified_at DESC, observation.priority DESC LIMIT 1
) fr ON true `

// UNION terminates cycles and deduplicates saved identities across all accounts.
// Only the bounded due result is materialized in Go, not the membership backlog.
const watchlistReleaseCandidates = `WITH RECURSIVE resolved(id,redirect_to_id) AS (
 SELECT p.id,p.redirect_to_id FROM public_movies p
 WHERE EXISTS(SELECT 1 FROM account_watchlist_items i WHERE i.public_movie_id=p.id)
 UNION SELECT p.id,p.redirect_to_id FROM resolved r JOIN public_movies p ON p.id=r.redirect_to_id
), identities AS (
 SELECT DISTINCT p.confirmed_tmdb_id FROM resolved r JOIN public_movies p ON p.id=r.id
 WHERE r.redirect_to_id IS NULL AND p.confirmed_tmdb_id IS NOT NULL
), due AS (
 SELECT p.confirmed_tmdb_id,fr.verified_at FROM identities p
 ` + watchlistReleaseObservation + `
 LEFT JOIN tmdb_french_release_cache c ON c.tmdb_id=p.confirmed_tmdb_id
 WHERE (c.retry_after IS NULL OR c.retry_after <= $1)
 AND (c.attempt_revision IS NULL OR c.attempt_revision < 9007199254740991)
 AND (fr.verified_at IS NULL OR fr.verified_at +
  CASE WHEN fr.french_release_date IS NULL THEN interval '7 days' ELSE interval '30 days' END <= $1)
) `

type WatchlistReleaseResult struct {
	Attempted   int
	Verified    int
	Unavailable int
}

// SweepWatchlistReleases is optional public-metadata maintenance. It never runs
// in a user request and never changes membership or public publication clocks.
func (s *Service) SweepWatchlistReleases(ctx context.Context) (WatchlistReleaseResult, error) {
	var result WatchlistReleaseResult
	if s.watchlistReleaseProvider == nil || !s.watchlistReleaseMu.TryLock() {
		return result, nil
	}
	defer s.watchlistReleaseMu.Unlock()
	if s.now().Before(s.watchlistReleasePausedUntil) {
		return result, nil
	}
	release, err := s.admitWatchlistExternal()
	if err != nil {
		return result, nil
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ids, err := s.dueWatchlistReleases(ctx)
	if err != nil {
		return result, err
	}
	providerCtx, providerCancel := context.WithTimeout(ctx, 5*time.Second)
	defer providerCancel()
	for _, id := range ids {
		if providerCtx.Err() != nil {
			break
		}
		revision, err := s.claimWatchlistRelease(providerCtx, id)
		if err != nil {
			return result, err
		}
		if revision == 0 {
			continue
		}
		if providerCtx.Err() != nil {
			break
		}
		result.Attempted++
		evidence, fetchErr := s.watchlistReleaseProvider.FrenchReleaseEvidence(providerCtx, id)
		if fetchErr == nil {
			var normalized tmdb.ReleaseEvidence
			normalized, fetchErr = tmdb.NormalizeFrenchReleases(evidence.Rows)
			if fetchErr == nil && normalized.FrenchReleaseDate != evidence.FrenchReleaseDate {
				fetchErr = ErrUnavailable
			}
			evidence = normalized
		}
		if fetchErr != nil {
			result.Unavailable++
		} else {
			result.Verified++
		}
		if errors.Is(fetchErr, tmdb.ErrStop) {
			s.watchlistReleasePausedUntil = s.now().Add(15 * time.Minute)
			break
		}
		if err := s.finishWatchlistRelease(ctx, id, revision, evidence, fetchErr); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *Service) dueWatchlistReleases(ctx context.Context) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ids := make([]int64, 0, 10)
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, watchlistReleaseCandidates+`SELECT confirmed_tmdb_id FROM due ORDER BY verified_at ASC NULLS FIRST,confirmed_tmdb_id LIMIT 10`, s.now().UTC())
		if err != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if rows.Scan(&id) != nil {
				return ErrUnavailable
			}
			ids = append(ids, id)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	return ids, err
}

func (s *Service) claimWatchlistRelease(ctx context.Context, id int64) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// One constant HMAC key shares admission across accounts and replicas. Lost
	// claims may consume quota, but provider calls can never bypass it.
	if err := s.quota(ctx, "watchlist_release_fetch", "global", false); err != nil {
		return 0, err
	}
	var revision int64
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		err := tx.QueryRow(ctx, watchlistReleaseCandidates+`
 INSERT INTO tmdb_french_release_cache(tmdb_id,retry_after,attempt_revision)
 SELECT confirmed_tmdb_id,$3,1 FROM due WHERE confirmed_tmdb_id=$2
 ON CONFLICT(tmdb_id) DO UPDATE SET retry_after=EXCLUDED.retry_after,
 attempt_revision=tmdb_french_release_cache.attempt_revision+1
 WHERE tmdb_french_release_cache.retry_after <= $1
 AND tmdb_french_release_cache.attempt_revision < 9007199254740991
 RETURNING attempt_revision`, now, id, now.Add(15*time.Minute)).Scan(&revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return revision, err
}

func (s *Service) finishWatchlistRelease(ctx context.Context, id, revision int64, evidence tmdb.ReleaseEvidence, fetchErr error) error {
	if fetchErr != nil && !errors.Is(fetchErr, tmdb.ErrNotFound) {
		return nil // preserve observation and finite reservation on unavailable evidence
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		if errors.Is(fetchErr, tmdb.ErrNotFound) {
			_, err := tx.Exec(ctx, `UPDATE tmdb_french_release_cache SET retry_after=$3 WHERE tmdb_id=$1 AND attempt_revision=$2`, id, revision, now.Add(24*time.Hour))
			if err != nil {
				return ErrUnavailable
			}
			return nil
		}
		rows, err := json.Marshal(evidence.Rows)
		if err != nil {
			return ErrUnavailable
		}
		ttl := 30 * 24 * time.Hour
		if evidence.FrenchReleaseDate == "" {
			ttl = 7 * 24 * time.Hour
		}
		_, err = tx.Exec(ctx, `UPDATE tmdb_french_release_cache SET french_release_date=NULLIF($3,'')::date,
 french_releases=$4,verified_at=$5,retry_after=$6 WHERE tmdb_id=$1 AND attempt_revision=$2`, id, revision, evidence.FrenchReleaseDate, rows, now, now.Add(ttl))
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
}
