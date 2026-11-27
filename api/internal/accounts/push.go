package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/webpush"
)

const maxPushDevices = 10

var (
	ErrPushUnavailable    = errors.New("push notifications unavailable")
	ErrPushDeviceLimit    = errors.New("push device limit reached")
	ErrPushDeviceNotFound = errors.New("push device not found")
	ErrPushDeviceExpired  = errors.New("push subscription expired")
	ErrPushDelivery       = errors.New("push delivery failed")
)

// PushSender delivers one encrypted message. Implemented by webpush.Sender.
type PushSender interface {
	Send(context.Context, webpush.Subscription, webpush.Message) error
}

type PushDeviceView struct {
	ID            string     `json:"id"`
	Label         string     `json:"label"`
	CreatedAt     time.Time  `json:"created_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
}

type PushView struct {
	Username   string           `json:"username"`
	Available  bool             `json:"available"`
	PublicKey  string           `json:"public_key"`
	Devices    []PushDeviceView `json:"devices"`
	MaxDevices int              `json:"max_devices"`
	// CurrentID identifies the registering browser; only set by subscription writes.
	CurrentID *string `json:"current_id"`
}

type pushSecret struct {
	Endpoint string `json:"e"`
	P256DH   string `json:"k"`
	Auth     string `json:"a"`
}

func (s *Service) pushAvailable() bool {
	return s.push != nil && s.pushCipher != nil && s.pushPublicKey != ""
}

func (s *Service) pushEndpointDigest(endpoint string) []byte {
	h := hmac.New(sha256.New, s.hmacKey)
	h.Write([]byte("messeances-push-endpoint-v1\x00" + endpoint))
	return h.Sum(nil)
}

func pushBinding(digest []byte) string {
	return "account_push_subscription:" + hex.EncodeToString(digest)
}

func normalizePushLabel(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalidInput
	}
	for _, r := range raw {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return "", ErrInvalidInput
		}
	}
	label := strings.Join(strings.Fields(raw), " ")
	if label == "" {
		label = "Navigateur"
	}
	if utf8.RuneCountInString(label) > 64 {
		return "", ErrInvalidInput
	}
	return label, nil
}

func readPushView(ctx context.Context, tx pgx.Tx, a account, s *Service) (PushView, error) {
	view := PushView{Username: *a.username, Available: s.pushAvailable(), Devices: []PushDeviceView{}, MaxDevices: maxPushDevices}
	if view.Available {
		view.PublicKey = s.pushPublicKey
	}
	rows, err := tx.Query(ctx, `SELECT id,label,created_at,last_success_at FROM account_push_subscriptions WHERE account_id=$1 AND auth_revision=$2 ORDER BY id`, a.id, a.revision)
	if err != nil {
		return view, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var d PushDeviceView
		var id int64
		if err := rows.Scan(&id, &d.Label, &d.CreatedAt, &d.LastSuccessAt); err != nil {
			return view, ErrUnavailable
		}
		d.ID, d.CreatedAt = strconv.FormatInt(id, 10), d.CreatedAt.UTC()
		if d.LastSuccessAt != nil {
			at := d.LastSuccessAt.UTC()
			d.LastSuccessAt = &at
		}
		view.Devices = append(view.Devices, d)
	}
	if rows.Err() != nil {
		return view, ErrUnavailable
	}
	return view, nil
}

func (s *Service) PushDevices(ctx context.Context, rawSession string) (PushView, error) {
	var view PushView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view, err = readPushView(ctx, tx, a, s)
		return err
	})
	if err != nil {
		return PushView{}, err
	}
	return view, nil
}

func (s *Service) pushMutation(ctx context.Context, rawSession, expectedUsername string, mutate func(pgx.Tx, account) error) (PushView, error) {
	var view PushView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		if err := mutate(tx, a); err != nil {
			return err
		}
		view, err = readPushView(ctx, tx, a, s)
		return err
	})
	if err != nil {
		return PushView{}, err
	}
	return view, nil
}

func deletePushSubscription(ctx context.Context, tx pgx.Tx, accountID, id int64) (bool, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM account_push_outbox WHERE account_id=$1 AND subscription_id=$2`, accountID, id); err != nil {
		return false, ErrUnavailable
	}
	tag, err := tx.Exec(ctx, `DELETE FROM account_push_subscriptions WHERE account_id=$1 AND id=$2`, accountID, id)
	if err != nil {
		return false, ErrUnavailable
	}
	return tag.RowsAffected() == 1, nil
}

