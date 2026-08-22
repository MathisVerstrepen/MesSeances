package schedule

import (
	"context"
	"errors"
)

var ErrNoCompleteSnapshot = errors.New("no complete schedule snapshot")

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
