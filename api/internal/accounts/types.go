package accounts

import (
	"context"
	"time"
)

const (
	PendingLifetime      = 7 * 24 * time.Hour
	SessionLifetime      = 30 * 24 * time.Hour
	SessionIdleLifetime  = 7 * 24 * time.Hour
	VerificationLifetime = 24 * time.Hour
	RecoveryLifetime     = 30 * time.Minute
	OAuthLifetime        = 10 * time.Minute
	EmailStepUpLifetime  = 10 * time.Minute
	GrantLifetime        = 5 * time.Minute
)

type State string

const (
	StateAnonymous       State = "anonymous"
	StatePendingEmail    State = "pending_email"
	StatePendingUsername State = "pending_username"
	StateComplete        State = "complete"
)

// SessionView is safe for private responses and request-scoped frontend state.
// Username is null until onboarding completes. No database identifiers escape.
type SessionView struct {
	Enabled bool         `json:"enabled"`
	State   State        `json:"state"`
	Account *AccountView `json:"account"`
}

type AccountView struct {
	Email        string  `json:"email"`
	Username     *string `json:"username"`
	HasPassword  bool    `json:"has_password"`
	GoogleLinked bool    `json:"google_linked"`
}

// AccountDetails distinguishes the contact/login address from Google's last claim.
type AccountDetails struct {
	AccountView
	GoogleEmail    *string       `json:"google_email"`
	PendingEmail   *string       `json:"pending_email"`
	AllowedMethods []LoginMethod `json:"allowed_methods"`
}

type LoginMethod string

const (
	LoginPassword LoginMethod = "password"
	LoginGoogle   LoginMethod = "google"
)

type TokenPurpose string

const (
	TokenVerification  TokenPurpose = "verification"
	TokenPasswordReset TokenPurpose = "password_reset"
	TokenEmailChange   TokenPurpose = "email_change"
	TokenEmailStepUp   TokenPurpose = "email_step_up"
	TokenReauthGrant   TokenPurpose = "reauth_grant"
	TokenGoogleReauth  TokenPurpose = "google_reauth"
)

type Action string

const (
	ActionPasswordAdd    Action = "password_add"
	ActionPasswordChange Action = "password_change"
	ActionEmailChange    Action = "email_change"
	ActionGoogleLink     Action = "google_link"
	ActionGoogleUnlink   Action = "google_unlink"
	ActionDelete         Action = "delete_account"
)

type FlowMode string

const (
	FlowLogin  FlowMode = "login"
	FlowLink   FlowMode = "link"
	FlowReauth FlowMode = "reauth"
)

// GoogleIdentity contains validated claims only, never provider bearer tokens.
// The adapter must validate signature, issuer, audience/azp, expiry, iat and nonce.
type GoogleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
	// EmailAuthoritative requires a verified Gmail address or a signed, nonempty
	// Workspace hd claim. EmailVerified alone may be historical third-party proof.
	EmailAuthoritative bool
}

// GoogleProvider performs no database work. Exchange runs only after single-use
// flow claim and browser binding validation; callers recheck account authority.
type GoogleProvider interface {
	AuthorizationURL(state, nonce, verifier string) (string, error)
	Exchange(ctx context.Context, code, verifier, nonce string) (GoogleIdentity, error)
}

type PasswordHasher interface {
	Hash(context.Context, string) (string, error)
	Verify(context.Context, string, string) (matches, needsRehash bool, err error)
	Dummy(context.Context, string) error
}
