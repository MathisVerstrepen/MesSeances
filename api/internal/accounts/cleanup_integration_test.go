package accounts

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAccountDeletionAtomicIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	complete := f.complete(t, "delete@example.com", "permanent_name")
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_google_identities(account_id,issuer,subject,observed_email_verified) SELECT id,$1,'deleted-subject',true FROM accounts WHERE email='delete@example.com'`, googleIssuer); err != nil {
		t.Fatal(err)
	}
	grant, err := f.service.ReauthPassword(ctx, complete.Cookie.Token, testPassword, ActionDelete, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Delete(ctx, grant.Cookie.Token, grant.Grant, "supprimer"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("explicit confirmation omitted")
	}
	if _, err = f.pool.Exec(ctx, `CREATE FUNCTION reject_account_delete() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic deletion rollback'; END$$; CREATE TRIGGER reject_account_delete BEFORE DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION reject_account_delete()`); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Delete(ctx, grant.Cookie.Token, grant.Grant, "SUPPRIMER"); err == nil {
		t.Fatal("failed deletion committed")
	}
	view, err := f.service.Session(ctx, grant.Cookie.Token)
	if err != nil || view.State != StateComplete {
		t.Fatal("rollback lost session")
	}
	var consumed bool
	if err = f.pool.QueryRow(ctx, `SELECT consumed_at IS NOT NULL FROM account_tokens WHERE purpose='reauth_grant'`).Scan(&consumed); err != nil || consumed {
		t.Fatal("rollback consumed proof")
	}
	if _, err = f.pool.Exec(ctx, `DROP TRIGGER reject_account_delete ON accounts; DROP FUNCTION reject_account_delete()`); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Delete(ctx, grant.Cookie.Token, grant.Grant, "SUPPRIMER"); err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, grant.Cookie.Token)
	for _, table := range []string{"accounts", "account_passwords", "account_google_identities", "account_sessions", "account_tokens", "account_oauth_flows", "account_mail_outbox"} {
		var count int
		if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("personal data retained in %s", table)
		}
	}
	var columns int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='account_username_claims'`).Scan(&columns); err != nil || columns != 2 {
		t.Fatal("tombstone gained metadata")
	}
	var username string
	var owner *int64
	if err = f.pool.QueryRow(ctx, `SELECT username,account_id FROM account_username_claims`).Scan(&username, &owner); err != nil || username != "permanent_name" || owner != nil {
		t.Fatal("username-only reservation missing")
	}
	if _, err = f.service.Login(ctx, "delete@example.com", testPassword, ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("deleted password restored account")
	}
	f.advance(time.Minute)
	pending := f.pending(t, "delete@example.com")
	if _, err = f.service.Username(ctx, pending.Cookie.Token, "permanent_name"); !errors.Is(err, ErrUsernameUnavailable) {
		t.Fatal("deleted username reclaimed")
	}
}

func TestGoogleCallbackDeletionRevocationRaceIntegration(t *testing.T) {
	for _, operation := range []string{"delete", "logout_all"} {
		t.Run(operation, func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := context.Background()
			complete := completeGoogle(t, f, "race@example.com", "race_owner")
			var deleteGrant GrantResult
			if operation == "delete" {
				deleteGrant = googleGrant(t, f, complete.Cookie.Token, "race@example.com", ActionDelete, "")
			} else {
				deleteGrant.Cookie = complete.Cookie
			}
			g := f.service.google.(*fakeGoogle)
			g.entered = make(chan struct{})
			g.resume = make(chan struct{})
			start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
			done := make(chan error, 1)
			go func() { _, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", ""); done <- err }()
			<-g.entered
			var err error
			if operation == "delete" {
				err = f.service.Delete(ctx, deleteGrant.Cookie.Token, deleteGrant.Grant, "SUPPRIMER")
			} else {
				err = f.service.Logout(ctx, deleteGrant.Cookie.Token, true)
			}
			close(g.resume)
			callbackErr := <-done
			if err != nil {
				t.Fatal(err)
			}
			if callbackErr == nil {
				t.Fatal("in-flight callback survived revocation")
			}
			var sessions int
			if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions`).Scan(&sessions); err != nil || sessions != 0 {
				t.Fatal("callback resurrected session")
			}
			var accounts int
			if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&accounts); err != nil {
				t.Fatal(err)
			}
			if operation == "delete" && accounts != 0 {
				t.Fatal("callback resurrected identity")
			}
			g.entered = nil
			g.resume = nil
			fresh := loginGoogle(t, f)
			if fresh.Session.Cookie.Token == "" {
				t.Fatal("fresh login after revocation blocked")
			}
		})
	}
}

func TestLoginDeletionRaceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	complete := f.complete(t, "loginrace@example.com", "loginrace_owner")
	grant, err := f.service.ReauthPassword(ctx, complete.Cookie.Token, testPassword, ActionDelete, "")
	if err != nil {
		t.Fatal(err)
	}
	f.hasher.verified = make(chan struct{})
	f.hasher.resume = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := f.service.Login(ctx, "loginrace@example.com", testPassword, ""); done <- err }()
	<-f.hasher.verified
	err = f.service.Delete(ctx, grant.Cookie.Token, grant.Grant, "SUPPRIMER")
	close(f.hasher.resume)
	loginErr := <-done
	if err != nil || !errors.Is(loginErr, ErrCredentials) {
		t.Fatalf("login deletion race: delete=%v login=%v", err, loginErr)
	}
}

