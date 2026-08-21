package schedule

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

type Source interface{ Snapshot() *SnapshotView }

type PostgresSource struct {
	reader   SnapshotReader
	view     atomic.Pointer[SnapshotView]
	revision SnapshotRevision
	logger   *slog.Logger
	observer RefreshObserver
}

type RefreshObserver interface {
	ObserveScheduleRefresh(result, stage, reason string, duration time.Duration)
	SetScheduleRevision(schedule, enrichment int64)
	SetScheduleFreshness(generatedAt, windowStart, windowEnd time.Time)
	SetScheduleRefreshLastSuccess(time.Time)
}

type SourceOptions struct {
	Logger   *slog.Logger
	Observer RefreshObserver
}

func NewPostgresSource(ctx context.Context, reader SnapshotReader, option ...SourceOptions) (*PostgresSource, error) {
	options := SourceOptions{}
	if len(option) != 0 {
		options = option[0]
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
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
	source := &PostgresSource{reader: reader, revision: revision, logger: options.Logger, observer: options.Observer}
	source.view.Store(NewSnapshotView(data))
	source.setRevisionMetrics(revision)
	source.setFreshnessMetrics(data)
	if source.observer != nil {
		source.observer.SetScheduleRefreshLastSuccess(time.Now())
	}
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
	started := time.Now()
	result := "unchanged"
	stage, reason := "none", "none"
	defer func() {
		if s.observer != nil {
			s.observer.ObserveScheduleRefresh(result, stage, reason, time.Since(started))
			if result == "unchanged" || result == "reloaded" {
				s.observer.SetScheduleRefreshLastSuccess(time.Now())
			}
		}
		if result != "unchanged" {
			level := slog.LevelWarn
			if result == "reloaded" {
				level = slog.LevelInfo
			}
			s.logger.Log(ctx, level, "schedule_refresh_completed", "component", "schedule", "result", result, "stage", stage, "reason", reason, "duration", time.Since(started).Seconds())
		}
	}()
	checkCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	currentRevision, err := s.reader.CurrentRevision(checkCtx)
	cancel()
	if err != nil {
		stage = "revision_check"
		if ctx.Err() != nil {
			result = "canceled"
			reason = "canceled"
		} else {
			result = "check_failed"
			reason = "read_failed"
		}
		return
	}
	if currentRevision.ScheduleVersion <= 0 || currentRevision.EnrichmentVersion < 0 {
		result = "invalid_revision"
		stage, reason = "revision_check", "invalid_revision"
		return
	}
	if currentRevision == s.revision {
		return
	}
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	data, loadedRevision, err := s.reader.Load(loadCtx)
	cancel()
	if err != nil {
		stage = "snapshot_load"
		if ctx.Err() != nil {
			result = "canceled"
			reason = "canceled"
		} else {
			result = "load_failed"
			reason = "read_failed"
		}
		return
	}
	if loadedRevision.ScheduleVersion <= 0 || loadedRevision.EnrichmentVersion < 0 {
		result = "invalid_revision"
		stage, reason = "snapshot_load", "invalid_revision"
		return
	}
	if ValidateDataset(data, true) != nil {
		result = "invalid_dataset"
		stage, reason = "dataset_validation", "invalid_dataset"
		return
	}
	view := NewSnapshotView(data)
	s.view.Store(view)
	s.revision = loadedRevision
	s.setRevisionMetrics(loadedRevision)
	s.setFreshnessMetrics(data)
	result = "reloaded"
}

func (s *PostgresSource) setFreshnessMetrics(data Dataset) {
	if s.observer == nil {
		return
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return
	}
	from, fromErr := time.ParseInLocation(dateLayout, data.Window.From, location)
	through, throughErr := time.ParseInLocation(dateLayout, data.Window.Through, location)
	if fromErr == nil && throughErr == nil {
		s.observer.SetScheduleFreshness(data.GeneratedAt, from, through.AddDate(0, 0, 1))
	}
}

func (s *PostgresSource) setRevisionMetrics(revision SnapshotRevision) {
	if s.observer != nil {
		s.observer.SetScheduleRevision(revision.ScheduleVersion, revision.EnrichmentVersion)
	}
}
