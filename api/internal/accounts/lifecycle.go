package accounts

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Service) Register(ctx context.Context, email, password string) (Cookie, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return Cookie{}, err
	}
	if err = ValidatePassword(password); err != nil {
		return Cookie{}, err
	}
	if err = s.quota(ctx, "verification_send", email, true); err != nil {
		return Cookie{}, err
	}
	if err = s.mailAdmission(ctx); err != nil {
		return Cookie{}, err
	}
	// Every syntactically valid registration hashes, regardless of existence.
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return Cookie{}, err
	}
	// Always return a fresh, indistinguishable proof cookie, including when the
	// address already belongs to a verified or Google account. It is not a session.
	raw, digest, err := s.newToken()
	if err != nil {
		return Cookie{}, err
	}
	cookie := Cookie{Token: raw, ExpiresAt: s.now().UTC().Add(PendingLifetime)}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		// Unique email arbitrates initial registrations. Restarting a pending
		// attempt never extends the account's original seven-day deadline.
		_, err := tx.Exec(ctx, `INSERT INTO accounts(email,created_at,pending_kind) VALUES($1,$2,'email') ON CONFLICT(email) DO NOTHING`, email, now)
		if err != nil {
			return ErrUnavailable
		}
		var id int64
		if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email=$1`, email).Scan(&id); err != nil {
			return ErrUnavailable
		}
		a, err := loadAccount(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a.expired(s.now().UTC()) {
			if err = purgeAccount(ctx, tx, a.id); err != nil {
				return ErrUnavailable
			}
			if err = tx.QueryRow(ctx, `INSERT INTO accounts(email,created_at,pending_kind) VALUES($1,$2,'email') RETURNING id`, email, s.now().UTC()).Scan(&id); err != nil {
				return ErrUnavailable
			}
			a, err = loadAccount(ctx, tx, id, true)
			if err != nil {
				return err
			}
		}
		if a.verified != nil || a.pending == nil || *a.pending != "email" {
			return nil
		}
		// A victim retry must bind their own chosen password, not adopt a prior
		// attacker credential. Only the latest attempt survives, including sessions
		// and links. Neither email possession nor tentative login can mint this proof.
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO account_passwords(account_id,encoded_hash,tentative,registration_digest) VALUES($1,$2,true,$3) ON CONFLICT(account_id) DO UPDATE SET encoded_hash=EXCLUDED.encoded_hash,tentative=true,registration_digest=EXCLUDED.registration_digest`, a.id, hash, digest[:]); err != nil {
			return ErrUnavailable
		}
		return s.issueMailToken(ctx, tx, a, TokenVerification, "")
	})
	return cookie, publicMailResult(err)
}

func (s *Service) RequestVerification(ctx context.Context, email, browser string) error {
	return s.requestMail(ctx, email, TokenVerification, browser)
}
func (s *Service) RequestReset(ctx context.Context, email string) error {
	return s.requestMail(ctx, email, TokenPasswordReset, "")
}

func (s *Service) requestMail(ctx context.Context, email string, purpose TokenPurpose, browser string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	quota := "verification_send"
	if purpose == TokenPasswordReset {
		quota = "reset_send"
	}
	if err = s.quota(ctx, quota, email, true); err != nil {
		return err
	}
	if err = s.mailAdmission(ctx); err != nil {
		return err
	}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email=$1`, email).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		a, err := loadAccount(ctx, tx, id, true)
		if errors.Is(err, ErrUnauthorized) {
			return nil
		}
		if err != nil {
			return err
		}
		if a.expired(s.now().UTC()) {
			return nil
		}
		if purpose == TokenVerification && (a.verified != nil || a.pending == nil) {
			return nil
		}
		// Missing/wrong proof must not send an attacker-bound link or supersede a
		// legitimate attempt. Google still requires its subject session at confirmation.
		if purpose == TokenVerification && *a.pending == "email" && !a.registrationMatches(browser) {
			return nil
		}
		if purpose == TokenPasswordReset && (a.verified == nil || a.hash == nil || a.tentative) {
			return nil
		}
		return s.issueMailToken(ctx, tx, a, purpose, "")
	})
	return publicMailResult(err)
}

// credentialSnapshot performs no expensive work while holding database locks.
func (s *Service) credentialSnapshot(ctx context.Context, email string) (account, error) {
	var a account
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email=$1`, email).Scan(&id); err != nil {
			return lookupError(err, ErrCredentials)
		}
		var err error
		a, err = loadAccount(ctx, tx, id, false)
		if errors.Is(err, ErrUnauthorized) {
			return ErrCredentials
		}
		return err
	})
	return a, err
}

