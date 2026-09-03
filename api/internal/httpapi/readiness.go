package httpapi

import (
	"context"
	"time"

	"messeances/api/internal/schedule"
)

type ReadinessOptions struct {
	Schedule  schedule.Source
	Database  DatabasePinger
	Revisions RevisionReader
	Now       func() time.Time
}

type DatabasePinger interface {
	Ping(context.Context) error
}

type RevisionReader interface {
	CurrentRevision(context.Context) (schedule.SnapshotRevision, error)
}

type probeResponse struct {
	Status string `json:"status"`
}

const readinessDatabaseTimeout = 250 * time.Millisecond

type readinessChecker struct {
	schedule  schedule.Source
	database  DatabasePinger
	revisions RevisionReader
	now       func() time.Time
}

func newReadinessChecker(options ReadinessOptions) readinessChecker {
	if options.Now == nil {
		options.Now = time.Now
	}
	return readinessChecker{
		schedule:  options.Schedule,
		database:  options.Database,
		revisions: options.Revisions,
		now:       options.Now,
	}
}

func (c readinessChecker) ready(ctx context.Context) bool {
	if c.schedule == nil || c.database == nil || c.revisions == nil {
		return false
	}
	view := c.schedule.Snapshot()
	if !view.ReadyAt(c.now()) {
		return false
	}
	databaseCtx, cancel := context.WithTimeout(ctx, readinessDatabaseTimeout)
	defer cancel()
	if c.database.Ping(databaseCtx) != nil {
		return false
	}
	revision, err := c.revisions.CurrentRevision(databaseCtx)
	if err != nil {
		return false
	}
	return revision.ScheduleVersion > 0 && revision.EnrichmentVersion >= 0 && revision.TheaterLocationVersion >= 0
}
