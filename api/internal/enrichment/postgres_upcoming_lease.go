package enrichment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/syncschedule"
)

const upcomingRunLockID int64 = 6211428337968320

type PostgresUpcomingLocker struct{ pool *pgxpool.Pool }

func NewPostgresUpcomingLocker(pool *pgxpool.Pool) *PostgresUpcomingLocker {
	return &PostgresUpcomingLocker{pool: pool}
}

func (l *PostgresUpcomingLocker) Acquire(ctx context.Context) (UpcomingLease, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("upcoming lease acquisition failed")
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", upcomingRunLockID).Scan(&acquired); err != nil {
		discardUpcomingSession(conn)
		return nil, fmt.Errorf("upcoming lease acquisition failed")
	}
	if !acquired {
		conn.Release()
		return nil, syncschedule.ErrInProgress
	}
	return &postgresUpcomingLease{conn: conn}, nil
}

type postgresUpcomingLease struct {
	mu   sync.Mutex
	conn *pgxpool.Conn
}

func (l *postgresUpcomingLease) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return fmt.Errorf("upcoming lease already released")
	}
	conn := l.conn
	l.conn = nil
	var unlocked bool
	if err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", upcomingRunLockID).Scan(&unlocked); err != nil || !unlocked {
		discardUpcomingSession(conn)
		return fmt.Errorf("upcoming lease release failed")
	}
	conn.Release()
	return nil
}

func discardUpcomingSession(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = conn.Hijack().Close(ctx)
}
