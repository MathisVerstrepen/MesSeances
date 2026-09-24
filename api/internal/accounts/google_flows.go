package accounts

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
)

type GoogleStart struct {
	Mode   FlowMode `json:"mode"`
	Grant  string   `json:"grant"`
	Action Action   `json:"action"`
	Target string   `json:"target"`
}
type GoogleStartResult struct {
	AuthorizationURL string
	Browser          Cookie
}
type GoogleCallbackResult struct {
	Session     SessionResult
	Destination string
}

type googleFlow struct {
	digest                  Digest
	mode                    FlowMode
	id, revision            *int64
	session, grant, binding []byte
	action                  *Action
	target                  *string
	nonce                   string
	verifier                accountmail.Envelope
	expires                 time.Time
	event                   int64
}

func flowBinding(digest Digest) string {
	return "messeances-oauth-verifier-v1\x00" + string(digest[:])
}

func (s *Service) StartGoogle(ctx context.Context, raw string, input GoogleStart) (GoogleStartResult, error) {
	if s.google == nil || s.flowCipher == nil {
		return GoogleStartResult{}, ErrUnavailable
	}
	var binding Digest
	var err error
	switch input.Mode {
	case FlowLogin:
		if input.Grant != "" || input.Action != "" || input.Target != "" {
			return GoogleStartResult{}, ErrInvalidInput
		}
	case FlowLink:
		if input.Grant == "" || input.Action != "" || input.Target != "" {
			return GoogleStartResult{}, ErrInvalidInput
		}
		input.Action = ActionGoogleLink
		_, binding, _ = actionBinding(input.Action, "")
	case FlowReauth:
		if input.Grant != "" {
			return GoogleStartResult{}, ErrInvalidInput
		}
		input.Target, binding, err = actionBinding(input.Action, input.Target)
		if err != nil {
			return GoogleStartResult{}, err
		}
	default:
		return GoogleStartResult{}, ErrInvalidInput
	}
	if input.Mode != FlowLogin {
		if err = s.sessionQuota(ctx, raw, "google_start", false, true); err != nil {
			return GoogleStartResult{}, err
		}
	}
	state, digest, err := s.newToken()
	if err != nil {
		return GoogleStartResult{}, err
	}
	browser, browserDigest, err := s.newToken()
	if err != nil {
		return GoogleStartResult{}, err
	}
	nonce, _, err := s.newToken()
	if err != nil {
		return GoogleStartResult{}, err
	}
	verifier, _, err := s.newToken()
	if err != nil {
		return GoogleStartResult{}, err
	}
	envelope, err := s.flowCipher.Seal(flowBinding(digest), []byte(verifier))
	if err != nil {
		return GoogleStartResult{}, ErrUnavailable
	}
	url, err := s.google.AuthorizationURL(state, nonce, verifier)
	if err != nil {
		return GoogleStartResult{}, ErrUnavailable
	}
	expires := s.now().UTC().Add(OAuthLifetime)
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		var id, revision *int64
		var sessionDigest, grantDigest, actionDigest []byte
		var action *Action
		var target *string
		if input.Mode != FlowLogin {
			a, ss, err := s.authorize(ctx, tx, raw, true)
			if err != nil {
				return err
			}
			if input.Mode == FlowReauth && (!a.google || a.hash != nil || input.Action == ActionPasswordChange) {
				return ErrRecentAuth
			}
			if input.Mode == FlowLink {
				if a.google {
					return ErrIdentityUnavailable
				}
				if err = s.consumeGrant(ctx, tx, a, ss, input.Grant, ActionGoogleLink, ""); err != nil {
					return err
				}
				d, _ := TokenDigest(input.Grant)
				grantDigest = d[:]
				var grantExpiry time.Time
				if err = tx.QueryRow(ctx, `SELECT expires_at FROM account_tokens WHERE token_digest=$1`, grantDigest).Scan(&grantExpiry); err != nil {
					return ErrUnavailable
				}
				if grantExpiry.Before(expires) {
					expires = grantExpiry
				}
			}
			id, revision, sessionDigest, action, actionDigest = &a.id, &a.revision, ss.digest[:], &input.Action, binding[:]
			if input.Target != "" {
				target = &input.Target
			}
			if _, err = tx.Exec(ctx, `DELETE FROM account_oauth_flows WHERE session_digest=$1`, ss.digest[:]); err != nil {
				return ErrUnavailable
			}
			if _, err = tx.Exec(ctx, `DELETE FROM account_tokens WHERE session_digest=$1 AND purpose IN ('google_reauth','email_step_up')`, ss.digest[:]); err != nil {
				return ErrUnavailable
			}
		}
		// Short, bounded admission lock, acquired only after any account lock.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(719423044)`); err != nil {
			return ErrUnavailable
		}
		// Reclaim a bounded batch through the expiry index before counting stored
		// rows. Anonymous flows have no parent locks; account-bound expiry remains
		// with account-first cleanup. Skip callbacks/cleanup already holding a row.
		if _, err := tx.Exec(ctx, `DELETE FROM account_oauth_flows WHERE state_digest IN (
		 SELECT state_digest FROM account_oauth_flows WHERE account_id IS NULL AND expires_at<=$1
		 ORDER BY expires_at LIMIT 100 FOR UPDATE SKIP LOCKED
		)`, s.now().UTC()); err != nil {
			return ErrUnavailable
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_oauth_flows`).Scan(&count); err != nil || count >= 10000 {
			return ErrUnavailable
		}
		_, err := tx.Exec(ctx, `INSERT INTO account_oauth_flows(state_digest,browser_digest,nonce,verifier_key_id,verifier_nonce,verifier_ciphertext,mode,account_id,session_digest,auth_revision,grant_digest,action,action_digest,target_email,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, digest[:], browserDigest[:], nonce, envelope.KeyID, envelope.Nonce, envelope.Ciphertext, input.Mode, id, sessionDigest, revision, grantDigest, action, actionDigest, target, s.now().UTC(), expires)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return GoogleStartResult{AuthorizationURL: url, Browser: Cookie{Token: browser, ExpiresAt: expires}}, err
}

