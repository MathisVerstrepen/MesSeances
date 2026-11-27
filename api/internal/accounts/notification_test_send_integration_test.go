package accounts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"messeances/api/internal/webpush"
)

type perEndpointPush struct {
	fakePush
	gone string
}

func (f *perEndpointPush) Send(ctx context.Context, sub webpush.Subscription, msg webpush.Message) error {
	_ = f.fakePush.Send(ctx, sub, msg)
	if sub.Endpoint == f.gone {
		return webpush.ErrGone
	}
	return nil
}

func TestSendTestNotificationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := t.Context()
	a := f.complete(t, "tester@example.com", "alert_checker")
	token := a.Cookie.Token
	if _, err := f.service.SendTestNotification(ctx, token, "alert_checker", "sms"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown channel", err)
	}
	if _, err := f.service.SendTestNotification(ctx, token, "alert_checker", TestChannelPush); !errors.Is(err, ErrPushUnavailable) {
		t.Fatal("push without configuration", err)
	}
	fake := &perEndpointPush{gone: "https://web.push.apple.com/old"}
	f.service.push, f.service.pushCipher, f.service.pushPublicKey = fake, f.cipher, "BKey"
	if _, err := f.service.SendTestNotification(ctx, token, "alert_checker", TestChannelPush); !errors.Is(err, ErrPushNoDevice) {
		t.Fatal("no device", err)
	}
	key, auth := pushKeys(t)
	for _, endpoint := range []string{"https://fcm.googleapis.com/fcm/send/a", "https://web.push.apple.com/old"} {
		if _, err := f.service.SavePushSubscription(ctx, token, "alert_checker", endpoint, key, auth, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.SendTestNotification(ctx, token, "other", TestChannelPush); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("client chose owner", err)
	}
	result, err := f.service.SendTestNotification(ctx, token, "alert_checker", TestChannelPush)
	if err != nil || result.Delivered != 1 || result.Expired != 1 || result.Channel != TestChannelPush {
		t.Fatalf("push %+v %v", result, err)
	}
	if sent, _ := fake.take(); len(sent) != 2 || !strings.Contains(string(sent[0].Payload), "Notification de test") {
		t.Fatal("push payload", sent)
	}
	if view, _ := f.service.PushDevices(ctx, token); len(view.Devices) != 1 {
		t.Fatal("expired device kept", view)
	}

	// Email: a preview with the latest inbox entries, no unsubscribe header.
	if _, err := f.service.SaveTheaterFollow(ctx, token, "alert_checker", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateNotificationRule(ctx, token, "alert_checker", notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, false, true)); err != nil {
		t.Fatal(err)
	}
	sweepNotifications(t, f)
	notificationEpisode(t, f, 8, "ugc-1", "102", "added_to_program")
	sweepNotifications(t, f)
	if result, err = f.service.SendTestNotification(ctx, token, "alert_checker", TestChannelEmail); err != nil || result.Delivered != 1 {
		t.Fatalf("email %+v %v", result, err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox WHERE purpose='notification_test' AND state='pending'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("test mail not queued", count, err)
	}
	message := testMail(t, f)
	if message.Subject != "E-mail de test · MesSeances" || message.UnsubscribeURL != "" || !strings.Contains(message.HTML, "E-mail de test") || !strings.Contains(message.HTML, "Current override") || !strings.Contains(message.Text, "demandé un test") {
		t.Fatalf("test mail %+v", message.Subject)
	}
	// One test mail per minute per account.
	var rate *RateLimitError
	if _, err := f.service.SendTestNotification(ctx, token, "alert_checker", TestChannelEmail); !errors.As(err, &rate) {
		t.Fatal("email quota", err)
	}
}
