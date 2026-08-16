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
	version   int64
}

func NewPostgresSource(ctx context.Context, reader SnapshotReader) (*PostgresSource, error) {
	data, version, err := reader.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load initial schedule snapshot: %w", err)
	}
	if version <= 0 {
		return nil, fmt.Errorf("invalid initial schedule snapshot version")
	}
	if err := ValidateDataset(data, true); err != nil {
		return nil, fmt.Errorf("invalid initial schedule snapshot: %w", err)
	}
	return &PostgresSource{reader: reader, data: cloneDataset(data), version: version}, nil
}

func (s *PostgresSource) Snapshot() Dataset {
	if !s.refreshMu.TryLock() {
		return s.currentClone()
	}
	defer s.refreshMu.Unlock()
	s.dataMu.RLock()
	rememberedVersion := s.version
	s.dataMu.RUnlock()
	checkCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	currentVersion, err := s.reader.CurrentVersion(checkCtx)
	cancel()
	if err != nil || currentVersion <= 0 || currentVersion == rememberedVersion {
		return s.currentClone()
	}
	loadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	data, loadedVersion, err := s.reader.Load(loadCtx)
	cancel()
	if err != nil || loadedVersion <= 0 || ValidateDataset(data, true) != nil {
		return s.currentClone()
	}
	s.dataMu.Lock()
	s.data = cloneDataset(data)
	s.version = loadedVersion
	result := cloneDataset(s.data)
	s.dataMu.Unlock()
	return result
}

func (s *PostgresSource) currentClone() Dataset {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	return cloneDataset(s.data)
}
