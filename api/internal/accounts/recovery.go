package accounts

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type actionToken struct {
	digest           Digest
	revision         int64
	target           *string
	action           *Action
	binding, session []byte
	expires          time.Time
	consumed         *time.Time
}

func (s *Service) readToken(ctx context.Context, tx pgx.Tx, a account, raw string, purpose TokenPurpose) (actionToken, error) {
	digest, err := TokenDigest(raw)
	if err != nil {
		return actionToken{}, ErrInvalidLink
	}
	t := actionToken{digest: digest}
	err = tx.QueryRow(ctx, `SELECT auth_revision,target_email,action,action_digest,session_digest,expires_at,consumed_at FROM account_tokens WHERE token_digest=$1 AND account_id=$2 AND purpose=$3 FOR UPDATE`, digest[:], a.id, purpose).Scan(&t.revision, &t.target, &t.action, &t.binding, &t.session, &t.expires, &t.consumed)
	if err != nil {
		return t, lookupError(err, ErrInvalidLink)
	}
	now := s.now().UTC()
	if t.revision != a.revision || t.consumed != nil || !now.Before(t.expires) || a.expired(now) {
		return t, ErrInvalidLink
	}
	return t, nil
}

func (s *Service) tokenAccount(ctx context.Context, tx pgx.Tx, raw string, purpose TokenPurpose) (account, actionToken, error) {
	digest, err := TokenDigest(raw)
	if err != nil {
		return account{}, actionToken{}, ErrInvalidLink
	}
	var id int64
	if err = tx.QueryRow(ctx, `SELECT account_id FROM account_tokens WHERE token_digest=$1 AND purpose=$2`, digest[:], purpose).Scan(&id); err != nil {
		return account{}, actionToken{}, lookupError(err, ErrInvalidLink)
	}
	a, err := loadAccount(ctx, tx, id, true)
	if errors.Is(err, ErrUnauthorized) {
		err = ErrInvalidLink
	}
	if err != nil {
		return a, actionToken{}, err
	}
	t, err := s.readToken(ctx, tx, a, raw, purpose)
	return a, t, err
}

func (s *Service) ConfirmReset(ctx context.Context, token, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if err := s.quota(ctx, "token_confirm", token, false); err != nil {
		return err
	}
	if err := s.mailAdmission(ctx); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.tokenAccount(ctx, tx, token, TokenPasswordReset)
		if err != nil {
			return err
		}
		if a.verified == nil || a.hash == nil || a.tentative {
			return ErrInvalidLink
		}
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE account_passwords SET encoded_hash=$2,tentative=false WHERE account_id=$1`, a.id, hash); err != nil {
			return ErrUnavailable
		}
		return s.notify(ctx, tx, a, a.email, "Votre mot de passe MesSeances a été réinitialisé.")
	})
}

type GrantResult struct {
	Grant  string
	Cookie Cookie
}

func (s *Service) ReauthPassword(ctx context.Context, raw, password string, action Action, target string) (GrantResult, error) {
	target, binding, err := actionBinding(action, target)
	if err != nil || len(password) > 512 {
		return GrantResult{}, ErrInvalidInput
	}
	if err = s.sessionQuota(ctx, raw, "step_up", false, true); err != nil {
		return GrantResult{}, err
	}
	var snapshot account
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error { var e error; snapshot, _, e = s.authorize(ctx, tx, raw, true); return e })
	if err != nil {
		return GrantResult{}, err
	}
	if snapshot.hash == nil || snapshot.tentative {
		if err = s.hasher.Dummy(ctx, password); err != nil {
			return GrantResult{}, err
		}
		return GrantResult{}, ErrRecentAuth
	}
	match, rehash, err := s.hasher.Verify(ctx, password, *snapshot.hash)
	if err != nil {
		return GrantResult{}, err
	}
	if !match {
		return GrantResult{}, ErrRecentAuth
	}
	var updated string
	if rehash {
		updated, err = s.hasher.Hash(ctx, password)
		if err != nil {
			return GrantResult{}, err
		}
	}
	var result GrantResult
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.revision != snapshot.revision || a.hash == nil || *a.hash != *snapshot.hash || action == ActionPasswordAdd {
			return ErrRecentAuth
		}
		if updated != "" {
			if _, err = tx.Exec(ctx, `UPDATE account_passwords SET encoded_hash=$2 WHERE account_id=$1`, a.id, updated); err != nil {
				return ErrUnavailable
			}
		}
		rotated, err := s.mintSession(ctx, tx, a, &ss)
		if err != nil {
			return err
		}
		result.Cookie = rotated.Cookie
		digest, err := TokenDigest(rotated.Cookie.Token)
		if err != nil {
			return err
		}
		result.Grant, err = s.issueGrant(ctx, tx, a, digest, action, target, binding)
		return err
	})
	return result, err
}

func (s *Service) issueGrant(ctx context.Context, tx pgx.Tx, a account, session Digest, action Action, target string, binding Digest) (string, error) {
	raw, digest, err := s.newToken()
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	var targetArg *string
	if target != "" {
		targetArg = &target
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_tokens(token_digest,account_id,auth_revision,purpose,target_email,action,action_digest,session_digest,created_at,expires_at) VALUES($1,$2,$3,'reauth_grant',$4,$5,$6,$7,$8,$9)`, digest[:], a.id, a.revision, targetArg, action, binding[:], session[:], now, now.Add(GrantLifetime))
	if err != nil {
		return "", ErrUnavailable
	}
	return raw, nil
}

