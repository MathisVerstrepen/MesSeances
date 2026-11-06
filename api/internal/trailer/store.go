package trailer

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SaveObservations participates in the caller's publication transaction. Unknown
// optional values do not erase evidence; a valid explicit absence withdraws it.
func SaveObservations(ctx context.Context, tx pgx.Tx, observations []Observation, now time.Time) (bool, error) {
	changed := false
	for _, o := range observations {
		if !o.Observed || o.Provider != "cgr" && o.Provider != "cineville" && o.Provider != "ugc" || o.Media != nil && (!Valid(o.Media) || o.Media.Source != o.Provider) {
			continue
		}
		keep := ""
		if o.Media != nil {
			keep = o.Media.URL
		}
		tag, err := tx.Exec(ctx, `UPDATE movie_trailer_candidates SET available=false WHERE provider=$1 AND source_movie_id=$2 AND available AND url<>$3`, o.Provider, o.MovieID, keep)
		if err != nil {
			return false, fmt.Errorf("withdraw trailer failed")
		}
		changed = changed || tag.RowsAffected() > 0
		if o.Media == nil {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO movie_trailer_candidates(provider,source_movie_id,media_type,url,first_seen_at,last_seen_at) VALUES($1,$2,$3,$4,$5,$5)
 ON CONFLICT(provider,source_movie_id,url) DO UPDATE SET available=true,last_seen_at=greatest(movie_trailer_candidates.last_seen_at,excluded.last_seen_at)`, o.Provider, o.MovieID, o.Media.Type, o.Media.URL, now)
		if err != nil {
			return false, fmt.Errorf("save trailer failed")
		}
		changed = true // Includes revivals and freshness changes.
	}
	return changed, nil
}

// Fallbacks follows current source mappings on every load, so corrected matches,
// merges, TMDB refreshes and admin overrides immediately change eligibility.
func Fallbacks(ctx context.Context, tx pgx.Tx) (map[int64]*Media, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON(e.public_movie_id) e.public_movie_id,c.media_type,c.url,c.provider
 FROM movie_trailer_eligible_sources e JOIN movie_trailer_candidates c ON c.provider=e.provider AND c.source_movie_id=e.source_movie_id
 WHERE c.available
 ORDER BY e.public_movie_id,CASE c.provider WHEN 'cgr' THEN 0 WHEN 'cineville' THEN 1 ELSE 2 END,c.last_seen_at DESC,c.url`)
	if err != nil {
		return nil, fmt.Errorf("read fallback trailers failed")
	}
	defer rows.Close()
	result := map[int64]*Media{}
	for rows.Next() {
		var id int64
		var m Media
		if err := rows.Scan(&id, &m.Type, &m.URL, &m.Source); err != nil {
			return nil, fmt.Errorf("read fallback trailers failed")
		}
		if Valid(&m) {
			result[id] = &m
		}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read fallback trailers failed")
	}
	return result, nil
}

type connection interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Begin(context.Context) (pgx.Tx, error)
}
type Store struct {
	Pool *pgxpool.Pool
	conn connection
}

func (s *Store) db() connection {
	if s.conn != nil {
		return s.conn
	}
	return s.Pool
}

