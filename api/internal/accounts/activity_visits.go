package accounts

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ActivityVisitView struct {
	Username            string `json:"username"`
	Scope               string `json:"scope"`
	PreviousLastEventID string `json:"previous_last_event_id"`
}

func activityVisitInput(scope, rawID string) (int64, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	validScope := scope == "followed" || (strings.HasPrefix(scope, "theater:") && theaterPreferenceID.MatchString(strings.TrimPrefix(scope, "theater:")))
	if !validScope || err != nil || id < 0 || strconv.FormatInt(id, 10) != rawID {
		return 0, ErrInvalidInput
	}
	return id, nil
}

// VisitActivity atomically captures the previous visit and advances its watermark.
// The account lock serializes visits from different sessions/devices, including
// the first insert. Older pages/devices can never move the watermark backwards.
func (s *Service) VisitActivity(ctx context.Context, rawSession, expectedUsername, scope, lastEventID string) (ActivityVisitView, error) {
	id, err := activityVisitInput(scope, lastEventID)
	if err != nil {
		return ActivityVisitView{}, err
	}
	var view ActivityVisitView
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		var previous int64
		err = tx.QueryRow(ctx, `SELECT last_event_id FROM account_activity_visits WHERE account_id=$1 AND scope=$2`, a.id, scope).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_activity_visits(account_id,scope,last_event_id) VALUES($1,$2,$3)
		 ON CONFLICT(account_id,scope) DO UPDATE SET last_event_id=greatest(account_activity_visits.last_event_id,excluded.last_event_id)`, a.id, scope, id)
		if err != nil {
			return ErrUnavailable
		}
		view = ActivityVisitView{Username: *a.username, Scope: scope, PreviousLastEventID: strconv.FormatInt(previous, 10)}
		return nil
	})
	if err != nil {
		return ActivityVisitView{}, err
	}
	return view, nil
}
