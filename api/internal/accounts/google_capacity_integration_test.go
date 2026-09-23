package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Seed storage using a real flow's shape, not replayable synthetic callbacks.
// Expired rows include abandoned starts and claimed exchanges, both reclaimable
// at the exact deadline. Remaining rows expire one OAuth lifetime later.
func seedGoogleFlows(t *testing.T, f *lifecycleFixture, state string, count, expired int) {
	t.Helper()
	digest, err := TokenDigest(state)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(t.Context(), `INSERT INTO account_oauth_flows
	 (state_digest,browser_digest,nonce,verifier_key_id,verifier_nonce,verifier_ciphertext,mode,created_at,expires_at,claimed_at)
	 SELECT sha256(state_digest || convert_to(i::text,'UTF8')),browser_digest,nonce,verifier_key_id,verifier_nonce,verifier_ciphertext,'login',
	 CASE WHEN i<=$3 THEN $4::timestamptz ELSE $5::timestamptz END,
	 CASE WHEN i<=$3 THEN $5::timestamptz ELSE $6::timestamptz END,
	 CASE WHEN i<=$3 AND i%2=0 THEN $4::timestamptz ELSE NULL END
	 FROM account_oauth_flows CROSS JOIN generate_series(1,$2::int) i WHERE state_digest=$1`, digest[:], count, expired, f.now().Add(-OAuthLifetime), f.now(), f.now().Add(OAuthLifetime))
	if err != nil {
		t.Fatal(err)
	}
}

func googleFlowCounts(t *testing.T, f *lifecycleFixture) (total, expired int) {
	t.Helper()
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER (WHERE expires_at<=$1) FROM account_oauth_flows`, f.now()).Scan(&total, &expired); err != nil {
		t.Fatal(err)
	}
	return total, expired
}

func TestGoogleAdmissionRecoveryIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	f.useGoogle(GoogleIdentity{Subject: "capacity", Email: "capacity@gmail.com", EmailVerified: true, EmailAuthoritative: true})
	live, liveState := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	claimed, claimedState := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	if _, err := f.service.claimGoogle(t.Context(), claimedState, claimed.Browser.Token, ""); err != nil {
		t.Fatal(err)
	}
	seedGoogleFlows(t, f, liveState, 9998, 9998)
	if total, expired := googleFlowCounts(t, f); total != 10000 || expired != 9998 {
		t.Fatalf("seed: total=%d expired=%d", total, expired)
	}
	startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	if total, expired := googleFlowCounts(t, f); total != 9901 || expired != 9898 {
		t.Fatalf("admission must reclaim exactly one bounded batch: total=%d expired=%d", total, expired)
	}
	claimedDigest, _ := TokenDigest(claimedState)
	var retained bool
	if err := f.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM account_oauth_flows WHERE state_digest=$1 AND claimed_at IS NOT NULL)`, claimedDigest[:]).Scan(&retained); err != nil || !retained {
		t.Fatalf("live in-flight fence removed: %v", err)
	}
	if _, err := f.service.GoogleCallback(t.Context(), liveState, live.Browser.Token, "valid", ""); err != nil {
		t.Fatalf("live callback invalidated by reclamation: %v", err)
	}
}

func TestGoogleAdmissionConcurrentBoundIntegration(t *testing.T) {
	for _, expired := range []int{0, 4} {
		name := "live_capacity"
		if expired > 0 {
			name = "expired_capacity"
		}
		t.Run(name, func(t *testing.T) {
			f := newLifecycleFixture(t)
			f.useGoogle(GoogleIdentity{})
			_, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
			seedGoogleFlows(t, f, state, 9998, expired)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			ready := make(chan struct{})
			done := make(chan error, 16)
			for range cap(done) {
				go func() {
					<-ready
					_, err := f.service.StartGoogle(ctx, "", GoogleStart{Mode: FlowLogin})
					done <- err
				}()
			}
			close(ready)
			succeeded := 0
			for range cap(done) {
				if err := <-done; err == nil {
					succeeded++
				} else if !errors.Is(err, ErrUnavailable) {
					t.Errorf("unexpected admission error: %v", err)
				}
			}
			if succeeded != expired+1 {
				t.Fatalf("concurrent admitted=%d want=%d", succeeded, expired+1)
			}
			if total, remaining := googleFlowCounts(t, f); total != 10000 || remaining != 0 {
				t.Fatalf("hard storage bound: total=%d expired=%d", total, remaining)
			}
			if _, err := f.service.StartGoogle(ctx, "", GoogleStart{Mode: FlowLogin}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("full live capacity admitted another flow: %v", err)
			}
		})
	}
}

func TestGoogleAdmissionExpiredExchangeIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	g := f.useGoogle(GoogleIdentity{Subject: "expired-exchange", Email: "expired@gmail.com", EmailVerified: true, EmailAuthoritative: true})
	g.entered, g.resume = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	done := make(chan error, 1)
	go func() {
		_, err := f.service.GoogleCallback(ctx, state, start.Browser.Token, "valid", "")
		done <- err
	}()
	select {
	case <-g.entered:
	case <-ctx.Done():
		t.Fatal("callback did not enter exchange")
	}
	f.advance(OAuthLifetime)
	_, admissionErr := f.service.StartGoogle(ctx, "", GoogleStart{Mode: FlowLogin})
	close(g.resume)
	if err := <-done; !errors.Is(err, ErrInvalidLink) || admissionErr != nil {
		t.Fatalf("expired exchange finalization=%v admission=%v", err, admissionErr)
	}
	if total, expired := googleFlowCounts(t, f); total != 1 || expired != 0 {
		t.Fatalf("expired exchange fence retained: total=%d expired=%d", total, expired)
	}
	var accounts, identities, sessions int
	if err := f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM accounts),(SELECT count(*) FROM account_google_identities),(SELECT count(*) FROM account_sessions)`).Scan(&accounts, &identities, &sessions); err != nil || accounts != 0 || identities != 0 || sessions != 0 {
		t.Fatalf("expired callback created authority: accounts=%d identities=%d sessions=%d error=%v", accounts, identities, sessions, err)
	}
}

func TestGoogleAdmissionSkipsAccountBoundExpiryIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	owner := completeGoogle(t, f, "capacity-owner@example.com", "capacity_owner")
	_, boundState := startGoogle(t, f, owner.Cookie.Token, GoogleStart{Mode: FlowReauth, Action: ActionDelete})
	f.advance(OAuthLifetime)
	_, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	seedGoogleFlows(t, f, state, 9998, 0)
	// Keep the account locked: admission must not try to reclaim its expired
	// flow or acquire a parent lock after the global admission lock.
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `SELECT id FROM accounts WHERE email='capacity-owner@example.com' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := f.service.StartGoogle(ctx, "", GoogleStart{Mode: FlowLogin}); !errors.Is(err, ErrUnavailable) || ctx.Err() != nil {
		t.Fatalf("account-bound expiry must still count without parent locking: %v", err)
	}
	if total, expired := googleFlowCounts(t, f); total != 10000 || expired != 1 {
		t.Fatalf("account-bound row reclaimed: total=%d expired=%d", total, expired)
	}
	digest, _ := TokenDigest(boundState)
	var retained bool
	if err := f.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM account_oauth_flows WHERE state_digest=$1 AND account_id IS NOT NULL)`, digest[:]).Scan(&retained); err != nil || !retained {
		t.Fatalf("account-bound flow removed: %v", err)
	}
}
