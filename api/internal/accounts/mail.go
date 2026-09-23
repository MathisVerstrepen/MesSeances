package accounts

import (
	"context"
	"errors"
	"html"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
)

func (s *Service) mailAdmission(ctx context.Context) error {
	if s.mail == nil {
		return ErrUnavailable
	}
	// Uniform admission before account lookup, including nonexistent addresses.
	return s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox WHERE state='pending'`).Scan(&count); err != nil || count >= 10000 {
			return ErrUnavailable
		}
		return nil
	})
}

func supersede(ctx context.Context, tx pgx.Tx, id int64, purpose TokenPurpose) error {
	// Cascading deletion erases the encrypted old message as well as its authority.
	if _, err := tx.Exec(ctx, `DELETE FROM account_tokens WHERE account_id=$1 AND purpose=$2`, id, purpose); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) issueMailToken(ctx context.Context, tx pgx.Tx, a account, purpose TokenPurpose, target string) error {
	if s.mail == nil {
		return ErrUnavailable
	}
	if err := supersede(ctx, tx, a.id, purpose); err != nil {
		return err
	}
	raw, digest, err := s.newToken()
	if err != nil {
		return err
	}
	now := s.now().UTC()
	expires := now.Add(RecoveryLifetime)
	recipient := a.email
	var action *Action
	var binding []byte
	var targetArg *string
	path := "/reinitialiser-mot-de-passe"
	if purpose == TokenVerification {
		path = "/verification"
		expires = now.Add(VerificationLifetime)
		if deadline := a.created.Add(PendingLifetime); deadline.Before(expires) {
			expires = deadline
		}
	}
	if purpose == TokenEmailChange {
		path = "/compte/confirmer-email"
		recipient = target
		targetArg = &target
		act := ActionEmailChange
		action = &act
		_, d, e := actionBinding(act, target)
		if e != nil {
			return e
		}
		binding = d[:]
	}
	if !now.Before(expires) {
		return ErrInvalidLink
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_tokens(token_digest,account_id,auth_revision,purpose,target_email,action,action_digest,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, digest[:], a.id, a.revision, purpose, targetArg, action, binding, now, expires)
	if err != nil {
		return ErrUnavailable
	}
	link := s.origin + path + "#token=" + raw
	message := accountmail.Message{Recipient: recipient, Subject: "Confirmez votre demande MesSeances", Text: "Pour confirmer votre demande, ouvrez ce lien puis validez le formulaire :\n" + link, HTML: `<p>Pour confirmer votre demande, ouvrez ce lien puis validez le formulaire :</p><p><a href="` + html.EscapeString(link) + `">Confirmer</a></p>`}
	return s.enqueue(ctx, tx, a, string(purpose), digest[:], digest[:], message, now, expires)
}

func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, a account, purpose string, event, token []byte, message accountmail.Message, now, expires time.Time) error {
	if s.mail == nil {
		return ErrUnavailable
	}
	address, err := AddressKey(s.hmacKey, "suppression", message.Recipient)
	if err != nil {
		return ErrUnavailable
	}
	err = s.mail.Enqueue(ctx, tx, accountmail.Job{EventDigest: event, TokenDigest: token, AddressDigest: address[:], AccountID: a.id, Revision: a.revision, Purpose: purpose, Message: message, CreatedAt: now, ExpiresAt: expires})
	if errors.Is(err, accountmail.ErrQueueFull) {
		return err
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// Public mail admission already rejects a full queue before identity lookup.
// If another transaction fills the last slot afterward, roll back this request
// but retain generic acceptance, rather than revealing eligible addresses through
// an existence-dependent 503. Acceptance never claims mailbox delivery.
func publicMailResult(err error) error {
	if errors.Is(err, accountmail.ErrQueueFull) {
		return nil
	}
	return err
}

func (s *Service) notify(ctx context.Context, tx pgx.Tx, a account, recipient, text string) error {
	_, digest, err := s.newToken()
	if err != nil {
		return err
	}
	now := s.now().UTC()
	return s.enqueue(ctx, tx, a, "security_notification", digest[:], nil, accountmail.Message{Recipient: recipient, Subject: "Sécurité de votre compte MesSeances", Text: text, HTML: "<p>" + html.EscapeString(text) + "</p>"}, now, now.Add(24*time.Hour))
}
