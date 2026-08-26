package geocoding

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type runLockRow struct {
	value bool
	err   error
}

func (r runLockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.value
	return nil
}

type fakeGeocodingRunLockSession struct {
	rows      []pgx.Row
	args      [][]any
	released  int
	discarded int
}

func (s *fakeGeocodingRunLockSession) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	s.args = append(s.args, args)
	row := s.rows[0]
	s.rows = s.rows[1:]
	return row
}

func (s *fakeGeocodingRunLockSession) Release() { s.released++ }

func (s *fakeGeocodingRunLockSession) Discard(context.Context) error {
	s.discarded++
	return nil
}

func TestPostgresGeocodingRunLockerLifecycle(t *testing.T) {
	t.Run("contention returns session", func(t *testing.T) {
		session := &fakeGeocodingRunLockSession{rows: []pgx.Row{runLockRow{value: false}}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		if _, err := locker.Acquire(context.Background()); !errors.Is(err, ErrRunInProgress) {
			t.Fatalf("error=%v", err)
		}
		if session.released != 1 || session.discarded != 0 {
			t.Fatalf("released=%d discarded=%d", session.released, session.discarded)
		}
	})

	t.Run("uncertain acquire discards session", func(t *testing.T) {
		session := &fakeGeocodingRunLockSession{rows: []pgx.Row{runLockRow{err: errors.New("secret connection error")}}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		if _, err := locker.Acquire(context.Background()); err == nil || errors.Is(err, ErrRunInProgress) {
			t.Fatalf("error=%v", err)
		}
		if session.released != 0 || session.discarded != 1 {
			t.Fatalf("released=%d discarded=%d", session.released, session.discarded)
		}
	})

	t.Run("success uses distinct lock and returns session", func(t *testing.T) {
		session := &fakeGeocodingRunLockSession{rows: []pgx.Row{runLockRow{value: true}, runLockRow{value: true}}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		lease, err := locker.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
		if session.released != 1 || session.discarded != 0 || len(session.args) != 2 || session.args[0][0] != geocodingRunLockID || session.args[1][0] != geocodingRunLockID || geocodingRunLockID == 6211428337968316 {
			t.Fatalf("released=%d discarded=%d args=%v lock=%d", session.released, session.discarded, session.args, geocodingRunLockID)
		}
	})

	for _, row := range []pgx.Row{runLockRow{value: false}, runLockRow{err: errors.New("secret unlock error")}} {
		session := &fakeGeocodingRunLockSession{rows: []pgx.Row{runLockRow{value: true}, row}}
		locker := &PostgresRunLocker{acquire: func(context.Context) (runLockSession, error) { return session, nil }}
		lease, err := locker.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Release(context.Background()); err == nil {
			t.Fatal("uncertain release succeeded")
		}
		if session.released != 0 || session.discarded != 1 {
			t.Fatalf("released=%d discarded=%d", session.released, session.discarded)
		}
	}
}
