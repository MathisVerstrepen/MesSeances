package syncschedule

import (
	"errors"
	"time"
)

type Kind string
type Target string

const (
	Timezone             = "Europe/Paris"
	KindDaily       Kind = "daily"
	KindWeekly      Kind = "weekly"
	KindCron        Kind = "cron"
	RefreshInterval      = 30 * time.Second
	RetryDelay           = 15 * time.Minute

	TargetUGC             Target = "ugc"
	TargetKinepolis       Target = "kinepolis"
	TargetPathe           Target = "pathe"
	TargetCGR             Target = "cgr"
	TargetMetadataRefresh Target = "tmdb_metadata_refresh"
)

var (
	ErrInvalidSchedule   = errors.New("invalid sync schedule")
	ErrScheduleMissing   = errors.New("sync schedule not found")
	ErrTargetUnavailable = errors.New("sync schedule target unavailable")
	ErrInProgress        = errors.New("scheduled operation already in progress")
	ErrOccurrenceClaimed = errors.New("scheduled occurrence already claimed")
)

// Definition is the persisted schedule union. Only fields belonging to Kind
// may be populated.
type Definition struct {
	Kind       Kind     `json:"kind"`
	Time       string   `json:"time,omitempty"`
	Weekdays   []string `json:"weekdays,omitempty"`
	Expression string   `json:"expression,omitempty"`
}

type Schedule struct {
	ID         int64      `json:"-"`
	Target     Target     `json:"target"`
	Revision   int64      `json:"revision"`
	Enabled    bool       `json:"enabled"`
	Definition Definition `json:"schedule"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type Occurrence struct {
	ScheduleID   int64
	Target       Target
	Revision     int64
	ScheduledFor time.Time
	Attempt      int
}

type Completion struct {
	Succeeded         bool
	FinalizationError error
}

func cloneSchedule(schedule Schedule) Schedule {
	schedule.Definition.Weekdays = append([]string(nil), schedule.Definition.Weekdays...)
	return schedule
}

func ValidTarget(target Target) bool {
	return target == TargetUGC || target == TargetKinepolis || target == TargetPathe || target == TargetCGR || target == TargetMetadataRefresh
}

func TargetOrder(target Target) int {
	switch target {
	case TargetUGC:
		return 0
	case TargetKinepolis:
		return 1
	case TargetPathe:
		return 2
	case TargetCGR:
		return 3
	case TargetMetadataRefresh:
		return 4
	default:
		return 5
	}
}
