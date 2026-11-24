package schedulepg

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/schedule"
)

func (s *Store) TheaterActivity(ctx context.Context, query schedule.TheaterActivityQuery) (schedule.TheaterActivity, error) {
	query, err := schedule.NormalizeTheaterActivityQuery(query)
	if err != nil {
		return schedule.TheaterActivity{}, err
	}
	result := schedule.TheaterActivity{Timezone: schedule.Timezone, Limit: query.Limit, Items: []schedule.ActivityEvent{}, Coverage: schedule.ActivityCoverage{Completeness: "unknown", Bootstrap: "baseline", ReturnMinimumBreakDays: schedule.ActivityReturnMinimumBreakDays}}
	err = s.historyRead(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&result.GeneratedAt); err != nil {
			return err
		}
		if err := activityTheater(ctx, tx, query.Slug, &result.Theater); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT history_started_at,last_publication_at,source_generated_at FROM cinema_activity_state WHERE theater_id=$1`, result.Theater.ID).Scan(&result.Coverage.HistoryStartedAt, &result.Coverage.LastPublicationAt, &result.Coverage.SourceGeneratedAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if result.Coverage.HistoryStartedAt != nil {
			result.Coverage.Completeness = "partial"
		}
		var upper, lastID int64
		var lastAt *time.Time
		if query.Cursor != "" {
			p, _ := schedule.DecodeActivityCursor(query.Cursor, query.Slug)
			upper, _ = schedule.ActivityID(p.UpperID)
			lastID, _ = schedule.ActivityID(p.LastID)
			at, _ := time.Parse(time.RFC3339Nano, p.LastDetectedAt)
			lastAt = &at
		} else {
			if err := tx.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM cinema_activity_episodes WHERE theater_id=$1 AND superseded_by_id IS NULL AND kind<>'baseline'`, result.Theater.ID).Scan(&upper); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, activityItemsSQL, result.Theater.ID, upper, lastAt, lastID, query.Limit+1)
		if err != nil {
			return err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (schedule.ActivityEvent, error) {
			var e schedule.ActivityEvent
			err := scanActivityEvent(row, &e)
			return e, err
		})
		if err != nil {
			return err
		}
		if len(items) > query.Limit {
			items = items[:query.Limit]
			cursor := schedule.EncodeActivityCursor(query.Slug, upper, items[len(items)-1])
			result.NextCursor = &cursor
		}
		result.Items = items
		return nil
	})
	if err != nil {
		return schedule.TheaterActivity{}, err
	}
	return result, nil
}

// ActivityPreview returns a single global page for the selected live theaters.
func (s *Store) ActivityPreview(ctx context.Context, theaterIDs []string) ([]schedule.ActivityPreviewItem, error) {
	items := []schedule.ActivityPreviewItem{}
	if len(theaterIDs) == 0 {
		return items, nil
	}
	err := s.historyRead(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, activityPreviewSQL, theaterIDs, schedule.HomePreviewLimit)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (schedule.ActivityPreviewItem, error) {
			var item schedule.ActivityPreviewItem
			theater := &item.Theater
			err := scanActivityEvent(row, &item.ActivityEvent, &theater.ID, &theater.Slug, &theater.Name, &theater.City, &theater.Provider)
			return item, err
		})
		return err
	})
	return items, err
}

func scanActivityEvent(row pgx.CollectableRow, event *schedule.ActivityEvent, extra ...any) error {
	var id, movieID int64
	fields := []any{&id, &event.Type, &event.DetectedAt, &event.FirstScreeningDate, &event.PreviousProgramEndDate, &movieID, &event.Movie.Title, &event.Movie.PosterURL, &event.Movie.UpdatedAt, &event.NextShowtimeDate}
	if err := row.Scan(append(fields, extra...)...); err != nil {
		return err
	}
	event.EventID = strconv.FormatInt(id, 10)
	event.Movie.Slug = "film-" + strconv.FormatInt(movieID, 10)
	event.HasUpcomingShowtimes = event.NextShowtimeDate != nil
	return nil
}

