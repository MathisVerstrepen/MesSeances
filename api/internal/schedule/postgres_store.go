package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoCompleteSnapshot = errors.New("no complete schedule snapshot")

const snapshotWriterLockID int64 = 6211428337968315

type SnapshotReader interface {
	CurrentRevision(context.Context) (SnapshotRevision, error)
	Load(context.Context) (Dataset, SnapshotRevision, error)
}

type SnapshotRevision struct {
	ScheduleVersion   int64
	EnrichmentVersion int64
}
type SnapshotWriter interface {
	Replace(context.Context, []Dataset) (int64, error)
}
type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) CurrentRevision(ctx context.Context) (SnapshotRevision, error) {
	var revision SnapshotRevision
	err := s.pool.QueryRow(ctx, `SELECT s.version, e.version FROM schedule_snapshot s CROSS JOIN movie_enrichment_state e WHERE s.singleton=true AND e.singleton=true`).Scan(&revision.ScheduleVersion, &revision.EnrichmentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return SnapshotRevision{}, ErrNoCompleteSnapshot
	}
	if err != nil {
		return SnapshotRevision{}, fmt.Errorf("read schedule snapshot revision failed")
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 {
		return SnapshotRevision{}, fmt.Errorf("invalid schedule snapshot revision")
	}
	return revision, nil
}

func (s *PostgresStore) CurrentVersion(ctx context.Context) (int64, error) {
	revision, err := s.CurrentRevision(ctx)
	return revision.ScheduleVersion, err
}

func rollbackScheduleTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
