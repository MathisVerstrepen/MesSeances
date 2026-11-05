package accounts

import (
	"errors"
	"testing"
)

func TestActivityNotificationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := t.Context()
	a := f.complete(t, "notification@example.com", "notification_owner")
	assertStatus := func(raw, username, revision string, expected bool) {
		t.Helper()
		v, err := f.service.ActivityNotification(ctx, raw)
		if err != nil || v.Username != username || v.FollowsRevision != revision || v.HasNewActivity != expected {
			t.Fatalf("status %+v, err %v; want new=%t", v, err, expected)
		}
	}
	assertStatus(a.Cookie.Token, "notification_owner", "0", false)
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "notification_owner", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	assertStatus(a.Cookie.Token, "notification_owner", "1", true)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_activity_visits`).Scan(&count); err != nil || count != 0 {
		t.Fatal("notification read advanced the visit", err)
	}
	if _, err := f.service.VisitActivity(ctx, a.Cookie.Token, "notification_owner", "theater:ugc-1", "6"); err != nil {
		t.Fatal(err)
	}
	assertStatus(a.Cookie.Token, "notification_owner", "1", true)
	if _, err := f.service.VisitActivity(ctx, a.Cookie.Token, "notification_owner", "followed", "5"); err != nil {
		t.Fatal(err)
	}
	// Baseline 1, superseded 6 and unfollowed 7 must not create a dot.
	assertStatus(a.Cookie.Token, "notification_owner", "1", false)
	device, err := f.service.Login(ctx, "notification@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(device.Cookie.Token, "notification_owner", "1", false)
	other := f.complete(t, "notification-other@example.com", "notification_other")
	if _, err := f.service.SaveTheaterFollow(ctx, other.Cookie.Token, "notification_other", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	assertStatus(other.Cookie.Token, "notification_other", "1", true)
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "notification_owner", "1", "ugc-3", true); err != nil {
		t.Fatal(err)
	}
	assertStatus(a.Cookie.Token, "notification_owner", "2", true)
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "notification_owner", "2", "ugc-3", false); err != nil {
		t.Fatal(err)
	}
	assertStatus(a.Cookie.Token, "notification_owner", "3", false)
	followedActivityExec(t, f, `INSERT INTO cinema_activity_episodes(id,theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,observed_from,observed_through,detecting_generation)
 OVERRIDING SYSTEM VALUE VALUES(8,'ugc-1','ugc','101','added_to_program',CURRENT_TIMESTAMP,CURRENT_DATE,CURRENT_DATE,CURRENT_DATE,1)`)
	assertStatus(a.Cookie.Token, "notification_owner", "3", true)
	if err := f.service.Logout(ctx, device.Cookie.Token, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ActivityNotification(ctx, device.Cookie.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked session read notification", err)
	}
}
