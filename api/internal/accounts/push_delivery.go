package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/webpush"
)

const (
	pushBatch           = 20
	pushLease           = time.Minute
	pushLifetime        = 12 * time.Hour
	pushMaxAttempts     = 5
	pushMaxPending      = 50
	pushMaxFailureCount = 5
)

type PushDeliveryResult struct {
	Sent, Retried, Dropped, Removed int
}

// enqueuePush queues new inbox entries of push-enabled rules for every current
// device of their owner. It runs in the delivery transaction, under the owner
// locks, and is bounded per account.
func (s *Service) enqueuePush(ctx context.Context, tx pgx.Tx, notificationIDs []int64, now time.Time) error {
	if !s.pushAvailable() || len(notificationIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO account_push_outbox(account_id,subscription_id,notification_id,created_at,expires_at,next_attempt_at)
 SELECT n.account_id,p.id,n.id,$2,$2::timestamptz+$3::bigint*interval '1 second',$2 FROM account_notifications n
 JOIN account_notification_rules r ON r.account_id=n.account_id AND r.id=n.rule_id AND r.push AND r.enabled
 JOIN accounts a ON a.id=n.account_id
 JOIN account_push_subscriptions p ON p.account_id=n.account_id AND p.auth_revision=a.auth_revision
 WHERE n.id=ANY($1) AND (SELECT count(*) FROM account_push_outbox o WHERE o.account_id=n.account_id)<$4
 ON CONFLICT (subscription_id,notification_id) DO NOTHING`, notificationIDs, now, int64(pushLifetime/time.Second), pushMaxPending)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

type pushJob struct {
	id, account, subscription, notification int64
	attempts                                int
}

// DeliverPush sends one bounded batch of due messages. Each claimed row is
// leased, so concurrent replicas never send the same row at the same time.
func (s *Service) DeliverPush(ctx context.Context) (PushDeliveryResult, error) {
	var result PushDeliveryResult
	if !s.pushAvailable() || !s.pushMu.TryLock() {
		return result, nil
	}
	defer s.pushMu.Unlock()
	jobs, err := s.claimPush(ctx)
	if err != nil {
		return result, err
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			break
		}
		outcome, err := s.deliverPushJob(ctx, job)
		if err != nil {
			return result, err
		}
		switch outcome {
		case "sent":
			result.Sent++
		case "retry":
			result.Retried++
		case "removed":
			result.Removed++
		default:
			result.Dropped++
		}
	}
	return result, nil
}

func (s *Service) claimPush(ctx context.Context) ([]pushJob, error) {
	var jobs []pushJob
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		if _, err := tx.Exec(ctx, `DELETE FROM account_push_outbox WHERE id IN (SELECT id FROM account_push_outbox WHERE expires_at<=$1 LIMIT 1000)`, now); err != nil {
			return ErrUnavailable
		}
		// Devices are bound to the authority revision. Revocation (logout-all,
		// credential changes) makes them invisible and undeliverable at once;
		// purge them here without touching the revocation path. Their pending
		// rows are dropped at delivery.
		if _, err := tx.Exec(ctx, `DELETE FROM account_push_subscriptions WHERE id IN (SELECT s.id FROM account_push_subscriptions s
 JOIN accounts a ON a.id=s.account_id WHERE a.auth_revision<>s.auth_revision LIMIT 1000)`); err != nil {
			return ErrUnavailable
		}
		rows, err := tx.Query(ctx, `UPDATE account_push_outbox SET lease_until=$2,attempts=attempts+1 WHERE id IN (
 SELECT id FROM account_push_outbox WHERE next_attempt_at<=$1 AND (lease_until IS NULL OR lease_until<=$1) AND attempts<$4
 ORDER BY next_attempt_at,id LIMIT $3 FOR UPDATE SKIP LOCKED)
 RETURNING id,account_id,subscription_id,notification_id,attempts`, now, now.Add(pushLease), pushBatch, pushMaxAttempts)
		if err != nil {
			return ErrUnavailable
		}
		jobs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (pushJob, error) {
			var j pushJob
			err := row.Scan(&j.id, &j.account, &j.subscription, &j.notification, &j.attempts)
			return j, err
		})
		if err != nil {
			return ErrUnavailable
		}
		// Exhausted rows were never claimable again; drop them.
		if _, err := tx.Exec(ctx, `DELETE FROM account_push_outbox WHERE attempts>=$1 AND (lease_until IS NULL OR lease_until<=$2)`, pushMaxAttempts, now); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return jobs, err
}

func (s *Service) deliverPushJob(ctx context.Context, job pushJob) (string, error) {
	var digest []byte
	var envelope accountmail.Envelope
	var payload []byte
	var topic string
	valid := false
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT p.endpoint_digest,p.payload_key_id,p.payload_nonce,p.payload_ciphertext FROM account_push_subscriptions p
 JOIN accounts a ON a.id=p.account_id AND a.auth_revision=p.auth_revision WHERE p.account_id=$1 AND p.id=$2`, job.account, job.subscription).
			Scan(&digest, &envelope.KeyID, &envelope.Nonce, &envelope.Ciphertext)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		var read bool
		var item NotificationView
		var ruleID *int64
		var movieIDs []int64
		var theaterIDs []string
		var id int64
		err = tx.QueryRow(ctx, `SELECT id,kind,rule_id,read_at IS NOT NULL,event_count,cardinality(movie_ids),cardinality(theater_ids),movie_ids[1:3],theater_ids[1:3]
 FROM account_notifications WHERE account_id=$1 AND id=$2`, job.account, job.notification).
			Scan(&id, &item.Kind, &ruleID, &read, &item.EventCount, &item.MovieCount, &item.TheaterCount, &movieIDs, &theaterIDs)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && read) {
			return nil // already read in the app or replaced by a merged entry
		}
		if err != nil {
			return ErrUnavailable
		}
		movies, err := readNotificationMovies(ctx, tx, setOf(movieIDs))
		if err != nil {
			return err
		}
		cinemas, err := readNotificationTheaters(ctx, tx, setOf(theaterIDs))
		if err != nil {
			return err
		}
		for _, m := range movieIDs {
			if movie, ok := movies[m]; ok {
				item.Movies = append(item.Movies, movie)
			}
		}
		for _, t := range theaterIDs {
			if theater, ok := cinemas[t]; ok {
				item.Theaters = append(item.Theaters, theater)
			}
		}
		message := renderPush(item, id, ruleID)
		if payload, err = json.Marshal(message); err != nil {
			return ErrUnavailable
		}
		topic = "n" + strconv.FormatInt(id, 10)
		if item.Kind == NotificationKindActivity && ruleID != nil {
			topic = "r" + strconv.FormatInt(*ruleID, 10)
		}
		valid = true
		return nil
	})
	if err != nil {
		return "", err
	}
	if !valid {
		return "dropped", s.finishPush(ctx, job, "dropped")
	}
	sub, err := s.openPushSubscription(digest, envelope)
	if err != nil {
		return "removed", s.finishPush(ctx, job, "removed")
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	sendErr := s.push.Send(sendCtx, sub, webpush.Message{Payload: payload, TTL: pushLifetime, Topic: topic, Urgency: "normal"})
	cancel()
	outcome := "sent"
	switch {
	case sendErr == nil:
	case errors.Is(sendErr, webpush.ErrGone):
		outcome = "removed"
	case errors.Is(sendErr, webpush.ErrRetry) && job.attempts < pushMaxAttempts:
		outcome = "retry"
	case errors.Is(sendErr, webpush.ErrRetry):
		outcome = "dropped"
	default:
		outcome = "rejected"
	}
	return outcome, s.finishPush(context.WithoutCancel(ctx), job, outcome)
}

