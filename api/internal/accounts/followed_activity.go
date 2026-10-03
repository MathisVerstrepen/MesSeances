package accounts

import (
	"context"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/schedule"
)

type FollowedActivityQuery struct {
	Limit  int
	Cursor string
}

type FollowedActivityTheater struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	City     string `json:"city"`
	Provider string `json:"provider"`
}

type FollowedActivityItem struct {
	schedule.ActivityEvent
	Theater FollowedActivityTheater `json:"theater"`
}

type FollowedActivityCoverage struct {
	InitializedTheaterCount int    `json:"initialized_theater_count"`
	Completeness            string `json:"completeness"`
	Bootstrap               string `json:"bootstrap"`
	ReturnMinimumBreakDays  int    `json:"return_minimum_break_days"`
}

type FollowedActivityView struct {
	Username             string                   `json:"username"`
	FollowsRevision      string                   `json:"follows_revision"`
	FollowedTheaterCount int                      `json:"followed_theater_count"`
	GeneratedAt          time.Time                `json:"generated_at"`
	Timezone             string                   `json:"timezone"`
	Coverage             FollowedActivityCoverage `json:"coverage"`
	Items                []FollowedActivityItem   `json:"items"`
	Limit                int                      `json:"limit"`
	NextCursor           *string                  `json:"next_cursor"`
}

