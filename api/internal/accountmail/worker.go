package accountmail

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AddressDigest validates and normalizes an address before returning its HMAC.
// Keeping this injected avoids coupling mail transport to account credentials.
type AddressDigest func(string) ([]byte, error)

type Worker struct {
	Pool    *pgxpool.Pool
	Cipher  PayloadCipher
	Sender  Sender
	Address AddressDigest
	Now     func() time.Time
}

type delivery struct {
	id           int64
	event, lease []byte
	purpose      string
	envelope     Envelope
	attempt      int
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// claim counts the attempt BEFORE external I/O. A crash consumes an attempt and
// preserves the same payload/token. The lease digest fences late completions.
func (w *Worker) claim(ctx context.Context) (delivery, error) {
	var d delivery
	now := w.now()
	_, err := w.Pool.Exec(ctx, `UPDATE account_mail_outbox SET state='failed',payload_key_id=NULL,payload_nonce=NULL,payload_ciphertext=NULL,lease_until=NULL,lease_digest=NULL,finished_at=$1,account_id=NULL,auth_revision=NULL,token_digest=NULL WHERE id IN (SELECT id FROM account_mail_outbox WHERE state='pending' AND (expires_at<=$1 OR (attempts>=6 AND (lease_until IS NULL OR lease_until<=$1))) ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED)`, now)
	if err != nil {
		return d, ErrUnavailable
	}
	d.lease = make([]byte, 32)
	if _, err = rand.Read(d.lease); err != nil {
		return d, ErrUnavailable
	}
	err = w.Pool.QueryRow(ctx, `UPDATE account_mail_outbox SET attempts=attempts+1,lease_until=$1::timestamptz+interval '60 seconds',lease_digest=$2,
	 next_attempt_at=$1::timestamptz+CASE attempts WHEN 0 THEN interval '1 minute' WHEN 1 THEN interval '5 minutes' WHEN 2 THEN interval '15 minutes' WHEN 3 THEN interval '1 hour' WHEN 4 THEN interval '4 hours' ELSE interval '60 seconds' END
	 WHERE id=(SELECT id FROM account_mail_outbox WHERE state='pending' AND attempts<6 AND expires_at>$1 AND next_attempt_at<=$1 AND (lease_until IS NULL OR lease_until<=$1) ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
	 RETURNING id,event_digest,purpose,payload_key_id,payload_nonce,payload_ciphertext,attempts`, now, d.lease).Scan(&d.id, &d.event, &d.purpose, &d.envelope.KeyID, &d.envelope.Nonce, &d.envelope.Ciphertext, &d.attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return delivery{}, nil
	}
	if err != nil {
		return delivery{}, ErrUnavailable
	}
	return d, nil
}

func (w *Worker) ready(ctx context.Context, d delivery, address []byte) (bool, error) {
	var ready bool
	err := w.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_mail_outbox o JOIN accounts a ON a.id=o.account_id
	 WHERE o.id=$1 AND o.lease_digest=$2 AND o.state='pending' AND o.lease_until>$3 AND o.expires_at>$3
	 AND a.auth_revision=o.auth_revision AND (a.pending_kind IS NULL OR a.created_at>$3::timestamptz-interval '168 hours')
	 AND NOT EXISTS(SELECT 1 FROM account_mail_suppressions WHERE address_digest=$4 AND expires_at>$3)
	 AND (o.purpose='security_notification' AND o.token_digest IS NULL OR EXISTS(SELECT 1 FROM account_tokens t
	 LEFT JOIN account_sessions s ON s.token_digest=t.session_digest
	 WHERE t.token_digest=o.token_digest AND t.account_id=a.id AND t.auth_revision=a.auth_revision AND t.purpose=o.purpose AND t.expires_at>$3 AND t.consumed_at IS NULL
	 AND (t.session_digest IS NULL OR s.auth_revision=a.auth_revision AND s.expires_at>$3 AND s.last_seen_at>$3::timestamptz-interval '168 hours'))))`, d.id, d.lease, w.now(), address).Scan(&ready)
	if err != nil {
		return false, ErrUnavailable
	}
	return ready, nil
}

func (w *Worker) finish(ctx context.Context, d delivery, state string) error {
	_, err := w.Pool.Exec(ctx, `UPDATE account_mail_outbox SET state=$3,payload_key_id=NULL,payload_nonce=NULL,payload_ciphertext=NULL,lease_until=NULL,lease_digest=NULL,finished_at=$4,account_id=NULL,auth_revision=NULL,token_digest=NULL WHERE id=$1 AND lease_digest=$2 AND state='pending'`, d.id, d.lease, state, w.now())
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// step sends at most one message, outside database locks. Revocation after the
// final readiness query can only invalidate an already in-flight single-use link.
func (w *Worker) step(ctx context.Context) (string, error) {
	d, err := w.claim(ctx)
	if err != nil || d.id == 0 {
		return "idle", err
	}
	plain, err := w.Cipher.Open(OutboxBinding(d.event, d.purpose), d.envelope)
	var message Message
	if err != nil || json.Unmarshal(plain, &message) != nil {
		return "failed", w.finish(ctx, d, "failed")
	}
	address, err := w.Address(message.Recipient)
	if err != nil {
		return "failed", w.finish(ctx, d, "failed")
	}
	ready, err := w.ready(ctx, d, address)
	if err != nil {
		return "failed", err
	}
	if !ready {
		return "discarded", w.finish(ctx, d, "failed")
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = w.Sender.Send(sendCtx, message)
	cancel()
	if err == nil {
		return "accepted", w.finish(ctx, d, "sent")
	}
	if !errors.Is(err, ErrTransient) || d.attempt >= 6 {
		return "failed", w.finish(ctx, d, "failed")
	}
	// Backoff was durably recorded at claim time, even if the process died.
	_, err = w.Pool.Exec(ctx, `UPDATE account_mail_outbox SET lease_until=NULL,lease_digest=NULL WHERE id=$1 AND lease_digest=$2 AND state='pending'`, d.id, d.lease)
	if err != nil {
		return "failed", ErrUnavailable
	}
	return "retry", nil
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Run holds a dedicated PostgreSQL advisory lock so multiple API replicas still
// share one sender. Waiting before every attempt also bounds restart bursts.
func (w *Worker) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		w.runLeader(ctx, logger)
		if !pause(ctx, time.Minute) {
			return
		}
	}
}

func (w *Worker) runLeader(ctx context.Context, logger *slog.Logger) {
	conn, err := w.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	// Close rather than return a session-level lock to the connection pool.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Conn().Close(closeCtx)
		conn.Release()
	}()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(714306290)`).Scan(&locked); err != nil || !locked {
		return
	}
	counts := map[string]int{}
	lastLog := time.Now()
	for pause(ctx, time.Second) {
		// A lost leader connection must not leave a second sender running.
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Ping(check)
		cancel()
		if err != nil {
			return
		}
		stepCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		result, err := w.step(stepCtx)
		cancel()
		if err != nil {
			counts["errors"]++
		} else {
			counts[result]++
		}
		if time.Since(lastLog) >= time.Minute {
			var pending int64
			var age float64
			statsCtx, statsCancel := context.WithTimeout(ctx, 5*time.Second)
			statsErr := w.Pool.QueryRow(statsCtx, `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM ($1::timestamptz-min(created_at))),0) FROM account_mail_outbox WHERE state='pending'`, w.now()).Scan(&pending, &age)
			statsCancel()
			if statsErr != nil {
				counts["errors"]++
			}
			logger.Info("account_mail_dispatch", "accepted", counts["accepted"], "failed", counts["failed"], "discarded", counts["discarded"], "retries", counts["retry"], "errors", counts["errors"], "pending", pending, "oldest_seconds", age)
			counts = map[string]int{}
			lastLog = time.Now()
		}
	}
}