// SavePushSubscription registers this browser for the session owner. A browser
// endpoint belongs to exactly one account: re-subscribing after switching
// accounts moves it, so the previous owner stops receiving pushes there.
func (s *Service) SavePushSubscription(ctx context.Context, rawSession, expectedUsername, endpoint, p256dh, auth, rawLabel string) (PushView, error) {
	if !s.pushAvailable() {
		return PushView{}, ErrPushUnavailable
	}
	sub, err := webpush.ParseSubscription(endpoint, p256dh, auth)
	if err != nil {
		return PushView{}, ErrInvalidInput
	}
	label, err := normalizePushLabel(rawLabel)
	if err != nil {
		return PushView{}, err
	}
	digest := s.pushEndpointDigest(sub.Endpoint)
	secret, err := json.Marshal(pushSecret{Endpoint: sub.Endpoint, P256DH: base64.RawURLEncoding.EncodeToString(sub.P256DH), Auth: base64.RawURLEncoding.EncodeToString(sub.Auth)})
	if err != nil {
		return PushView{}, ErrUnavailable
	}
	envelope, err := s.pushCipher.Seal(pushBinding(digest), secret)
	if err != nil {
		return PushView{}, ErrUnavailable
	}
	view, err := s.pushMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account) error {
		now := s.now().UTC()
		var owner, id int64
		err := tx.QueryRow(ctx, `SELECT account_id,id FROM account_push_subscriptions WHERE endpoint_digest=$1 FOR UPDATE`, digest).Scan(&owner, &id)
		switch {
		case err == nil && owner == a.id:
			_, err = tx.Exec(ctx, `UPDATE account_push_subscriptions SET auth_revision=$3,payload_key_id=$4,payload_nonce=$5,payload_ciphertext=$6,label=$7,updated_at=greatest(created_at,$8),failure_count=0 WHERE account_id=$1 AND id=$2`,
				a.id, id, a.revision, envelope.KeyID, envelope.Nonce, envelope.Ciphertext, label, now)
			if err != nil {
				return ErrUnavailable
			}
			return nil
		case err == nil:
			// Only this row of the other account is touched, never its account lock.
			if _, err := deletePushSubscription(ctx, tx, owner, id); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return ErrUnavailable
		}
		// Stale-revision rows are invisible and never delivered: drop them first.
		if _, err := tx.Exec(ctx, `DELETE FROM account_push_subscriptions WHERE account_id=$1 AND auth_revision<>$2`, a.id, a.revision); err != nil {
			return ErrUnavailable
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_push_subscriptions WHERE account_id=$1`, a.id).Scan(&count); err != nil {
			return ErrUnavailable
		}
		if count >= maxPushDevices {
			return ErrPushDeviceLimit
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_push_subscriptions(account_id,auth_revision,endpoint_digest,payload_key_id,payload_nonce,payload_ciphertext,label,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
			a.id, a.revision, digest, envelope.KeyID, envelope.Nonce, envelope.Ciphertext, label, now)
		if uniqueError(err) {
			return ErrUnavailable // concurrent registration of the same endpoint
		}
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return PushView{}, err
	}
	// The digest is unique, so this read identifies the row just written.
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM account_push_subscriptions WHERE endpoint_digest=$1`, digest).Scan(&id); err != nil {
			return nil // concurrently moved or removed: no current device
		}
		current := strconv.FormatInt(id, 10)
		for _, d := range view.Devices {
			if d.ID == current {
				view.CurrentID = &current
			}
		}
		return nil
	})
	return view, err
}