// withLease pins a connection for the entire cycle. Other API replicas skip it.
func (s *Store) withLease(ctx context.Context, run func(*Store) error) error {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("trailer lease unavailable")
	}
	defer conn.Release()
	const lockID int64 = 6211428337968391
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&ok); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.Conn().Close(cleanup)
		cancel()
		return fmt.Errorf("trailer lease unavailable")
	}
	if !ok {
		return nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRow(cleanup, `SELECT pg_advisory_unlock($1)`, lockID).Scan(&unlocked); err != nil || !unlocked {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	return run(&Store{Pool: s.Pool, conn: conn})
}

func (s *Store) due(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db().Query(ctx, `SELECT source_movie_id FROM (
 SELECT DISTINCT ON(e.public_movie_id) e.public_movie_id,e.source_movie_id,l.checked_at
 FROM movie_trailer_eligible_sources e
 LEFT JOIN movie_trailer_lookups l ON l.provider=e.provider AND l.source_movie_id=e.source_movie_id
 WHERE e.provider='ugc' AND (l.retry_after IS NULL OR l.retry_after<=$1)
 AND EXISTS(SELECT 1 FROM showtimes sh JOIN schedule_snapshot ss ON ss.version=sh.generation_id AND ss.singleton JOIN public_movie_sources ps ON ps.source_provider=sh.provider AND ps.source_movie_id=sh.movie_provider_id WHERE ps.public_movie_id=e.public_movie_id AND sh.start_time>$1)
 AND NOT EXISTS(SELECT 1 FROM movie_trailer_eligible_sources other JOIN movie_trailer_candidates c ON c.provider=other.provider AND c.source_movie_id=other.source_movie_id WHERE other.public_movie_id=e.public_movie_id AND c.available AND (c.provider<>'ugc' OR c.last_seen_at>$1-interval '7 days'))
 ORDER BY e.public_movie_id,l.checked_at ASC NULLS FIRST,e.source_movie_id
 ) due ORDER BY checked_at ASC NULLS FIRST,source_movie_id LIMIT 20`, now)
	if err != nil {
		return nil, fmt.Errorf("select missing trailers failed")
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil, fmt.Errorf("select missing trailers failed")
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("select missing trailers failed")
	}
	return ids, nil
}

func (s *Store) saveLookup(ctx context.Context, id string, m *Media, failed bool, now time.Time) error {
	tx, err := s.db().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin trailer lookup failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := lockPublication(ctx, tx); err != nil {
		return err
	}
	// Recheck after the network request; an in-flight lookup cannot overwrite a
	// new manual/TMDB decision or attach evidence to an unverified movie mapping.
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM movie_trailer_eligible_sources WHERE provider='ugc' AND source_movie_id=$1)`, id).Scan(&eligible); err != nil {
		return fmt.Errorf("check trailer eligibility failed")
	}
	if !eligible {
		return nil
	}
	outcome := "missing"
	retry := 24 * time.Hour
	if m != nil {
		outcome = "found"
		retry = 7 * 24 * time.Hour
	}
	if failed {
		outcome = "failed"
		retry = time.Hour
	}
	if _, err := tx.Exec(ctx, `INSERT INTO movie_trailer_lookups(provider,source_movie_id,outcome,checked_at,retry_after) VALUES('ugc',$1,$2,$3,$4) ON CONFLICT(provider,source_movie_id) DO UPDATE SET outcome=excluded.outcome,checked_at=excluded.checked_at,retry_after=excluded.retry_after`, id, outcome, now, now.Add(retry)); err != nil {
		return fmt.Errorf("save trailer lookup failed")
	}
	if !failed {
		changed, err := SaveObservations(ctx, tx, []Observation{{Provider: "ugc", MovieID: id, Observed: true, Media: m}}, now)
		if err != nil {
			return err
		}
		if changed {
			if _, err := tx.Exec(ctx, `UPDATE movie_enrichment_state SET version=version+1 WHERE singleton`); err != nil {
				return fmt.Errorf("publish trailer revision failed")
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit trailer lookup failed")
	}
	return nil
}

// Match schedulepg/publicmoviepg writer ordering before touching candidates or
// enrichment state. Network requests never hold this transaction lock.
func lockPublication(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(6211428337968315)`); err != nil {
		return fmt.Errorf("lock trailer publication failed")
	}
	return nil
}

func (s *Store) expire(ctx context.Context, now time.Time) error {
	tx, err := s.db().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin trailer expiry failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := lockPublication(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `WITH expired AS(UPDATE movie_trailer_candidates SET available=false WHERE available AND last_seen_at<$1 RETURNING 1) UPDATE movie_enrichment_state SET version=version+1 WHERE singleton AND EXISTS(SELECT 1 FROM expired)`, now.Add(-30*24*time.Hour)); err != nil {
		return fmt.Errorf("expire trailers failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit trailer expiry failed")
	}
	return nil
}
