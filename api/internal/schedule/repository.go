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

type PublicationMetrics struct {
	Movies       int
	NewMovies    int
	Showtimes    int
	NewShowtimes int
}

type PublicationResult struct {
	Version   int64
	Providers map[Provider]PublicationMetrics
}

type SnapshotWriter interface {
	Replace(context.Context, []Dataset) (PublicationResult, error)
}
