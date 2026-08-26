package geocoding

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const geocodingRunLockID int64 = 6211428337968317

type RunLocker interface {
	Acquire(context.Context) (RunLease, error)
}

type RunLease interface {
	Release(context.Context) error
}

type PostgresRunLocker struct {
	acquire func(context.Context) (runLockSession, error)
}

func NewPostgresRunLocker(pool *pgxpool.Pool) *PostgresRunLocker {
	return &PostgresRunLocker{acquire: func(ctx context.Context) (runLockSession, error) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		return postgresRunLockSession{conn: conn}, nil
	}}
}

func (l *PostgresRunLocker) Acquire(ctx context.Context) (RunLease, error) {
	if l == nil || l.acquire == nil {
		return nil, errors.New("theater geocoding run lease acquisition failed")
	}
	session, err := l.acquire(ctx)
	if err != nil {
		return nil, errors.New("theater geocoding run lease acquisition failed")
	}
	var acquired bool
	if err := session.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", geocodingRunLockID).Scan(&acquired); err != nil {
		_ = session.Discard(ctx)
		return nil, errors.New("theater geocoding run lease acquisition failed")
	}
	if !acquired {
		session.Release()
		return nil, ErrRunInProgress
	}
	return &postgresRunLease{session: session}, nil
}

type postgresRunLease struct {
	mu       sync.Mutex
	session  runLockSession
	released bool
}

func (l *postgresRunLease) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return errors.New("theater geocoding run lease already released")
	}
	l.released = true
	var unlocked bool
	err := l.session.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", geocodingRunLockID).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = l.session.Discard(ctx)
		return errors.New("theater geocoding run lease release failed")
	}
	l.session.Release()
	return nil
}

type runLockSession interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Release()
	Discard(context.Context) error
}

type postgresRunLockSession struct{ conn *pgxpool.Conn }

func (s postgresRunLockSession) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return s.conn.QueryRow(ctx, query, args...)
}

func (s postgresRunLockSession) Release() { s.conn.Release() }

func (s postgresRunLockSession) Discard(ctx context.Context) error {
	return s.conn.Hijack().Close(ctx)
}
