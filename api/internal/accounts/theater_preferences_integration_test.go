package accounts

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestTheaterPreferencesIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "one@example.com", "theater_one")
	two := f.complete(t, "two@example.com", "theater_two")
	var initialRevision int64
	if err := f.pool.QueryRow(ctx, `SELECT auth_revision FROM accounts WHERE email='one@example.com'`).Scan(&initialRevision); err != nil {
		t.Fatal(err)
	}
	read := func(raw, username, revision string, ids []string) {
		t.Helper()
		view, err := f.service.TheaterPreferences(ctx, raw)
		if err != nil || view.Username != username || view.Revision != revision || view.TheaterIDs == nil || !slices.Equal(view.TheaterIDs, ids) {
			t.Fatalf("unexpected preference view: %+v, %v", view, err)
		}
	}
	read(one.Cookie.Token, "theater_one", "0", []string{})
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_theater_preferences`).Scan(&count); err != nil || count != 0 {
		t.Fatal("read initialized account")
	}
	if _, err := f.service.SaveTheaterPreferences(ctx, two.Cookie.Token, "theater_one", "0", []string{"ugc-1"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("cookie owner switch accepted")
	}
	view, err := f.service.SaveTheaterPreferences(ctx, one.Cookie.Token, "theater_one", "0", []string{"unknown-z", "ugc-1"})
	if err != nil || view.Revision != "1" || !slices.Equal(view.TheaterIDs, []string{"ugc-1", "unknown-z"}) {
		t.Fatalf("first save: %+v %v", view, err)
	}
	view, err = f.service.SaveTheaterPreferences(ctx, one.Cookie.Token, "theater_one", "1", []string{"unknown-z", "ugc-1"})
	if err != nil || view.Revision != "1" {
		t.Fatal("no-op advanced revision")
	}
	if _, err = f.service.SaveTheaterPreferences(ctx, one.Cookie.Token, "theater_one", "0", view.TheaterIDs); !errors.Is(err, ErrTheaterSelectionChanged) {
		t.Fatal("identical stale write accepted")
	}
	view, err = f.service.SaveTheaterPreferences(ctx, one.Cookie.Token, "theater_one", "1", []string{})
	if err != nil || view.Revision != "2" || view.TheaterIDs == nil {
		t.Fatal("explicit empty save failed")
	}
	read(one.Cookie.Token, "theater_one", "2", []string{})
	read(two.Cookie.Token, "theater_two", "0", []string{})
	// Explicit empty initialization is still initialized, not a no-op.
	if v, err := f.service.SaveTheaterPreferences(ctx, two.Cookie.Token, "theater_two", "0", []string{}); err != nil || v.Revision != "1" {
		t.Fatal("empty first save failed")
	}
	if _, err = f.pool.Exec(ctx, `UPDATE account_theater_preferences SET revision=$1 WHERE account_id=(SELECT id FROM accounts WHERE email='one@example.com')`, maxTheaterPreferenceRevision); err != nil {
		t.Fatal(err)
	}
	maxRevision := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	if _, err = f.service.SaveTheaterPreferences(ctx, one.Cookie.Token, "theater_one", maxRevision, []string{"ugc-2"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revision overflow accepted")
	}
	read(one.Cookie.Token, "theater_one", maxRevision, []string{})
	if _, err = f.service.SaveTheaterPreferences(ctx, one.Cookie.Token, "theater_one", maxRevision, []string{}); err != nil {
		t.Fatal("no-op at maximum failed")
	}
	// Preferences must not revoke sessions or change auth_revision.
	var revision int64
	if err = f.pool.QueryRow(ctx, `SELECT auth_revision FROM accounts WHERE email='one@example.com'`).Scan(&revision); err != nil || revision != initialRevision {
		t.Fatal("preference update changed account authority")
	}
}

func TestTheaterPreferencesConcurrentCASIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "race@example.com", "theater_race")
	two, err := f.service.Login(t.Context(), "race@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "1"} {
		start := make(chan struct{})
		errorsCh := make(chan error, 2)
		for i, raw := range []string{one.Cookie.Token, two.Cookie.Token} {
			go func() {
				<-start
				_, err := f.service.SaveTheaterPreferences(t.Context(), raw, "theater_race", revision, []string{"unknown-" + revision + "-" + strconv.Itoa(i)})
				errorsCh <- err
			}()
		}
		close(start)
		var success, conflict int
		for range 2 {
			err := <-errorsCh
			if err == nil {
				success++
			} else if errors.Is(err, ErrTheaterSelectionChanged) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("CAS winners %d conflicts %d", success, conflict)
		}
		a, err := f.service.TheaterPreferences(t.Context(), one.Cookie.Token)
		if err != nil {
			t.Fatal(err)
		}
		b, err := f.service.TheaterPreferences(t.Context(), two.Cookie.Token)
		if err != nil || a.Revision != b.Revision || !slices.Equal(a.TheaterIDs, b.TheaterIDs) {
			t.Fatal("sessions did not converge")
		}
	}
}

func TestTheaterPreferencesAuthorizationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	assertDenied := func(raw string, want error) {
		t.Helper()
		if _, err := f.service.TheaterPreferences(ctx, raw); !errors.Is(err, want) {
			t.Fatalf("read error %v want %v", err, want)
		}
		if _, err := f.service.SaveTheaterPreferences(ctx, raw, "theater_auth", "0", []string{}); !errors.Is(err, want) {
			t.Fatalf("write error %v want %v", err, want)
		}
	}
	assertDenied("", ErrUnauthorized)
	f.register(t, "pending@example.com", testPassword)
	pending, err := f.service.Login(ctx, "pending@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	assertDenied(pending.Cookie.Token, ErrPending)
	username := f.pending(t, "username@example.com")
	assertDenied(username.Cookie.Token, ErrPending)
	complete := f.complete(t, "auth@example.com", "theater_auth")
	if err = f.service.Logout(ctx, complete.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	assertDenied(complete.Cookie.Token, ErrUnauthorized)
	complete, err = f.service.Login(ctx, "auth@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	f.advance(SessionIdleLifetime)
	assertDenied(complete.Cookie.Token, ErrUnauthorized)
	complete, err = f.service.Login(ctx, "auth@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	f.advance(SessionLifetime)
	assertDenied(complete.Cookie.Token, ErrUnauthorized)
}

func TestTheaterPreferencesRollbackIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	a := f.complete(t, "rollback@example.com", "theater_rollback")
	if _, err := f.pool.Exec(ctx, `ALTER TABLE account_theater_preferences ADD CONSTRAINT fixture_reject_selection CHECK (NOT ('reject-me'=ANY(theater_ids)))`); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "1"} {
		var last time.Time
		digest, err := TokenDigest(a.Cookie.Token)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.pool.QueryRow(ctx, `SELECT last_seen_at FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&last); err != nil {
			t.Fatal(err)
		}
		f.advance(time.Minute)
		view, err := f.service.SaveTheaterPreferences(ctx, a.Cookie.Token, "theater_rollback", revision, []string{"reject-me"})
		if !errors.Is(err, ErrUnavailable) || view.Username != "" || view.Revision != "" || view.TheaterIDs != nil {
			t.Fatal("failed write leaked tentative result")
		}
		var after time.Time
		if err = f.pool.QueryRow(ctx, `SELECT last_seen_at FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&after); err != nil || !last.Equal(after) {
			t.Fatal("failed transaction retained session touch")
		}
		view, err = f.service.TheaterPreferences(ctx, a.Cookie.Token)
		if err != nil || view.Revision != revision || len(view.TheaterIDs) != 0 {
			t.Fatal("failed write mutated durable state")
		}
		if revision == "0" {
			if _, err = f.service.SaveTheaterPreferences(ctx, a.Cookie.Token, "theater_rollback", "0", []string{}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestTheaterPreferencesRevocationRaceIntegration(t *testing.T) {
	for _, mutation := range []string{"revoke", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			f := newLifecycleFixture(t)
			a := f.complete(t, "race@example.com", "theater_race")
			if _, err := f.service.SaveTheaterPreferences(t.Context(), a.Cookie.Token, "theater_race", "0", []string{"ugc-1"}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			var id int64
			if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email='race@example.com' FOR UPDATE`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := f.service.SaveTheaterPreferences(ctx, a.Cookie.Token, "theater_race", "1", []string{"ugc-2"})
				result <- err
			}()
			if mutation == "delete" {
				_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id)
			} else {
				err = revoke(ctx, tx, &account{id: id, revision: 1})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-result; !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("stale authority write: %v", err)
			}
			var revision int64
			err = f.pool.QueryRow(ctx, `SELECT revision FROM account_theater_preferences WHERE account_id=$1`, id).Scan(&revision)
			if mutation == "delete" {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal("delete failed to cascade")
				}
			} else if err != nil || revision != 1 {
				t.Fatal("revoked write mutated preference")
			}
		})
	}
}
