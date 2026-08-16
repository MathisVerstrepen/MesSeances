package schedule

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Source interface{ Snapshot() Dataset }

type PostgresSource struct {
	reader    SnapshotReader
	dataMu    sync.RWMutex
	refreshMu sync.Mutex
	data      Dataset
	revision  SnapshotRevision
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
	return &PostgresSource{reader: reader, data: cloneDataset(data), revision: revision}, nil
}

func (s *PostgresSource) Snapshot() Dataset {
	if !s.refreshMu.TryLock() {
		return s.currentClone()
	}
	defer s.refreshMu.Unlock()
	s.dataMu.RLock()
	rememberedRevision := s.revision
	s.dataMu.RUnlock()
	checkCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	currentRevision, err := s.reader.CurrentRevision(checkCtx)
	cancel()
	if err != nil || currentRevision.ScheduleVersion <= 0 || currentRevision.EnrichmentVersion < 0 || currentRevision == rememberedRevision {
		return s.currentClone()
	}
	loadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	data, loadedRevision, err := s.reader.Load(loadCtx)
	cancel()
	if err != nil || loadedRevision.ScheduleVersion <= 0 || loadedRevision.EnrichmentVersion < 0 || ValidateDataset(data, true) != nil {
		return s.currentClone()
	}
	s.dataMu.Lock()
	s.data = cloneDataset(data)
	s.revision = loadedRevision
	result := cloneDataset(s.data)
	s.dataMu.Unlock()
	return result
}

func (s *PostgresSource) currentClone() Dataset {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	return cloneDataset(s.data)
}