// claimGoogle burns the flow before any external I/O. A retained claimed row is
// a revocation fence, not replayable authority; finalization must still find it.
func (s *Service) claimGoogle(ctx context.Context, state, browser, raw string) (googleFlow, error) {
	digest, err := TokenDigest(state)
	if err != nil {
		return googleFlow{}, ErrInvalidLink
	}
	bd, err := TokenDigest(browser)
	if err != nil {
		return googleFlow{}, ErrInvalidLink
	}
	f := googleFlow{digest: digest}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT mode,account_id,session_digest,auth_revision,grant_digest,action,action_digest,target_email,nonce,verifier_key_id,verifier_nonce,verifier_ciphertext,expires_at,authority_event FROM account_oauth_flows WHERE state_digest=$1 AND browser_digest=$2 AND claimed_at IS NULL AND expires_at>$3`, digest[:], bd[:], s.now().UTC()).Scan(&f.mode, &f.id, &f.session, &f.revision, &f.grant, &f.action, &f.binding, &f.target, &f.nonce, &f.verifier.KeyID, &f.verifier.Nonce, &f.verifier.Ciphertext, &f.expires, &f.event)
		if err != nil {
			return lookupError(err, ErrInvalidLink)
		}
		if f.mode != FlowLogin {
			a, ss, err := s.authorize(ctx, tx, raw, true)
			if err != nil {
				return err
			}
			if a.id != *f.id || a.revision != *f.revision || !bytes.Equal(ss.digest[:], f.session) {
				return ErrRecentAuth
			}
		}
		result, err := tx.Exec(ctx, `UPDATE account_oauth_flows SET claimed_at=$3 WHERE state_digest=$1 AND browser_digest=$2 AND claimed_at IS NULL AND expires_at>$3`, digest[:], bd[:], s.now().UTC())
		if err != nil {
			return ErrUnavailable
		}
		if result.RowsAffected() != 1 {
			return ErrInvalidLink
		}
		return nil
	})
	return f, err
}

func (s *Service) GoogleCallback(ctx context.Context, state, browser, code, raw string) (GoogleCallbackResult, error) {
	if s.google == nil || s.flowCipher == nil {
		return GoogleCallbackResult{}, ErrUnavailable
	}
	f, err := s.claimGoogle(ctx, state, browser, raw)
	if err != nil {
		return GoogleCallbackResult{}, err
	}
	if code == "" {
		return GoogleCallbackResult{}, ErrInvalidLink
	} // Denial also burns state.
	plain, err := s.flowCipher.Open(flowBinding(f.digest), f.verifier)
	if err != nil {
		return GoogleCallbackResult{}, ErrInvalidLink
	}
	identity, err := s.google.Exchange(ctx, code, string(plain), f.nonce)
	if err != nil {
		return GoogleCallbackResult{}, ErrInvalidLink
	}
	if identity.Subject == "" || len(identity.Subject) > 255 {
		return GoogleCallbackResult{}, ErrInvalidLink
	}
	identity.Email, _ = NormalizeEmail(identity.Email)
	var result GoogleCallbackResult
	var candidate *avatarImport
	var purged []*string
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		var a account
		var ss session
		var err error
		if f.mode == FlowLogin {
			a, err = s.googleLoginAccount(ctx, tx, identity, f, raw, &purged)
		} else {
			a, ss, err = s.authorize(ctx, tx, raw, true)
			if err == nil && (a.id != *f.id || a.revision != *f.revision || !bytes.Equal(ss.digest[:], f.session)) {
				err = ErrRecentAuth
			}
		}
		if err != nil {
			return err
		}
		// Account-first order, including flows whose external exchange is in flight.
		deleted, err := tx.Exec(ctx, `DELETE FROM account_oauth_flows WHERE state_digest=$1 AND claimed_at IS NOT NULL AND expires_at>$2`, f.digest[:], s.now().UTC())
		if err != nil {
			return ErrUnavailable
		}
		if deleted.RowsAffected() != 1 {
			return ErrInvalidLink
		}
		switch f.mode {
		case FlowLogin:
			result.Session, err = s.mintSession(ctx, tx, a, nil)
			result.Destination = accountDestination(a.state())
		case FlowLink:
			if a.google {
				return ErrIdentityUnavailable
			}
			if err = s.insertGoogleIdentity(ctx, tx, a.id, identity); err != nil {
				return err
			}
			if err = revoke(ctx, tx, &a); err != nil {
				return err
			}
			a.google = true
			if err = s.notify(ctx, tx, a, a.email, "Une connexion Google a été ajoutée à votre compte MesSeances."); err != nil {
				return err
			}
			result.Session, err = s.mintSession(ctx, tx, a, &ss)
			result.Destination = "/compte"
		case FlowReauth:
			if a.hash != nil || !a.google {
				return ErrRecentAuth
			}
			var subject string
			if err = tx.QueryRow(ctx, `SELECT subject FROM account_google_identities WHERE account_id=$1`, a.id).Scan(&subject); err != nil {
				return ErrUnavailable
			}
			if subject != identity.Subject {
				return ErrRecentAuth
			}
			result.Session, err = s.mintSession(ctx, tx, a, &ss)
			if err != nil {
				return err
			}
			d, _ := TokenDigest(result.Session.Cookie.Token)
			_, proof, e := s.newToken()
			if e != nil {
				return e
			}
			// Expiry remains bounded by the original fresh Google flow, not callback time.
			_, err = tx.Exec(ctx, `INSERT INTO account_tokens(token_digest,account_id,auth_revision,purpose,target_email,action,action_digest,session_digest,created_at,expires_at) VALUES($1,$2,$3,'google_reauth',$4,$5,$6,$7,$8,$9)`, proof[:], a.id, a.revision, f.target, f.action, f.binding, d[:], s.now().UTC(), f.expires)
			result.Destination = "/compte/confirmer-identite"
		}
		if err != nil {
			return ErrUnavailable
		}
		if f.mode != FlowReauth && a.avatarSource == "none" && a.avatarPath == nil {
			candidate = &avatarImport{id: a.id, revision: a.revision, avatarRevision: a.avatarRevision, subject: identity.Subject, picture: identity.Picture}
		}
		return nil
	})
	if err == nil {
		for _, path := range purged {
			s.removeAvatar(path)
		}
		s.importAvatar(ctx, candidate)
	}
	return result, err
}

func accountDestination(state State) string {
	switch state {
	case StatePendingEmail:
		return "/verification"
	case StatePendingUsername:
		return "/finaliser"
	default:
		return "/compte"
	}
}

func (s *Service) insertGoogleIdentity(ctx context.Context, tx pgx.Tx, id int64, identity GoogleIdentity) error {
	var email *string
	if identity.Email != "" {
		email = &identity.Email
	}
	_, err := tx.Exec(ctx, `INSERT INTO account_google_identities(account_id,issuer,subject,observed_email,observed_email_verified) VALUES($1,$2,$3,$4,$5)`, id, googleIssuer, identity.Subject, email, identity.EmailVerified)
	if uniqueError(err) {
		return ErrIdentityUnavailable
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) googleLoginAccount(ctx context.Context, tx pgx.Tx, identity GoogleIdentity, f googleFlow, previous string, purged *[]*string) (account, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT account_id FROM account_google_identities WHERE issuer=$1 AND subject=$2`, googleIssuer, identity.Subject).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return account{}, ErrUnavailable
	}
	existing := err == nil
	// Email is never a link key. An expired registration may be purged, but a
	// live collision always requires ordinary sign-in plus explicit linking.
	if !existing && identity.Email != "" {
		err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email=$1`, identity.Email).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return account{}, ErrUnavailable
		}
	}
	previousDigest, previousErr := TokenDigest(previous)
	if previousErr == nil {
		var previousID int64
		err := tx.QueryRow(ctx, `SELECT account_id FROM account_sessions WHERE token_digest=$1`, previousDigest[:]).Scan(&previousID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return account{}, ErrUnavailable
		}
		if _, err = tx.Exec(ctx, `SELECT id FROM accounts WHERE id=$1 OR id=$2 ORDER BY id FOR UPDATE`, id, previousID); err != nil {
			return account{}, ErrUnavailable
		}
	}
	if id != 0 {
		old, err := loadAccount(ctx, tx, id, true)
		if err != nil {
			return account{}, err
		}
		if old.expired(s.now().UTC()) {
			*purged = append(*purged, old.avatarPath)
			if err = purgeAccountExceptFlow(ctx, tx, id, f.digest[:]); err != nil {
				return account{}, err
			}
			existing = false
		} else if !existing {
			if identity.EmailVerified {
				return account{}, ErrGoogleEmailInUse
			}
			return account{}, ErrIdentityUnavailable
		}
	}
	if !existing {
		if identity.Email == "" {
			return account{}, ErrInvalidLink
		}
		var verified *time.Time
		var source *string
		now := s.now().UTC()
		if identity.EmailVerified && identity.EmailAuthoritative {
			verified = &now
			value := "google"
			source = &value
		}
		err = tx.QueryRow(ctx, `INSERT INTO accounts(email,email_verified_at,verification_source,created_at,pending_kind) VALUES($1,$2,$3,$4,'google') RETURNING id`, identity.Email, verified, source, now).Scan(&id)
		if uniqueError(err) {
			return account{}, ErrIdentityUnavailable
		}
		if err != nil {
			return account{}, ErrUnavailable
		}
		if err = s.insertGoogleIdentity(ctx, tx, id, identity); err != nil {
			return account{}, err
		}
	}
	a, err := loadAccount(ctx, tx, id, true)
	if err != nil {
		return a, err
	}
	if a.expired(s.now().UTC()) {
		return a, ErrInvalidLink
	}
	if existing {
		var subject string
		var event int64
		if err = tx.QueryRow(ctx, `SELECT g.subject,a.authority_event FROM accounts a JOIN account_google_identities g ON g.account_id=a.id WHERE a.id=$1`, id).Scan(&subject, &event); err != nil {
			return a, ErrInvalidLink
		}
		if subject != identity.Subject || event > f.event {
			return a, ErrInvalidLink
		}
		var email *string
		if identity.Email != "" {
			email = &identity.Email
		}
		if _, err = tx.Exec(ctx, `UPDATE account_google_identities SET observed_email=$2,observed_email_verified=$3 WHERE account_id=$1`, id, email, identity.EmailVerified); err != nil {
			return a, ErrUnavailable
		}
	}
	if !existing && a.verified == nil {
		if err = s.issueMailToken(ctx, tx, a, TokenVerification, ""); err != nil {
			return a, err
		}
	}
	if previousErr == nil {
		if _, err = tx.Exec(ctx, `DELETE FROM account_sessions WHERE token_digest=$1`, previousDigest[:]); err != nil {
			return a, ErrUnavailable
		}
	}
	return a, nil
}
