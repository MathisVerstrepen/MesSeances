package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func (f *lifecycleFixture) register(t *testing.T, email, password string) Cookie {
	t.Helper()
	cookie, err := f.service.Register(t.Context(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	return cookie
}

func TestRegistrationBrowserBindingIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	const email = "binding@example.com"
	browser := f.register(t, email, testPassword)
	token := f.token(t, email, TokenVerification)
	first, err := f.service.Login(ctx, email, testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Login(ctx, email, testPassword, first.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, first.Cookie.Token)
	wrong, _, err := NewToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, proof := range []string{"", "malformed", wrong, second.Cookie.Token} {
		if _, err = f.service.ConfirmVerification(ctx, token, proof, second.Cookie.Token); !errors.Is(err, ErrVerificationBrowser) {
			t.Fatal("tentative-password login substituted for original attempt proof")
		}
	}
	var before string
	if err = f.pool.QueryRow(ctx, `SELECT encoded_hash FROM account_passwords WHERE account_id=(SELECT id FROM accounts WHERE email=$1)`, email).Scan(&before); err != nil {
		t.Fatal(err)
	}
	verified, err := f.service.ConfirmVerification(ctx, token, browser.Token, "")
	if err != nil || verified.View.State != StatePendingUsername {
		t.Fatalf("original browser proof lost on session rotation: %v", err)
	}
	var after string
	var tentative bool
	var digest []byte
	if err = f.pool.QueryRow(ctx, `SELECT encoded_hash,tentative,registration_digest FROM account_passwords WHERE account_id=(SELECT id FROM accounts WHERE email=$1)`, email).Scan(&after, &tentative, &digest); err != nil || before != after || tentative || digest != nil {
		t.Fatal("verification changed chosen password or retained attempt authority")
	}
	assertAnonymous(t, f.service, second.Cookie.Token)
	if _, err = f.service.Login(ctx, email, testPassword, ""); err != nil {
		t.Fatal("originally chosen password did not survive verification")
	}
	if _, err = f.service.ConfirmVerification(ctx, token, browser.Token, ""); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("verification replay accepted")
	}
}

func TestRegistrationResendBindingIntegration(t *testing.T) {
	for _, proofKind := range []string{"missing", "wrong", "login", "original"} {
		t.Run(proofKind, func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := t.Context()
			const email = "resend@example.com"
			browser := f.register(t, email, testPassword)
			old := f.token(t, email, TokenVerification)
			proof := ""
			switch proofKind {
			case "wrong":
				var err error
				proof, _, err = NewToken(nil)
				if err != nil {
					t.Fatal(err)
				}
			case "login":
				login, err := f.service.Login(ctx, email, testPassword, "")
				if err != nil {
					t.Fatal(err)
				}
				proof = login.Cookie.Token
			case "original":
				proof = browser.Token
			}
			f.advance(time.Minute)
			if err := f.service.RequestVerification(ctx, email, proof); err != nil {
				t.Fatal("resend must be generic regardless of proof")
			}
			current := f.token(t, email, TokenVerification)
			if proofKind == "original" {
				if current == old {
					t.Fatal("resend did not replace link")
				}
				if _, err := f.service.ConfirmVerification(ctx, old, browser.Token, ""); !errors.Is(err, ErrInvalidLink) {
					t.Fatal("resend did not supersede old link")
				}
			} else if current != old {
				t.Fatal("unbound resend disturbed legitimate attempt")
			}
			if _, err := f.service.ConfirmVerification(ctx, current, browser.Token, ""); err != nil {
				t.Fatal("resend lost original attempt proof")
			}
		})
	}
}

func TestRegistrationRestartAndDeadlinesIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	const email = "restart@example.com"
	old := f.register(t, email, testPassword)
	first := f.token(t, email, TokenVerification)
	created := f.now()
	f.advance(VerificationLifetime)
	if _, err := f.service.ConfirmVerification(ctx, first, old.Token, ""); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("exact verification expiry accepted")
	}
	// Lost browser cookie recovers only by a new registration, which binds that
	// registration's chosen password. It never lends proof to the old credential.
	newBrowser := f.register(t, email, changedPassword)
	second := f.token(t, email, TokenVerification)
	var actual time.Time
	if err := f.pool.QueryRow(ctx, `SELECT created_at FROM accounts WHERE email=$1`, email).Scan(&actual); err != nil || !actual.Equal(created) {
		t.Fatal("registration restart extended pending account lifetime")
	}
	if _, err := f.service.ConfirmVerification(ctx, second, old.Token, ""); !errors.Is(err, ErrVerificationBrowser) {
		t.Fatal("old proof adopted new attempt")
	}
	if _, err := f.service.Login(ctx, email, testPassword, ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("old tentative password survived restart")
	}
	f.advance(PendingLifetime - VerificationLifetime - time.Second)
	if err := f.service.RequestVerification(ctx, email, newBrowser.Token); err != nil {
		t.Fatal(err)
	}
	last := f.token(t, email, TokenVerification)
	f.advance(time.Second)
	if _, err := f.service.ConfirmVerification(ctx, last, newBrowser.Token, ""); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("resend extended pending lifetime")
	}
	// After cleanup/recreation even matching email/password cannot revive proof.
	f.advance(time.Minute)
	fresh := f.register(t, email, changedPassword)
	token := f.token(t, email, TokenVerification)
	if _, err := f.service.ConfirmVerification(ctx, token, newBrowser.Token, ""); !errors.Is(err, ErrVerificationBrowser) {
		t.Fatal("expired account proof revived")
	}
	if _, err := f.service.ConfirmVerification(ctx, token, fresh.Token, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationRevocationAndLoginRaceIntegration(t *testing.T) {
	t.Run("revocation", func(t *testing.T) {
		f := newLifecycleFixture(t)
		ctx := t.Context()
		const email = "revoke_pending@example.com"
		browser := f.register(t, email, testPassword)
		old := f.token(t, email, TokenVerification)
		login, err := f.service.Login(ctx, email, testPassword, "")
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.Logout(ctx, login.Cookie.Token, true); err != nil {
			t.Fatal(err)
		}
		f.advance(time.Minute)
		if err = f.service.RequestVerification(ctx, email, browser.Token); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_tokens`).Scan(&count); err != nil || count != 0 {
			t.Fatal("revoked attempt regained verification authority")
		}
		if _, err = f.service.ConfirmVerification(ctx, old, browser.Token, ""); !errors.Is(err, ErrInvalidLink) {
			t.Fatal("revoked link accepted")
		}
	})
	t.Run("login snapshot cannot cross restart", func(t *testing.T) {
		f := newLifecycleFixture(t)
		ctx := t.Context()
		const email = "pending_race@example.com"
		f.register(t, email, testPassword)
		f.hasher.verified, f.hasher.resume = make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() { _, err := f.service.Login(ctx, email, testPassword, ""); done <- err }()
		<-f.hasher.verified
		f.advance(time.Minute)
		f.register(t, email, changedPassword)
		close(f.hasher.resume)
		if err := <-done; !errors.Is(err, ErrCredentials) {
			t.Fatal("stale tentative login survived attempt revision")
		}
	})
}

func TestRegistrationRestartConfirmationRaceIntegration(t *testing.T) {
	for range 4 {
		f := newLifecycleFixture(t)
		ctx := context.Background()
		const email = "attempt_race@example.com"
		old := f.register(t, email, testPassword)
		token := f.token(t, email, TokenVerification)
		f.advance(time.Minute)
		start := make(chan struct{})
		confirmation := make(chan error, 1)
		restart := make(chan error, 1)
		proof := make(chan Cookie, 1)
		go func() {
			<-start
			_, err := f.service.ConfirmVerification(ctx, token, old.Token, "")
			confirmation <- err
		}()
		go func() {
			<-start
			cookie, err := f.service.Register(ctx, email, changedPassword)
			proof <- cookie
			restart <- err
		}()
		close(start)
		if err := <-restart; err != nil {
			t.Fatal(err)
		}
		newBrowser := <-proof
		confirmErr := <-confirmation
		want, wrong := testPassword, changedPassword
		if confirmErr != nil {
			if !errors.Is(confirmErr, ErrInvalidLink) {
				t.Fatal(confirmErr)
			}
			want, wrong = changedPassword, testPassword
			current := f.token(t, email, TokenVerification)
			if _, err := f.service.ConfirmVerification(ctx, current, old.Token, ""); !errors.Is(err, ErrVerificationBrowser) {
				t.Fatal("racing attempt borrowed predecessor browser")
			}
			if _, err := f.service.ConfirmVerification(ctx, current, newBrowser.Token, ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.service.Login(ctx, email, wrong, ""); !errors.Is(err, ErrCredentials) {
			t.Fatal("losing attempt credential activated")
		}
		if _, err := f.service.Login(ctx, email, want, ""); err != nil {
			t.Fatal("winning attempt credential not activated")
		}
	}
}
