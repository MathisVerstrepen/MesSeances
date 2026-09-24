package accounts

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const maxTheaterPreferenceRevision int64 = 9007199254740991
const maxTheaterPreferences = 4096

var theaterPreferenceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

var ErrTheaterSelectionChanged = errors.New("account theater selection changed")

// TheaterPreferencesView distinguishes an unset selection (revision "0") from
// an explicitly saved empty selection. IDs do not depend on the live catalog.
type TheaterPreferencesView struct {
	Username   string   `json:"username"`
	Revision   string   `json:"revision"`
	TheaterIDs []string `json:"theater_ids"`
}

func theaterRevision(raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || n > maxTheaterPreferenceRevision || strconv.FormatInt(n, 10) != raw {
		return 0, ErrInvalidInput
	}
	return n, nil
}

func canonicalTheaterIDs(ids []string) ([]string, error) {
	if ids == nil || len(ids) > maxTheaterPreferences {
		return nil, ErrInvalidInput
	}
	result := append([]string{}, ids...)
	for _, id := range result {
		if !theaterPreferenceID.MatchString(id) {
			return nil, ErrInvalidInput
		}
	}
	slices.Sort(result)
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, ErrInvalidInput
		}
	}
	return result, nil
}

// readTheaterPreferences is called only after authorize has locked the owner.
func readTheaterPreferences(ctx context.Context, tx pgx.Tx, a account) (TheaterPreferencesView, int64, error) {
	view := TheaterPreferencesView{Username: *a.username, Revision: "0", TheaterIDs: []string{}}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision,theater_ids FROM account_theater_preferences WHERE account_id=$1`, a.id).Scan(&revision, &view.TheaterIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, 0, nil
	}
	if err != nil {
		return TheaterPreferencesView{}, 0, ErrUnavailable
	}
	view.Revision = strconv.FormatInt(revision, 10)
	return view, revision, nil
}

func (s *Service) TheaterPreferences(ctx context.Context, rawSession string) (TheaterPreferencesView, error) {
	var view TheaterPreferencesView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view, _, err = readTheaterPreferences(ctx, tx, a)
		return err
	})
	if err != nil {
		return TheaterPreferencesView{}, err
	}
	return view, nil
}

func (s *Service) SaveTheaterPreferences(ctx context.Context, rawSession, expectedUsername, expectedRevision string, theaterIDs []string) (TheaterPreferencesView, error) {
	revision, err := theaterRevision(expectedRevision)
	if err != nil {
		return TheaterPreferencesView{}, err
	}
	ids, err := canonicalTheaterIDs(theaterIDs)
	if err != nil {
		return TheaterPreferencesView{}, err
	}
	var view TheaterPreferencesView
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		// The account lock serializes even first writes, when no preference row
		// exists. Never resolve an account from the client-supplied username.
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		current, stored, err := readTheaterPreferences(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrTheaterSelectionChanged
		}
		if stored != 0 && slices.Equal(current.TheaterIDs, ids) {
			view = current
			return nil
		}
		if stored == maxTheaterPreferenceRevision {
			return ErrUnavailable
		}
		if stored == 0 {
			_, err = tx.Exec(ctx, `INSERT INTO account_theater_preferences(account_id,revision,theater_ids) VALUES($1,1,$2)`, a.id, ids)
		} else {
			_, err = tx.Exec(ctx, `UPDATE account_theater_preferences SET revision=$2,theater_ids=$3 WHERE account_id=$1`, a.id, stored+1, ids)
		}
		if err != nil {
			return ErrUnavailable
		}
		view = TheaterPreferencesView{Username: *a.username, Revision: strconv.FormatInt(stored+1, 10), TheaterIDs: ids}
		return nil
	})
	if err != nil {
		return TheaterPreferencesView{}, err
	}
	return view, nil
}
