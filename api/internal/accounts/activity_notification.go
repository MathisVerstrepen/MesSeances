package accounts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type ActivityNotificationView struct {
	Username        string `json:"username"`
	FollowsRevision string `json:"follows_revision"`
	HasNewActivity  bool   `json:"has_new_activity"`
}

// ActivityNotification reads the followed feed without advancing its visit.
func (s *Service) ActivityNotification(ctx context.Context, rawSession string) (ActivityNotificationView, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case s.followedActivityGate <- struct{}{}:
		defer func() { <-s.followedActivityGate }()
	default:
		return ActivityNotificationView{}, ErrUnavailable
	}
	var view ActivityNotificationView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='2s'`); err != nil {
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
		view.Username, view.FollowsRevision = follows.Username, follows.Revision
		if len(follows.TheaterIDs) == 0 {
			return nil
		}
		// Match identities visible in the followed feed. Baselines, superseded
		// episodes and activity in unfollowed cinemas never notify.
		err = tx.QueryRow(ctx, `SELECT EXISTS (
		 SELECT 1 FROM unnest($1::text[]) f(theater_id)
		 JOIN cinema_activity_episodes e USING(theater_id)
		 JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(e.anchor_provider,e.anchor_source_movie_id)
		 JOIN public_movies p ON p.id=s.public_movie_id
		 JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id) AND c.redirect_to_id IS NULL
		 WHERE e.superseded_by_id IS NULL AND e.kind<>'baseline'
		 AND e.id>coalesce((SELECT last_event_id FROM account_activity_visits WHERE account_id=$2 AND scope='followed'),0)
		)`, follows.TheaterIDs, a.id).Scan(&view.HasNewActivity)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return ActivityNotificationView{}, err
	}
	return view, nil
}