func (s *Service) Login(ctx context.Context, email, password, previous string) (SessionResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil || len(password) > 512 {
		return SessionResult{}, ErrInvalidInput
	}
	if err = s.quota(ctx, "login", email, false); err != nil {
		return SessionResult{}, err
	}
	snapshot, err := s.credentialSnapshot(ctx, email)
	if err != nil && !errors.Is(err, ErrCredentials) {
		return SessionResult{}, err
	}
	if err != nil || snapshot.hash == nil || snapshot.expired(s.now().UTC()) || (snapshot.verified != nil && snapshot.tentative) {
		if err = s.hasher.Dummy(ctx, password); err != nil {
			return SessionResult{}, err
		}
		return SessionResult{}, ErrCredentials
	}
	match, rehash, err := s.hasher.Verify(ctx, password, *snapshot.hash)
	if err != nil {
		return SessionResult{}, err
	}
	if !match {
		return SessionResult{}, ErrCredentials
	}
	var updated string
	if rehash {
		updated, err = s.hasher.Hash(ctx, password)
		if err != nil {
			return SessionResult{}, err
		}
	}
	var result SessionResult
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		// Switching accounts also invalidates the browser's predecessor. Acquire
		// both account locks in ID order before touching either session.
		previousDigest, previousErr := TokenDigest(previous)
		if previousErr == nil {
			var previousID int64
			err := tx.QueryRow(ctx, `SELECT account_id FROM account_sessions WHERE token_digest=$1`, previousDigest[:]).Scan(&previousID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ErrUnavailable
			}
			if _, err = tx.Exec(ctx, `SELECT id FROM accounts WHERE id=$1 OR id=$2 ORDER BY id FOR UPDATE`, snapshot.id, previousID); err != nil {
				return ErrUnavailable
			}
		}
		a, err := loadAccount(ctx, tx, snapshot.id, true)
		if errors.Is(err, ErrUnauthorized) {
			return ErrCredentials
		}
		if err != nil {
			return err
		}
		if a.revision != snapshot.revision || a.hash == nil || *a.hash != *snapshot.hash || a.email != email || a.expired(s.now().UTC()) {
			return ErrCredentials
		}
		if updated != "" {
			if _, err = tx.Exec(ctx, `UPDATE account_passwords SET encoded_hash=$2 WHERE account_id=$1`, a.id, updated); err != nil {
				return ErrUnavailable
			}
		}
		if previousErr == nil {
			if _, err = tx.Exec(ctx, `DELETE FROM account_sessions WHERE token_digest=$1`, previousDigest[:]); err != nil {
				return ErrUnavailable
			}
		}
		result, err = s.mintSession(ctx, tx, a, nil)
		return err
	})
	return result, err
}

func (a account) registrationMatches(browser string) bool {
	digest, err := TokenDigest(browser)
	return err == nil && subtle.ConstantTimeCompare(a.registration, digest[:]) == 1
}

// ConfirmVerification chooses the method from locked account state, never client
// input. The email attempt proof survives ordinary session rotation independently.
func (s *Service) ConfirmVerification(ctx context.Context, token, browser, rawSession string) (SessionResult, error) {
	if err := s.quota(ctx, "token_confirm", token, false); err != nil {
		return SessionResult{}, err
	}
	var result SessionResult
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.tokenAccount(ctx, tx, token, TokenVerification)
		if err != nil {
			return err
		}
		if a.verified != nil || a.pending == nil {
			return ErrInvalidLink
		}
		var predecessor *session
		switch *a.pending {
		case "email":
			if a.hash == nil || !a.tentative || a.google {
				return ErrInvalidLink
			}
			if !a.registrationMatches(browser) {
				return ErrVerificationBrowser
			}
		case "google":
			ss, err := s.verificationGoogleSession(ctx, tx, a, rawSession)
			if err != nil {
				return err
			}
			predecessor = &ss
		default:
			return ErrInvalidLink
		}
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		now := s.now().UTC()
		if _, err = tx.Exec(ctx, `UPDATE accounts SET email_verified_at=$2,verification_source='email' WHERE id=$1`, a.id, now); err != nil {
			return ErrUnavailable
		}
		if _, err = tx.Exec(ctx, `UPDATE account_passwords SET tentative=false WHERE account_id=$1`, a.id); err != nil {
			return ErrUnavailable
		}
		a.verified = &now
		a.tentative = false
		result, err = s.mintSession(ctx, tx, a, predecessor)
		return err
	})
	return result, err
}

func (s *Service) verificationGoogleSession(ctx context.Context, tx pgx.Tx, a account, raw string) (session, error) {
	if !a.google || a.hash != nil {
		return session{}, ErrInvalidLink
	}
	digest, err := TokenDigest(raw)
	if err != nil {
		return session{}, ErrUnauthorized
	}
	// Do not acquire a second account lock from an unrelated session after locking
	// the token's account. Account-first order holds even for cross-account attacks.
	var id int64
	if err = tx.QueryRow(ctx, `SELECT account_id FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&id); err != nil {
		return session{}, lookupError(err, ErrUnauthorized)
	}
	if id != a.id {
		return session{}, ErrUnauthorized
	}
	_, ss, err := s.authorize(ctx, tx, raw, false)
	return ss, err
}

func (s *Service) Username(ctx context.Context, raw, name string) (SessionResult, error) {
	name, err := NormalizeUsername(name)
	if err != nil {
		return SessionResult{}, err
	}
	if err = s.sessionQuota(ctx, raw, "username", false, false); err != nil {
		return SessionResult{}, err
	}
	var result SessionResult
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, false)
		if err != nil {
			return err
		}
		if a.state() != StatePendingUsername {
			return ErrPending
		}
		if _, err = tx.Exec(ctx, `INSERT INTO account_username_claims(username,account_id) VALUES($1,$2)`, name, a.id); err != nil {
			if uniqueError(err) {
				return ErrUsernameUnavailable
			}
			return ErrUnavailable
		}
		if _, err = tx.Exec(ctx, `UPDATE accounts SET pending_kind=NULL WHERE id=$1`, a.id); err != nil {
			return ErrUnavailable
		}
		// Restricted sibling sessions cannot acquire completed authority later.
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		a.username = &name
		a.pending = nil
		result, err = s.mintSession(ctx, tx, a, &ss)
		return err
	})
	return result, err
}

func (s *Service) Logout(ctx context.Context, raw string, all bool) error {
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, false)
		if errors.Is(err, ErrUnauthorized) && !all {
			return nil
		}
		if err != nil {
			return err
		}
		if all {
			return revoke(ctx, tx, &a)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM account_sessions WHERE token_digest=$1`, ss.digest[:]); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}
