package accounts

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/schedule"
)

const (
	notificationPreviewSize   = 3
	maxNotificationPageSize   = 50
	maxNotificationReadIDs    = 100
	defaultNotificationsLimit = 20
)

type NotificationTheater struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	City string `json:"city"`
}

// NotificationView exposes counts and a bounded preview. Movies and cinemas are
// resolved at read time to current canonical public metadata.
type NotificationView struct {
	ID           string                   `json:"id"`
	Kind         string                   `json:"kind"`
	RuleID       *string                  `json:"rule_id"`
	CreatedAt    time.Time                `json:"created_at"`
	Read         bool                     `json:"read"`
	EventCount   int                      `json:"event_count"`
	MovieCount   int                      `json:"movie_count"`
	TheaterCount int                      `json:"theater_count"`
	Movies       []schedule.ActivityMovie `json:"movies"`
	Theaters     []NotificationTheater    `json:"theaters"`
}

type NotificationStatusView struct {
	Username    string `json:"username"`
	UnreadCount int    `json:"unread_count"`
	LatestID    string `json:"latest_id"`
}

type NotificationsView struct {
	NotificationStatusView
	Items      []NotificationView `json:"items"`
	Limit      int                `json:"limit"`
	NextCursor *string            `json:"next_cursor"`
}

func NormalizeNotificationsQuery(limit int, before string) (int, int64, error) {
	if limit < 0 || limit > maxNotificationPageSize {
		return 0, 0, ErrInvalidInput
	}
	if limit == 0 {
		limit = defaultNotificationsLimit
	}
	if before == "" {
		return limit, 0, nil
	}
	id, err := parseNotificationID(before)
	if err != nil {
		return 0, 0, ErrInvalidInput
	}
	return limit, id, nil
}

func readNotificationStatus(ctx context.Context, tx pgx.Tx, a account) (NotificationStatusView, error) {
	view := NotificationStatusView{Username: *a.username, LatestID: "0"}
	var latest int64
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_notifications WHERE account_id=$1 AND read_at IS NULL),
 coalesce((SELECT max(id) FROM account_notifications WHERE account_id=$1),0)`, a.id).Scan(&view.UnreadCount, &latest)
	if err != nil {
		return NotificationStatusView{}, ErrUnavailable
	}
	view.LatestID = strconv.FormatInt(latest, 10)
	return view, nil
}

func (s *Service) NotificationStatus(ctx context.Context, rawSession string) (NotificationStatusView, error) {
	var view NotificationStatusView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view, err = readNotificationStatus(ctx, tx, a)
		return err
	})
	if err != nil {
		return NotificationStatusView{}, err
	}
	return view, nil
}

func (s *Service) Notifications(ctx context.Context, rawSession string, limit int, before string) (NotificationsView, error) {
	limit, beforeID, err := NormalizeNotificationsQuery(limit, before)
	if err != nil {
		return NotificationsView{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	view := NotificationsView{Items: []NotificationView{}, Limit: limit}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='2s'`); err != nil {
			return ErrUnavailable
		}
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if view.NotificationStatusView, err = readNotificationStatus(ctx, tx, a); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,kind,rule_id,created_at,read_at IS NOT NULL,event_count,cardinality(movie_ids),cardinality(theater_ids),movie_ids[1:$4],theater_ids[1:$4]
 FROM account_notifications WHERE account_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3`, a.id, beforeID, limit+1, notificationPreviewSize)
		if err != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		previews := make([][]int64, 0, limit+1)
		theaters := make([][]string, 0, limit+1)
		movieSet, theaterSet := map[int64]bool{}, map[string]bool{}
		for rows.Next() {
			var item NotificationView
			var id int64
			var ruleID *int64
			var movieIDs []int64
			var theaterIDs []string
			if err := rows.Scan(&id, &item.Kind, &ruleID, &item.CreatedAt, &item.Read, &item.EventCount, &item.MovieCount, &item.TheaterCount, &movieIDs, &theaterIDs); err != nil {
				return ErrUnavailable
			}
			item.ID, item.CreatedAt = strconv.FormatInt(id, 10), item.CreatedAt.UTC()
			if ruleID != nil {
				rule := strconv.FormatInt(*ruleID, 10)
				item.RuleID = &rule
			}
			view.Items = append(view.Items, item)
			previews, theaters = append(previews, movieIDs), append(theaters, theaterIDs)
			for _, m := range movieIDs {
				movieSet[m] = true
			}
			for _, t := range theaterIDs {
				theaterSet[t] = true
			}
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		rows.Close()
		movies, err := readNotificationMovies(ctx, tx, movieSet)
		if err != nil {
			return err
		}
		cinemas, err := readNotificationTheaters(ctx, tx, theaterSet)
		if err != nil {
			return err
		}
		for i := range view.Items {
			view.Items[i].Movies, view.Items[i].Theaters = []schedule.ActivityMovie{}, []NotificationTheater{}
			for _, id := range previews[i] {
				if m, ok := movies[id]; ok {
					view.Items[i].Movies = append(view.Items[i].Movies, m)
				}
			}
			for _, id := range theaters[i] {
				if t, ok := cinemas[id]; ok {
					view.Items[i].Theaters = append(view.Items[i].Theaters, t)
				}
			}
		}
		if len(view.Items) > limit {
			view.Items = view.Items[:limit]
			cursor := view.Items[limit-1].ID
			view.NextCursor = &cursor
		}
		return nil
	})
	if err != nil {
		return NotificationsView{}, err
	}
	return view, nil
}

// Saved durable IDs follow one redirect hop to the current canonical identity,
// like the followed activity feed; unresolvable identities are omitted.
func readNotificationMovies(ctx context.Context, tx pgx.Tx, set map[int64]bool) (map[int64]schedule.ActivityMovie, error) {
	result := map[int64]schedule.ActivityMovie{}
	if len(set) == 0 {
		return result, nil
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT m.id,c.id,CASE WHEN o.title_overridden THEN o.title ELSE c.title END,
 CASE WHEN o.poster_url_overridden THEN o.poster_url ELSE c.poster_url END,c.updated_at
 FROM unnest($1::bigint[]) m(id) JOIN public_movies p ON p.id=m.id
 JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id) AND c.redirect_to_id IS NULL
 LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=c.id`, ids)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id, canonical int64
		var movie schedule.ActivityMovie
		var title *string
		if err := rows.Scan(&id, &canonical, &title, &movie.PosterURL, &movie.UpdatedAt); err != nil {
			return nil, ErrUnavailable
		}
		if title == nil {
			continue
		}
		movie.Slug, movie.Title, movie.UpdatedAt = "film-"+strconv.FormatInt(canonical, 10), *title, movie.UpdatedAt.UTC()
		result[id] = movie
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return result, nil
}

