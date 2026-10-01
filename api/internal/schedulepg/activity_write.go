package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/publicmoviepg"
	"messeances/api/internal/schedule"
)

type activitySource struct {
	theater, provider, id string
	movieID               int64
	from, through         time.Time
	eligible              *time.Time
}

// Read before the retained-history upsert: new rows must not count as familiarity.
func activityFamiliarity(ctx context.Context, tx pgx.Tx, generation int64, providers []string) ([]activitySource, error) {
	rows, err := tx.Query(ctx, `WITH familiar AS (
 SELECT h.theater_id,h.provider,h.movie_provider_id,min(h.service_date) first_date,max(h.service_date) last_date
 FROM screening_history_showtimes h
 WHERE h.theater_id IN (SELECT id FROM theaters WHERE generation_id=$1 AND provider=ANY($2))
 GROUP BY h.theater_id,h.provider,h.movie_provider_id
 UNION ALL
 SELECT n.theater_id,n.provider,n.movie_provider_id,min(n.service_date),max(n.service_date)
 FROM showtimes n WHERE n.generation_id=$1 AND n.provider=ANY($2) AND EXISTS (
 SELECT 1 FROM screening_history_showtimes h WHERE h.theater_id=n.theater_id AND h.provider=n.provider
 AND (h.provider_showing_id=n.provider_showing_id AND h.service_date=n.service_date OR h.start_time=n.start_time AND h.service_date=n.service_date AND h.room=n.room)
 AND h.movie_provider_id<>n.movie_provider_id)
 GROUP BY n.theater_id,n.provider,n.movie_provider_id)
 SELECT f.theater_id,f.provider,f.movie_provider_id,coalesce(p.redirect_to_id,p.id),min(first_date),max(last_date)
 FROM familiar f JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(f.provider,f.movie_provider_id)
 JOIN public_movies p ON p.id=s.public_movie_id
 GROUP BY f.theater_id,f.provider,f.movie_provider_id,coalesce(p.redirect_to_id,p.id)`, generation, providers)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (activitySource, error) {
		var s activitySource
		err := row.Scan(&s.theater, &s.provider, &s.id, &s.movieID, &s.from, &s.through)
		return s, err
	})
}

