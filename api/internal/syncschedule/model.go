package syncschedule

import (
	"errors"
	"time"

	"messeances/api/internal/synccontrol"
)

type Kind string

const (
	Timezone             = "Europe/Paris"
	KindDaily       Kind = "daily"
	KindWeekly      Kind = "weekly"
	KindCron        Kind = "cron"
	RefreshInterval      = 30 * time.Second
	RetryDelay           = 15 * time.Minute
)

var (
	ErrInvalidSchedule = errors.New("invalid sync schedule")
	ErrScheduleMissing = errors.New("sync schedule not found")
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
	Provider   synccontrol.Target `json:"provider"`
	Revision   int64              `json:"revision"`
	Enabled    bool               `json:"enabled"`
	Definition Definition         `json:"schedule"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

func cloneSchedule(schedule Schedule) Schedule {
	schedule.Definition.Weekdays = append([]string(nil), schedule.Definition.Weekdays...)
	return schedule
}

func validProvider(provider synccontrol.Target) bool {
	return provider == synccontrol.TargetUGC || provider == synccontrol.TargetKinepolis || provider == synccontrol.TargetPathe
}
