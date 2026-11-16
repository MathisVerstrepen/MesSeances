package accounts

import (
	"errors"
	"testing"
)

func TestActivityManualReadingIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := t.Context()
	a := f.complete(t, "reading@example.com", "reading_owner")
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "reading_owner", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	read := func(raw string) ActivityReadingSnapshot {
		t.Helper()
		v, err := f.service.ActivityReading(ctx, raw, []string{"5", "4", "2"})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	assertStates := func(v ActivityReadingSnapshot, manual bool, last string, states ...bool) {
		t.Helper()
		if v.Reading.Username != "reading_owner" || v.Reading.ManualReading != manual || v.Reading.LastReadEventID != last || len(v.Items) != len(states) {
			t.Fatalf("unexpected reading metadata: %+v", v)
		}
		for i, want := range states {
			if v.Items[i].Consulted != want {
				t.Fatalf("event %s consulted=%t, want %t", v.Items[i].EventID, v.Items[i].Consulted, want)
			}
		}
	}
	assertStates(read(a.Cookie.Token), false, "0", false, false, false)
	if _, err := f.service.SaveActivityRead(ctx, a.Cookie.Token, "reading_owner", "5", true); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("individual read allowed outside manual mode", err)
	}
	if _, err := f.service.SaveActivityReading(ctx, a.Cookie.Token, "reading_owner", true); err != nil {
		t.Fatal(err)
	}
	// Loading the feed, reading state, and even stale automatic visits are inert.
	for range 2 {
		if _, err := f.service.VisitActivity(ctx, a.Cookie.Token, "reading_owner", "followed", "5"); err != nil {
			t.Fatal(err)
		}
		v, err := f.service.FollowedActivity(ctx, a.Cookie.Token, FollowedActivityQuery{Limit: 1})
		if err != nil || !v.Reading.ManualReading || len(v.Items) != 1 || v.Items[0].Consulted || v.NextCursor == nil {
			t.Fatalf("manual feed: %+v, %v", v, err)
		}
		assertStates(read(a.Cookie.Token), true, "0", false, false, false)
	}
	set := func(id string, consulted bool) {
		t.Helper()
		v, err := f.service.SaveActivityRead(ctx, a.Cookie.Token, "reading_owner", id, consulted)
		if err != nil || v.EventID != id || v.Consulted != consulted || v.Username != "reading_owner" {
			t.Fatalf("save read: %+v, %v", v, err)
		}
	}
	set("5", true)
	set("5", true) // Idempotent retry.
	assertStates(read(a.Cookie.Token), true, "0", true, false, false)
	device, err := f.service.Login(ctx, "reading@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	assertStates(read(device.Cookie.Token), true, "0", true, false, false)
	set("5", false)
	assertStates(read(a.Cookie.Token), true, "0", false, false, false)
	for _, id := range []string{"0", "01", "9223372036854775808", "1", "3", "6", "7", "999"} {
		if _, err := f.service.SaveActivityRead(ctx, a.Cookie.Token, "reading_owner", id, true); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid/unfollowed/baseline/superseded ID %s accepted: %v", id, err)
		}
	}
	if _, err := f.service.SaveActivityReading(ctx, a.Cookie.Token, "someone_else", false); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("wrong preference owner accepted", err)
	}
	if _, err := f.service.SaveActivityRead(ctx, a.Cookie.Token, "someone_else", "5", true); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("wrong read owner accepted", err)
	}
	if _, err := f.service.MarkAllActivityConsulted(ctx, a.Cookie.Token, "reading_owner", "4"); err != nil {
		t.Fatal(err)
	}
	// Explicit bulk applies to older unloaded rows, leaving newer activity new.
	assertStates(read(a.Cookie.Token), true, "4", false, true, true)
	set("2", false) // A consulted older item can become new again.
	assertStates(read(a.Cookie.Token), true, "4", false, true, false)
	set("5", true)
	notification, err := f.service.ActivityNotification(ctx, a.Cookie.Token)
	if err != nil || !notification.HasNewActivity {
		t.Fatal("older unread override did not notify", err)
	}
	set("2", true)
	notification, err = f.service.ActivityNotification(ctx, a.Cookie.Token)
	if err != nil || notification.HasNewActivity {
		t.Fatal("all individually consulted activities still notify", err)
	}
	other := f.complete(t, "reading-other@example.com", "reading_other")
	otherState, err := f.service.ActivityReading(ctx, other.Cookie.Token, []string{"5"})
	if err != nil || otherState.Reading.ManualReading || otherState.Items[0].Consulted {
		t.Fatal("another account inherited reading state", err)
	}
	if _, err := f.service.SaveActivityReading(ctx, a.Cookie.Token, "reading_owner", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.VisitActivity(ctx, a.Cookie.Token, "reading_owner", "followed", "5"); err != nil {
		t.Fatal(err)
	}
	assertStates(read(a.Cookie.Token), false, "5", true, true, true)
	if _, err := f.service.SaveActivityReading(ctx, a.Cookie.Token, "reading_owner", true); err != nil {
		t.Fatal(err)
	}
	assertStates(read(a.Cookie.Token), true, "5", true, true, true)
	// Retention removes per-item data; account deletion removes both new tables.
	set("5", false)
	followedActivityExec(t, f, `DELETE FROM cinema_activity_episode_sources WHERE episode_id=5`)
	followedActivityExec(t, f, `DELETE FROM cinema_activity_episodes WHERE id=5`)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_activity_reads WHERE event_id=5`).Scan(&count); err != nil || count != 0 {
		t.Fatal("episode retention did not cascade", err)
	}
	followedActivityExec(t, f, `DELETE FROM accounts WHERE email='reading@example.com'`)
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_activity_reads)+(SELECT count(*) FROM account_activity_preferences)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("account deletion did not cascade", err)
	}
	if _, err := f.service.ActivityReading(ctx, device.Cookie.Token, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("deleted session read private state", err)
	}
}