func (s *Service) consumeGrant(ctx context.Context, tx pgx.Tx, a account, ss session, raw string, action Action, target string) error {
	_, binding, err := actionBinding(action, target)
	if err != nil {
		return err
	}
	t, err := s.readToken(ctx, tx, a, raw, TokenReauthGrant)
	if errors.Is(err, ErrInvalidLink) {
		return ErrRecentAuth
	}
	if err != nil {
		return err
	}
	if t.action == nil || *t.action != action || !bytes.Equal(t.binding, binding[:]) || !bytes.Equal(t.session, ss.digest[:]) {
		return ErrRecentAuth
	}
	if _, err = tx.Exec(ctx, `UPDATE account_tokens SET consumed_at=$2 WHERE token_digest=$1`, t.digest[:], s.now().UTC()); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, raw, password, grant string) (Cookie, error) {
	if err := ValidatePassword(password); err != nil {
		return Cookie{}, err
	}
	if err := s.mailAdmission(ctx); err != nil {
		return Cookie{}, err
	}
	// Validate before hashing to avoid spending work on unauthenticated callers;
	// authority is checked again after hashing under the account lock.
	var snapshot account
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error { var e error; snapshot, _, e = s.authorize(ctx, tx, raw, true); return e })
	if err != nil {
		return Cookie{}, err
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return Cookie{}, err
	}
	var cookie Cookie
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.revision != snapshot.revision {
			return ErrRecentAuth
		}
		action := ActionPasswordChange
		if a.hash == nil {
			action = ActionPasswordAdd
		}
		if err = s.consumeGrant(ctx, tx, a, ss, grant, action, ""); err != nil {
			return err
		}
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO account_passwords(account_id,encoded_hash,tentative) VALUES($1,$2,false) ON CONFLICT(account_id) DO UPDATE SET encoded_hash=EXCLUDED.encoded_hash,tentative=false`, a.id, hash); err != nil {
			return ErrUnavailable
		}
		a.hash = &hash
		a.tentative = false
		if err = s.notify(ctx, tx, a, a.email, "Le mot de passe de votre compte MesSeances a été modifié."); err != nil {
			return err
		}
		rotated, err := s.mintSession(ctx, tx, a, &ss)
		cookie = rotated.Cookie
		return err
	})
	return cookie, err
}

func (s *Service) RequestEmailChange(ctx context.Context, raw, email, grant string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err = s.sessionQuota(ctx, raw, "email_change", true, true); err != nil {
		return err
	}
	if err = s.mailAdmission(ctx); err != nil {
		return err
	}
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.email == email {
			return ErrInvalidInput
		}
		if err = s.consumeGrant(ctx, tx, a, ss, grant, ActionEmailChange, email); err != nil {
			return err
		}
		if err = s.issueMailToken(ctx, tx, a, TokenEmailChange, email); err != nil {
			return err
		}
		return s.notify(ctx, tx, a, a.email, "Un changement d’adresse email a été demandé pour votre compte MesSeances.")
	})
}

func (s *Service) ConfirmEmailChange(ctx context.Context, raw, token, grant string) error {
	if err := s.mailAdmission(ctx); err != nil {
		return err
	}
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		t, err := s.readToken(ctx, tx, a, token, TokenEmailChange)
		if err != nil {
			return err
		}
		if t.target == nil {
			return ErrInvalidLink
		}
		if err = s.consumeGrant(ctx, tx, a, ss, grant, ActionEmailChange, *t.target); err != nil {
			return err
		}
		old := a.email
		if _, err = tx.Exec(ctx, `UPDATE accounts SET email=$2,email_verified_at=$3,verification_source='email' WHERE id=$1`, a.id, *t.target, s.now().UTC()); err != nil {
			if uniqueError(err) {
				return ErrEmailUnavailable
			}
			return ErrUnavailable
		}
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		a.email = *t.target
		if err = s.notify(ctx, tx, a, old, "L’adresse email de votre compte MesSeances a été modifiée."); err != nil {
			return err
		}
		return s.notify(ctx, tx, a, a.email, "Cette adresse email est maintenant associée à votre compte MesSeances.")
	})
}

func (s *Service) CancelEmailChange(ctx context.Context, raw string) error {
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		return supersede(ctx, tx, a.id, TokenEmailChange)
	})
}
