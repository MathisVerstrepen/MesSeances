package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/schedule"
)

const (
	DigestOff    = "off"
	DigestDaily  = "daily"
	DigestWeekly = "weekly"

	digestHour       = 8
	digestMaxItems   = 20
	digestMaxAge     = 8 * 24 * time.Hour
	digestBatch      = 20
	digestQueueLimit = 2000 // leave the shared outbox to security mail first
	digestLifetime   = 12 * time.Hour
)

var ErrDigestQueueBusy = errors.New("notification digest queue busy")

type DigestView struct {
	Username   string     `json:"username"`
	Frequency  string     `json:"frequency"`
	NextDueAt  *time.Time `json:"next_due_at"`
	LastSentAt *time.Time `json:"last_sent_at"`
	Available  bool       `json:"available"`
}

// nextDigest returns the first 08:00 Europe/Paris slot strictly after now:
// every day, or every Wednesday (French release day) for weekly digests.
func nextDigest(frequency string, now time.Time) (time.Time, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, ErrUnavailable
	}
	local := now.In(location)
	slot := time.Date(local.Year(), local.Month(), local.Day(), digestHour, 0, 0, 0, location)
	for !slot.After(now) || (frequency == DigestWeekly && slot.Weekday() != time.Wednesday) {
		slot = time.Date(slot.Year(), slot.Month(), slot.Day()+1, digestHour, 0, 0, 0, location)
	}
	return slot.UTC(), nil
}

func readDigest(ctx context.Context, tx pgx.Tx, a account, s *Service) (DigestView, error) {
	view := DigestView{Username: *a.username, Frequency: DigestOff, Available: s.mail != nil}
	err := tx.QueryRow(ctx, `SELECT frequency,next_due_at,last_sent_at FROM account_notification_digests WHERE account_id=$1`, a.id).Scan(&view.Frequency, &view.NextDueAt, &view.LastSentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return DigestView{}, ErrUnavailable
	}
	for _, at := range []*time.Time{view.NextDueAt, view.LastSentAt} {
		if at != nil {
			*at = at.UTC()
		}
	}
	return view, nil
}

func (s *Service) NotificationDigest(ctx context.Context, rawSession string) (DigestView, error) {
	var view DigestView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view, err = readDigest(ctx, tx, a, s)
		return err
	})
	if err != nil {
		return DigestView{}, err
	}
	return view, nil
}

// SaveNotificationDigest changes the account digest frequency. Enabling starts
// after the newest existing inbox entry, so history is never mailed.
func (s *Service) SaveNotificationDigest(ctx context.Context, rawSession, expectedUsername, frequency string) (DigestView, error) {
	switch frequency {
	case DigestOff, DigestDaily, DigestWeekly:
	default:
		return DigestView{}, ErrInvalidInput
	}
	if frequency != DigestOff && s.mail == nil {
		return DigestView{}, ErrUnavailable
	}
	var view DigestView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		current, err := readDigest(ctx, tx, a, s)
		if err != nil {
			return err
		}
		if current.Frequency != frequency {
			if err := s.setDigest(ctx, tx, a.id, frequency, current.Frequency == DigestOff); err != nil {
				return err
			}
		}
		view, err = readDigest(ctx, tx, a, s)
		return err
	})
	if err != nil {
		return DigestView{}, err
	}
	return view, nil
}

