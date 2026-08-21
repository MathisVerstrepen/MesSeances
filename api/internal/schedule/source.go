package schedule

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

type Source interface{ Snapshot() *SnapshotView }

type PostgresSource struct {
	reader   SnapshotReader
	view     atomic.Pointer[SnapshotView]
	revision SnapshotRevision
}

func NewPostgresSource(ctx context.Context, reader SnapshotReader) (*PostgresSource, error) {
	data, revision, err := reader.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load initial schedule snapshot: %w", err)
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 {
		return nil, fmt.Errorf("invalid initial schedule snapshot revision")
	}
	if err := ValidateDataset(data, true); err != nil {
		return nil, fmt.Errorf("invalid initial schedule snapshot: %w", err)
	}
	source := &PostgresSource{reader: reader, revision: revision}
	source.view.Store(NewSnapshotView(data))
	return source, nil
}

func (s *PostgresSource) Snapshot() *SnapshotView { return s.view.Load() }

func (s *PostgresSource) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	s.runTicks(ctx, ticker.C)
}

func (s *PostgresSource) runTicks(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.refresh(ctx)
		}
	}
}

func (s *PostgresSource) refresh(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	currentRevision, err := s.reader.CurrentRevision(checkCtx)
	cancel()
	if err != nil || currentRevision.ScheduleVersion <= 0 || currentRevision.EnrichmentVersion < 0 || currentRevision == s.revision {
		return
	}
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	data, loadedRevision, err := s.reader.Load(loadCtx)
	cancel()
	if err != nil || loadedRevision.ScheduleVersion <= 0 || loadedRevision.EnrichmentVersion < 0 || ValidateDataset(data, true) != nil {
		return
	}
	view := NewSnapshotView(data)
	s.view.Store(view)
	s.revision = loadedRevision
}
