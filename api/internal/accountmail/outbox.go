package accountmail

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrUnavailable = errors.New("account mail unavailable")
var ErrQueueFull = errors.New("account mail queue full")

// Job is an in-memory transaction input. Never log it. Delivery consumes the
// encrypted Message and rechecks token, revision and suppression before sending.
type Job struct {
	EventDigest, TokenDigest, AddressDigest []byte
	AccountID, Revision                     int64
	Purpose                                 string
	Message                                 Message
	CreatedAt, ExpiresAt                    time.Time
}

type Enqueuer interface {
	Enqueue(context.Context, pgx.Tx, Job) error
}

type Outbox struct{ Cipher PayloadCipher }

func OutboxBinding(event []byte, purpose string) string {
	return "account_mail_outbox:" + hex.EncodeToString(event) + ":" + purpose
}

func (o *Outbox) Enqueue(ctx context.Context, tx pgx.Tx, job Job) error {
	if o == nil || o.Cipher == nil {
		return ErrUnavailable
	}
	// Serialize admission, not delivery. Every producer acquires this after its
	// account lock, and never acquires another account lock afterward.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(714306289)`); err != nil {
		return ErrUnavailable
	}
	var suppressed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_mail_suppressions WHERE address_digest=$1 AND expires_at>$2)`, job.AddressDigest, job.CreatedAt).Scan(&suppressed); err != nil {
		return ErrUnavailable
	}
	if suppressed {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox WHERE state='pending'`).Scan(&count); err != nil {
		return ErrUnavailable
	}
	if count >= 10000 {
		return ErrQueueFull
	}
	payload, err := json.Marshal(job.Message)
	if err != nil {
		return ErrUnavailable
	}
	envelope, err := o.Cipher.Seal(OutboxBinding(job.EventDigest, job.Purpose), payload)
	if err != nil {
		return ErrUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_mail_outbox(event_digest,account_id,token_digest,auth_revision,purpose,payload_key_id,payload_nonce,payload_ciphertext,created_at,expires_at,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$9)`, job.EventDigest, job.AccountID, job.TokenDigest, job.Revision, job.Purpose, envelope.KeyID, envelope.Nonce, envelope.Ciphertext, job.CreatedAt, job.ExpiresAt)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
