package syncschedule

import (
	"errors"
	"slices"
	"time"

	"messeances/api/internal/schedule"
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
	TargetMegarama        Target = "megarama"
	TargetCineville       Target = "cineville"
	TargetMK2             Target = "mk2"
	TargetCinewest        Target = "cinewest"
	TargetGrandEcran      Target = "grandecran"
	TargetNoeCinemas      Target = "noecinemas"
	TargetVeo             Target = "veo"
	TargetMajestic        Target = "majestic"
	TargetCineode         Target = "cineode"
	TargetCinemovida      Target = "cinemovida"
	TargetOcine           Target = "ocine"
	TargetMetadataRefresh Target = "tmdb_metadata_refresh"
	TargetUpcomingMovies  Target = "tmdb_upcoming_movies"
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

// ProviderTargets returns the synchronized provider targets in canonical order.
func ProviderTargets() []Target {
	providers := schedule.SourceProviders()
	targets := make([]Target, len(providers))
	for i, provider := range providers {
		targets[i] = Target(provider)
	}
	return targets
}

// OrderedTargets returns every schedulable target: providers first, then the
// TMDB maintenance jobs.
func OrderedTargets() []Target {
	return append(ProviderTargets(), TargetMetadataRefresh, TargetUpcomingMovies)
}

func ValidTarget(target Target) bool {
	return slices.Contains(OrderedTargets(), target)
}

// TargetOrder ranks targets for display; unknown targets sort last.
func TargetOrder(target Target) int {
	targets := OrderedTargets()
	if index := slices.Index(targets, target); index >= 0 {
		return index
	}
	return len(targets)
}
