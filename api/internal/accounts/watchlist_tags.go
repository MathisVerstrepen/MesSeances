package accounts

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const maxWatchlistTags = 50

var (
	ErrWatchlistTagNameTaken  = errors.New("watchlist tag name taken")
	ErrWatchlistTagLimit      = errors.New("watchlist tag limit reached")
	ErrWatchlistTagNotFound   = errors.New("watchlist tag not found")
	ErrWatchlistMovieNotSaved = errors.New("watchlist movie not saved")
)

type WatchlistTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func normalizeWatchlistTag(name string) (string, string, error) {
	if !utf8.ValidString(name) {
		return "", "", ErrInvalidInput
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", "", ErrInvalidInput
		}
	}
	name = norm.NFC.String(strings.Join(strings.Fields(name), " "))
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 {
		return "", "", ErrInvalidInput
	}
	key := norm.NFC.String(cases.Fold().String(name))
	return name, key, nil
}

func readWatchlistTags(ctx context.Context, tx pgx.Tx, accountID int64) ([]WatchlistTag, error) {
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.name FROM account_watchlist_tags t WHERE t.account_id=$1 ORDER BY t.id`, accountID)
	if err != nil {
		return nil, ErrWatchlistUnavailable
	}
	defer rows.Close()
	tags := []WatchlistTag{}
	for rows.Next() {
		var tag WatchlistTag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, ErrWatchlistUnavailable
		}
		tags = append(tags, tag)
	}
	if rows.Err() != nil {
		return nil, ErrWatchlistUnavailable
	}
	return tags, nil
}

// All tag operations share the membership/sort/import CAS and account lock.
// A changed callback is rolled back if the resource revision is exhausted.
func (s *Service) mutateWatchlistTag(ctx context.Context, raw, username, expectedRevision string, change func(pgx.Tx, account) (bool, error)) (WatchlistView, error) {
	revision, err := theaterRevision(expectedRevision)
	if err != nil {
		return WatchlistView{}, ErrInvalidInput
	}
	if err := s.sessionQuota(ctx, raw, "watchlist_write", false, true); err != nil {
		return WatchlistView{}, watchlistError(err)
	}
	var view WatchlistView
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if *a.username != username {
			return ErrUnauthorized
		}
		var stored int64
		view, stored, err = s.readWatchlist(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrWatchlistChanged
		}
		changed, err := change(tx, a)
		if err != nil {
			return err
		}
		if changed {
			if stored == maxTheaterPreferenceRevision {
				return ErrWatchlistUnavailable
			}
			_, err = tx.Exec(ctx, `INSERT INTO account_watchlist_state(account_id,revision) VALUES($1,$2)
 ON CONFLICT(account_id) DO UPDATE SET revision=EXCLUDED.revision`, a.id, stored+1)
			if err != nil {
				return ErrWatchlistUnavailable
			}
		}
		view, _, err = s.readWatchlist(ctx, tx, a)
		return err
	})
	if err != nil {
		return WatchlistView{}, watchlistError(err)
	}
	return view, nil
}

func watchlistTagNameAvailable(ctx context.Context, tx pgx.Tx, accountID, exceptID int64, key string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_watchlist_tags WHERE account_id=$1 AND name_key=$2 AND id<>$3)`, accountID, key, exceptID).Scan(&exists); err != nil {
		return ErrWatchlistUnavailable
	}
	if exists {
		return ErrWatchlistTagNameTaken
	}
	return nil
}

func (s *Service) CreateWatchlistTag(ctx context.Context, raw, username, expectedRevision, name string) (WatchlistView, error) {
	name, key, err := normalizeWatchlistTag(name)
	if err != nil {
		return WatchlistView{}, err
	}
	return s.mutateWatchlistTag(ctx, raw, username, expectedRevision, func(tx pgx.Tx, a account) (bool, error) {
		if err := watchlistTagNameAvailable(ctx, tx, a.id, 0, key); err != nil {
			return false, err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_tags WHERE account_id=$1`, a.id).Scan(&count); err != nil {
			return false, ErrWatchlistUnavailable
		}
		if count >= maxWatchlistTags {
			return false, ErrWatchlistTagLimit
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_watchlist_tags(account_id,name,name_key) VALUES($1,$2,$3)`, a.id, name, key); err != nil {
			return false, ErrWatchlistUnavailable
		}
		return true, nil
	})
}