func retainActivity(ctx context.Context, tx pgx.Tx, generation int64, datasets []schedule.Dataset, received time.Time, familiar []activitySource) error {
	for _, data := range datasets {
		if err := retainActivityCoverage(ctx, tx, generation, data, received); err != nil {
			return err
		}
		for _, theater := range data.Theaters {
			var known bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cinema_activity_state WHERE theater_id=$1)`, theater.ID).Scan(&known); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO cinema_activity_state (theater_id,history_started_at,last_publication_at,source_generated_at)
 VALUES ($1,$2,$2,$3) ON CONFLICT (theater_id) DO UPDATE SET last_publication_at=excluded.last_publication_at,source_generated_at=excluded.source_generated_at
 `, theater.ID, received, data.GeneratedAt)
			if err != nil {
				return fmt.Errorf("retain activity state: %w", err)
			}
			// Admit old positive sources once, silently. Bounds of existing episodes
			// are not reseeded from all-time history on every publication.
			for _, group := range activityGroups(familiar, theater.ID) {
				missing, err := activityMissingSources(ctx, tx, group)
				if err != nil {
					return err
				}
				if len(missing) > 0 {
					if err := observeActivityGroup(ctx, tx, generation, received, missing, true); err != nil {
						return err
					}
				}
			}
			positive, err := activityPositiveSources(ctx, tx, generation, theater.ID, received)
			if err != nil {
				return err
			}
			for _, group := range activityGroups(positive, theater.ID) {
				if err := observeActivityGroup(ctx, tx, generation, received, group, !known); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func retainActivityCoverage(ctx context.Context, tx pgx.Tx, generation int64, data schedule.Dataset, received time.Time) error {
	for _, c := range data.Coverage {
		if _, err := tx.Exec(ctx, `INSERT INTO cinema_activity_coverage (provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, string(data.Provider), generation, c.TheaterID, c.ServiceDate, c.Status, c.Basis, data.GeneratedAt, received); err != nil {
			return fmt.Errorf("retain activity coverage: %w", err)
		}
	}
	// Each known cinema gets a current-day unknown unless this exact day has
	// acquisition evidence. Omission never masquerades as an empty schedule.
	_, err := tx.Exec(ctx, `INSERT INTO cinema_activity_coverage (provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at)
 SELECT provider,$2,id,(($4::timestamptz AT TIME ZONE 'Europe/Paris')-interval '3 hours')::date,'unknown','unproven',$3,$4
 FROM screening_history_theaters WHERE provider=$1 ON CONFLICT DO NOTHING`, string(data.Provider), generation, data.GeneratedAt, received)
	return err
}

func activityPositiveSources(ctx context.Context, tx pgx.Tx, generation int64, theater string, received time.Time) ([]activitySource, error) {
	rows, err := tx.Query(ctx, `SELECT h.theater_id,h.provider,h.movie_provider_id,coalesce(p.redirect_to_id,p.id),min(h.service_date),max(h.service_date),min(h.service_date) FILTER(WHERE h.start_time>$3)
 FROM showtimes h JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(h.provider,h.movie_provider_id)
 JOIN public_movies p ON p.id=s.public_movie_id WHERE h.generation_id=$1 AND h.theater_id=$2
 GROUP BY h.theater_id,h.provider,h.movie_provider_id,coalesce(p.redirect_to_id,p.id)`, generation, theater, received)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (activitySource, error) {
		var s activitySource
		err := row.Scan(&s.theater, &s.provider, &s.id, &s.movieID, &s.from, &s.through, &s.eligible)
		return s, err
	})
}

func activityGroups(sources []activitySource, theater string) [][]activitySource {
	byMovie := map[int64][]activitySource{}
	for _, s := range sources {
		if s.theater == theater {
			byMovie[s.movieID] = append(byMovie[s.movieID], s)
		}
	}
	ids := []int64{}
	for id := range byMovie {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	groups := [][]activitySource{}
	for _, id := range ids {
		g := byMovie[id]
		sort.Slice(g, func(i, j int) bool {
			if g[i].provider != g[j].provider {
				return g[i].provider < g[j].provider
			}
			return g[i].id < g[j].id
		})
		groups = append(groups, g)
	}
	return groups
}

func activityMissingSources(ctx context.Context, tx pgx.Tx, sources []activitySource) ([]activitySource, error) {
	missing := []activitySource{}
	for _, s := range sources {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cinema_activity_episode_sources WHERE theater_id=$1 AND source_provider=$2 AND source_movie_id=$3)`, s.theater, s.provider, s.id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			missing = append(missing, s)
		}
	}
	return missing, nil
}

func observeActivityGroup(ctx context.Context, tx pgx.Tx, generation int64, received time.Time, group []activitySource, silent bool) error {
	anchor := group[0]
	from, through := anchor.from, anchor.through
	var first *time.Time
	for _, s := range group {
		if s.from.Before(from) {
			from = s.from
		}
		if s.through.After(through) {
			through = s.through
		}
		if s.eligible != nil && (first == nil || s.eligible.Before(*first)) {
			first = s.eligible
		}
	}
	// Only an anchored surviving run of this canonical film is extendable.
	// Detached split sources remain familiar via links, but do not move old events.
	var episode int64
	var previous time.Time
	err := tx.QueryRow(ctx, `SELECT e.id,e.observed_through FROM cinema_activity_episodes e
 JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(e.anchor_provider,e.anchor_source_movie_id)
 JOIN public_movies p ON p.id=s.public_movie_id WHERE e.theater_id=$1 AND e.superseded_by_id IS NULL AND coalesce(p.redirect_to_id,p.id)=$2
 ORDER BY e.observed_through DESC,e.id DESC LIMIT 1`, anchor.theater, anchor.movieID).Scan(&episode, &previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	kind := "baseline"
	var previousArg, breakFrom, breakThrough any
	if episode == 0 {
		var linkedEnd *time.Time
		if err := tx.QueryRow(ctx, `SELECT max(l.observed_through) FROM cinema_activity_episode_sources l
 JOIN public_movie_sources s USING(source_provider,source_movie_id) JOIN public_movies p ON p.id=s.public_movie_id
 WHERE l.theater_id=$1 AND coalesce(p.redirect_to_id,p.id)=$2`, anchor.theater, anchor.movieID).Scan(&linkedEnd); err != nil {
			return err
		}
		if !silent && first != nil && linkedEnd == nil {
			kind = "added_to_program"
		} else if !silent && first != nil && linkedEnd != nil && from.After(*linkedEnd) {
			complete, err := publicmoviepg.ActivityBreakComplete(ctx, tx, anchor.theater, anchor.movieID, *linkedEnd, *first, received)
			if err != nil {
				return err
			}
			if complete {
				kind = "return_to_program"
				previousArg = *linkedEnd
				breakFrom = linkedEnd.AddDate(0, 0, 1)
				breakThrough = first.AddDate(0, 0, -1)
			}
		}
	} else if !silent && first != nil && from.After(previous) {
		var unambiguous bool
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (
 SELECT 1 FROM cinema_activity_episode_sources l JOIN public_movie_sources s USING(source_provider,source_movie_id)
 JOIN public_movies p ON p.id=s.public_movie_id WHERE l.episode_id=$1 AND coalesce(p.redirect_to_id,p.id)<>$2)`, episode, anchor.movieID).Scan(&unambiguous); err != nil {
			return err
		}
		if unambiguous {
			complete, err := publicmoviepg.ActivityBreakComplete(ctx, tx, anchor.theater, anchor.movieID, previous, *first, received)
			if err != nil {
				return err
			}
			if complete {
				kind = "return_to_program"
				previousArg = previous
				breakFrom = previous.AddDate(0, 0, 1)
				breakThrough = first.AddDate(0, 0, -1)
				episode = 0
			}
		}
	}
	if episode == 0 {
		date := from
		if first != nil {
			date = *first
		}
		if err := tx.QueryRow(ctx, `INSERT INTO cinema_activity_episodes (theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,previous_program_end_date,observed_from,observed_through,break_from,break_through,detecting_generation)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, anchor.theater, anchor.provider, anchor.id, kind, received, date, previousArg, from, through, breakFrom, breakThrough, generation).Scan(&episode); err != nil {
			return fmt.Errorf("retain activity episode: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE cinema_activity_episodes SET observed_from=least(observed_from,$2),observed_through=greatest(observed_through,$3) WHERE id=$1`, episode, from, through); err != nil {
			return err
		}
	}
	for _, s := range group {
		if _, err := tx.Exec(ctx, `INSERT INTO cinema_activity_episode_sources (theater_id,episode_id,source_provider,source_movie_id,observed_from,observed_through,first_seen_at,last_seen_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$7) ON CONFLICT (theater_id,episode_id,source_provider,source_movie_id)
 DO UPDATE SET observed_from=least(cinema_activity_episode_sources.observed_from,excluded.observed_from),observed_through=greatest(cinema_activity_episode_sources.observed_through,excluded.observed_through),last_seen_at=excluded.last_seen_at`, s.theater, episode, s.provider, s.id, s.from, s.through, received); err != nil {
			return err
		}
	}
	return nil
}