// DeletePushSubscription removes a device by ID, or by the browser's own
// endpoint when it unsubscribes. Unknown endpoints are an idempotent success.
func (s *Service) DeletePushSubscription(ctx context.Context, rawSession, expectedUsername, rawID, endpoint string) (PushView, error) {
	if (rawID == "") == (endpoint == "") {
		return PushView{}, ErrInvalidInput
	}
	var id int64
	if rawID != "" {
		var err error
		if id, err = parseNotificationID(rawID); err != nil {
			return PushView{}, err
		}
	} else if webpush.ValidateEndpoint(endpoint) != nil {
		return PushView{}, ErrInvalidInput
	}
	return s.pushMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account) error {
		if endpoint != "" {
			err := tx.QueryRow(ctx, `SELECT id FROM account_push_subscriptions WHERE account_id=$1 AND endpoint_digest=$2`, a.id, s.pushEndpointDigest(endpoint)).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return ErrUnavailable
			}
		}
		deleted, err := deletePushSubscription(ctx, tx, a.id, id)
		if err != nil {
			return err
		}
		if !deleted && rawID != "" {
			return ErrPushDeviceNotFound
		}
		return nil
	})
}

func (s *Service) openPushSubscription(digest []byte, envelope accountmail.Envelope) (webpush.Subscription, error) {
	plain, err := s.pushCipher.Open(pushBinding(digest), envelope)
	if err != nil {
		return webpush.Subscription{}, ErrUnavailable
	}
	var secret pushSecret
	if err := json.Unmarshal(plain, &secret); err != nil {
		return webpush.Subscription{}, ErrUnavailable
	}
	sub, err := webpush.ParseSubscription(secret.Endpoint, secret.P256DH, secret.Auth)
	if err != nil {
		return webpush.Subscription{}, ErrUnavailable
	}
	return sub, nil
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// TestPushSubscription sends one synchronous test message to an owned device.
// A permanently invalid subscription is removed and reported as expired.
func (s *Service) TestPushSubscription(ctx context.Context, rawSession, expectedUsername, rawID string) error {
	if !s.pushAvailable() {
		return ErrPushUnavailable
	}
	id, err := parseNotificationID(rawID)
	if err != nil {
		return err
	}
	var digest []byte
	var envelope accountmail.Envelope
	var accountID int64
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		accountID = a.id
		err = tx.QueryRow(ctx, `SELECT endpoint_digest,payload_key_id,payload_nonce,payload_ciphertext FROM account_push_subscriptions WHERE account_id=$1 AND id=$2 AND auth_revision=$3`, a.id, id, a.revision).
			Scan(&digest, &envelope.KeyID, &envelope.Nonce, &envelope.Ciphertext)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPushDeviceNotFound
		}
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return err
	}
	sub, err := s.openPushSubscription(digest, envelope)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(pushPayload{Title: "MesSeances", Body: "Les notifications sont activées sur cet appareil.", URL: "/compte/notifications", Tag: "push-test"})
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	sendErr := s.push.Send(sendCtx, sub, webpush.Message{Payload: payload, TTL: time.Hour, Urgency: "normal", Topic: "test"})
	gone := errors.Is(sendErr, webpush.ErrGone)
	if sendErr != nil && !gone {
		return ErrPushDelivery
	}
	ctx = context.WithoutCancel(ctx)
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if gone {
			_, err := deletePushSubscription(ctx, tx, accountID, id)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE account_push_subscriptions SET last_success_at=$3,failure_count=0 WHERE account_id=$1 AND id=$2`, accountID, id, s.now().UTC()); err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err == nil && gone {
		// Report only after the removal committed.
		return ErrPushDeviceExpired
	}
	return err
}