func readNotificationTheaters(ctx context.Context, tx pgx.Tx, set map[string]bool) (map[string]NotificationTheater, error) {
	result := map[string]NotificationTheater{}
	if len(set) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT ht.id,coalesce(t.slug,ht.slug),coalesce(t.name,ht.name),coalesce(t.city,ht.city)
 FROM unnest($1::text[]) x(id) JOIN screening_history_theaters ht ON ht.id=x.id
 LEFT JOIN theaters t ON t.id=ht.id AND t.generation_id=(SELECT version FROM schedule_snapshot)`, ids)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var t NotificationTheater
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.City); err != nil {
			return nil, ErrUnavailable
		}
		result[t.ID] = t
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (s *Service) notificationReadMutation(ctx context.Context, rawSession, expectedUsername string, mutate func(pgx.Tx, account, time.Time) error) (NotificationStatusView, error) {
	var view NotificationStatusView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		if err := mutate(tx, a, s.now().UTC()); err != nil {
			return err
		}
		view, err = readNotificationStatus(ctx, tx, a)
		return err
	})
	if err != nil {
		return NotificationStatusView{}, err
	}
	return view, nil
}

// MarkNotificationsRead ignores unknown, foreign and already read IDs, so
// concurrent retention or another device cannot fail an idempotent read.
func (s *Service) MarkNotificationsRead(ctx context.Context, rawSession, expectedUsername string, rawIDs []string) (NotificationStatusView, error) {
	if len(rawIDs) == 0 || len(rawIDs) > maxNotificationReadIDs {
		return NotificationStatusView{}, ErrInvalidInput
	}
	ids := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseNotificationID(raw)
		if err != nil {
			return NotificationStatusView{}, err
		}
		ids = append(ids, id)
	}
	return s.notificationReadMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account, now time.Time) error {
		if _, err := tx.Exec(ctx, `UPDATE account_notifications SET read_at=greatest(created_at,$3) WHERE account_id=$1 AND id=ANY($2) AND read_at IS NULL`, a.id, ids, now); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

// MarkAllNotificationsRead is bounded by the newest ID the client displayed;
// entries delivered afterwards stay unread.
func (s *Service) MarkAllNotificationsRead(ctx context.Context, rawSession, expectedUsername, rawThrough string) (NotificationStatusView, error) {
	through, err := parseNotificationID(rawThrough)
	if err != nil {
		return NotificationStatusView{}, err
	}
	return s.notificationReadMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account, now time.Time) error {
		if _, err := tx.Exec(ctx, `UPDATE account_notifications SET read_at=greatest(created_at,$3) WHERE account_id=$1 AND id<=$2 AND read_at IS NULL`, a.id, through, now); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}
