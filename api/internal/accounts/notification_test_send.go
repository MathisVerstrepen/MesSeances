package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/webpush"
)

const (
	TestChannelPush  = "push"
	TestChannelEmail = "email"

	testEmailPreviewItems = 3
)

var (
	ErrPushNoDevice = errors.New("no push device")
	ErrMailNotReady = errors.New("account email not verified")
)

// NotificationTestResult reports a test send. For email, Delivered is 1 once
// queued: acceptance by SES and the mailbox are asynchronous.
type NotificationTestResult struct {
	Channel   string `json:"channel"`
	Delivered int    `json:"delivered"`
	Expired   int    `json:"expired"`
	Failed    int    `json:"failed"`
}

// SendTestNotification lets the owner check a channel end to end: a browser
// push to every current device, or a preview digest email.
func (s *Service) SendTestNotification(ctx context.Context, rawSession, expectedUsername, channel string) (NotificationTestResult, error) {
	switch channel {
	case TestChannelPush:
		return s.sendTestPush(ctx, rawSession, expectedUsername)
	case TestChannelEmail:
		return s.sendTestEmail(ctx, rawSession, expectedUsername)
	}
	return NotificationTestResult{}, ErrInvalidInput
}

type testDevice struct {
	id       int64
	digest   []byte
	envelope accountmail.Envelope
}

func (s *Service) sendTestPush(ctx context.Context, rawSession, expectedUsername string) (NotificationTestResult, error) {
	result := NotificationTestResult{Channel: TestChannelPush}
	if !s.pushAvailable() {
		return result, ErrPushUnavailable
	}
	var accountID int64
	var devices []testDevice
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		accountID = a.id
		rows, err := tx.Query(ctx, `SELECT id,endpoint_digest,payload_key_id,payload_nonce,payload_ciphertext FROM account_push_subscriptions WHERE account_id=$1 AND auth_revision=$2 ORDER BY id`, a.id, a.revision)
		if err != nil {
			return ErrUnavailable
		}
		devices, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (testDevice, error) {
			var d testDevice
			err := r.Scan(&d.id, &d.digest, &d.envelope.KeyID, &d.envelope.Nonce, &d.envelope.Ciphertext)
			return d, err
		})
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if len(devices) == 0 {
		return result, ErrPushNoDevice
	}
	if err := s.quota(ctx, "notification_test", "push:account:"+strconv.FormatInt(accountID, 10), false); err != nil {
		return result, err
	}
	payload, _ := json.Marshal(pushPayload{Title: "MesSeances", Body: "Notification de test : vos alertes arrivent bien sur cet appareil.", URL: "/compte/notifications", Tag: "push-test"})
	outcomes := make([]error, len(devices))
	var wg sync.WaitGroup
	for i, device := range devices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, err := s.openPushSubscription(device.digest, device.envelope)
			if err != nil {
				outcomes[i] = webpush.ErrGone
				return
			}
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			outcomes[i] = s.push.Send(sendCtx, sub, webpush.Message{Payload: payload, TTL: time.Hour, Urgency: "normal", Topic: "test"})
		}()
	}
	wg.Wait()
	now := s.now().UTC()
	err = s.store.withTransaction(context.WithoutCancel(ctx), func(tx pgx.Tx) error {
		for i, device := range devices {
			switch {
			case outcomes[i] == nil:
				result.Delivered++
				if _, err := tx.Exec(ctx, `UPDATE account_push_subscriptions SET last_success_at=$3,failure_count=0 WHERE account_id=$1 AND id=$2`, accountID, device.id, now); err != nil {
					return ErrUnavailable
				}
			case errors.Is(outcomes[i], webpush.ErrGone):
				result.Expired++
				if _, err := deletePushSubscription(ctx, tx, accountID, device.id); err != nil {
					return err
				}
			default:
				result.Failed++
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if result.Delivered == 0 && result.Failed == 0 {
		return result, ErrPushDeviceExpired
	}
	if result.Delivered == 0 {
		return result, ErrPushDelivery
	}
	return result, nil
}

func (s *Service) sendTestEmail(ctx context.Context, rawSession, expectedUsername string) (NotificationTestResult, error) {
	result := NotificationTestResult{Channel: TestChannelEmail}
	if s.mail == nil {
		return result, ErrUnavailable
	}
	var accountID int64
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		accountID = a.id
		return nil
	})
	if err != nil {
		return result, err
	}
	// Mail quota: one per minute, three per hour, ten per day.
	if err := s.quota(ctx, "notification_test", "email:account:"+strconv.FormatInt(accountID, 10), true); err != nil {
		return result, err
	}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if a.id != accountID || a.verified == nil {
			return ErrMailNotReady
		}
		items, err := queryNotificationViews(ctx, tx, `SELECT n.id,n.kind,n.rule_id,n.event_count,cardinality(n.movie_ids),cardinality(n.theater_ids),n.movie_ids[1:4],n.theater_ids[1:1]
 FROM account_notifications n WHERE n.account_id=$1 ORDER BY n.id DESC LIMIT $2`, a.id, testEmailPreviewItems)
		if err != nil {
			return err
		}
		message, err := testDigestMailMessage(s.origin, a.email, items)
		if err != nil {
			return ErrUnavailable
		}
		_, event, err := s.newToken()
		if err != nil {
			return err
		}
		now := s.now().UTC()
		return s.enqueue(ctx, tx, a, "notification_test", event[:], nil, message, now, now.Add(time.Hour))
	})
	if errors.Is(err, accountmail.ErrQueueFull) {
		return result, ErrUnavailable
	}
	if err != nil {
		return result, err
	}
	result.Delivered = 1
	return result, nil
}