func TestAccountCleanupDeadlinesAndBoundsIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	pending := f.pending(t, "pendingcleanup@example.com")
	complete := f.complete(t, "completecleanup@example.com", "cleanup_owner")
	f.useGoogle(GoogleIdentity{Subject: "unused", Email: "unused@example.com", EmailVerified: true})
	startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	f.advance(PendingLifetime - time.Microsecond)
	result, err := f.service.Cleanup(ctx)
	if err != nil || result.PendingAccounts != 0 {
		t.Fatalf("early cleanup: %+v %v", result, err)
	}
	f.advance(time.Microsecond)
	assertAnonymous(t, f.service, pending.Cookie.Token)
	result, err = f.service.Cleanup(ctx)
	if err != nil || result.PendingAccounts != 1 {
		t.Fatalf("exact pending cleanup: %+v %v", result, err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil || count != 1 {
		t.Fatal("completed account purged")
	}
	assertAnonymous(t, f.service, complete.Cookie.Token)
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired sessions retained")
	}
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox WHERE state='pending'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired encrypted payload retained")
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO accounts(email,created_at,pending_kind) SELECT 'batch'||n||'@example.com',$1,'email' FROM generate_series(1,105) n`, f.now().Add(-PendingLifetime)); err != nil {
		t.Fatal(err)
	}
	result, err = f.service.Cleanup(ctx)
	if err != nil || result.PendingAccounts != 100 {
		t.Fatalf("cleanup batch not bounded: %+v %v", result, err)
	}
	result, err = f.service.Cleanup(ctx)
	if err != nil || result.PendingAccounts != 5 {
		t.Fatalf("cleanup second batch: %+v %v", result, err)
	}
}

func TestCleanupOnboardingRaceIntegration(t *testing.T) {
	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := context.Background()
			pending := f.pending(t, "onboardingrace@example.com")
			f.advance(PendingLifetime - time.Second)
			start := make(chan struct{})
			completed := make(chan error, 1)
			cleaned := make(chan error, 1)
			go func() {
				<-start
				_, err := f.service.Username(ctx, pending.Cookie.Token, "onboardingrace_owner")
				completed <- err
			}()
			go func() { <-start; _, err := f.service.Cleanup(ctx); cleaned <- err }()
			close(start)
			if err := <-completed; err != nil {
				t.Fatal(err)
			}
			if err := <-cleaned; err != nil {
				t.Fatal(err)
			}
			f.advance(time.Second)
			if _, err := f.service.Cleanup(ctx); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil || count != 1 {
				t.Fatal("cleanup purged completed account")
			}
		})
	}
}

func TestCleanupAncillaryRetentionAndRollbackIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	now := f.now()
	// Independent HMAC suppressions expire at 180 days, terminal metadata at 7.
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_mail_suppressions(address_digest,reason,created_at,updated_at,expires_at) VALUES(decode(repeat('01',32),'hex'),'complaint',$1,$1,$2)`, now.Add(-180*24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_mail_outbox(event_digest,purpose,state,created_at,expires_at,next_attempt_at,finished_at) VALUES(decode(repeat('02',32),'hex'),'security_notification','sent',$1,$2,$1,$1)`, now.Add(-7*24*time.Hour), now.Add(-6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_rate_limits(purpose,key_digest,window_start,window_seconds,count,expires_at) SELECT 'login',decode(lpad(to_hex(n),64,'0'),'hex'),$1,60,1,$2 FROM generate_series(1,1100) n`, now.Add(-49*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Cleanup(ctx)
	if err != nil || !result.Overdue || result.ExpiredRows != 1002 {
		t.Fatalf("retention/overdue: %+v %v", result, err)
	}
	result, err = f.service.Cleanup(ctx)
	if err != nil || result.Overdue || result.ExpiredRows != 100 {
		t.Fatalf("backlog drained: %+v %v", result, err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO accounts(email,created_at,pending_kind) VALUES('rollbackcleanup@example.com',$1,'email')`, now.Add(-PendingLifetime)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `CREATE FUNCTION reject_cleanup_delete() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic cleanup rollback'; END$$; CREATE TRIGGER reject_cleanup_delete BEFORE DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION reject_cleanup_delete()`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Cleanup(ctx); err == nil {
		t.Fatal("cleanup rollback failure ignored")
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed cleanup removed pending account")
	}
	if _, err = f.pool.Exec(ctx, `DROP TRIGGER reject_cleanup_delete ON accounts; DROP FUNCTION reject_cleanup_delete()`); err != nil {
		t.Fatal(err)
	}
	result, err = f.service.Cleanup(ctx)
	if err != nil || result.PendingAccounts != 1 {
		t.Fatalf("cleanup recovery: %+v %v", result, err)
	}
}
