package accounts

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxWatchedItems = 1000

var (
	ErrWatchedChanged     = errors.New("watched changed")
	ErrWatchedLimit       = errors.New("watched limit reached")
	ErrWatchedUnavailable = errors.New("watched unavailable")
)

type WatchedPreferences struct {
	DefaultVisibility    string `json:"default_visibility"`
	SearchVisibility     string `json:"search_visibility"`
	PlanningVisibility   string `json:"planning_visibility"`
	FilmsVisibility      string `json:"films_visibility"`
	UpcomingVisibility   string `json:"upcoming_visibility"`
	HomeVisibility       string `json:"home_visibility"`
	CinemaCityVisibility string `json:"cinema_city_visibility"`
	AutoRemoveWatchlist  bool   `json:"auto_remove_watchlist"`
}

type WatchedItem struct {
	WatchlistMovie
	WatchedAt time.Time `json:"watched_at"`
}

type WatchedSnapshot struct {
	Username    string             `json:"username"`
	Revision    string             `json:"revision"`
	Items       []WatchedItem      `json:"items"`
	Preferences WatchedPreferences `json:"preferences"`
}

type WatchedMutationResult struct {
	Watched   WatchedSnapshot `json:"watched"`
	Watchlist *WatchlistView  `json:"watchlist"`
}

func defaultWatchedPreferences() WatchedPreferences {
	return WatchedPreferences{DefaultVisibility: "hide", SearchVisibility: "inherit", PlanningVisibility: "inherit", FilmsVisibility: "inherit", UpcomingVisibility: "inherit", HomeVisibility: "inherit", CinemaCityVisibility: "inherit"}
}

func (p WatchedPreferences) valid() bool {
	if p.DefaultVisibility != "hide" && p.DefaultVisibility != "show" {
		return false
	}
	for _, value := range []string{p.SearchVisibility, p.PlanningVisibility, p.FilmsVisibility, p.UpcomingVisibility, p.HomeVisibility, p.CinemaCityVisibility} {
		if value != "inherit" && value != "hide" && value != "show" {
			return false
		}
	}
	return true
}

const watchedResolution = `WITH RECURSIVE resolved(saved_id,id,redirect_to_id,watched_at) AS (
 SELECT i.public_movie_id,p.id,p.redirect_to_id,i.watched_at FROM account_watched_items i
 JOIN public_movies p ON p.id=i.public_movie_id WHERE i.account_id=$1
 UNION SELECT r.saved_id,p.id,p.redirect_to_id,r.watched_at FROM resolved r
 JOIN public_movies p ON p.id=r.redirect_to_id
) `