func activityTheater(ctx context.Context, tx pgx.Tx, slug string, theater *schedule.Theater) error {
	var live bool
	err := tx.QueryRow(ctx, `WITH candidates AS (
 SELECT t.provider,t.id,t.slug,t.name,t.address,t.city,t.postal_code,true live FROM theaters t JOIN schedule_snapshot v ON v.version=t.generation_id WHERE t.slug=$1
 UNION ALL SELECT provider,id,slug,name,address,city,postal_code,false FROM screening_history_theaters WHERE slug=$1)
 SELECT provider,id,slug,name,address,city,postal_code,live FROM candidates ORDER BY live DESC LIMIT 1`, slug).Scan(&theater.Provider, &theater.ID, &theater.Slug, &theater.Name, &theater.Address, &theater.City, &theater.PostalCode, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		return &schedule.NotFoundError{Message: "Cinéma introuvable."}
	}
	if err != nil {
		return err
	}
	theater.AvailableDates = []string{}
	theater.AcceptedPasses = []string{}
	if !live {
		return tx.QueryRow(ctx, `SELECT city_slug,passes FROM screening_history_theaters WHERE id=$1`, theater.ID).Scan(&theater.CitySlug, &theater.AcceptedPasses)
	}
	if err := tx.QueryRow(ctx, `SELECT ARRAY(SELECT service_date::text FROM theater_dates d JOIN schedule_snapshot v ON v.version=d.generation_id WHERE theater_id=$1 ORDER BY service_date),ARRAY(SELECT pass_code FROM theater_passes p JOIN schedule_snapshot v ON v.version=p.generation_id WHERE theater_id=$1 ORDER BY pass_code)`, theater.ID).Scan(&theater.AvailableDates, &theater.AcceptedPasses); err != nil {
		return err
	}
	// Current city collision identities use the same inventory algorithm as snapshots.
	rows, err := tx.Query(ctx, `SELECT id,city FROM theaters t JOIN schedule_snapshot v ON v.version=t.generation_id ORDER BY id`)
	if err != nil {
		return err
	}
	inventory, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (schedule.TheaterRecord, error) {
		var t schedule.TheaterRecord
		err := row.Scan(&t.ID, &t.City)
		return t, err
	})
	if err != nil {
		return err
	}
	for i, c := range schedule.TheaterCityIdentities(inventory) {
		if inventory[i].ID == theater.ID {
			theater.CitySlug = c.Slug
			break
		}
	}
	return nil
}

const activityItemsSQL = `WITH page AS MATERIALIZED (
 SELECT * FROM cinema_activity_episodes WHERE theater_id=$1 AND superseded_by_id IS NULL AND kind<>'baseline' AND id<=$2
 AND ($3::timestamptz IS NULL OR (detected_at,id)<($3::timestamptz,$4::bigint))
 ORDER BY detected_at DESC,id DESC LIMIT $5
)
SELECT ` + activityEventColumns + ` FROM page e ` + activityEventJoins + `
 ORDER BY e.detected_at DESC,e.id DESC`

// Take only the latest indexed events from each theater. Resolve movie metadata
// and check future screenings after limiting the combined page to four rows.
const activityPreviewSQL = `WITH selected AS MATERIALIZED (
 SELECT t.id,t.slug,t.name,t.city,t.provider FROM theaters t JOIN schedule_snapshot v ON v.version=t.generation_id
 WHERE t.id=ANY($1::text[])
), page AS MATERIALIZED (
 SELECT e.* FROM selected t CROSS JOIN LATERAL (
  SELECT * FROM cinema_activity_episodes
  WHERE theater_id=t.id AND superseded_by_id IS NULL AND kind<>'baseline'
  ORDER BY detected_at DESC,id DESC LIMIT $2
 ) e
 ORDER BY e.detected_at DESC,e.id DESC LIMIT $2
)
SELECT ` + activityEventColumns + `,t.id,t.slug,t.name,t.city,t.provider
 FROM page e JOIN selected t ON t.id=e.theater_id ` + activityEventJoins + `
 ORDER BY e.detected_at DESC,e.id DESC`

const activityEventColumns = `e.id,e.kind,e.detected_at,e.first_screening_date::text,e.previous_program_end_date::text,c.id,
 CASE WHEN o.title_overridden THEN o.title ELSE c.title END,
 CASE WHEN o.poster_url_overridden THEN o.poster_url ELSE c.poster_url END,c.updated_at,
 (SELECT min(h.service_date)::text FROM showtimes h JOIN schedule_snapshot v ON v.version=h.generation_id
 JOIN public_movie_sources a ON (a.source_provider,a.source_movie_id)=(h.provider,h.movie_provider_id)
 JOIN public_movies m ON m.id=a.public_movie_id WHERE h.theater_id=e.theater_id AND h.start_time>transaction_timestamp() AND coalesce(m.redirect_to_id,m.id)=c.id)`

const activityEventJoins = `JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(e.anchor_provider,e.anchor_source_movie_id)
JOIN public_movies p ON p.id=s.public_movie_id LEFT JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id) AND c.redirect_to_id IS NULL
LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=c.id`