func (s *Service) setDigest(ctx context.Context, tx pgx.Tx, accountID int64, frequency string, rebase bool) error {
	now := s.now().UTC()
	var next *time.Time
	if frequency != DigestOff {
		at, err := nextDigest(frequency, now)
		if err != nil {
			return err
		}
		next = &at
	}
	_, err := tx.Exec(ctx, `INSERT INTO account_notification_digests(account_id,frequency,last_notification_id,next_due_at,updated_at)
 VALUES($1,$2,(SELECT coalesce(max(id),0) FROM account_notifications WHERE account_id=$1),$3,$4)
 ON CONFLICT (account_id) DO UPDATE SET frequency=EXCLUDED.frequency,next_due_at=EXCLUDED.next_due_at,updated_at=EXCLUDED.updated_at,
 last_notification_id=CASE WHEN $5 THEN EXCLUDED.last_notification_id ELSE account_notification_digests.last_notification_id END`,
		accountID, frequency, next, now, rebase)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// UnsubscribeToken authorizes only turning the digest off. It is bound to the
// account and its current address, so an email change invalidates old links.
func (s *Service) unsubscribeToken(accountID int64, email string) string {
	payload := binary.BigEndian.AppendUint64(nil, uint64(accountID))
	return base64.RawURLEncoding.EncodeToString(append(payload, s.unsubscribeMAC(accountID, email)...))
}

func (s *Service) unsubscribeMAC(accountID int64, email string) []byte {
	h := hmac.New(sha256.New, s.hmacKey)
	h.Write([]byte("messeances-digest-unsubscribe-v1\x00" + strconv.FormatInt(accountID, 10) + "\x00" + email))
	return h.Sum(nil)[:16]
}

// UnsubscribeDigest turns the digest off for a valid token. Unknown, stale or
// already unsubscribed tokens report the same success: the response never
// reveals whether an account exists.
func (s *Service) UnsubscribeDigest(ctx context.Context, token string) error {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 24 {
		return ErrInvalidLink
	}
	id := int64(binary.BigEndian.Uint64(raw[:8]))
	if id <= 0 {
		return ErrInvalidLink
	}
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, err := loadAccount(ctx, tx, id, true)
		if errors.Is(err, ErrUnauthorized) {
			return ErrInvalidLink
		}
		if err != nil {
			return err
		}
		if !hmac.Equal(raw[8:], s.unsubscribeMAC(a.id, a.email)) {
			return ErrInvalidLink
		}
		_, err = tx.Exec(ctx, `UPDATE account_notification_digests SET frequency='off',next_due_at=NULL,updated_at=$2 WHERE account_id=$1 AND frequency<>'off'`, a.id, s.now().UTC())
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

type DigestResult struct {
	Sent, Empty, Deferred int
}

// SweepDigests sends due digests in bounded batches, one account transaction
// each, taking the account lock before the shared mail admission lock.
func (s *Service) SweepDigests(ctx context.Context) (DigestResult, error) {
	var result DigestResult
	if s.mail == nil || !s.digestMu.TryLock() {
		return result, nil
	}
	defer s.digestMu.Unlock()
	var ids []int64
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT account_id FROM account_notification_digests WHERE next_due_at<=$1 ORDER BY next_due_at,account_id LIMIT $2`, s.now().UTC(), digestBatch)
		if err != nil {
			return ErrUnavailable
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		outcome, err := s.sendDigest(ctx, id)
		if errors.Is(err, ErrDigestQueueBusy) {
			result.Deferred++
			break
		}
		if err != nil {
			return result, err
		}
		if outcome {
			result.Sent++
		} else {
			result.Empty++
		}
	}
	return result, nil
}

func (s *Service) sendDigest(ctx context.Context, accountID int64) (bool, error) {
	sent := false
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		var pending int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox WHERE state='pending'`).Scan(&pending); err != nil {
			return ErrUnavailable
		}
		if pending >= digestQueueLimit {
			return ErrDigestQueueBusy
		}
		a, err := loadAccount(ctx, tx, accountID, true)
		if errors.Is(err, ErrUnauthorized) {
			return nil // deleted meanwhile; cascade removed the setting
		}
		if err != nil {
			return err
		}
		var frequency string
		var last int64
		var due *time.Time
		err = tx.QueryRow(ctx, `SELECT frequency,last_notification_id,next_due_at FROM account_notification_digests WHERE account_id=$1 FOR UPDATE`, accountID).Scan(&frequency, &last, &due)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		if due == nil || due.After(now) {
			return nil // changed or already handled by another replica
		}
		next, err := nextDigest(frequency, now)
		if err != nil {
			return err
		}
		var newest int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM account_notifications WHERE account_id=$1`, accountID).Scan(&newest); err != nil {
			return ErrUnavailable
		}
		items, total, err := readDigestItems(ctx, tx, accountID, last, now.Add(-digestMaxAge))
		if err != nil {
			return err
		}
		eligible := a.state() == StateComplete && a.verified != nil
		if eligible && len(items) > 0 {
			message, err := digestMailMessage(s.origin, a.email, frequency, items, total, s.origin+"/api/v1/notifications/unsubscribe?token="+s.unsubscribeToken(a.id, a.email), s.origin+"/notifications/desabonnement?token="+s.unsubscribeToken(a.id, a.email))
			if err != nil {
				return ErrUnavailable
			}
			_, event, err := s.newToken()
			if err != nil {
				return err
			}
			if err := s.enqueue(ctx, tx, a, "notification_digest", event[:], nil, message, now, now.Add(digestLifetime)); err != nil {
				if errors.Is(err, accountmail.ErrQueueFull) {
					return ErrDigestQueueBusy
				}
				return err
			}
			sent = true
		}
		var sentAt *time.Time
		if sent {
			sentAt = &now
		}
		_, err = tx.Exec(ctx, `UPDATE account_notification_digests SET last_notification_id=greatest(last_notification_id,$2),next_due_at=$3,last_sent_at=coalesce($4,last_sent_at) WHERE account_id=$1`, accountID, newest, next, sentAt)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return sent, err
}

// readDigestItems selects unread entries of email-enabled alerts delivered
// since the previous digest, newest first, with up to four resolved films
// (posters included) and their first cinema.
func readDigestItems(ctx context.Context, tx pgx.Tx, accountID, after int64, since time.Time) ([]NotificationView, int, error) {
	var total int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM account_notifications n JOIN account_notification_rules r ON r.account_id=n.account_id AND r.id=n.rule_id AND r.email
 WHERE n.account_id=$1 AND n.id>$2 AND n.read_at IS NULL AND n.created_at>$3`, accountID, after, since).Scan(&total)
	if err != nil {
		return nil, 0, ErrUnavailable
	}
	if total == 0 {
		return nil, 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT n.id,n.kind,n.rule_id,n.event_count,cardinality(n.movie_ids),cardinality(n.theater_ids),n.movie_ids[1:4],n.theater_ids[1:1]
 FROM account_notifications n JOIN account_notification_rules r ON r.account_id=n.account_id AND r.id=n.rule_id AND r.email
 WHERE n.account_id=$1 AND n.id>$2 AND n.read_at IS NULL AND n.created_at>$3 ORDER BY n.id DESC LIMIT $4`, accountID, after, since, digestMaxItems)
	if err != nil {
		return nil, 0, ErrUnavailable
	}
	type row struct {
		id       int64
		rule     *int64
		item     NotificationView
		movies   []int64
		theaters []string
	}
	collected, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var v row
		err := r.Scan(&v.id, &v.item.Kind, &v.rule, &v.item.EventCount, &v.item.MovieCount, &v.item.TheaterCount, &v.movies, &v.theaters)
		return v, err
	})
	if err != nil {
		return nil, 0, ErrUnavailable
	}
	movieSet, theaterSet := map[int64]bool{}, map[string]bool{}
	for _, v := range collected {
		for _, m := range v.movies {
			movieSet[m] = true
		}
		for _, t := range v.theaters {
			theaterSet[t] = true
		}
	}
	movies, err := readNotificationMovies(ctx, tx, movieSet)
	if err != nil {
		return nil, 0, err
	}
	cinemas, err := readNotificationTheaters(ctx, tx, theaterSet)
	if err != nil {
		return nil, 0, err
	}
	items := make([]NotificationView, 0, len(collected))
	for _, v := range collected {
		for _, m := range v.movies {
			if movie, ok := movies[m]; ok {
				v.item.Movies = append(v.item.Movies, movie)
			}
		}
		for _, t := range v.theaters {
			if theater, ok := cinemas[t]; ok {
				v.item.Theaters = append(v.item.Theaters, theater)
			}
		}
		items = append(items, v.item)
	}
	return items, total, nil
}
