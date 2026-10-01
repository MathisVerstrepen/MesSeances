package accounts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const adminAccountsSQL = `
SELECT a.email,u.username,a.created_at,a.email_verified_at,
       EXISTS (SELECT 1 FROM account_passwords p WHERE p.account_id=a.id),
       EXISTS (SELECT 1 FROM account_google_identities g WHERE g.account_id=a.id)
FROM (
    SELECT id,email,created_at,email_verified_at FROM accounts
    ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2
) a
LEFT JOIN account_username_claims u ON u.account_id=a.id
ORDER BY a.created_at DESC,a.id DESC`

func (s *PostgresStore) listAdminAccounts(ctx context.Context, query AdminAccountsQuery, now time.Time) (AdminAccountsPage, error) {
	// Count and page share one snapshot, including concurrent registrations and
	// deletions. No locks, account cleanup, or other mutation belongs in this read.
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AdminAccountsPage{}, ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	page := AdminAccountsPage{Items: make([]AdminAccount, 0, query.Limit), Limit: query.Limit, Offset: query.Offset}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&page.Total); err != nil {
		return AdminAccountsPage{}, ErrUnavailable
	}
	rows, err := tx.Query(ctx, adminAccountsSQL, query.Limit, query.Offset)
	if err != nil {
		return AdminAccountsPage{}, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var a account
		var hasPassword bool
		if err := rows.Scan(&a.email, &a.username, &a.created, &a.verified, &hasPassword, &a.google); err != nil {
			return AdminAccountsPage{}, ErrUnavailable
		}
		page.Items = append(page.Items, adminAccountView(a, hasPassword, now))
	}
	if rows.Err() != nil {
		return AdminAccountsPage{}, ErrUnavailable
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return AdminAccountsPage{}, ErrUnavailable
	}
	return page, nil
}
