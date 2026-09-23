package accounts

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type fakeGoogle struct {
	identity        GoogleIdentity
	exchanges       atomic.Int32
	entered, resume chan struct{}
}

func (*fakeGoogle) AuthorizationURL(state, nonce, verifier string) (string, error) {
	return "https://accounts.google.com/test?state=" + state, nil
}
func (g *fakeGoogle) Exchange(ctx context.Context, code, verifier, nonce string) (GoogleIdentity, error) {
	g.exchanges.Add(1)
	if g.entered != nil {
		close(g.entered)
		select {
		case <-g.resume:
		case <-ctx.Done():
			return GoogleIdentity{}, ctx.Err()
		}
	}
	if code != "valid" {
		return GoogleIdentity{}, ErrInvalidLink
	}
	return g.identity, nil
}
func (f *lifecycleFixture) useGoogle(identity GoogleIdentity) *fakeGoogle {
	g := &fakeGoogle{identity: identity}
	f.service.google = g
	f.service.flowCipher = f.cipher
	return g
}
func startGoogle(t *testing.T, f *lifecycleFixture, raw string, input GoogleStart) (GoogleStartResult, string) {
	t.Helper()
	start, err := f.service.StartGoogle(context.Background(), raw, input)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	return start, u.Query().Get("state")
}
func loginGoogle(t *testing.T, f *lifecycleFixture) GoogleCallbackResult {
	t.Helper()
	start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	result, err := f.service.GoogleCallback(context.Background(), state, start.Browser.Token, "valid", "")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func completeGoogle(t *testing.T, f *lifecycleFixture, email, name string) SessionResult {
	t.Helper()
	f.useGoogle(GoogleIdentity{Subject: "subject-" + name, Email: email, EmailVerified: true})
	login := loginGoogle(t, f)
	complete, err := f.service.Username(context.Background(), login.Session.Cookie.Token, name)
	if err != nil {
		t.Fatal(err)
	}
	return complete
}
func googleProof(t *testing.T, f *lifecycleFixture, raw string, action Action, target string) SessionResult {
	t.Helper()
	start, state := startGoogle(t, f, raw, GoogleStart{Mode: FlowReauth, Action: action, Target: target})
	result, err := f.service.GoogleCallback(context.Background(), state, start.Browser.Token, "valid", raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Destination != "/compte/confirmer-identite" {
		t.Fatal("unsafe continuation destination")
	}
	return result.Session
}
func googleGrant(t *testing.T, f *lifecycleFixture, raw, email string, action Action, target string) GrantResult {
	t.Helper()
	proof := googleProof(t, f, raw, action, target)
	if err := f.service.RequestReauthEmail(context.Background(), proof.Cookie.Token); err != nil {
		t.Fatal(err)
	}
	grant, err := f.service.ConfirmReauthEmail(context.Background(), proof.Cookie.Token, f.token(t, email, TokenEmailStepUp), action, target)
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestGoogleSignupAdvancingClockIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	base := f.now()
	var ticks atomic.Int64
	f.service.now = func() time.Time {
		return base.Add(time.Duration(ticks.Add(1)) * time.Microsecond)
	}
	f.useGoogle(GoogleIdentity{Subject: "advancing-clock", Email: "clock@example.com", EmailVerified: true})
	login := loginGoogle(t, f)
	if login.Destination != "/finaliser" || login.Session.View.State != StatePendingUsername {
		t.Fatal("verified Google signup failed with an advancing clock")
	}
	var created, verified time.Time
	var source string
	if err := f.pool.QueryRow(t.Context(), `SELECT created_at,email_verified_at,verification_source FROM accounts WHERE email=$1`, "clock@example.com").Scan(&created, &verified, &source); err != nil {
		t.Fatal(err)
	}
	if !created.Equal(verified) || source != "google" {
		t.Fatal("Google signup must use the same creation and verification timestamp")
	}
}

func TestGoogleSignupLoginAndPendingIntegration(t *testing.T) {
	ctx := context.Background()
	t.Run("subject_not_email", func(t *testing.T) {
		f := newLifecycleFixture(t)
		g := f.useGoogle(GoogleIdentity{Subject: "subject-one", Email: "google@example.com", EmailVerified: true})
		login := loginGoogle(t, f)
		if login.Destination != "/finaliser" || login.Session.View.State != StatePendingUsername {
			t.Fatal("Google verification not honored")
		}
		complete, err := f.service.Username(ctx, login.Session.Cookie.Token, "google_owner")
		if err != nil {
			t.Fatal(err)
		}
		g.identity.Email = "changed@example.com"
		g.identity.EmailVerified = false
		again := loginGoogle(t, f)
		if again.Session.View.Account.Email != "google@example.com" || again.Destination != "/compte" {
			t.Fatal("Google rewrote account email")
		}
		details, err := f.service.Details(ctx, again.Session.Cookie.Token)
		if err != nil || details.GoogleEmail == nil || *details.GoogleEmail != "changed@example.com" {
			t.Fatal("observed email not separated")
		}
		g.identity = GoogleIdentity{Subject: "different-subject", Email: "google@example.com", EmailVerified: true}
		start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
		if _, err = f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); !errors.Is(err, ErrIdentityUnavailable) {
			t.Fatalf("automatic email merge: %v", err)
		}
		if err = f.service.RequestReset(ctx, "google@example.com"); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_tokens WHERE purpose='password_reset'`).Scan(&count); err != nil || count != 0 {
			t.Fatal("Google-only password recovery created credential")
		}
		if _, err = f.service.ReauthPassword(ctx, complete.Cookie.Token, testPassword, ActionDelete, ""); !errors.Is(err, ErrRecentAuth) {
			t.Fatal("Google-only password proof accepted")
		}
	})
	t.Run("unverified_subject_and_mailbox", func(t *testing.T) {
		f := newLifecycleFixture(t)
		f.useGoogle(GoogleIdentity{Subject: "pending-subject", Email: "pending@example.com"})
		login := loginGoogle(t, f)
		if login.Destination != "/verification" || login.Session.View.State != StatePendingEmail {
			t.Fatal("missing SES fallback")
		}
		token := f.token(t, "pending@example.com", TokenVerification)
		if _, err := f.service.ConfirmVerification(ctx, token, testPassword, ""); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("Google verification added password")
		}
		other := f.complete(t, "other@example.com", "other_google")
		if _, err := f.service.ConfirmVerification(ctx, token, "", other.Cookie.Token); err == nil {
			t.Fatal("wrong-subject verification accepted")
		}
		if _, err := f.service.ConfirmVerification(ctx, token, "", ""); err == nil {
			t.Fatal("mailbox alone verified Google")
		}
		f.advance(time.Minute)
		if err := f.service.RequestVerification(ctx, "pending@example.com", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.ConfirmVerification(ctx, token, "", login.Session.Cookie.Token); !errors.Is(err, ErrInvalidLink) {
			t.Fatal("superseded proof accepted")
		}
		result, err := f.service.ConfirmVerification(ctx, f.token(t, "pending@example.com", TokenVerification), "", login.Session.Cookie.Token)
		if err != nil || result.View.State != StatePendingUsername || result.View.Account.HasPassword {
			t.Fatalf("pending confirmation: %v", err)
		}
		assertAnonymous(t, f.service, login.Session.Cookie.Token)
	})
	t.Run("missing_email", func(t *testing.T) {
		f := newLifecycleFixture(t)
		f.useGoogle(GoogleIdentity{Subject: "no-email", EmailVerified: true})
		start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
		if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); !errors.Is(err, ErrInvalidLink) {
			t.Fatal("new account missing email accepted")
		}
	})
	t.Run("expired_restart", func(t *testing.T) {
		f := newLifecycleFixture(t)
		f.useGoogle(GoogleIdentity{Subject: "restart", Email: "restart@example.com", EmailVerified: true})
		old := loginGoogle(t, f)
		f.advance(PendingLifetime)
		assertAnonymous(t, f.service, old.Session.Cookie.Token)
		fresh := loginGoogle(t, f)
		if fresh.Session.View.State != StatePendingUsername {
			t.Fatal("expired restart failed")
		}
		var count int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil || count != 1 {
			t.Fatal("expired registration retained")
		}
	})
}

func TestGoogleFlowSecurityIntegration(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"binding", "replay", "denial", "expiry", "exchange_failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newLifecycleFixture(t)
			g := f.useGoogle(GoogleIdentity{Subject: "flow", Email: "flow@example.com", EmailVerified: true})
			start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
			switch kind {
			case "binding":
				wrong, _, _ := NewToken(nil)
				if _, err := f.service.GoogleCallback(ctx, state, wrong, "valid", ""); err == nil {
					t.Fatal("wrong browser accepted")
				}
				if g.exchanges.Load() != 0 {
					t.Fatal("exchanged before binding")
				}
				if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); err != nil {
					t.Fatal(err)
				}
			case "replay":
				if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); err != nil {
					t.Fatal(err)
				}
			case "denial":
				if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "", ""); err == nil {
					t.Fatal("denial accepted")
				}
			case "expiry":
				f.advance(OAuthLifetime)
			case "exchange_failure":
				if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "invalid", ""); err == nil {
					t.Fatal("failed exchange accepted")
				}
			}
			if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); err == nil {
				t.Fatal("burned/expired flow reused")
			}
		})
	}
	t.Run("wrong_subject_and_session", func(t *testing.T) {
		f := newLifecycleFixture(t)
		complete := completeGoogle(t, f, "reauth@example.com", "reauth_owner")
		start, state := startGoogle(t, f, complete.Cookie.Token, GoogleStart{Mode: FlowReauth, Action: ActionDelete})
		other := loginGoogle(t, f)
		if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", other.Session.Cookie.Token); !errors.Is(err, ErrRecentAuth) {
			t.Fatalf("wrong session: %v", err)
		}
		f.service.google.(*fakeGoogle).identity.Subject = "wrong-subject"
		if _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", complete.Cookie.Token); !errors.Is(err, ErrRecentAuth) {
			t.Fatalf("wrong subject: %v", err)
		}
		if _, err := f.service.ReauthContinuation(ctx, complete.Cookie.Token); !errors.Is(err, ErrRecentAuth) {
			t.Fatal("failed flow produced continuation")
		}
	})
}

func TestGoogleStepUpAndPasswordAddIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	email := "step@example.com"
	complete := completeGoogle(t, f, email, "step_owner")
	if err := f.service.RequestReauthEmail(ctx, complete.Cookie.Token); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("email challenge before Google proof")
	}
	// First invalid request spends a quota; advance the cooldown before valid request.
	f.advance(time.Minute)
	proof := googleProof(t, f, complete.Cookie.Token, ActionPasswordAdd, "")
	continuation, err := f.service.ReauthContinuation(ctx, proof.Cookie.Token)
	if err != nil || continuation.Action != ActionPasswordAdd || continuation.Target != nil {
		t.Fatalf("continuation: %v", err)
	}
	assertAnonymous(t, f.service, complete.Cookie.Token)
	if err = f.service.RequestReauthEmail(ctx, proof.Cookie.Token); err != nil {
		t.Fatal(err)
	}
	token := f.token(t, email, TokenEmailStepUp)
	if _, err = f.service.ConfirmReauthEmail(ctx, proof.Cookie.Token, token, ActionDelete, ""); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("action not bound")
	}
	other := loginGoogle(t, f)
	if _, err = f.service.ConfirmReauthEmail(ctx, other.Session.Cookie.Token, token, ActionPasswordAdd, ""); err == nil {
		t.Fatal("challenge crossed sessions")
	}
	grant, err := f.service.ConfirmReauthEmail(ctx, proof.Cookie.Token, token, ActionPasswordAdd, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ReauthContinuation(ctx, grant.Cookie.Token); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("Google proof survived grant issuance")
	}
	if _, err = f.service.ConfirmReauthEmail(ctx, proof.Cookie.Token, token, ActionPasswordAdd, ""); err == nil {
		t.Fatal("challenge replay")
	}
	cookie, err := f.service.ChangePassword(ctx, grant.Cookie.Token, testPassword, grant.Grant)
	if err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, other.Session.Cookie.Token)
	if _, err = f.service.Login(ctx, email, testPassword, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.StartGoogle(ctx, cookie.Token, GoogleStart{Mode: FlowReauth, Action: ActionDelete}); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("password account bypassed password proof")
	}
}

func TestGoogleTargetAndProofExpiryIntegration(t *testing.T) {
	ctx := context.Background()
	t.Run("target", func(t *testing.T) {
		f := newLifecycleFixture(t)
		complete := completeGoogle(t, f, "target@example.com", "target_owner")
		proof := googleProof(t, f, complete.Cookie.Token, ActionEmailChange, " NEW@example.com ")
		continuation, err := f.service.ReauthContinuation(ctx, proof.Cookie.Token)
		if err != nil || continuation.Target == nil || *continuation.Target != "new@example.com" {
			t.Fatal("recoverable normalized target missing")
		}
		if err = f.service.RequestReauthEmail(ctx, proof.Cookie.Token); err != nil {
			t.Fatal(err)
		}
		token := f.token(t, "target@example.com", TokenEmailStepUp)
		if _, err = f.service.ConfirmReauthEmail(ctx, proof.Cookie.Token, token, ActionEmailChange, "wrong@example.com"); !errors.Is(err, ErrRecentAuth) {
			t.Fatal("target not bound")
		}
		grant, err := f.service.ConfirmReauthEmail(ctx, proof.Cookie.Token, token, ActionEmailChange, "new@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.RequestEmailChange(ctx, grant.Cookie.Token, "new@example.com", grant.Grant); err != nil {
			t.Fatal(err)
		}
		confirmation := f.token(t, "target@example.com", TokenEmailChange)
		if err = f.service.ConfirmEmailChange(ctx, grant.Cookie.Token, confirmation, grant.Grant); !errors.Is(err, ErrRecentAuth) {
			t.Fatal("request proof reused for confirmation")
		}
		f.advance(time.Minute)
		fresh := googleGrant(t, f, grant.Cookie.Token, "target@example.com", ActionEmailChange, "new@example.com")
		if err = f.service.ConfirmEmailChange(ctx, fresh.Cookie.Token, confirmation, fresh.Grant); err != nil {
			t.Fatal(err)
		}
		assertAnonymous(t, f.service, fresh.Cookie.Token)
	})
	t.Run("exact_proof_deadline", func(t *testing.T) {
		f := newLifecycleFixture(t)
		complete := completeGoogle(t, f, "expires@example.com", "expires_owner")
		proof := googleProof(t, f, complete.Cookie.Token, ActionDelete, "")
		if err := f.service.RequestReauthEmail(ctx, proof.Cookie.Token); err != nil {
			t.Fatal(err)
		}
		token := f.token(t, "expires@example.com", TokenEmailStepUp)
		f.advance(OAuthLifetime)
		if _, err := f.service.ReauthContinuation(ctx, proof.Cookie.Token); !errors.Is(err, ErrRecentAuth) {
			t.Fatal("expired continuation")
		}
		if _, err := f.service.ConfirmReauthEmail(ctx, proof.Cookie.Token, token, ActionDelete, ""); err == nil {
			t.Fatal("expired combined proof")
		}
	})
}

func TestGoogleLinkUnlinkIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	complete := f.complete(t, "password@example.com", "link_owner")
	f.useGoogle(GoogleIdentity{Subject: "link-subject", Email: "different@example.com", EmailVerified: true})
	proof, err := f.service.ReauthPassword(ctx, complete.Cookie.Token, testPassword, ActionGoogleLink, "")
	if err != nil {
		t.Fatal(err)
	}
	start, state := startGoogle(t, f, proof.Cookie.Token, GoogleStart{Mode: FlowLink, Grant: proof.Grant})
	if _, err = f.service.StartGoogle(ctx, proof.Cookie.Token, GoogleStart{Mode: FlowLink, Grant: proof.Grant}); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("link grant replay")
	}
	linked, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", proof.Cookie.Token)
	if err != nil || !linked.Session.View.Account.GoogleLinked || linked.Session.View.Account.Email != "password@example.com" {
		t.Fatalf("link: %v", err)
	}
	assertAnonymous(t, f.service, proof.Cookie.Token)
	proof, err = f.service.ReauthPassword(ctx, linked.Session.Cookie.Token, testPassword, ActionGoogleUnlink, "")
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := f.service.UnlinkGoogle(ctx, proof.Cookie.Token, proof.Grant)
	if err != nil {
		t.Fatal(err)
	}
	details, err := f.service.Details(ctx, cookie.Token)
	if err != nil || details.GoogleLinked {
		t.Fatal("unlink failed")
	}

	googleOnly := completeGoogle(t, f, "only@example.com", "only_owner")
	grant := googleGrant(t, f, googleOnly.Cookie.Token, "only@example.com", ActionGoogleUnlink, "")
	if _, err = f.service.UnlinkGoogle(ctx, grant.Cookie.Token, grant.Grant); !errors.Is(err, ErrLastMethod) {
		t.Fatal("last-method removed")
	}
}

func TestGoogleConcurrentClaimAndIdentityOwnershipIntegration(t *testing.T) {
	ctx := context.Background()
	t.Run("one_exchange", func(t *testing.T) {
		f := newLifecycleFixture(t)
		g := f.useGoogle(GoogleIdentity{Subject: "concurrent", Email: "concurrent@example.com", EmailVerified: true})
		g.entered = make(chan struct{})
		g.resume = make(chan struct{})
		start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
		done := make(chan error, 1)
		go func() { _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); done <- err }()
		<-g.entered
		_, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", "")
		close(g.resume)
		firstErr := <-done
		if !errors.Is(err, ErrInvalidLink) || firstErr != nil || g.exchanges.Load() != 1 {
			t.Fatalf("flow claim not exclusive: first=%v second=%v", firstErr, err)
		}
	})
	t.Run("owned_identity", func(t *testing.T) {
		f := newLifecycleFixture(t)
		completeGoogle(t, f, "owned@example.com", "owned_subject")
		other := f.complete(t, "otherlink@example.com", "other_link")
		grant, err := f.service.ReauthPassword(ctx, other.Cookie.Token, testPassword, ActionGoogleLink, "")
		if err != nil {
			t.Fatal(err)
		}
		start, state := startGoogle(t, f, grant.Cookie.Token, GoogleStart{Mode: FlowLink, Grant: grant.Grant})
		if _, err = f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", grant.Cookie.Token); !errors.Is(err, ErrIdentityUnavailable) {
			t.Fatal("owned identity moved")
		}
		view, err := f.service.Session(ctx, grant.Cookie.Token)
		if err != nil || view.Account.GoogleLinked {
			t.Fatal("failed link mutated account")
		}
		login := loginGoogle(t, f)
		if login.Session.View.Account.Email != "owned@example.com" {
			t.Fatal("owned identity changed owner")
		}
	})
	t.Run("link_deadline", func(t *testing.T) {
		f := newLifecycleFixture(t)
		other := f.complete(t, "linkexpiry@example.com", "link_expiry")
		f.useGoogle(GoogleIdentity{Subject: "late", Email: "late@example.com", EmailVerified: true})
		grant, err := f.service.ReauthPassword(ctx, other.Cookie.Token, testPassword, ActionGoogleLink, "")
		if err != nil {
			t.Fatal(err)
		}
		start, state := startGoogle(t, f, grant.Cookie.Token, GoogleStart{Mode: FlowLink, Grant: grant.Grant})
		f.advance(GrantLifetime)
		if _, err = f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", grant.Cookie.Token); !errors.Is(err, ErrInvalidLink) {
			t.Fatal("link outlived consumed grant")
		}
	})
}