func watchlistTagName(ctx context.Context, tx pgx.Tx, accountID, tagID int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM account_watchlist_tags WHERE account_id=$1 AND id=$2`, accountID, tagID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrWatchlistTagNotFound
	}
	if err != nil {
		return "", ErrWatchlistUnavailable
	}
	return name, nil
}

func (s *Service) RenameWatchlistTag(ctx context.Context, raw, username, expectedRevision, tagID, name string) (WatchlistView, error) {
	id, err := watchlistTMDBID(tagID)
	if err != nil {
		return WatchlistView{}, ErrInvalidInput
	}
	name, key, err := normalizeWatchlistTag(name)
	if err != nil {
		return WatchlistView{}, err
	}
	return s.mutateWatchlistTag(ctx, raw, username, expectedRevision, func(tx pgx.Tx, a account) (bool, error) {
		current, err := watchlistTagName(ctx, tx, a.id, id)
		if err != nil || current == name {
			return false, err
		}
		if err := watchlistTagNameAvailable(ctx, tx, a.id, id, key); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE account_watchlist_tags SET name=$3,name_key=$4 WHERE account_id=$1 AND id=$2`, a.id, id, name, key); err != nil {
			return false, ErrWatchlistUnavailable
		}
		return true, nil
	})
}

func (s *Service) DeleteWatchlistTag(ctx context.Context, raw, username, expectedRevision, tagID string) (WatchlistView, error) {
	id, err := watchlistTMDBID(tagID)
	if err != nil {
		return WatchlistView{}, ErrInvalidInput
	}
	return s.mutateWatchlistTag(ctx, raw, username, expectedRevision, func(tx pgx.Tx, a account) (bool, error) {
		if _, err := watchlistTagName(ctx, tx, a.id, id); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM account_watchlist_tags WHERE account_id=$1 AND id=$2`, a.id, id); err != nil {
			return false, ErrWatchlistUnavailable
		}
		return true, nil
	})
}

func (s *Service) AssignWatchlistTag(ctx context.Context, raw, username, expectedRevision, slug, tagID string, assigned bool) (WatchlistView, error) {
	id, err := watchlistTMDBID(tagID)
	if err != nil || !theaterPreferenceID.MatchString(slug) {
		return WatchlistView{}, ErrInvalidInput
	}
	return s.mutateWatchlistTag(ctx, raw, username, expectedRevision, func(tx pgx.Tx, a account) (bool, error) {
		// Same account-before-catalog lock order as membership mutations.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(6211428337968315)); err != nil {
			return false, ErrWatchlistUnavailable
		}
		if _, err := watchlistTagName(ctx, tx, a.id, id); err != nil {
			return false, err
		}
		movieID, err := resolveWatchlistMovie(ctx, tx, slug)
		if err != nil {
			return false, err
		}
		var savedID int64
		var present bool
		err = tx.QueryRow(ctx, watchlistResolution+`SELECT saved_id,
 EXISTS(SELECT 1 FROM resolved r JOIN account_watchlist_item_tags t ON t.account_id=$1 AND t.public_movie_id=r.saved_id
 WHERE r.id=$2 AND r.redirect_to_id IS NULL AND t.tag_id=$3)
 FROM resolved WHERE id=$2 AND redirect_to_id IS NULL ORDER BY added_at,saved_id LIMIT 1`, a.id, movieID, id).Scan(&savedID, &present)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrWatchlistMovieNotSaved
		}
		if err != nil {
			return false, ErrWatchlistUnavailable
		}
		if present == assigned {
			return false, nil
		}
		if assigned {
			_, err = tx.Exec(ctx, `INSERT INTO account_watchlist_item_tags(account_id,public_movie_id,tag_id) VALUES($1,$2,$3)`, a.id, savedID, id)
		} else {
			_, err = tx.Exec(ctx, watchlistResolution+`DELETE FROM account_watchlist_item_tags WHERE account_id=$1 AND tag_id=$3
 AND public_movie_id IN (SELECT saved_id FROM resolved WHERE id=$2 AND redirect_to_id IS NULL)`, a.id, movieID, id)
		}
		if err != nil {
			return false, ErrWatchlistUnavailable
		}
		return true, nil
	})
}
