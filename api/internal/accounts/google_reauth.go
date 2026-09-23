package accounts

import (
	"bytes"
	"context"
	"html"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/accountmail"
)

type ReauthContinuation struct {
	Action    Action    `json:"action"`
	Target    *string   `json:"target"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) googleProof(ctx context.Context, tx pgx.Tx, a account, ss session) (actionToken, error) {
	if a.hash != nil || !a.google {
		return actionToken{}, ErrRecentAuth
	}
	var t actionToken
	var digest []byte
	err := tx.QueryRow(ctx, `SELECT token_digest,action,target_email,action_digest,expires_at FROM account_tokens WHERE account_id=$1 AND session_digest=$2 AND auth_revision=$3 AND purpose='google_reauth' AND consumed_at IS NULL AND expires_at>$4 FOR UPDATE`, a.id, ss.digest[:], a.revision, s.now().UTC()).Scan(&digest, &t.action, &t.target, &t.binding, &t.expires)
	if err != nil {
		return t, lookupError(err, ErrRecentAuth)
	}
	copy(t.digest[:], digest)
	return t, nil
}

func (s *Service) ReauthContinuation(ctx context.Context, raw string) (ReauthContinuation, error) {
	var result ReauthContinuation
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		proof, err := s.googleProof(ctx, tx, a, ss)
		if err != nil {
			return err
		}
		result = ReauthContinuation{Action: *proof.action, Target: proof.target, ExpiresAt: proof.expires}
		return nil
	})
	return result, err
}

func (s *Service) RequestReauthEmail(ctx context.Context, raw string) error {
	if err := s.sessionQuota(ctx, raw, "step_up", true, true); err != nil {
		return err
	}
	if err := s.mailAdmission(ctx); err != nil {
		return err
	}
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		proof, err := s.googleProof(ctx, tx, a, ss)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM account_tokens WHERE session_digest=$1 AND purpose='email_step_up'`, ss.digest[:]); err != nil {
			return ErrUnavailable
		}
		token, digest, err := s.newToken()
		if err != nil {
			return err
		}
		now := s.now().UTC()
		_, err = tx.Exec(ctx, `INSERT INTO account_tokens(token_digest,account_id,auth_revision,purpose,target_email,action,action_digest,session_digest,created_at,expires_at) VALUES($1,$2,$3,'email_step_up',$4,$5,$6,$7,$8,$9)`, digest[:], a.id, a.revision, proof.target, proof.action, proof.binding, ss.digest[:], now, proof.expires)
		if err != nil {
			return ErrUnavailable
		}
		link := s.origin + "/compte/confirmer-identite#token=" + token
		message := accountmail.Message{Recipient: a.email, Subject: "Confirmez votre identité MesSeances", Text: "Pour confirmer votre identité, ouvrez ce lien puis validez le formulaire :\n" + link, HTML: `<p><a href="` + html.EscapeString(link) + `">Confirmer mon identité</a></p>`}
		return s.enqueue(ctx, tx, a, string(TokenEmailStepUp), digest[:], digest[:], message, now, proof.expires)
	})
}

func (s *Service) ConfirmReauthEmail(ctx context.Context, raw, token string, action Action, target string) (GrantResult, error) {
	target, binding, err := actionBinding(action, target)
	if err != nil {
		return GrantResult{}, err
	}
	if err = s.sessionQuota(ctx, raw, "step_up", false, true); err != nil {
		return GrantResult{}, err
	}
	var result GrantResult
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		proof, err := s.googleProof(ctx, tx, a, ss)
		if err != nil {
			return err
		}
		challenge, err := s.readToken(ctx, tx, a, token, TokenEmailStepUp)
		if err != nil {
			return err
		}
		if *proof.action != action || challenge.action == nil || *challenge.action != action || !bytes.Equal(proof.binding, binding[:]) || !bytes.Equal(challenge.binding, binding[:]) || !bytes.Equal(challenge.session, ss.digest[:]) {
			return ErrRecentAuth
		}
		// Rotation cascades both single-use proofs and any sibling grants/flows.
		rotated, err := s.mintSession(ctx, tx, a, &ss)
		if err != nil {
			return err
		}
		result.Cookie = rotated.Cookie
		digest, _ := TokenDigest(result.Cookie.Token)
		result.Grant, err = s.issueGrant(ctx, tx, a, digest, action, target, binding)
		return err
	})
	return result, err
}

func (s *Service) UnlinkGoogle(ctx context.Context, raw, grant string) (Cookie, error) {
	if err := s.mailAdmission(ctx); err != nil {
		return Cookie{}, err
	}
	var cookie Cookie
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if err = s.consumeGrant(ctx, tx, a, ss, grant, ActionGoogleUnlink, ""); err != nil {
			return err
		}
		if a.hash == nil || a.tentative {
			return ErrLastMethod
		}
		if !a.google {
			return ErrIdentityUnavailable
		}
		if _, err = tx.Exec(ctx, `DELETE FROM account_google_identities WHERE account_id=$1`, a.id); err != nil {
			return ErrUnavailable
		}
		if err = revoke(ctx, tx, &a); err != nil {
			return err
		}
		a.google = false
		if err = s.notify(ctx, tx, a, a.email, "La connexion Google a été retirée de votre compte MesSeances."); err != nil {
			return err
		}
		rotated, err := s.mintSession(ctx, tx, a, &ss)
		cookie = rotated.Cookie
		return err
	})
	return cookie, err
}
