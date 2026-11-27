package accounts

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"

	"messeances/api/internal/webpush"
)

type fakePush struct {
	mu       sync.Mutex
	sent     []webpush.Message
	to       []string
	response error
}

func (f *fakePush) Send(_ context.Context, sub webpush.Subscription, msg webpush.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	f.to = append(f.to, sub.Endpoint)
	return f.response
}

func (f *fakePush) take() ([]webpush.Message, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sent, to := f.sent, f.to
	f.sent, f.to = nil, nil
	return sent, to
}

func pushKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(secret)
}

func enablePush(f *lifecycleFixture) *fakePush {
	fake := &fakePush{}
	f.service.push, f.service.pushCipher, f.service.pushPublicKey = fake, f.cipher, "BPublicKey"
	return fake
}

func pushCount(t *testing.T, f *lifecycleFixture, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPushSubscriptionsIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	a := f.complete(t, "push@example.com", "push_owner")
	b := f.complete(t, "push-other@example.com", "push_other")
	if view, err := f.service.PushDevices(ctx, a.Cookie.Token); err != nil || view.Available || view.PublicKey != "" || view.CurrentID != nil {
		t.Fatal("push advertised without configuration", view, err)
	}
	key, auth := pushKeys(t)
	if _, err := f.service.SavePushSubscription(ctx, a.Cookie.Token, "push_owner", "https://fcm.googleapis.com/fcm/send/a", key, auth, "Chrome"); !errors.Is(err, ErrPushUnavailable) {
		t.Fatal("unconfigured subscription", err)
	}
	enablePush(f)
	endpoint := "https://fcm.googleapis.com/fcm/send/device-a"
	view, err := f.service.SavePushSubscription(ctx, a.Cookie.Token, "push_owner", endpoint, key, auth, "  Chrome   ·  Linux ")
	if err != nil || !view.Available || view.PublicKey != "BPublicKey" || len(view.Devices) != 1 || view.Devices[0].Label != "Chrome · Linux" || view.MaxDevices != 10 || view.CurrentID == nil || *view.CurrentID != view.Devices[0].ID {
		t.Fatalf("subscribe %+v %v", view, err)
	}
	var ciphertext []byte
	if err := f.pool.QueryRow(ctx, `SELECT payload_ciphertext FROM account_push_subscriptions`).Scan(&ciphertext); err != nil || bytes.Contains(ciphertext, []byte("device-a")) {
		t.Fatal("endpoint stored in clear", err)
	}
	// Re-subscribing the same browser updates in place.
	if view, err = f.service.SavePushSubscription(ctx, a.Cookie.Token, "push_owner", endpoint, key, auth, ""); err != nil || len(view.Devices) != 1 || view.Devices[0].Label != "Navigateur" {
		t.Fatalf("resubscribe %+v %v", view, err)
	}
	for name, args := range map[string][3]string{
		"label control": {endpoint, key, "\x01"},
		"endpoint":      {"https://example.com/push", key, "x"},
		"key":           {endpoint, "AAAA", "x"},
	} {
		if _, err := f.service.SavePushSubscription(ctx, a.Cookie.Token, "push_owner", args[0], args[1], auth, args[2]); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if _, err := f.service.SavePushSubscription(ctx, b.Cookie.Token, "push_owner", endpoint, key, auth, "x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("client chose owner", err)
	}
	// The same browser signing into another account moves the device.
	if view, err = f.service.SavePushSubscription(ctx, b.Cookie.Token, "push_other", endpoint, key, auth, "Firefox"); err != nil || len(view.Devices) != 1 {
		t.Fatalf("move %+v %v", view, err)
	}
	if view, err = f.service.PushDevices(ctx, a.Cookie.Token); err != nil || len(view.Devices) != 0 {
		t.Fatal("previous owner kept device", view, err)
	}
	for i := range 10 {
		if _, err = f.service.SavePushSubscription(ctx, a.Cookie.Token, "push_owner", "https://updates.push.services.mozilla.com/wpush/v2/"+strconv.Itoa(i), key, auth, "x"); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err = f.service.SavePushSubscription(ctx, a.Cookie.Token, "push_owner", "https://web.push.apple.com/extra", key, auth, "x"); !errors.Is(err, ErrPushDeviceLimit) {
		t.Fatal("device limit", err)
	}
	view, _ = f.service.PushDevices(ctx, a.Cookie.Token)
	if _, err = f.service.DeletePushSubscription(ctx, b.Cookie.Token, "push_other", view.Devices[0].ID, ""); !errors.Is(err, ErrPushDeviceNotFound) {
		t.Fatal("foreign device deleted", err)
	}
	if view, err = f.service.DeletePushSubscription(ctx, a.Cookie.Token, "push_owner", view.Devices[0].ID, ""); err != nil || len(view.Devices) != 9 {
		t.Fatal("delete by id", err)
	}
	if view, err = f.service.DeletePushSubscription(ctx, a.Cookie.Token, "push_owner", "", "https://updates.push.services.mozilla.com/wpush/v2/9"); err != nil || len(view.Devices) != 8 {
		t.Fatal("delete by endpoint", err)
	}
	if _, err = f.service.DeletePushSubscription(ctx, a.Cookie.Token, "push_owner", "", "https://updates.push.services.mozilla.com/wpush/v2/unknown"); err != nil {
		t.Fatal("unknown endpoint must be idempotent", err)
	}
	// Revocation detaches every device of the account: hidden and never
	// delivered at once, purged by the next delivery cycle.
	if err = f.service.Logout(ctx, a.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	login, err := f.service.Login(ctx, "push@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if view, err = f.service.PushDevices(ctx, login.Cookie.Token); err != nil || len(view.Devices) != 0 {
		t.Fatal("revoked devices still listed", view, err)
	}
	if _, err = f.service.DeliverPush(ctx); err != nil {
		t.Fatal(err)
	}
	if n := pushCount(t, f, `SELECT count(*) FROM account_push_subscriptions s JOIN accounts a ON a.id=s.account_id WHERE a.email='push@example.com'`); n != 0 {
		t.Fatal("logout-all kept devices", n)
	}
}

func TestPushDeliveryIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := t.Context()
	a := f.complete(t, "push-delivery@example.com", "push_delivery")
	token := a.Cookie.Token
	fake := enablePush(f)
	if _, err := f.service.SaveTheaterFollow(ctx, token, "push_delivery", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveWatchlist(ctx, token, "push_delivery", "0", "film-2", true); err != nil {
		t.Fatal(err)
	}
	activity := notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, false, true)
	activity.Push = true
	rules, err := f.service.CreateNotificationRule(ctx, token, "push_delivery", activity)
	if err != nil || !rules.Rules[0].Push {
		t.Fatal("push rule", rules, err)
	}
	activityRule := rules.Rules[0].ID
	// Watchlist alert without push: inbox only.
	if _, err = f.service.CreateNotificationRule(ctx, token, "push_delivery", notificationRule(NotificationKindWatchlist, NotificationScopeFollowed, nil, false, true)); err != nil {
		t.Fatal(err)
	}
	key, auth := pushKeys(t)
	view, err := f.service.SavePushSubscription(ctx, token, "push_delivery", "https://fcm.googleapis.com/fcm/send/one", key, auth, "Chrome")
	if err != nil || len(view.Devices) != 1 {
		t.Fatal(err)
	}
	if _, err = f.service.SavePushSubscription(ctx, token, "push_delivery", "https://web.push.apple.com/two", key, auth, "Safari"); err != nil {
		t.Fatal(err)
	}
	sweepNotifications(t, f)
	notificationEpisode(t, f, 8, "ugc-1", "102", "added_to_program")
	if r := sweepNotifications(t, f); r.Notifications != 2 {
		t.Fatalf("sweep %+v", r)
	}
	if n := pushCount(t, f, `SELECT count(*) FROM account_push_outbox`); n != 2 {
		t.Fatal("outbox must hold one row per device for the push rule only", n)
	}
	result, err := f.service.DeliverPush(ctx)
	if err != nil || result.Sent != 2 {
		t.Fatalf("deliver %+v %v", result, err)
	}
	sent, to := fake.take()
	var payload pushPayload
	if len(sent) != 2 || json.Unmarshal(sent[0].Payload, &payload) != nil || payload.Body != "Current override arrive à Active 1" || payload.URL != "/compte/activite" || payload.Tag != "rule-"+activityRule || sent[0].Topic != "r"+activityRule || to[0] == to[1] {
		t.Fatalf("payload %+v %+v %v", payload, sent, to)
	}
	if n := pushCount(t, f, `SELECT count(*) FROM account_push_outbox`); n != 0 {
		t.Fatal("sent rows kept", n)
	}
	if n := pushCount(t, f, `SELECT count(*) FROM account_push_subscriptions WHERE last_success_at IS NOT NULL`); n != 2 {
		t.Fatal("success not recorded", n)
	}

	// Transient failure reschedules; a read entry is never pushed afterwards.
	fake.response = webpush.ErrRetry
	notificationEpisode(t, f, 9, "ugc-1", "101", "added_to_program")
	sweepNotifications(t, f)
	if result, err = f.service.DeliverPush(ctx); err != nil || result.Retried != 2 {
		t.Fatalf("retry %+v %v", result, err)
	}
	fake.take()
	if result, _ = f.service.DeliverPush(ctx); result != (PushDeliveryResult{}) {
		t.Fatal("rescheduled row sent before backoff", result)
	}
	inbox, _ := f.service.Notifications(ctx, token, 0, "")
	if _, err = f.service.MarkAllNotificationsRead(ctx, token, "push_delivery", inbox.LatestID); err != nil {
		t.Fatal(err)
	}
	f.advance(pushLease + 31*1e9)
	fake.response = nil
	if result, err = f.service.DeliverPush(ctx); err != nil || result.Dropped != 2 || result.Sent != 0 {
		t.Fatalf("read entry pushed %+v %v", result, err)
	}

	// A gone subscription is removed with its pending rows.
	fake.response = webpush.ErrGone
	notificationEpisode(t, f, 10, "ugc-1", "102", "added_to_program")
	sweepNotifications(t, f)
	if result, err = f.service.DeliverPush(ctx); err != nil || result.Removed != 2 {
		t.Fatalf("gone %+v %v", result, err)
	}
	fake.take()
	if n := pushCount(t, f, `SELECT count(*) FROM account_push_subscriptions`) + pushCount(t, f, `SELECT count(*) FROM account_push_outbox`); n != 0 {
		t.Fatal("gone device kept", n)
	}

	// Test pushes report expired devices and remove them.
	if view, err = f.service.SavePushSubscription(ctx, token, "push_delivery", "https://fcm.googleapis.com/fcm/send/three", key, auth, "Chrome"); err != nil {
		t.Fatal(err)
	}
	device := view.Devices[0].ID
	fake.response = nil
	if err = f.service.TestPushSubscription(ctx, token, "push_delivery", device); err != nil {
		t.Fatal(err)
	}
	if sent, _ = fake.take(); len(sent) != 1 || sent[0].Topic != "test" {
		t.Fatal("test push", sent)
	}
	fake.response = webpush.ErrRejected
	if err = f.service.TestPushSubscription(ctx, token, "push_delivery", device); !errors.Is(err, ErrPushDelivery) {
		t.Fatal("rejected test", err)
	}
	fake.response = webpush.ErrGone
	if err = f.service.TestPushSubscription(ctx, token, "push_delivery", device); !errors.Is(err, ErrPushDeviceExpired) {
		t.Fatal("gone test", err)
	}
	if err = f.service.TestPushSubscription(ctx, token, "push_delivery", device); !errors.Is(err, ErrPushDeviceNotFound) {
		t.Fatal("expired device kept", err)
	}
	// Disabling push on the rule stops enqueueing without touching the inbox.
	fake.response = nil
	if _, err = f.service.SavePushSubscription(ctx, token, "push_delivery", "https://fcm.googleapis.com/fcm/send/four", key, auth, "Chrome"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateNotificationRule(ctx, token, "push_delivery", activityRule, notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, false, true)); err != nil {
		t.Fatal(err)
	}
	notificationEpisode(t, f, 11, "ugc-1", "101", "added_to_program")
	if r := sweepNotifications(t, f); r.Notifications != 1 || pushCount(t, f, `SELECT count(*) FROM account_push_outbox`) != 0 {
		t.Fatalf("push disabled %+v", r)
	}
}
