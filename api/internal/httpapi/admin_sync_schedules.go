package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

type syncSchedulesResponse struct {
	Timezone  string             `json:"timezone"`
	Schedules []syncScheduleItem `json:"schedules"`
}

type syncScheduleItem struct {
	Provider  synccontrol.Target      `json:"provider"`
	Revision  int64                   `json:"revision"`
	Enabled   bool                    `json:"enabled"`
	Schedule  syncschedule.Definition `json:"schedule"`
	NextRuns  []time.Time             `json:"next_runs"`
	UpdatedAt time.Time               `json:"updated_at"`
}

type saveSyncScheduleRequest struct {
	Enabled  json.RawMessage `json:"enabled"`
	Schedule json.RawMessage `json:"schedule"`
}

type syncScheduleDefinitionRequest struct {
	Kind       json.RawMessage `json:"kind"`
	Time       json.RawMessage `json:"time"`
	Weekdays   json.RawMessage `json:"weekdays"`
	Expression json.RawMessage `json:"expression"`
}

func (a *adminAPI) syncSchedules(w http.ResponseWriter, r *http.Request) {
	if a.schedules == nil {
		writeSyncScheduleUnavailable(w)
		return
	}
	schedules, err := a.schedules.List(r.Context())
	if err != nil {
		writeSyncScheduleFailure(w)
		return
	}
	items, err := a.syncScheduleItems(schedules)
	if err != nil {
		writeSyncScheduleFailure(w)
		return
	}
	writeJSON(w, http.StatusOK, syncSchedulesResponse{Timezone: syncschedule.Timezone, Schedules: items})
}

func (a *adminAPI) saveSyncSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedules == nil {
		writeSyncScheduleUnavailable(w)
		return
	}
	provider := synccontrol.Target(chi.URLParam(r, "provider"))
	if provider != synccontrol.TargetUGC && provider != synccontrol.TargetKinepolis && provider != synccontrol.TargetPathe && provider != synccontrol.TargetCGR {
		writeInvalidSyncSchedule(w)
		return
	}
	var request saveSyncScheduleRequest
	if err := decodeAdminJSON(w, r, &request); err != nil {
		writeInvalidSyncSchedule(w)
		return
	}
	enabled, definition, err := parseSyncScheduleRequest(request)
	if err != nil {
		writeInvalidSyncSchedule(w)
		return
	}
	saved, err := a.schedules.Save(r.Context(), provider, enabled, definition)
	if errors.Is(err, syncschedule.ErrInvalidSchedule) {
		writeInvalidSyncSchedule(w)
		return
	}
	if err != nil {
		writeSyncScheduleFailure(w)
		return
	}
	item, err := a.syncScheduleItem(saved)
	if err != nil {
		writeSyncScheduleFailure(w)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *adminAPI) syncScheduleItems(schedules []syncschedule.Schedule) ([]syncScheduleItem, error) {
	items := make([]syncScheduleItem, 0, len(schedules))
	for _, schedule := range schedules {
		item, err := a.syncScheduleItem(schedule)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return syncScheduleProviderOrder(items[i].Provider) < syncScheduleProviderOrder(items[j].Provider)
	})
	return items, nil
}

func (a *adminAPI) syncScheduleItem(schedule syncschedule.Schedule) (syncScheduleItem, error) {
	nextRuns, err := a.schedules.NextRuns(schedule.Definition)
	if err != nil || len(nextRuns) != 5 {
		return syncScheduleItem{}, errors.New("sync schedule preview failed")
	}
	return syncScheduleItem{
		Provider: schedule.Provider, Revision: schedule.Revision, Enabled: schedule.Enabled,
		Schedule: schedule.Definition, NextRuns: nextRuns, UpdatedAt: schedule.UpdatedAt,
	}, nil
}

func parseSyncScheduleRequest(request saveSyncScheduleRequest) (bool, syncschedule.Definition, error) {
	var enabled bool
	if missingOrNull(request.Enabled) || json.Unmarshal(request.Enabled, &enabled) != nil {
		return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
	}
	var input syncScheduleDefinitionRequest
	if missingOrNull(request.Schedule) || decodeStrictJSON(request.Schedule, &input) != nil {
		return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
	}
	var kind syncschedule.Kind
	if missingOrNull(input.Kind) || json.Unmarshal(input.Kind, &kind) != nil {
		return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
	}
	definition := syncschedule.Definition{Kind: kind}
	switch kind {
	case syncschedule.KindDaily:
		if missingOrNull(input.Time) || len(input.Weekdays) != 0 || len(input.Expression) != 0 || json.Unmarshal(input.Time, &definition.Time) != nil {
			return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
		}
	case syncschedule.KindWeekly:
		if missingOrNull(input.Time) || missingOrNull(input.Weekdays) || len(input.Expression) != 0 || json.Unmarshal(input.Time, &definition.Time) != nil || json.Unmarshal(input.Weekdays, &definition.Weekdays) != nil || definition.Weekdays == nil {
			return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
		}
	case syncschedule.KindCron:
		if missingOrNull(input.Expression) || len(input.Time) != 0 || len(input.Weekdays) != 0 || json.Unmarshal(input.Expression, &definition.Expression) != nil {
			return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
		}
	default:
		return false, syncschedule.Definition{}, syncschedule.ErrInvalidSchedule
	}
	return enabled, definition, nil
}

func missingOrNull(value json.RawMessage) bool {
	return len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func decodeStrictJSON(value []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func syncScheduleProviderOrder(provider synccontrol.Target) int {
	if provider == synccontrol.TargetUGC {
		return 0
	}
	if provider == synccontrol.TargetKinepolis {
		return 1
	}
	if provider == synccontrol.TargetPathe {
		return 2
	}
	if provider == synccontrol.TargetCGR {
		return 3
	}
	return 4
}

func writeInvalidSyncSchedule(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
}

func writeSyncScheduleUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "sync_schedule_unavailable", "Planification des synchronisations indisponible.")
}

func writeSyncScheduleFailure(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "sync_schedule_failed", "La planification des synchronisations n'a pas pu être traitée.")
}
