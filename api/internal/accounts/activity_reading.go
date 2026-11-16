package accounts

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type ActivityReadingView struct {
	Username        string `json:"username"`
	ManualReading   bool   `json:"manual_reading"`
	LastReadEventID string `json:"last_read_event_id"`
}

type ActivityReadView struct {
	Username  string `json:"username"`
	EventID   string `json:"event_id"`
	Consulted bool   `json:"consulted"`
}

type ActivityReadingSnapshot struct {
	Reading ActivityReadingView `json:"reading"`
	Items   []ActivityReadView  `json:"items"`
}

func (s *Service) ActivityReading(ctx context.Context, rawSession string, eventIDs []string) (ActivityReadingSnapshot, error) {
	if len(eventIDs) > 100 {
		return ActivityReadingSnapshot{}, ErrInvalidInput
	}
	items := make([]FollowedActivityItem, len(eventIDs))
	for i, raw := range eventIDs {
		id, err := activityVisitInput("followed", raw)
		if err != nil || id == 0 {
			return ActivityReadingSnapshot{}, ErrInvalidInput
		}
		items[i].EventID = raw
	}
	view := ActivityReadingSnapshot{Items: []ActivityReadView{}}
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view.Reading, err = readActivityReading(ctx, tx, a)
		if err != nil {
			return err
		}
		if err = readActivityItems(ctx, tx, a, items, view.Reading.LastReadEventID); err != nil {
			return err
		}
		for _, item := range items {
			view.Items = append(view.Items, ActivityReadView{Username: *a.username, EventID: item.EventID, Consulted: item.Consulted})
		}
		return nil
	})
	if err != nil {
		return ActivityReadingSnapshot{}, err
	}
	return view, nil
}

// The caller holds the account lock. Missing rows preserve automatic mode and
// the existing visit marker; changing modes never resets reading history.
func readActivityReading(ctx context.Context, tx pgx.Tx, a account) (ActivityReadingView, error) {
	view := ActivityReadingView{Username: *a.username}
	var last int64
	err := tx.QueryRow(ctx, `SELECT
	 coalesce((SELECT manual_reading FROM account_activity_preferences WHERE account_id=$1),false),
	 coalesce((SELECT last_event_id FROM account_activity_visits WHERE account_id=$1 AND scope='followed'),0)`, a.id).Scan(&view.ManualReading, &last)
	if err != nil {
		return ActivityReadingView{}, ErrUnavailable
	}
	view.LastReadEventID = strconv.FormatInt(last, 10)
	return view, nil
}

func (s *Service) SaveActivityReading(ctx context.Context, rawSession, expectedUsername string, manual bool) (ActivityReadingView, error) {
	var view ActivityReadingView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		if _, err = tx.Exec(ctx, `INSERT INTO account_activity_preferences(account_id,manual_reading) VALUES($1,$2)
		 ON CONFLICT(account_id) DO UPDATE SET manual_reading=excluded.manual_reading`, a.id, manual); err != nil {
			return ErrUnavailable
		}
		view, err = readActivityReading(ctx, tx, a)
		return err
	})
	if err != nil {
		return ActivityReadingView{}, err
	}
	return view, nil
}

func (s *Service) SaveActivityRead(ctx context.Context, rawSession, expectedUsername, eventID string, consulted bool) (ActivityReadView, error) {
	id, err := activityVisitInput("followed", eventID)
	if err != nil || id == 0 {
		return ActivityReadView{}, ErrInvalidInput
	}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		reading, err := readActivityReading(ctx, tx, a)
		if err != nil {
			return err
		}
		if !reading.ManualReading {
			return ErrInvalidInput
		}
		// Limit writes to real, currently followed activity, including older
		// events made new again. Baselines and superseded events are excluded.
		result, err := tx.Exec(ctx, `INSERT INTO account_activity_reads(account_id,event_id,consulted)
		 SELECT $1,e.id,$3 FROM cinema_activity_episodes e
		 JOIN account_theater_follows f ON f.account_id=$1 AND e.theater_id=ANY(f.theater_ids)
		 WHERE e.id=$2 AND e.kind<>'baseline' AND e.superseded_by_id IS NULL
		 ON CONFLICT(account_id,event_id) DO UPDATE SET consulted=excluded.consulted`, a.id, id, consulted)
		if err != nil {
			return ErrUnavailable
		}
		if result.RowsAffected() != 1 {
			return ErrInvalidInput
		}
		return nil
	})
	if err != nil {
		return ActivityReadView{}, err
	}
	return ActivityReadView{Username: expectedUsername, EventID: eventID, Consulted: consulted}, nil
}

// Only read exceptions for this bounded page, never an account's entire history.
func readActivityItems(ctx context.Context, tx pgx.Tx, a account, items []FollowedActivityItem, last string) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i], _ = strconv.ParseInt(items[i].EventID, 10, 64)
	}
	rows, err := tx.Query(ctx, `SELECT e.id,coalesce(r.consulted,e.id<=$3::bigint)
	 FROM unnest($2::bigint[]) e(id) LEFT JOIN account_activity_reads r ON r.account_id=$1 AND r.event_id=e.id`, a.id, ids, last)
	if err != nil {
		return ErrUnavailable
	}
	defer rows.Close()
	states := make(map[string]bool, len(items))
	for rows.Next() {
		var id int64
		var consulted bool
		if err := rows.Scan(&id, &consulted); err != nil {
			return ErrUnavailable
		}
		states[strconv.FormatInt(id, 10)] = consulted
	}
	if rows.Err() != nil {
		return ErrUnavailable
	}
	for i := range items {
		items[i].Consulted = states[items[i].EventID]
	}
	return nil
}
