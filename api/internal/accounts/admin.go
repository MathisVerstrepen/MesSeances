package accounts

import (
	"context"
	"math"
	"time"
)

const adminAccountsTimeout = 5 * time.Second

type AdminAccountsQuery struct {
	Limit  int
	Offset int
}

func (q AdminAccountsQuery) Valid() bool {
	return q.Limit >= 1 && q.Limit <= 100 && q.Offset >= 0 && q.Offset <= math.MaxInt32
}

// AdminAccount exposes only the account overview, never internal identifiers,
// credentials, provider claims, or private account content.
type AdminAccount struct {
	Email           string  `json:"email"`
	Username        *string `json:"username"`
	State           State   `json:"state"`
	CreatedAt       string  `json:"created_at"`
	EmailVerifiedAt *string `json:"email_verified_at"`
	HasPassword     bool    `json:"has_password"`
	GoogleLinked    bool    `json:"google_linked"`
}

type AdminAccountsPage struct {
	Items  []AdminAccount `json:"items"`
	Total  int64          `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// AdminService is a read-only overview independent of account feature flags,
// sign-in providers, and account session authority. HTTP callers must authorize
// administrators before invoking it.
type AdminService struct {
	store *PostgresStore
	now   func() time.Time
}

func NewAdminService(store *PostgresStore, now func() time.Time) *AdminService {
	if now == nil {
		now = time.Now
	}
	return &AdminService{store: store, now: now}
}

func (s *AdminService) List(ctx context.Context, query AdminAccountsQuery) (AdminAccountsPage, error) {
	if !query.Valid() {
		return AdminAccountsPage{}, ErrInvalidInput
	}
	if s == nil || s.store == nil || s.store.db == nil {
		return AdminAccountsPage{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, adminAccountsTimeout)
	defer cancel()
	return s.store.listAdminAccounts(ctx, query, s.now().UTC())
}

func adminAccountView(a account, hasPassword bool, now time.Time) AdminAccount {
	state := a.state()
	if a.expired(now) {
		state = "expired"
	}
	var verified *string
	if a.verified != nil {
		value := a.verified.UTC().Format(time.RFC3339Nano)
		verified = &value
	}
	return AdminAccount{
		Email: a.email, Username: a.username, State: state,
		CreatedAt: a.created.UTC().Format(time.RFC3339Nano), EmailVerifiedAt: verified,
		HasPassword: hasPassword, GoogleLinked: a.google,
	}
}
