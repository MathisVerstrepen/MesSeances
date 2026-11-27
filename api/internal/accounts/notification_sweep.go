package accounts

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// notificationJournalWriterLock is schedulepg's snapshot writer lock. Every
	// journal insert happens while a publication holds it exclusively, so a
	// shared holder observes a fully committed, gap-stable identity prefix.
	notificationJournalWriterLock int64 = 6211428337968315

	notificationSweepBatch     = 500
	notificationSweepMaxRounds = 10
	notificationRetention      = 90 * 24 * time.Hour
	maxStoredNotifications     = 200
	maxNotificationMovies      = 1000
	maxNotificationTheaters    = maxTheaterPreferences
)

type NotificationSweepResult struct {
	Events, Notifications, Expired int64
	Busy                           bool
}

// notificationMatchSQL projects surviving nonbaseline journal events in
// ($1,$2] onto enabled rules whose arming position precedes the event. Cinema
// scope is resolved at sweep time; $3 optionally restricts owner accounts.
const notificationMatchSQL = `WITH ev AS MATERIALIZED (
 SELECT e.id,e.theater_id,e.kind,c.id movie_id FROM cinema_activity_episodes e
 JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(e.anchor_provider,e.anchor_source_movie_id)
 JOIN public_movies p ON p.id=s.public_movie_id
 JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id) AND c.redirect_to_id IS NULL
 WHERE e.id>$1 AND e.id<=$2 AND e.superseded_by_id IS NULL AND e.kind<>'baseline'),
 rules AS MATERIALIZED (
 SELECT r.account_id,r.id,r.kind,r.include_returns,r.armed_after_event_id,
 CASE r.theater_scope WHEN 'custom' THEN r.theater_ids WHEN 'followed' THEN coalesce(f.theater_ids,'{}'::text[])
 ELSE coalesce(tp.theater_ids,'{}'::text[]) END theater_ids
 FROM account_notification_rules r
 LEFT JOIN account_theater_follows f ON f.account_id=r.account_id
 LEFT JOIN account_theater_preferences tp ON tp.account_id=r.account_id
 WHERE r.enabled AND r.armed_after_event_id<$2 AND ($3::bigint[] IS NULL OR r.account_id=ANY($3::bigint[])))
 SELECT r.account_id,r.id,r.kind,e.id,e.theater_id,e.movie_id FROM rules r
 JOIN ev e ON e.id>r.armed_after_event_id AND e.theater_id=ANY(r.theater_ids)
 WHERE (r.kind='cinema_activity' AND (e.kind='added_to_program' OR r.include_returns))
 OR (r.kind='watchlist_screening' AND e.kind='added_to_program' AND EXISTS (
 SELECT 1 FROM account_watchlist_items w JOIN public_movies wp ON wp.id=w.public_movie_id
 WHERE w.account_id=r.account_id AND coalesce(wp.redirect_to_id,wp.id)=e.movie_id))`

type notificationMatch struct {
	account, rule, event, movie int64
	kind, theater               string
}

type notificationGroup struct {
	account, rule, movie int64
	kind                 string
	movies               []int64
	theaters             []string
	events               int
	last                 int64
}

// SweepNotifications converts newly committed journal events into inbox
// entries. It never runs in a user request and never blocks publication for
// longer than one short shared-lock read.
func (s *Service) SweepNotifications(ctx context.Context) (NotificationSweepResult, error) {
	var result NotificationSweepResult
	if !s.notificationMu.TryLock() {
		result.Busy = true
		return result, nil
	}
	defer s.notificationMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for range notificationSweepMaxRounds {
		lo, hi, count, busy, err := s.notificationRange(ctx)
		if err != nil {
			return result, err
		}
		if busy {
			result.Busy = true
			break
		}
		if count == 0 {
			break
		}
		delivered, err := s.deliverNotifications(ctx, lo, hi)
		if err != nil {
			return result, err
		}
		result.Events += int64(count)
		result.Notifications += delivered
		if count < notificationSweepBatch {
			break
		}
	}
	expired, err := s.expireNotifications(ctx)
	result.Expired = expired
	return result, err
}

