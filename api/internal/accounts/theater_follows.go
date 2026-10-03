package accounts

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
)

var (
	ErrTheaterFollowsChanged = errors.New("account theater follows changed")
	ErrTheaterFollowLimit    = errors.New("account theater follow limit reached")
)

// TheaterFollowsView is independent of showtime selection and authentication revisions.
type TheaterFollowsView struct {
	Username   string   `json:"username"`
	Revision   string   `json:"revision"`
	TheaterIDs []string `json:"theater_ids"`
}

// readTheaterFollows requires the authorized account lock, including for absent state.
func readTheaterFollows(ctx context.Context, tx pgx.Tx, a account) (TheaterFollowsView, int64, error) {
	view := TheaterFollowsView{Username: *a.username, Revision: "0", TheaterIDs: []string{}}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision,theater_ids FROM account_theater_follows WHERE account_id=$1`, a.id).Scan(&revision, &view.TheaterIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, 0, nil
	}
	if err != nil {
		return TheaterFollowsView{}, 0, ErrUnavailable
	}
	slices.Sort(view.TheaterIDs)
	view.Revision = strconv.FormatInt(revision, 10)
	return view, revision, nil
}

func (s *Service) TheaterFollows(ctx context.Context, rawSession string) (TheaterFollowsView, error) {
	var view TheaterFollowsView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view, _, err = readTheaterFollows(ctx, tx, a)
		return err
	})
	if err != nil {
		return TheaterFollowsView{}, err
	}
	return view, nil
}

func (s *Service) SaveTheaterFollow(ctx context.Context, rawSession, expectedUsername, expectedRevision, theaterID string, followed bool) (TheaterFollowsView, error) {
	revision, err := theaterRevision(expectedRevision)
	if err != nil || !theaterPreferenceID.MatchString(theaterID) {
		return TheaterFollowsView{}, ErrInvalidInput
	}
	var view TheaterFollowsView
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		current, stored, err := readTheaterFollows(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrTheaterFollowsChanged
		}
		index, present := slices.BinarySearch(current.TheaterIDs, theaterID)
		if present == followed {
			view = current
			return nil
		}
		if followed && len(current.TheaterIDs) == maxTheaterPreferences {
			return ErrTheaterFollowLimit
		}
		if stored == maxTheaterPreferenceRevision {
			return ErrUnavailable
		}
		ids := current.TheaterIDs
		if followed {
			ids = slices.Insert(ids, index, theaterID)
		} else {
			ids = slices.Delete(ids, index, index+1)
		}
		if stored == 0 {
			_, err = tx.Exec(ctx, `INSERT INTO account_theater_follows(account_id,revision,theater_ids) VALUES($1,1,$2)`, a.id, ids)
		} else {
			_, err = tx.Exec(ctx, `UPDATE account_theater_follows SET revision=$2,theater_ids=$3 WHERE account_id=$1`, a.id, stored+1, ids)
		}
		if err != nil {
			return ErrUnavailable
		}
		view = TheaterFollowsView{Username: *a.username, Revision: strconv.FormatInt(stored+1, 10), TheaterIDs: ids}
		return nil
	})
	if err != nil {
		return TheaterFollowsView{}, err
	}
	return view, nil
}