func readWatchedState(ctx context.Context, tx pgx.Tx, a account) (WatchedSnapshot, int64, error) {
	view := WatchedSnapshot{Username: *a.username, Revision: "0", Items: []WatchedItem{}, Preferences: defaultWatchedPreferences()}
	p := &view.Preferences
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision,default_visibility,search_visibility,planning_visibility,films_visibility,upcoming_visibility,home_visibility,cinema_city_visibility,auto_remove_watchlist FROM account_watched_state WHERE account_id=$1`, a.id).Scan(&revision, &p.DefaultVisibility, &p.SearchVisibility, &p.PlanningVisibility, &p.FilmsVisibility, &p.UpcomingVisibility, &p.HomeVisibility, &p.CinemaCityVisibility, &p.AutoRemoveWatchlist)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return view, 0, ErrWatchedUnavailable
	}
	view.Revision = strconv.FormatInt(revision, 10)
	return view, revision, nil
}

func readWatched(ctx context.Context, tx pgx.Tx, a account) (WatchedSnapshot, error) {
	view, _, err := readWatchedState(ctx, tx, a)
	if err != nil {
		return view, err
	}
	rows, err := tx.Query(ctx, watchedResolution+`SELECT `+watchlistSummary+`,saved.watched_at
 FROM (SELECT id,min(watched_at) watched_at FROM resolved WHERE redirect_to_id IS NULL GROUP BY id) saved
 JOIN public_movies p ON p.id=saved.id LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=p.id
 ORDER BY saved.watched_at DESC, 'film-' || p.id::text`, a.id)
	if err != nil {
		return view, ErrWatchedUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var item WatchedItem
		if err := rows.Scan(&item.Slug, &item.Title, &item.PosterURL, &item.ReleaseDate, &item.WatchedAt); err != nil {
			return view, ErrWatchedUnavailable
		}
		item.WatchedAt = item.WatchedAt.UTC()
		view.Items = append(view.Items, item)
	}
	if rows.Err() != nil {
		return view, ErrWatchedUnavailable
	}
	return view, nil
}

func watchedError(err error) error {
	if errors.Is(err, ErrUnavailable) {
		return ErrWatchedUnavailable
	}
	return err
}

func (s *Service) Watched(ctx context.Context, raw string) (WatchedSnapshot, error) {
	var view WatchedSnapshot
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		view, err = readWatched(ctx, tx, a)
		return err
	})
	if err != nil {
		return WatchedSnapshot{}, watchedError(err)
	}
	return view, nil
}

// SaveWatched fences both resources before business lookup or no-op detection.
// Only the transition to watched may remove watchlist membership. Both returned
// snapshots are read before the same commit; a commit error is ambiguous and
// must be reconciled by GET, never automatically replayed.
func (s *Service) SaveWatched(ctx context.Context, raw, username, expectedRevision, slug, expectedWatchlistRevision string, watched bool) (WatchedMutationResult, error) {
	revision, err := theaterRevision(expectedRevision)
	watchlistRevision, watchlistErr := theaterRevision(expectedWatchlistRevision)
	if err != nil || watchlistErr != nil || !theaterPreferenceID.MatchString(slug) {
		return WatchedMutationResult{}, ErrInvalidInput
	}
	if err := s.sessionQuota(ctx, raw, "watchlist_write", false, true); err != nil {
		return WatchedMutationResult{}, watchedError(err)
	}
	var result WatchedMutationResult
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if *a.username != username {
			return ErrUnauthorized
		}
		view, stored, err := readWatchedState(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrWatchedChanged
		}
		remove := watched && view.Preferences.AutoRemoveWatchlist
		if remove {
			var current int64
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM account_watchlist_state WHERE account_id=$1),0)`, a.id).Scan(&current); err != nil {
				return ErrWatchlistUnavailable
			}
			if current != watchlistRevision {
				return ErrWatchlistChanged
			}
		}
		// Same account-then-catalog order as watchlist; catalog writers never lock accounts.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(6211428337968315)); err != nil {
			return ErrWatchedUnavailable
		}
		id, err := resolveWatchlistMovie(ctx, tx, slug)
		if errors.Is(err, ErrWatchlistUnavailable) {
			return ErrWatchedUnavailable
		}
		if err != nil {
			return err
		}
		var exists bool
		var count int
		if err := tx.QueryRow(ctx, watchedResolution+`SELECT EXISTS(SELECT 1 FROM resolved WHERE id=$2 AND redirect_to_id IS NULL),
 (SELECT count(*) FROM account_watched_items WHERE account_id=$1)`, a.id, id).Scan(&exists, &count); err != nil {
			return ErrWatchedUnavailable
		}
		if exists != watched {
			if watched && count >= maxWatchedItems {
				return ErrWatchedLimit
			}
			if stored == maxTheaterPreferenceRevision {
				return ErrWatchedUnavailable
			}
			if watched {
				_, err = tx.Exec(ctx, `INSERT INTO account_watched_items(account_id,public_movie_id,watched_at) VALUES($1,$2,$3)`, a.id, id, s.now().UTC())
			} else {
				_, err = tx.Exec(ctx, watchedResolution+`DELETE FROM account_watched_items WHERE account_id=$1 AND public_movie_id IN (SELECT saved_id FROM resolved WHERE id=$2 AND redirect_to_id IS NULL)`, a.id, id)
			}
			if err != nil {
				return ErrWatchedUnavailable
			}
			if _, err := tx.Exec(ctx, `INSERT INTO account_watched_state(account_id,revision) VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET revision=EXCLUDED.revision`, a.id, stored+1); err != nil {
				return ErrWatchedUnavailable
			}
			if remove {
				if err := s.changeWatchlist(ctx, tx, a, id, watchlistRevision, false); err != nil {
					return err
				}
			}
		}
		result.Watched, err = readWatched(ctx, tx, a)
		if err != nil {
			return err
		}
		if remove {
			list, _, err := s.readWatchlist(ctx, tx, a)
			if err != nil {
				return err
			}
			result.Watchlist = &list
		}
		return nil
	})
	if err != nil {
		return WatchedMutationResult{}, watchedError(err)
	}
	return result, nil
}

func (s *Service) SaveWatchedPreferences(ctx context.Context, raw, username, expectedRevision string, preferences WatchedPreferences) (WatchedSnapshot, error) {
	revision, err := theaterRevision(expectedRevision)
	if err != nil || !preferences.valid() {
		return WatchedSnapshot{}, ErrInvalidInput
	}
	if err := s.sessionQuota(ctx, raw, "watchlist_write", false, true); err != nil {
		return WatchedSnapshot{}, watchedError(err)
	}
	var view WatchedSnapshot
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if *a.username != username {
			return ErrUnauthorized
		}
		current, stored, err := readWatchedState(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrWatchedChanged
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(6211428337968315)); err != nil {
			return ErrWatchedUnavailable
		}
		if current.Preferences != preferences {
			if stored == maxTheaterPreferenceRevision {
				return ErrWatchedUnavailable
			}
			p := preferences
			_, err := tx.Exec(ctx, `INSERT INTO account_watched_state(account_id,revision,default_visibility,search_visibility,planning_visibility,films_visibility,upcoming_visibility,home_visibility,cinema_city_visibility,auto_remove_watchlist)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(account_id) DO UPDATE SET revision=EXCLUDED.revision,
 default_visibility=EXCLUDED.default_visibility,search_visibility=EXCLUDED.search_visibility,planning_visibility=EXCLUDED.planning_visibility,films_visibility=EXCLUDED.films_visibility,upcoming_visibility=EXCLUDED.upcoming_visibility,home_visibility=EXCLUDED.home_visibility,cinema_city_visibility=EXCLUDED.cinema_city_visibility,auto_remove_watchlist=EXCLUDED.auto_remove_watchlist`, a.id, stored+1, p.DefaultVisibility, p.SearchVisibility, p.PlanningVisibility, p.FilmsVisibility, p.UpcomingVisibility, p.HomeVisibility, p.CinemaCityVisibility, p.AutoRemoveWatchlist)
			if err != nil {
				return ErrWatchedUnavailable
			}
		}
		view, err = readWatched(ctx, tx, a)
		return err
	})
	if err != nil {
		return WatchedSnapshot{}, watchedError(err)
	}
	return view, nil
}
