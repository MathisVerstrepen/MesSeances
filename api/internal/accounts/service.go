package accounts

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"messeances/api/internal/accountmail"
)

var (
	ErrUnauthorized        = errors.New("account authentication required")
	ErrCredentials         = errors.New("invalid credentials")
	ErrPending             = errors.New("account onboarding required")
	ErrInvalidLink         = errors.New("invalid account link")
	ErrVerificationBrowser = errors.New("original registration browser required")
	ErrRecentAuth          = errors.New("recent account authentication required")
	ErrEmailUnavailable    = errors.New("email unavailable")
	ErrIdentityUnavailable = errors.New("google identity unavailable")
	ErrLastMethod          = errors.New("last login method")
)

type RateLimitError struct{ RetryAfter int }

func (e *RateLimitError) Error() string { return "account rate limited" }

type ServiceOptions struct {
	Now            func() time.Time
	Random         io.Reader
	Hasher         PasswordHasher
	Mail           accountmail.Enqueuer
	Origin         string
	AddressHMACKey []byte
	Google         GoogleProvider
	FlowCipher     accountmail.PayloadCipher
}

type Service struct {
	store      *PostgresStore
	now        func() time.Time
	random     io.Reader
	randomMu   sync.Mutex
	hasher     PasswordHasher
	mail       accountmail.Enqueuer
	origin     string
	hmacKey    []byte
	google     GoogleProvider
	flowCipher accountmail.PayloadCipher
}

func NewService(store *PostgresStore, options ServiceOptions) (*Service, error) {
	if store == nil || options.Hasher == nil || len(options.AddressHMACKey) != 32 || options.Origin == "" {
		return nil, ErrUnavailable
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{store: store, now: options.Now, random: options.Random, hasher: options.Hasher, mail: options.Mail, origin: options.Origin, hmacKey: append([]byte(nil), options.AddressHMACKey...), google: options.Google, flowCipher: options.FlowCipher}, nil
}

func (s *Service) newToken() (string, Digest, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	return NewToken(s.random)
}

// Cookie is transport-only authority, never part of a public JSON DTO.
type Cookie struct {
	Token     string
	ExpiresAt time.Time
}
type SessionResult struct {
	View   SessionView
	Cookie Cookie
}

type account struct {
	id, revision int64
	email        string
	verified     *time.Time
	created      time.Time
	pending      *string
	username     *string
	hash         *string
	tentative    bool
	registration []byte
	googleEmail  *string
	google       bool
}

func (a account) state() State {
	if a.verified == nil {
		return StatePendingEmail
	}
	if a.username == nil {
		return StatePendingUsername
	}
	return StateComplete
}
func (a account) expired(now time.Time) bool {
	return a.state() != StateComplete && !now.Before(a.created.Add(PendingLifetime))
}
func (a account) view() SessionView {
	return SessionView{Enabled: true, State: a.state(), Account: &AccountView{Email: a.email, Username: a.username, HasPassword: a.hash != nil, GoogleLinked: a.google}}
}

func loadAccount(ctx context.Context, tx pgx.Tx, id int64, lock bool) (account, error) {
	var a account
	query := `SELECT id,email,email_verified_at,created_at,pending_kind,auth_revision FROM accounts WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, id).Scan(&a.id, &a.email, &a.verified, &a.created, &a.pending, &a.revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrUnauthorized
	}
	if err != nil {
		return a, ErrUnavailable
	}
	err = tx.QueryRow(ctx, `SELECT u.username,p.encoded_hash,COALESCE(p.tentative,false),g.observed_email,g.account_id IS NOT NULL,p.registration_digest FROM accounts a LEFT JOIN account_username_claims u ON u.account_id=a.id LEFT JOIN account_passwords p ON p.account_id=a.id LEFT JOIN account_google_identities g ON g.account_id=a.id WHERE a.id=$1`, id).Scan(&a.username, &a.hash, &a.tentative, &a.googleEmail, &a.google, &a.registration)
	if err != nil {
		return a, ErrUnavailable
	}
	return a, nil
}

type session struct {
	digest           Digest
	created, expires time.Time
}

func (s *Service) authorize(ctx context.Context, tx pgx.Tx, raw string, complete bool) (account, session, error) {
	digest, err := TokenDigest(raw)
	if err != nil {
		return account{}, session{}, ErrUnauthorized
	}
	var id int64
	if err = tx.QueryRow(ctx, `SELECT account_id FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&id); err != nil {
		return account{}, session{}, lookupError(err, ErrUnauthorized)
	}
	a, err := loadAccount(ctx, tx, id, true)
	if err != nil {
		return a, session{}, err
	}
	var revision int64
	var last time.Time
	var scope State
	ss := session{digest: digest}
	err = tx.QueryRow(ctx, `SELECT auth_revision,created_at,expires_at,last_seen_at,scope FROM account_sessions WHERE token_digest=$1 AND account_id=$2 FOR UPDATE`, digest[:], id).Scan(&revision, &ss.created, &ss.expires, &last, &scope)
	if err != nil {
		return a, ss, lookupError(err, ErrUnauthorized)
	}
	now := s.now().UTC()
	if revision != a.revision || !now.Before(ss.expires) || !now.Before(last.Add(SessionIdleLifetime)) || now.Before(last) || a.expired(now) || scope != a.state() {
		return a, ss, ErrUnauthorized
	}
	if complete && a.state() != StateComplete {
		return a, ss, ErrPending
	}
	_, err = tx.Exec(ctx, `UPDATE account_sessions SET last_seen_at=$2 WHERE token_digest=$1`, digest[:], now)
	if err != nil {
		return a, ss, ErrUnavailable
	}
	return a, ss, nil
}

