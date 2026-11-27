package accounts

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accountmail"
)

func digestMail(t *testing.T, f *lifecycleFixture, email string) (accountmail.Message, int) {
	t.Helper()
	rows, err := f.pool.Query(t.Context(), `SELECT o.event_digest,o.payload_key_id,o.payload_nonce,o.payload_ciphertext FROM account_mail_outbox o JOIN accounts a ON a.id=o.account_id
 WHERE a.email=$1 AND o.purpose='notification_digest' AND o.state='pending' ORDER BY o.id`, email)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var last accountmail.Message
	count := 0
	for rows.Next() {
		var event []byte
		var env accountmail.Envelope
		if err := rows.Scan(&event, &env.KeyID, &env.Nonce, &env.Ciphertext); err != nil {
			t.Fatal(err)
		}
		plain, err := f.cipher.Open(accountmail.OutboxBinding(event, "notification_digest"), env)
		if err != nil || json.Unmarshal(plain, &last) != nil {
			t.Fatal("undecryptable digest", err)
		}
		count++
	}
	return last, count
}

func TestNotificationDigestIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := t.Context()
	// 2026-11-26 is a Thursday; 06:00 UTC is before the 08:00 Paris slot.
	f.clock.Store(time.Date(2026, 11, 26, 6, 0, 0, 0, time.UTC).UnixNano())
	a := f.complete(t, "digest@example.com", "digest_owner")
	token := a.Cookie.Token
	if _, err := f.service.SaveTheaterFollow(ctx, token, "digest_owner", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	view, err := f.service.NotificationDigest(ctx, token)
	if err != nil || view.Frequency != DigestOff || view.NextDueAt != nil || !view.Available {
		t.Fatalf("initial %+v %v", view, err)
	}
	if _, err = f.service.SaveNotificationDigest(ctx, token, "digest_owner", "hourly"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown frequency", err)
	}
	if _, err = f.service.SaveNotificationDigest(ctx, token, "other", DigestDaily); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("client chose owner", err)
	}
	mailed := notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, false, true)
	mailed.Email = true
	rules, err := f.service.CreateNotificationRule(ctx, token, "digest_owner", mailed)
	if err != nil || !rules.Rules[0].Email {
		t.Fatal(rules, err)
	}
	// An alert without email stays out of the digest.
	if _, err = f.service.CreateNotificationRule(ctx, token, "digest_owner", notificationRule(NotificationKindActivity, NotificationScopeCustom, []string{"ugc-3"}, false, true)); err != nil {
		t.Fatal(err)
	}
	sweepNotifications(t, f)
	notificationEpisode(t, f, 8, "ugc-1", "102", "added_to_program")
	sweepNotifications(t, f)
	// Entries delivered before enabling the digest are never mailed.
	view, err = f.service.SaveNotificationDigest(ctx, token, "digest_owner", DigestDaily)
	if err != nil || view.Frequency != DigestDaily || view.NextDueAt == nil || !view.NextDueAt.Equal(time.Date(2026, 11, 26, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("enable %+v %v", view, err)
	}
	notificationEpisode(t, f, 9, "ugc-1", "101", "added_to_program")
	notificationEpisode(t, f, 10, "ugc-3", "101", "added_to_program")
	sweepNotifications(t, f)
	if r, err := f.service.SweepDigests(ctx); err != nil || r != (DigestResult{}) {
		t.Fatalf("sent before due %+v %v", r, err)
	}
	f.advance(90 * time.Minute)
	if r, err := f.service.SweepDigests(ctx); err != nil || r.Sent != 1 {
		t.Fatalf("due %+v %v", r, err)
	}
	message, count := digestMail(t, f, "digest@example.com")
	if count != 1 || message.Subject != "Vos alertes MesSeances : 1 notification" || !strings.Contains(message.Text, "Current override arrive à Active 1") || strings.Contains(message.Text, "Active 3") {
		t.Fatalf("digest %d %+v", count, message)
	}
	unsubscribe, err := url.Parse(message.UnsubscribeURL)
	if err != nil || unsubscribe.Path != "/api/v1/notifications/unsubscribe" || !strings.Contains(message.Text, "/notifications/desabonnement?token=") {
		t.Fatal("unsubscribe links", message.UnsubscribeURL)
	}
	view, _ = f.service.NotificationDigest(ctx, token)
	if view.LastSentAt == nil || !view.NextDueAt.Equal(time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("after send %+v", view)
	}
	// Nothing new (or only read entries): the slot advances without a mail.
	notificationEpisode(t, f, 11, "ugc-1", "102", "added_to_program")
	sweepNotifications(t, f)
	inbox, _ := f.service.Notifications(ctx, token, 0, "")
	if _, err = f.service.MarkAllNotificationsRead(ctx, token, "digest_owner", inbox.LatestID); err != nil {
		t.Fatal(err)
	}
	f.advance(24 * time.Hour)
	if r, err := f.service.SweepDigests(ctx); err != nil || r.Empty != 1 || r.Sent != 0 {
		t.Fatalf("empty %+v %v", r, err)
	}
	if _, count = digestMail(t, f, "digest@example.com"); count != 1 {
		t.Fatal("empty digest mailed")
	}
	// Weekly digests wait for Wednesday.
	if view, err = f.service.SaveNotificationDigest(ctx, token, "digest_owner", DigestWeekly); err != nil || view.NextDueAt.Weekday() != time.Wednesday {
		t.Fatalf("weekly %+v %v", view, err)
	}

	// One-click unsubscribe: tampered and foreign tokens fail, valid is idempotent.
	raw := unsubscribe.Query().Get("token")
	for _, bad := range []string{"", "x", raw[:len(raw)-2] + "AA", strings.Repeat("A", 32)} {
		if err := f.service.UnsubscribeDigest(ctx, bad); !errors.Is(err, ErrInvalidLink) {
			t.Fatalf("token %q accepted: %v", bad, err)
		}
	}
	for range 2 {
		if err := f.service.UnsubscribeDigest(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	if view, _ = f.service.NotificationDigest(ctx, token); view.Frequency != DigestOff || view.NextDueAt != nil {
		t.Fatalf("unsubscribed %+v", view)
	}
	// Re-enabling starts after the newest entry again.
	notificationEpisode(t, f, 12, "ugc-1", "101", "added_to_program")
	sweepNotifications(t, f)
	if _, err = f.service.SaveNotificationDigest(ctx, token, "digest_owner", DigestDaily); err != nil {
		t.Fatal(err)
	}
	f.advance(48 * time.Hour)
	if r, err := f.service.SweepDigests(ctx); err != nil || r.Empty != 1 {
		t.Fatalf("re-enabled digest mailed history %+v %v", r, err)
	}
	// Changing the address invalidates outstanding unsubscribe links.
	if _, err := f.pool.Exec(ctx, `UPDATE accounts SET email='digest-new@example.com' WHERE email='digest@example.com'`); err != nil {
		t.Fatal(err)
	}
	if err := f.service.UnsubscribeDigest(ctx, raw); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("stale address token accepted", err)
	}
}
