package publicmoviepg

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// ActivityBreakComplete checks elapsed same-day receipts, not advance coverage.
// The latest acquisition in each cinema day wins; receipt must be in that day too.
func ActivityBreakComplete(ctx context.Context, tx pgx.Tx, theater string, movieID int64, previous, next, detected time.Time) (bool, error) {
	var complete bool
	err := tx.QueryRow(ctx, `WITH bounds AS (
 SELECT $3::date+1 first_date,$4::date-1 last_date,
 (($5::timestamptz AT TIME ZONE 'Europe/Paris')-interval '3 hours')::date today
), latest AS (
 SELECT DISTINCT ON (c.service_date) c.service_date,c.status,c.detected_at
 FROM cinema_activity_coverage c,bounds b
 WHERE c.theater_id=$1 AND c.service_date BETWEEN b.first_date AND b.last_date
 AND c.source_generated_at >= (c.service_date+time '03:00') AT TIME ZONE 'Europe/Paris'
 AND c.source_generated_at < (c.service_date+1+time '03:00') AT TIME ZONE 'Europe/Paris'
 ORDER BY c.service_date,c.source_generated_at DESC,c.detected_at DESC,c.generation DESC
)
SELECT b.last_date-b.first_date+1 >= 28 AND b.last_date < b.today
 AND (SELECT count(*) FROM latest c WHERE c.status='complete'
 AND c.detected_at >= (c.service_date+time '03:00') AT TIME ZONE 'Europe/Paris'
 AND c.detected_at < (c.service_date+1+time '03:00') AT TIME ZONE 'Europe/Paris')=b.last_date-b.first_date+1
 AND NOT EXISTS (
 SELECT 1 FROM screening_history_showtimes h JOIN public_movie_sources s
 ON (s.source_provider,s.source_movie_id)=(h.provider,h.movie_provider_id)
 JOIN public_movies p ON p.id=s.public_movie_id
 WHERE h.theater_id=$1 AND coalesce(p.redirect_to_id,p.id)=$2
 AND h.service_date BETWEEN b.first_date AND b.last_date)
 AND NOT EXISTS (
 SELECT 1 FROM showtimes h JOIN schedule_snapshot v ON v.version=h.generation_id
 JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(h.provider,h.movie_provider_id)
 JOIN public_movies p ON p.id=s.public_movie_id
 WHERE h.theater_id=$1 AND coalesce(p.redirect_to_id,p.id)=$2
 AND h.service_date BETWEEN b.first_date AND b.last_date)
 AND NOT EXISTS (
 SELECT 1 FROM cinema_activity_episode_sources l JOIN public_movie_sources s
 USING(source_provider,source_movie_id) JOIN public_movies p ON p.id=s.public_movie_id
 WHERE l.theater_id=$1 AND coalesce(p.redirect_to_id,p.id)=$2
 AND l.observed_from<=b.last_date AND l.observed_through>=b.first_date)
 FROM bounds b`, theater, movieID, previous, next, detected).Scan(&complete)
	return complete, err
}

type activityIdentityEpisode struct {
	id                             int64
	theater                        string
	movieID                        int64
	kind                           string
	from, through, first, detected time.Time
	previous                       *time.Time
}

// normalizeActivityIdentity only visits cinemas with changed source assignments.
// Immutable anchors and announcement facts survive; losers and their links remain.
func normalizeActivityIdentity(ctx context.Context, tx pgx.Tx, changed []sourceKey) error {
	if len(changed) == 0 {
		return nil
	}
	providers, ids := []string{}, []string{}
	for _, key := range changed {
		providers = append(providers, key.provider)
		ids = append(ids, key.id)
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.theater_id,coalesce(p.redirect_to_id,p.id),e.kind,e.observed_from,e.observed_through,e.first_screening_date,e.detected_at,e.previous_program_end_date
 FROM cinema_activity_episodes e JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(e.anchor_provider,e.anchor_source_movie_id)
 JOIN public_movies p ON p.id=s.public_movie_id
 WHERE e.superseded_by_id IS NULL AND e.theater_id IN (
 SELECT DISTINCT l.theater_id FROM cinema_activity_episode_sources l JOIN unnest($1::text[],$2::text[]) u(provider,id)
 ON (l.source_provider,l.source_movie_id)=(u.provider,u.id))`, providers, ids)
	if err != nil {
		return fmt.Errorf("read activity identity: %w", err)
	}
	episodes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (activityIdentityEpisode, error) {
		var e activityIdentityEpisode
		err := row.Scan(&e.id, &e.theater, &e.movieID, &e.kind, &e.from, &e.through, &e.first, &e.detected, &e.previous)
		return e, err
	})
	if err != nil {
		return err
	}
	sort.Slice(episodes, func(i, j int) bool {
		a, b := episodes[i], episodes[j]
		if a.theater != b.theater {
			return a.theater < b.theater
		}
		if a.movieID != b.movieID {
			return a.movieID < b.movieID
		}
		if !a.from.Equal(b.from) {
			return a.from.Before(b.from)
		}
		return a.id < b.id
	})
	var run *activityIdentityEpisode
	for i := range episodes {
		e := episodes[i]
		if run == nil || run.theater != e.theater || run.movieID != e.movieID {
			run = &e
			continue
		}
		separate := false
		if e.kind == "return_to_program" && e.previous != nil && run.through.Equal(*e.previous) && e.from.After(run.through) {
			separate, err = ActivityBreakComplete(ctx, tx, e.theater, e.movieID, *e.previous, e.first, e.detected)
			if err != nil {
				return err
			}
		}
		if separate {
			run = &e
			continue
		}
		survivor, loser := run.id, e.id
		if survivor > loser {
			survivor, loser = loser, survivor
		}
		if err = mergeActivityEpisodes(ctx, tx, survivor, loser); err != nil {
			return err
		}
		if e.from.Before(run.from) {
			run.from = e.from
		}
		if e.through.After(run.through) {
			run.through = e.through
		}
		run.id = survivor
	}
	return nil
}

func mergeActivityEpisodes(ctx context.Context, tx pgx.Tx, survivor, loser int64) error {
	queries := []string{
		`UPDATE cinema_activity_episodes e SET observed_from=least(e.observed_from,l.observed_from),observed_through=greatest(e.observed_through,l.observed_through) FROM cinema_activity_episodes l WHERE e.id=$1 AND l.id=$2`,
		`INSERT INTO cinema_activity_episode_sources (theater_id,episode_id,source_provider,source_movie_id,observed_from,observed_through,first_seen_at,last_seen_at)
 SELECT theater_id,$1,source_provider,source_movie_id,observed_from,observed_through,first_seen_at,last_seen_at FROM cinema_activity_episode_sources WHERE episode_id=$2
 ON CONFLICT (theater_id,episode_id,source_provider,source_movie_id) DO UPDATE SET observed_from=least(cinema_activity_episode_sources.observed_from,excluded.observed_from),observed_through=greatest(cinema_activity_episode_sources.observed_through,excluded.observed_through),first_seen_at=least(cinema_activity_episode_sources.first_seen_at,excluded.first_seen_at),last_seen_at=greatest(cinema_activity_episode_sources.last_seen_at,excluded.last_seen_at)`,
		`UPDATE cinema_activity_episodes SET superseded_by_id=$1 WHERE id=$2 OR superseded_by_id=$2`,
	}
	for _, q := range queries {
		if _, err := tx.Exec(ctx, q, survivor, loser); err != nil {
			return fmt.Errorf("normalize activity identity: %w", err)
		}
	}
	return nil
}
