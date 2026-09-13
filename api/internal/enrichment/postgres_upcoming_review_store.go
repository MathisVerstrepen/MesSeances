package enrichment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const upcomingReviewFrom = ` FROM tmdb_upcoming_movies upcoming
JOIN public_movies movie ON movie.id=upcoming.public_movie_id AND movie.redirect_to_id IS NULL
LEFT JOIN public_movie_metadata_overrides override ON override.public_movie_id=movie.id `
const upcomingReviewTitle = `CASE WHEN override.title_overridden THEN override.title ELSE movie.title END`

// Match canonical movie reads, including an explicitly cleared poster override.
const upcomingReviewPoster = `CASE WHEN override.poster_url_overridden THEN override.poster_url ELSE movie.poster_url END`
const upcomingReviewColumns = `upcoming.tmdb_id,movie.id::text,` + upcomingReviewTitle + `,` + upcomingReviewPoster + `,upcoming.french_release_date::text,upcoming.active,upcoming.assessed_at,upcoming.french_releases,upcoming.reason_codes,upcoming.decision,upcoming.review_revision`

func scanUpcomingReview(row pgx.Row, now time.Time) (AdminUpcomingMovie, error) {
	var item AdminUpcomingMovie
	if err := row.Scan(&item.TMDBID, &item.PublicMovieID, &item.Title, &item.PosterURL, &item.FrenchReleaseDate, &item.Active, &item.AssessedAt, &item.FrenchReleases, &item.ReasonCodes, &item.Decision, &item.Revision); err != nil {
		return item, err
	}
	completeUpcomingReview(&item, now)
	return item, nil
}

func (s *PostgresStore) UpcomingReviews(ctx context.Context, q UpcomingReviewQuery, now time.Time) (UpcomingReviewList, error) {
	if !ValidUpcomingReviewQuery(q) {
		return UpcomingReviewList{}, ErrUpcomingReviewInvalid
	}
	// Count and page share one snapshot, including out-of-range pages.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return UpcomingReviewList{}, fmt.Errorf("begin upcoming reviews failed")
	}
	defer rollback(tx)
	where := map[string]string{
		"all":                "true",
		"needs_review":       "upcoming.assessed_at IS NOT NULL AND cardinality(upcoming.reason_codes)>0 AND upcoming.decision='unreviewed'",
		"pending_assessment": "upcoming.assessed_at IS NULL",
		"approved":           "upcoming.decision='approved'",
		"excluded":           "upcoming.decision='excluded'",
	}[q.Filter]
	// strpos is literal substring matching, so %, _ and backslash are not LIKE patterns.
	where += ` AND ($1='' OR strpos(lower(` + upcomingReviewTitle + `),lower($1))>0 OR upcoming.tmdb_id::text=$1)`
	result := UpcomingReviewList{Items: []AdminUpcomingMovie{}, Limit: q.Limit, Offset: q.Offset}
	if err := tx.QueryRow(ctx, "SELECT count(*)"+upcomingReviewFrom+"WHERE "+where, q.Search).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count upcoming reviews failed")
	}
	rows, err := tx.Query(ctx, "SELECT "+upcomingReviewColumns+upcomingReviewFrom+"WHERE "+where+` ORDER BY ('non_theatrical_before_or_same_day'=ANY(upcoming.reason_codes)) DESC,upcoming.french_release_date ASC NULLS LAST,lower(`+upcomingReviewTitle+`),upcoming.tmdb_id LIMIT $2 OFFSET $3`, q.Search, q.Limit, q.Offset)
	if err != nil {
		return result, fmt.Errorf("read upcoming reviews failed")
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanUpcomingReview(rows, now)
		if err != nil {
			return result, fmt.Errorf("read upcoming review failed")
		}
		result.Items = append(result.Items, item)
	}
	if rows.Err() != nil {
		return result, fmt.Errorf("read upcoming reviews failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("finish upcoming reviews failed")
	}
	return result, nil
}

func (s *PostgresStore) SetUpcomingDecision(ctx context.Context, id int64, input UpcomingDecisionUpdate, now time.Time) (AdminUpcomingMovie, error) {
	var result AdminUpcomingMovie
	if !ValidUpcomingDecision(id, input) {
		return result, ErrUpcomingReviewInvalid
	}
	err := s.withWriteTransaction(ctx, "begin upcoming decision failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		item, err := scanUpcomingReview(tx.QueryRow(ctx, "SELECT "+upcomingReviewColumns+upcomingReviewFrom+"WHERE upcoming.tmdb_id=$1 FOR UPDATE OF upcoming", id), now)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUpcomingReviewNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("read upcoming decision failed")
		}
		if item.Revision != input.ExpectedRevision {
			return nil, ErrUpcomingReviewConflict
		}
		result = item
		if item.Decision == input.Decision {
			return nil, nil
		}
		if _, err := tx.Exec(ctx, "UPDATE tmdb_upcoming_movies SET decision=$2,review_revision=review_revision+1 WHERE tmdb_id=$1", id, input.Decision); err != nil {
			return nil, fmt.Errorf("write upcoming decision failed")
		}
		result.Decision = input.Decision
		result.Revision++
		completeUpcomingReview(&result, now)
		return &writeFinalization{advanceVersion: true, mapCommitError: func(error) error { return fmt.Errorf("commit upcoming decision failed") }}, nil
	})
	if err != nil {
		return AdminUpcomingMovie{}, err
	}
	return result, nil
}
