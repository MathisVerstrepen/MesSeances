package accounts

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxWatchlistItems = 1000

var (
	ErrWatchlistChanged             = errors.New("watchlist changed")
	ErrWatchlistLimit               = errors.New("watchlist limit reached")
	ErrMovieNotFound                = errors.New("movie not found")
	ErrWatchlistUnavailable         = errors.New("watchlist unavailable")
	ErrWatchlistExternalUnavailable = errors.New("watchlist external unavailable")
)

type WatchlistMovie struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	PosterURL   string `json:"poster_url,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"`
}

type WatchlistItem struct {
	WatchlistMovie
	AddedAt           time.Time `json:"added_at"`
	FrenchReleaseDate string    `json:"french_release_date,omitempty"`
}

type WatchlistView struct {
	Username                string          `json:"username"`
	Revision                string          `json:"revision"`
	Items                   []WatchlistItem `json:"items"`
	ExternalSearchAvailable bool            `json:"external_search_available"`
}

// UNION (rather than UNION ALL) terminates even corrupt redirect cycles. Only
// actual canonical targets are returned; no title-based identity inference.
const watchlistResolution = `WITH RECURSIVE resolved(saved_id,id,redirect_to_id,added_at) AS (
 SELECT i.public_movie_id,p.id,p.redirect_to_id,i.added_at FROM account_watchlist_items i
 JOIN public_movies p ON p.id=i.public_movie_id WHERE i.account_id=$1
 UNION SELECT r.saved_id,p.id,p.redirect_to_id,r.added_at FROM resolved r
 JOIN public_movies p ON p.id=r.redirect_to_id
) `

const watchlistSummary = ` 'film-' || p.id::text,
 CASE WHEN o.title_overridden THEN o.title ELSE p.title END,
 COALESCE(CASE WHEN o.poster_url_overridden THEN o.poster_url ELSE p.poster_url END,''),
 COALESCE((CASE WHEN o.release_date_overridden THEN o.release_date ELSE p.release_date END)::text,'') `

func (s *Service) readWatchlist(ctx context.Context, tx pgx.Tx, a account) (WatchlistView, int64, error) {
	view := WatchlistView{Username: *a.username, Revision: "0", Items: []WatchlistItem{}, ExternalSearchAvailable: s.watchlistProvider != nil}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM account_watchlist_state WHERE account_id=$1`, a.id).Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return view, 0, ErrWatchlistUnavailable
	}
	view.Revision = strconv.FormatInt(revision, 10)
	rows, err := tx.Query(ctx, watchlistResolution+`SELECT `+watchlistSummary+`, saved.added_at, COALESCE(fr.french_release_date::text,'')
 FROM (SELECT id,min(added_at) added_at FROM resolved WHERE redirect_to_id IS NULL GROUP BY id) saved
 JOIN public_movies p ON p.id=saved.id LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=p.id
 `+watchlistReleaseObservation+` ORDER BY saved.added_at DESC, 'film-' || p.id::text`, a.id)
	if err != nil {
		return view, 0, ErrWatchlistUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var item WatchlistItem
		if err := rows.Scan(&item.Slug, &item.Title, &item.PosterURL, &item.ReleaseDate, &item.AddedAt, &item.FrenchReleaseDate); err != nil {
			return view, 0, ErrWatchlistUnavailable
		}
		item.AddedAt = item.AddedAt.UTC()
		view.Items = append(view.Items, item)
	}
	if rows.Err() != nil {
		return view, 0, ErrWatchlistUnavailable
	}
	return view, revision, nil
}

func watchlistError(err error) error {
	if errors.Is(err, ErrUnavailable) {
		return ErrWatchlistUnavailable
	}
	return err
}

func (s *Service) Watchlist(ctx context.Context, raw string) (WatchlistView, error) {
	var view WatchlistView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		view, _, err = s.readWatchlist(ctx, tx, a)
		return err
	})
	if err != nil {
		return WatchlistView{}, watchlistError(err)
	}
	return view, nil
}

func resolveWatchlistMovie(ctx context.Context, tx pgx.Tx, slug string) (int64, error) {
	if !theaterPreferenceID.MatchString(slug) {
		return 0, ErrInvalidInput
	}
	var id int64
	err := tx.QueryRow(ctx, `WITH RECURSIVE target(id,redirect_to_id) AS (
 SELECT p.id,p.redirect_to_id FROM public_movies p WHERE 'film-' || p.id::text=$1
 UNION SELECT p.id,p.redirect_to_id FROM movie_slug_aliases a JOIN public_movies p ON p.id=a.public_movie_id WHERE a.slug=$1
 UNION SELECT p.id,p.redirect_to_id FROM target t JOIN public_movies p ON p.id=t.redirect_to_id
) SELECT id FROM target WHERE redirect_to_id IS NULL`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrMovieNotFound
	}
	if err != nil {
		return 0, ErrWatchlistUnavailable
	}
	return id, nil
}

func watchlistMembership(ctx context.Context, tx pgx.Tx, accountID, movieID int64) (bool, int, error) {
	var saved bool
	var count int
	err := tx.QueryRow(ctx, watchlistResolution+`SELECT EXISTS(SELECT 1 FROM resolved WHERE id=$2 AND redirect_to_id IS NULL),
 (SELECT count(*) FROM account_watchlist_items WHERE account_id=$1)`, accountID, movieID).Scan(&saved, &count)
	if err != nil {
		return false, 0, ErrWatchlistUnavailable
	}
	return saved, count, nil
}

func (s *Service) changeWatchlist(ctx context.Context, tx pgx.Tx, a account, id, revision int64, saved bool) error {
	exists, count, err := watchlistMembership(ctx, tx, a.id, id)
	if err != nil {
		return err
	}
	if exists == saved {
		return nil
	}
	if saved && count >= maxWatchlistItems {
		return ErrWatchlistLimit
	}
	if revision == maxTheaterPreferenceRevision {
		return ErrWatchlistUnavailable
	}
	if saved {
		_, err = tx.Exec(ctx, `INSERT INTO account_watchlist_items(account_id,public_movie_id,added_at) VALUES($1,$2,$3)`, a.id, id, s.now().UTC())
	} else {
		_, err = tx.Exec(ctx, watchlistResolution+`DELETE FROM account_watchlist_items WHERE account_id=$1 AND public_movie_id IN (SELECT saved_id FROM resolved WHERE id=$2 AND redirect_to_id IS NULL)`, a.id, id)
	}
	if err != nil {
		return ErrWatchlistUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_watchlist_state(account_id,revision) VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET revision=EXCLUDED.revision`, a.id, revision+1)
	if err != nil {
		return ErrWatchlistUnavailable
	}
	return nil
}

func (s *Service) SaveWatchlist(ctx context.Context, raw, username, expectedRevision, slug string, saved bool) (WatchlistView, error) {
	revision, err := theaterRevision(expectedRevision)
	if err != nil || !theaterPreferenceID.MatchString(slug) {
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
		_, stored, err := s.readWatchlist(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrWatchlistChanged
		}
		// Catalog writers never acquire account locks, so this order cannot invert.
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(6211428337968315)); err != nil {
			return ErrWatchlistUnavailable
		}
		id, err := resolveWatchlistMovie(ctx, tx, slug)
		if err != nil {
			return err
		}
		if err = s.changeWatchlist(ctx, tx, a, id, stored, saved); err != nil {
			return err
		}
		view, _, err = s.readWatchlist(ctx, tx, a)
		return err
	})
	if err != nil {
		return WatchlistView{}, watchlistError(err)
	}
	return view, nil
}