func NormalizeFollowedActivityQuery(q FollowedActivityQuery) (FollowedActivityQuery, error) {
	if q.Limit < 0 || q.Limit > 100 || len(q.Cursor) > 1024 || !utf8.ValidString(q.Cursor) {
		return q, ErrInvalidInput
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	return q, nil
}

func (s *Service) FollowedActivity(ctx context.Context, rawSession string, query FollowedActivityQuery) (FollowedActivityView, error) {
	query, err := NormalizeFollowedActivityQuery(query)
	if err != nil {
		return FollowedActivityView{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return FollowedActivityView{}, ErrUnavailable
	}
	select {
	case s.followedActivityGate <- struct{}{}:
		defer func() { <-s.followedActivityGate }()
	default:
		return FollowedActivityView{}, ErrUnavailable
	}
	result := FollowedActivityView{Timezone: schedule.Timezone, Limit: query.Limit, Items: []FollowedActivityItem{}, Coverage: FollowedActivityCoverage{Completeness: "unknown", Bootstrap: "baseline", ReturnMinimumBreakDays: schedule.ActivityReturnMinimumBreakDays}}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='2s'; SET LOCAL TIME ZONE 'UTC'; SET LOCAL plan_cache_mode=force_custom_plan`); err != nil {
			return ErrUnavailable
		}
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		follows, _, err := readTheaterFollows(ctx, tx, a)
		if err != nil {
			return err
		}
		result.Username, result.FollowsRevision, result.FollowedTheaterCount = follows.Username, follows.Revision, len(follows.TheaterIDs)
		var upper, lastID int64
		var lastAt *time.Time
		if query.Cursor != "" {
			p, err := s.decodeFollowedActivityCursor(query.Cursor, a.id)
			if err != nil {
				return err
			}
			if p.FollowsRevision != follows.Revision {
				return ErrTheaterFollowsChanged
			}
			upper, _ = schedule.ActivityID(p.UpperID)
			lastID, _ = schedule.ActivityID(p.LastID)
			at, _ := time.Parse(time.RFC3339Nano, p.LastDetectedAt)
			lastAt = &at
		}
		if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&result.GeneratedAt); err != nil {
			return ErrUnavailable
		}
		if len(follows.TheaterIDs) == 0 {
			return nil
		}
		rows, err := tx.Query(ctx, followedActivitySQL, follows.TheaterIDs, upper, lastAt, lastID, query.Limit+1, result.GeneratedAt)
		if err != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var item FollowedActivityItem
			var id, movieID *int64
			// The outer metadata row exists even when the page has no events.
			var kind, firstDate, title, theaterID, slug, name, city, provider *string
			var detected, updated *time.Time
			err := rows.Scan(&upper, &result.Coverage.InitializedTheaterCount, &id, &kind, &detected, &firstDate, &item.PreviousProgramEndDate, &movieID, &title, &item.Movie.PosterURL, &updated, &item.NextShowtimeDate, &theaterID, &slug, &name, &city, &provider)
			if err != nil {
				return ErrUnavailable
			}
			if id == nil {
				continue
			}
			if kind == nil || detected == nil || firstDate == nil || movieID == nil || title == nil || updated == nil || theaterID == nil || slug == nil || name == nil || city == nil || provider == nil {
				return ErrUnavailable
			}
			item.EventID, item.Type, item.DetectedAt, item.FirstScreeningDate = strconv.FormatInt(*id, 10), *kind, detected.UTC(), *firstDate
			item.Movie.Slug, item.Movie.Title, item.Movie.UpdatedAt = "film-"+strconv.FormatInt(*movieID, 10), *title, updated.UTC()
			item.HasUpcomingShowtimes = item.NextShowtimeDate != nil
			item.Theater = FollowedActivityTheater{ID: *theaterID, Slug: *slug, Name: *name, City: *city, Provider: *provider}
			result.Items = append(result.Items, item)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		if result.Coverage.InitializedTheaterCount > 0 {
			result.Coverage.Completeness = "partial"
		}
		if len(result.Items) > query.Limit {
			result.Items = result.Items[:query.Limit]
			cursor := s.encodeFollowedActivityCursor(a.id, follows.Revision, upper, result.Items[len(result.Items)-1].ActivityEvent)
			result.NextCursor = &cursor
		}
		return nil
	})
	if err != nil {
		return FollowedActivityView{}, err
	}
	if ctx.Err() != nil {
		return FollowedActivityView{}, ErrUnavailable
	}
	result.GeneratedAt = result.GeneratedAt.UTC()
	return result, nil
}

// Both upper-ID capture and page selection share one statement snapshot. Indexed
// per-theater probes and the materialized global page bound metadata/showtime work.
const followedActivitySQL = `WITH followed AS MATERIALIZED (SELECT unnest($1::text[]) theater_id),
 bounds AS MATERIALIZED (
 SELECT CASE WHEN $2::bigint>0 THEN $2 ELSE coalesce((SELECT max(probe.id) FROM followed f
 LEFT JOIN LATERAL (SELECT id FROM cinema_activity_episodes WHERE theater_id=f.theater_id
 AND superseded_by_id IS NULL AND kind<>'baseline' ORDER BY id DESC LIMIT 1) probe ON true),0) END upper_id,
 (SELECT count(*) FROM cinema_activity_state WHERE theater_id=ANY($1::text[])) initialized_count),
 page AS MATERIALIZED (
 SELECT e.* FROM followed f CROSS JOIN bounds b CROSS JOIN LATERAL (
 SELECT * FROM cinema_activity_episodes WHERE theater_id=f.theater_id AND superseded_by_id IS NULL
 AND kind<>'baseline' AND id<=b.upper_id
 AND ($3::timestamptz IS NULL OR (detected_at,id)<($3::timestamptz,$4::bigint))
 ORDER BY detected_at DESC,id DESC LIMIT $5) e
 ORDER BY e.detected_at DESC,e.id DESC LIMIT $5),
 projected AS (
 SELECT e.id,e.kind,e.detected_at,e.first_screening_date::text,e.previous_program_end_date::text,c.id movie_id,
 CASE WHEN o.title_overridden THEN o.title ELSE c.title END title,
 CASE WHEN o.poster_url_overridden THEN o.poster_url ELSE c.poster_url END poster_url,c.updated_at,
 (SELECT min(h.service_date)::text FROM showtimes h JOIN schedule_snapshot v ON v.version=h.generation_id
 JOIN public_movie_sources a ON (a.source_provider,a.source_movie_id)=(h.provider,h.movie_provider_id)
 JOIN public_movies m ON m.id=a.public_movie_id WHERE h.theater_id=e.theater_id AND h.start_time>$6 AND coalesce(m.redirect_to_id,m.id)=c.id) next_date,
 e.theater_id,coalesce(t.slug,ht.slug) slug,coalesce(t.name,ht.name) name,
 coalesce(t.city,ht.city) city,coalesce(t.provider,ht.provider) provider
 FROM page e JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(e.anchor_provider,e.anchor_source_movie_id)
 JOIN public_movies p ON p.id=s.public_movie_id LEFT JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id) AND c.redirect_to_id IS NULL
 LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=c.id
 JOIN screening_history_theaters ht ON ht.id=e.theater_id
 LEFT JOIN theaters t ON t.id=e.theater_id AND t.generation_id=(SELECT version FROM schedule_snapshot))
 SELECT b.upper_id,b.initialized_count,p.* FROM bounds b LEFT JOIN projected p ON true ORDER BY p.detected_at DESC,p.id DESC`
