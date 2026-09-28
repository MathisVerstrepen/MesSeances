package accounts

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/tmdb"
)

type WatchlistProvider interface {
	Search(context.Context, string) ([]tmdb.Candidate, error)
	Details(context.Context, int64) (tmdb.Details, error)
}

type WatchlistExternalMovie struct {
	TMDBID        string `json:"tmdb_id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	PosterURL     string `json:"poster_url,omitempty"`
	ReleaseDate   string `json:"release_date,omitempty"`
}

type WatchlistSearchView struct {
	Username       string                   `json:"username"`
	Catalog        []WatchlistMovie         `json:"catalog"`
	External       []WatchlistExternalMovie `json:"external"`
	ExternalStatus string                   `json:"external_status"`
	CatalogHasMore bool                     `json:"catalog_has_more"`
}

type WatchlistImportView struct {
	Watchlist WatchlistView `json:"watchlist"`
	MovieSlug string        `json:"movie_slug"`
}

func watchlistQuery(raw string) (string, error) {
	query := strings.TrimSpace(raw)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 200 || strings.ContainsRune(query, 0) {
		return "", ErrInvalidInput
	}
	return query, nil
}

func watchlistTMDBID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return 0, ErrInvalidInput
	}
	return id, nil
}

func (s *Service) watchlistOwner(ctx context.Context, raw, username, purpose string) (account, error) {
	var owner account
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if *a.username != username {
			return ErrUnauthorized
		}
		owner = a
		return nil
	})
	if err != nil {
		return account{}, watchlistError(err)
	}
	if err = s.quota(ctx, purpose, "account:"+strconv.FormatInt(owner.id, 10), false); err != nil {
		return account{}, watchlistError(err)
	}
	return owner, nil
}

func (s *Service) admitWatchlistExternal() (func(), error) {
	select {
	case s.watchlistGate <- struct{}{}:
		return func() { <-s.watchlistGate }, nil
	default:
		return nil, &RateLimitError{RetryAfter: 1}
	}
}

func (s *Service) SearchWatchlist(ctx context.Context, raw, username, rawQuery string) (WatchlistSearchView, error) {
	query, err := watchlistQuery(rawQuery)
	if err != nil {
		return WatchlistSearchView{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	owner, err := s.watchlistOwner(ctx, raw, username, "watchlist_search")
	if err != nil {
		return WatchlistSearchView{}, err
	}
	view := WatchlistSearchView{Username: username, Catalog: []WatchlistMovie{}, External: []WatchlistExternalMovie{}, ExternalStatus: "disabled"}
	var candidates []tmdb.Candidate
	if s.watchlistProvider != nil {
		release, err := s.admitWatchlistExternal()
		if err != nil {
			return WatchlistSearchView{}, err
		}
		defer release()
		providerCtx, providerCancel := context.WithTimeout(ctx, 5*time.Second)
		candidates, err = s.watchlistProvider.Search(providerCtx, query)
		providerCancel()
		view.ExternalStatus = "ready"
		if err != nil {
			candidates = nil
			view.ExternalStatus = "unavailable"
		}
	}
	// Reauthorize after all upstream work, including errors, before exposing any
	// results. Durable catalog reads also see imports outside the live snapshot.
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.id != owner.id || a.revision != owner.revision || *a.username != username {
			return ErrUnauthorized
		}
		rows, err := tx.Query(ctx, `SELECT `+watchlistSummary+` FROM public_movies p
 LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=p.id
 WHERE p.redirect_to_id IS NULL AND strpos(lower(CASE WHEN o.title_overridden THEN o.title ELSE p.title END),lower($1))>0
 ORDER BY lower(CASE WHEN o.title_overridden THEN o.title ELSE p.title END), 'film-' || p.id::text LIMIT 21`, query)
		if err != nil {
			return ErrWatchlistUnavailable
		}
		for rows.Next() {
			var movie WatchlistMovie
			if err = rows.Scan(&movie.Slug, &movie.Title, &movie.PosterURL, &movie.ReleaseDate); err != nil {
				rows.Close()
				return ErrWatchlistUnavailable
			}
			view.Catalog = append(view.Catalog, movie)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrWatchlistUnavailable
		}
		if len(view.Catalog) > 20 {
			view.CatalogHasMore = true
			view.Catalog = view.Catalog[:20]
		}
		ids := make([]int64, 0, min(len(candidates), 20))
		for _, candidate := range candidates[:min(len(candidates), 20)] {
			ids = append(ids, candidate.ID)
		}
		rows, err = tx.Query(ctx, `SELECT confirmed_tmdb_id FROM public_movies WHERE redirect_to_id IS NULL AND confirmed_tmdb_id=ANY($1::bigint[])`, ids)
		if err != nil {
			return ErrWatchlistUnavailable
		}
		known := map[int64]bool{}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) != nil {
				rows.Close()
				return ErrWatchlistUnavailable
			}
			known[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrWatchlistUnavailable
		}
		for _, candidate := range candidates[:min(len(candidates), 20)] {
			if candidate.ID <= 0 || known[candidate.ID] {
				continue
			}
			known[candidate.ID] = true
			view.External = append(view.External, WatchlistExternalMovie{TMDBID: strconv.FormatInt(candidate.ID, 10), Title: candidate.Title, OriginalTitle: candidate.OriginalTitle, PosterURL: candidate.PosterURL, ReleaseDate: candidate.ReleaseDate})
		}
		return nil
	})
	if err != nil {
		return WatchlistSearchView{}, watchlistError(err)
	}
	return view, nil
}

func (s *Service) ImportWatchlist(ctx context.Context, raw, username, expectedRevision, rawID string) (WatchlistImportView, error) {
	id, err := watchlistTMDBID(rawID)
	if err != nil {
		return WatchlistImportView{}, err
	}
	revision, err := theaterRevision(expectedRevision)
	if err != nil {
		return WatchlistImportView{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	owner, err := s.watchlistOwner(ctx, raw, username, "watchlist_import")
	if err != nil {
		return WatchlistImportView{}, err
	}
	if s.watchlistProvider == nil || s.watchlistRefresh == nil {
		return WatchlistImportView{}, ErrWatchlistExternalUnavailable
	}
	release, err := s.admitWatchlistExternal()
	if err != nil {
		return WatchlistImportView{}, err
	}
	defer release()
	providerCtx, providerCancel := context.WithTimeout(ctx, 5*time.Second)
	details, providerErr := s.watchlistProvider.Details(providerCtx, id)
	providerCancel()
	var result WatchlistImportView
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.id != owner.id || a.revision != owner.revision || *a.username != username {
			return ErrUnauthorized
		}
		_, stored, err := s.readWatchlist(ctx, tx, a)
		if err != nil {
			return err
		}
		if stored != revision {
			return ErrWatchlistChanged
		}
		if errors.Is(providerErr, tmdb.ErrNotFound) {
			return enrichment.ErrMovieNotImportable
		}
		if providerErr != nil {
			return ErrWatchlistExternalUnavailable
		}
		if details.ID != id || details.Adult == nil || *details.Adult {
			return enrichment.ErrMovieNotImportable
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(6211428337968315)); err != nil {
			return ErrWatchlistUnavailable
		}
		var existingID int64
		err = tx.QueryRow(ctx, `SELECT id FROM public_movies WHERE confirmed_tmdb_id=$1 AND redirect_to_id IS NULL`, id).Scan(&existingID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ErrWatchlistUnavailable
		}
		saved, count, err := watchlistMembership(ctx, tx, a.id, existingID)
		if err != nil {
			return err
		}
		if !saved && count >= maxWatchlistItems {
			return ErrWatchlistLimit
		}
		if !saved && stored == maxTheaterPreferenceRevision {
			return ErrWatchlistUnavailable
		}
		publicID, err := enrichment.ImportCatalogMovie(ctx, tx, details, s.now())
		if errors.Is(err, enrichment.ErrMovieNotImportable) {
			return err
		}
		if err != nil {
			return ErrWatchlistUnavailable
		}
		if err = s.changeWatchlist(ctx, tx, a, publicID, stored, true); err != nil {
			return err
		}
		result.Watchlist, _, err = s.readWatchlist(ctx, tx, a)
		result.MovieSlug = "film-" + strconv.FormatInt(publicID, 10)
		return err
	})
	if err != nil {
		return WatchlistImportView{}, watchlistError(err)
	}
	// Failure here is an uncertain committed write. Never compensate or replay.
	if err = s.watchlistRefresh(ctx, result.MovieSlug); err != nil {
		return WatchlistImportView{}, ErrWatchlistUnavailable
	}
	// Refresh can block too: don't return private state after logout/revocation.
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.id != owner.id || a.revision != owner.revision || *a.username != username {
			return ErrUnauthorized
		}
		result.Watchlist, _, err = s.readWatchlist(ctx, tx, a)
		return err
	})
	if err != nil {
		return WatchlistImportView{}, watchlistError(err)
	}
	return result, nil
}
