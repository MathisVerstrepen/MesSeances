package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/schedule"
)

const snapshotWriterLockID int64 = 6211428337968315

type Store struct{ pool *pgxpool.Pool }

var _ schedule.SnapshotReader = (*Store)(nil)
var _ schedule.SnapshotWriter = (*Store)(nil)

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CurrentRevision(ctx context.Context) (schedule.SnapshotRevision, error) {
	var revision schedule.SnapshotRevision
	err := s.pool.QueryRow(ctx, `SELECT s.version, e.version, l.version FROM schedule_snapshot s CROSS JOIN movie_enrichment_state e CROSS JOIN theater_location_state l WHERE s.singleton=true AND e.singleton=true AND l.singleton=true`).Scan(&revision.ScheduleVersion, &revision.EnrichmentVersion, &revision.TheaterLocationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return schedule.SnapshotRevision{}, schedule.ErrNoCompleteSnapshot
	}
	if err != nil {
		return schedule.SnapshotRevision{}, fmt.Errorf("read schedule snapshot revision failed")
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 || revision.TheaterLocationVersion < 0 {
		return schedule.SnapshotRevision{}, fmt.Errorf("invalid schedule snapshot revision")
	}
	return revision, nil
}

func (s *Store) CurrentVersion(ctx context.Context) (int64, error) {
	revision, err := s.CurrentRevision(ctx)
	return revision.ScheduleVersion, err
}

func rollbackScheduleTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