func lookupError(err, missing error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return missing
	}
	return ErrUnavailable
}
func uniqueError(err error) bool {
	var p *pgconn.PgError
	return errors.As(err, &p) && p.Code == "23505"
}

func (s *Service) mintSession(ctx context.Context, tx pgx.Tx, a account, predecessor *session) (SessionResult, error) {
	now := s.now().UTC()
	created := now
	expiry := now.Add(SessionLifetime)
	if predecessor != nil {
		created = predecessor.created
		expiry = predecessor.expires
	}
	if !now.Before(expiry) || a.expired(now) {
		return SessionResult{}, ErrUnauthorized
	}
	raw, digest, err := s.newToken()
	if err != nil {
		return SessionResult{}, err
	}
	if predecessor != nil {
		if _, err = tx.Exec(ctx, `DELETE FROM account_sessions WHERE token_digest=$1 AND account_id=$2`, predecessor.digest[:], a.id); err != nil {
			return SessionResult{}, ErrUnavailable
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_sessions(token_digest,account_id,auth_revision,created_at,expires_at,last_seen_at,scope) VALUES($1,$2,$3,$4,$5,$6,$7)`, digest[:], a.id, a.revision, created, expiry, now, a.state())
	if err != nil {
		return SessionResult{}, ErrUnavailable
	}
	return SessionResult{View: a.view(), Cookie: Cookie{Token: raw, ExpiresAt: expiry}}, nil
}

func (s *Service) Session(ctx context.Context, raw string) (SessionView, error) {
	view := SessionView{Enabled: true, State: StateAnonymous}
	// Still visit the database for anonymous requests: an outage must not masquerade as logout.
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if raw == "" {
			return nil
		}
		a, _, err := s.authorize(ctx, tx, raw, false)
		if errors.Is(err, ErrUnauthorized) {
			return nil
		}
		if err != nil {
			return err
		}
		view = a.view()
		return nil
	})
	return view, err
}

func (s *Service) Details(ctx context.Context, raw string) (AccountDetails, error) {
	var result AccountDetails
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		result.AccountView = *a.view().Account
		result.GoogleEmail = a.googleEmail
		result.AllowedMethods = []LoginMethod{}
		if a.hash != nil && !a.tentative {
			result.AllowedMethods = append(result.AllowedMethods, LoginPassword)
		}
		if a.google {
			result.AllowedMethods = append(result.AllowedMethods, LoginGoogle)
		}
		err = tx.QueryRow(ctx, `SELECT target_email FROM account_tokens WHERE account_id=$1 AND purpose='email_change' AND auth_revision=$2 AND consumed_at IS NULL AND expires_at>$3 ORDER BY created_at DESC LIMIT 1`, a.id, a.revision, s.now().UTC()).Scan(&result.PendingEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return result, err
}

// revoke changes the durable authority before deleting child credentials. Call
// only with the account row locked; racing login/callback must recheck revision.
func revoke(ctx context.Context, tx pgx.Tx, a *account) error {
	if _, err := tx.Exec(ctx, `UPDATE accounts SET auth_revision=auth_revision+1,authority_event=nextval('account_authority_events') WHERE id=$1`, a.id); err != nil {
		return ErrUnavailable
	}
	a.revision++
	if _, err := tx.Exec(ctx, `UPDATE account_passwords SET registration_digest=NULL WHERE account_id=$1`, a.id); err != nil {
		return ErrUnavailable
	}
	for _, query := range []string{`DELETE FROM account_sessions WHERE account_id=$1`, `DELETE FROM account_tokens WHERE account_id=$1`, `DELETE FROM account_oauth_flows WHERE account_id=$1`, `DELETE FROM account_mail_outbox WHERE account_id=$1 AND state='pending' AND purpose<>'security_notification'`} {
		if _, err := tx.Exec(ctx, query, a.id); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func actionBinding(action Action, target string) (string, Digest, error) {
	switch action {
	case ActionEmailChange:
		var err error
		target, err = NormalizeEmail(target)
		if err != nil {
			return "", Digest{}, err
		}
	case ActionPasswordChange, ActionPasswordAdd, ActionGoogleLink, ActionGoogleUnlink, ActionDelete:
		if target != "" {
			return "", Digest{}, ErrInvalidInput
		}
	default:
		return "", Digest{}, ErrInvalidInput
	}
	return target, sha256.Sum256([]byte("messeances-action-v1\x00" + string(action) + "\x00" + target)), nil
}
