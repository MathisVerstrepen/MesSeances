package synccontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type lockRow struct {
	value bool
	err   error
}

func (r lockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.value
	return nil
}

type fakeRunLockSession struct {
	rows      []pgx.Row
	queries   []string
	released  int
	discarded int
}

func (s *fakeRunLockSession) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	s.queries = append(s.queries, query)
	row := s.rows[0]
	s.rows = s.rows[1:]
	return row
}

func (s *fakeRunLockSession) Release() { s.released++ }

func (s *fakeRunLockSession) Discard(context.Context) error {
	s.discarded++
	return nil
}

func TestPostgresRunLockerAcquisitionAndRelease(t *testing.T) {
	t.Run("contended session returns to pool", func(t *testing.T) {
		session := &fakeRunLockSession{rows: []pgx.Row{lockRow{value: false}}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		if _, err := locker.Acquire(context.Background()); !errors.Is(err, ErrInProgress) {
			t.Fatalf("acquire err=%v", err)
		}
		if session.released != 1 || session.discarded != 0 {
			t.Fatalf("released=%d discarded=%d", session.released, session.discarded)
		}
	})

	t.Run("acquisition uncertainty discards session", func(t *testing.T) {
		session := &fakeRunLockSession{rows: []pgx.Row{lockRow{err: errors.New("connection lost")}}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		if _, err := locker.Acquire(context.Background()); err == nil || errors.Is(err, ErrInProgress) {
			t.Fatalf("acquire err=%v", err)
		}
		if session.released != 0 || session.discarded != 1 {
			t.Fatalf("released=%d discarded=%d", session.released, session.discarded)
		}
	})

	t.Run("successful unlock returns session", func(t *testing.T) {
		session := &fakeRunLockSession{rows: []pgx.Row{lockRow{value: true}, lockRow{value: true}}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		lease, err := locker.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
		if session.released != 1 || session.discarded != 0 || len(session.queries) != 2 {
			t.Fatalf("released=%d discarded=%d queries=%v", session.released, session.discarded, session.queries)
		}
	})

	for _, test := range []struct {
		name string
		row  pgx.Row
	}{
		{name: "false unlock", row: lockRow{value: false}},
		{name: "unlock query failure", row: lockRow{err: errors.New("connection lost")}},
	} {
		t.Run(test.name+" discards session", func(t *testing.T) {
			session := &fakeRunLockSession{rows: []pgx.Row{lockRow{value: true}, test.row}}
			locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
			lease, err := locker.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := lease.Release(context.Background()); err == nil {
				t.Fatal("release succeeded")
			}
			if session.released != 0 || session.discarded != 1 {
				t.Fatalf("released=%d discarded=%d", session.released, session.discarded)
			}
		})
	}
}
