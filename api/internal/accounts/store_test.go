package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type testBeginner struct {
	tx  *testTx
	err error
}

func (b testBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return b.tx, b.err }

type testTx struct {
	pgx.Tx
	committed, rolledBack bool
	commitErr             error
	rollbackContextActive bool
}

func (tx *testTx) Commit(context.Context) error { tx.committed = true; return tx.commitErr }
func (tx *testTx) Rollback(ctx context.Context) error {
	tx.rolledBack = true
	tx.rollbackContextActive = ctx.Err() == nil
	return nil
}

func TestTransactionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		operationErr, commitErr error
		commit                  bool
	}{
		{"success", nil, nil, true}, {"operation failure", ErrInvalidInput, nil, false}, {"commit failure", nil, errors.New("private database details"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &testTx{commitErr: tc.commitErr}
			err := NewPostgresStore(testBeginner{tx: tx}).withTransaction(context.Background(), func(pgx.Tx) error { return tc.operationErr })
			if tx.committed != tc.commit || !tx.rolledBack || !tx.rollbackContextActive {
				t.Fatal("transaction lifecycle mismatch")
			}
			want := tc.operationErr
			if tc.commitErr != nil {
				want = ErrUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatalf("error %v want %v", err, want)
			}
		})
	}
	if err := NewPostgresStore(nil).withTransaction(context.Background(), nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil store did not fail closed")
	}
}
