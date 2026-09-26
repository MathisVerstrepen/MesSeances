package accounts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// TransactionBeginner is implemented by pgxpool.Pool. Account operations own
// their transaction, independent of schedule/admin writer transactions.
type TransactionBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct{ db TransactionBeginner }

func NewPostgresStore(db TransactionBeginner) *PostgresStore { return &PostgresStore{db: db} }

// withTransaction is the atomic boundary for upcoming lifecycle operations.
// Callbacks lock account first, then session/token/identity, recheck revision and
// deadlines, and enqueue mail in this transaction. Never call providers here.
func (s *PostgresStore) withTransaction(ctx context.Context, fn func(pgx.Tx) error) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Rollback after commit returns pgx.ErrTxClosed, which is expected.
		_ = tx.Rollback(cleanup)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}