// notificationRange runs under the shared writer lock and releases it before
// any account lock: user writes take account then writer locks.
func (s *Service) notificationRange(ctx context.Context) (lo, hi int64, count int, busy bool, err error) {
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='2s'`); err != nil {
			return ErrUnavailable
		}
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock_shared($1)`, notificationJournalWriterLock).Scan(&locked); err != nil {
			return ErrUnavailable
		}
		if !locked {
			busy = true
			return nil
		}
		var top *int64
		err := tx.QueryRow(ctx, `SELECT c.last_event_id,(SELECT max(id) FROM (SELECT id FROM cinema_activity_episodes WHERE id>c.last_event_id ORDER BY id LIMIT $1) b),
 (SELECT count(*) FROM (SELECT id FROM cinema_activity_episodes WHERE id>c.last_event_id ORDER BY id LIMIT $1) b)
 FROM account_notification_cursor c`, notificationSweepBatch).Scan(&lo, &top, &count)
		if err != nil {
			return ErrUnavailable
		}
		if top != nil {
			hi = *top
		}
		return nil
	})
	return lo, hi, count, busy, err
}

func (s *Service) deliverNotifications(ctx context.Context, lo, hi int64) (int64, error) {
	var delivered int64
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='10s'`); err != nil {
			return ErrUnavailable
		}
		var current int64
		if err := tx.QueryRow(ctx, `SELECT last_event_id FROM account_notification_cursor FOR UPDATE`).Scan(&current); err != nil {
			return ErrUnavailable
		}
		if current != lo {
			return nil // another replica consumed this range
		}
		owners, err := notificationOwners(ctx, tx, lo, hi)
		if err != nil {
			return err
		}
		if len(owners) > 0 {
			// Account-first convention, in ID order, before any inbox row lock.
			if _, err := tx.Exec(ctx, `SELECT id FROM accounts WHERE id=ANY($1) ORDER BY id FOR UPDATE`, owners); err != nil {
				return ErrUnavailable
			}
			matches, err := readNotificationMatches(ctx, tx, lo, hi, owners)
			if err != nil {
				return err
			}
			now := s.now().UTC()
			for _, g := range groupNotificationMatches(matches) {
				inserted, err := writeNotificationGroup(ctx, tx, g, now)
				if err != nil {
					return err
				}
				if inserted {
					delivered++
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM account_notifications WHERE id IN (SELECT id FROM (
 SELECT id,row_number() OVER (PARTITION BY account_id ORDER BY id DESC) n FROM account_notifications WHERE account_id=ANY($1)) ranked WHERE n>$2)`, owners, maxStoredNotifications); err != nil {
				return ErrUnavailable
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE account_notification_cursor SET last_event_id=$1,updated_at=$2`, hi, s.now().UTC()); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return delivered, err
}

func notificationOwners(ctx context.Context, tx pgx.Tx, lo, hi int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT m.account_id FROM (`+notificationMatchSQL+`) m(account_id,rule_id,kind,event_id,theater_id,movie_id) ORDER BY 1`, lo, hi, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	owners, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, ErrUnavailable
	}
	return owners, nil
}

func readNotificationMatches(ctx context.Context, tx pgx.Tx, lo, hi int64, owners []int64) ([]notificationMatch, error) {
	rows, err := tx.Query(ctx, notificationMatchSQL+` ORDER BY 1,2,4`, lo, hi, owners)
	if err != nil {
		return nil, ErrUnavailable
	}
	matches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (notificationMatch, error) {
		var m notificationMatch
		err := row.Scan(&m.account, &m.rule, &m.kind, &m.event, &m.theater, &m.movie)
		return m, err
	})
	if err != nil {
		return nil, ErrUnavailable
	}
	return matches, nil
}

// groupNotificationMatches expects (account, rule, event) ordering. Activity
// rules produce one group per rule; watchlist rules one group per film. Lists
// are distinct and newest first.
func groupNotificationMatches(matches []notificationMatch) []*notificationGroup {
	groups := []*notificationGroup{}
	index := map[[3]int64]*notificationGroup{}
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		key := [3]int64{m.account, m.rule, 0}
		if m.kind == NotificationKindWatchlist {
			key[2] = m.movie
		}
		g := index[key]
		if g == nil {
			g = &notificationGroup{account: m.account, rule: m.rule, kind: m.kind, last: m.event}
			if m.kind == NotificationKindWatchlist {
				g.movie = m.movie
			}
			index[key] = g
			groups = append(groups, g)
		}
		g.events++
		g.movies = appendDistinct(g.movies, m.movie)
		g.theaters = appendDistinct(g.theaters, m.theater)
	}
	// Restore deterministic (account, rule, oldest group first) write order.
	for i, j := 0, len(groups)-1; i < j; i, j = i+1, j-1 {
		groups[i], groups[j] = groups[j], groups[i]
	}
	return groups
}

