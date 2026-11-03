package accounts

import (
	"errors"
	"sync"
	"testing"
)

func TestActivityVisitsPersistenceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "visits-one@example.com", "visits_one")
	two := f.complete(t, "visits-two@example.com", "visits_two")
	visit := func(raw, username, scope, latest, previous string) {
		t.Helper()
		v, err := f.service.VisitActivity(ctx, raw, username, scope, latest)
		if err != nil || v.Username != username || v.Scope != scope || v.PreviousLastEventID != previous {
			t.Fatalf("visit %+v: %v; expected previous %s", v, err, previous)
		}
	}
	visit(one.Cookie.Token, "visits_one", "followed", "9007199254740993", "0")
	device, err := f.service.Login(ctx, "visits-one@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	// An independent session observes the saved marker without browser state.
	visit(device.Cookie.Token, "visits_one", "followed", "9007199254740994", "9007199254740993")
	visit(one.Cookie.Token, "visits_one", "followed", "10", "9007199254740994")
	visit(one.Cookie.Token, "visits_one", "followed", "0", "9007199254740994")
	visit(one.Cookie.Token, "visits_one", "theater:ugc-1", "42", "0")
	visit(one.Cookie.Token, "visits_one", "theater:ugc-2", "43", "0")
	visit(two.Cookie.Token, "visits_two", "followed", "1", "0")
	if _, err = f.service.VisitActivity(ctx, two.Cookie.Token, "visits_one", "followed", "100"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("wrong owner accepted", err)
	}
	if err = f.service.Logout(ctx, one.Cookie.Token, false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.VisitActivity(ctx, one.Cookie.Token, "visits_one", "followed", "100"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked session accepted", err)
	}
	visit(device.Cookie.Token, "visits_one", "theater:ugc-1", "42", "42")
	for _, sql := range []string{
		`UPDATE account_activity_visits SET last_event_id=-1`,
		`UPDATE account_activity_visits SET scope='theater:'`,
		`UPDATE account_activity_visits SET scope='unexpected'`,
	} {
		if _, err = f.pool.Exec(ctx, sql); err == nil {
			t.Fatal("schema accepted invalid visit")
		}
	}
	if _, err = f.pool.Exec(ctx, `DELETE FROM accounts WHERE email='visits-one@example.com'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_activity_visits`).Scan(&count); err != nil || count != 1 {
		t.Fatal("visit deletion did not cascade or affected another owner", err)
	}
}

func TestActivityVisitsConcurrentIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	a := f.complete(t, "visits-race@example.com", "visits_race")
	var wg sync.WaitGroup
	for _, id := range []string{"90", "10", "100", "20"} {
		wg.Go(func() {
			if _, err := f.service.VisitActivity(t.Context(), a.Cookie.Token, "visits_race", "followed", id); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	v, err := f.service.VisitActivity(t.Context(), a.Cookie.Token, "visits_race", "followed", "0")
	if err != nil || v.PreviousLastEventID != "100" {
		t.Fatal("concurrent writes lost newest visit", err)
	}
}