func (s *Service) finishPush(ctx context.Context, job pushJob, outcome string) error {
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		var err error
		switch outcome {
		case "retry":
			backoff := 30 * time.Second << (2 * (job.attempts - 1)) // 30s, 2m, 8m, 32m
			_, err = tx.Exec(ctx, `UPDATE account_push_outbox SET lease_until=NULL,next_attempt_at=greatest(created_at,$2) WHERE id=$1`, job.id, now.Add(backoff))
		case "removed":
			_, err = deletePushSubscription(ctx, tx, job.account, job.subscription)
		case "sent":
			if _, err = tx.Exec(ctx, `DELETE FROM account_push_outbox WHERE id=$1`, job.id); err == nil {
				_, err = tx.Exec(ctx, `UPDATE account_push_subscriptions SET last_success_at=$3,failure_count=0 WHERE account_id=$1 AND id=$2`, job.account, job.subscription, now)
			}
		case "rejected":
			if _, err = tx.Exec(ctx, `DELETE FROM account_push_outbox WHERE id=$1`, job.id); err == nil {
				var failures int
				err = tx.QueryRow(ctx, `UPDATE account_push_subscriptions SET failure_count=least(failure_count+1,100) WHERE account_id=$1 AND id=$2 RETURNING failure_count`, job.account, job.subscription).Scan(&failures)
				if errors.Is(err, pgx.ErrNoRows) {
					err = nil
				} else if err == nil && failures >= pushMaxFailureCount {
					_, err = deletePushSubscription(ctx, tx, job.account, job.subscription)
				}
			}
		default:
			_, err = tx.Exec(ctx, `DELETE FROM account_push_outbox WHERE id=$1`, job.id)
		}
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func setOf[T comparable](items []T) map[T]bool {
	set := make(map[T]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

func plural(count int, singular, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}

// renderPush mirrors the in-app wording. Links stay same-origin paths.
func renderPush(item NotificationView, id int64, ruleID *int64) pushPayload {
	theater := strconv.Itoa(item.TheaterCount) + " " + plural(item.TheaterCount, "cinéma", "cinémas")
	if len(item.Theaters) > 0 {
		theater = item.Theaters[0].Name
		if others := item.TheaterCount - 1; others > 0 {
			theater += " et " + strconv.Itoa(others) + " " + plural(others, "autre cinéma", "autres cinémas")
		}
	}
	if item.Kind == NotificationKindWatchlist {
		payload := pushPayload{Title: "Film de votre watchlist", Body: "Un film de votre watchlist est programmé à " + theater, URL: "/compte/watchlist", Tag: "notification-" + strconv.FormatInt(id, 10)}
		if len(item.Movies) > 0 {
			payload.Body = item.Movies[0].Title + " est programmé à " + theater
			payload.URL = "/film/" + item.Movies[0].Slug
		}
		return payload
	}
	payload := pushPayload{Title: "Nouveautés dans vos cinémas", URL: "/compte/activite", Tag: "notification-" + strconv.FormatInt(id, 10)}
	if ruleID != nil {
		payload.Tag = "rule-" + strconv.FormatInt(*ruleID, 10)
	}
	if item.MovieCount == 1 && len(item.Movies) == 1 {
		payload.Body = item.Movies[0].Title + " arrive à " + theater
		return payload
	}
	payload.Body = strconv.Itoa(item.MovieCount) + " nouveaux films à " + theater
	if len(item.Movies) > 0 {
		titles := make([]string, 0, len(item.Movies))
		for _, m := range item.Movies {
			titles = append(titles, m.Title)
		}
		list := strings.Join(titles, ", ")
		if others := item.MovieCount - len(titles); others > 0 {
			list += " et " + strconv.Itoa(others) + " " + plural(others, "autre", "autres")
		}
		payload.Body += " : " + list
	}
	return payload
}