func appendDistinct[T comparable](items []T, item T) []T {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func mergeDistinct[T comparable](newer, older []T, limit int) []T {
	merged := append([]T{}, newer...)
	for _, item := range older {
		merged = appendDistinct(merged, item)
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

func writeNotificationGroup(ctx context.Context, tx pgx.Tx, g *notificationGroup, now time.Time) (bool, error) {
	if g.kind == NotificationKindWatchlist {
		tag, err := tx.Exec(ctx, `INSERT INTO account_notifications(account_id,rule_id,kind,public_movie_id,movie_ids,theater_ids,event_count,last_event_id,created_at)
 VALUES($1,$2,'watchlist_screening',$3,ARRAY[$3::bigint],$4,$5,$6,$7)
 ON CONFLICT (account_id,rule_id,public_movie_id) WHERE kind='watchlist_screening' DO NOTHING`,
			g.account, g.rule, g.movie, limitSlice(g.theaters, maxNotificationTheaters), g.events, g.last, now)
		if err != nil {
			return false, ErrUnavailable
		}
		return tag.RowsAffected() == 1, nil
	}
	// An unread activity entry for the same rule is replaced by a merged entry
	// with a new ID, so the inbox stays ordered by latest delivery.
	rows, err := tx.Query(ctx, `DELETE FROM account_notifications WHERE account_id=$1 AND rule_id=$2 AND kind='cinema_activity' AND read_at IS NULL RETURNING id,movie_ids,theater_ids,event_count`, g.account, g.rule)
	if err != nil {
		return false, ErrUnavailable
	}
	type previous struct {
		id       int64
		movies   []int64
		theaters []string
		events   int
	}
	olds, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (previous, error) {
		var p previous
		err := row.Scan(&p.id, &p.movies, &p.theaters, &p.events)
		return p, err
	})
	if err != nil {
		return false, ErrUnavailable
	}
	slices.SortFunc(olds, func(a, b previous) int { return cmp.Compare(b.id, a.id) })
	movies, theaters, events := g.movies, g.theaters, g.events
	for _, old := range olds { // newest previous first
		movies = mergeDistinct(movies, old.movies, maxNotificationMovies)
		theaters = mergeDistinct(theaters, old.theaters, maxNotificationTheaters)
		events += old.events
	}
	movies, theaters = limitSlice(movies, maxNotificationMovies), limitSlice(theaters, maxNotificationTheaters)
	_, err = tx.Exec(ctx, `INSERT INTO account_notifications(account_id,rule_id,kind,movie_ids,theater_ids,event_count,last_event_id,created_at)
 VALUES($1,$2,'cinema_activity',$3,$4,$5,$6,$7)`, g.account, g.rule, movies, theaters, events, g.last, now)
	if err != nil {
		return false, ErrUnavailable
	}
	return true, nil
}

func limitSlice[T any](items []T, limit int) []T {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

// expireNotifications applies age retention under ordered account locks,
// skipping accounts currently busy with a request.
func (s *Service) expireNotifications(ctx context.Context) (int64, error) {
	var expired int64
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		cutoff := s.now().UTC().Add(-notificationRetention)
		rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE id IN (SELECT DISTINCT account_id FROM account_notifications WHERE created_at<$1 LIMIT 100) ORDER BY id FOR UPDATE SKIP LOCKED`, cutoff)
		if err != nil {
			return ErrUnavailable
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return ErrUnavailable
		}
		if len(ids) == 0 {
			return nil
		}
		tag, err := tx.Exec(ctx, `DELETE FROM account_notifications WHERE account_id=ANY($1) AND created_at<$2`, ids, cutoff)
		if err != nil {
			return ErrUnavailable
		}
		expired = tag.RowsAffected()
		return nil
	})
	return expired, err
}
