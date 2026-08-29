package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/syncschedule"
)

type syncSchedulesResponse struct {
	Timezone         string                `json:"timezone"`
	AvailableTargets []syncschedule.Target `json:"available_targets"`
	Schedules        []syncScheduleItem    `json:"schedules"`
}

type syncScheduleItem struct {
	ID        string                  `json:"id"`
	Target    syncschedule.Target     `json:"target"`
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
	writeJSON(w, http.StatusOK, syncSchedulesResponse{
		Timezone: syncschedule.Timezone, AvailableTargets: a.schedules.AvailableTargets(), Schedules: items,
	})
}

func (a *adminAPI) createSyncSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedules == nil {
		writeSyncScheduleUnavailable(w)
		return
	}
	target, ok := syncScheduleTarget(w, r)
	if !ok {
		return
	}
	enabled, definition, ok := decodeSyncScheduleRequest(w, r)
	if !ok {
		return
	}
	saved, err := a.schedules.Create(r.Context(), target, enabled, definition)
	a.writeSyncScheduleMutation(w, saved, err, http.StatusCreated)
}

func (a *adminAPI) updateSyncSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedules == nil {
		writeSyncScheduleUnavailable(w)
		return
	}
	target, ok := syncScheduleTarget(w, r)
	if !ok {
		return
	}
	id, ok := syncScheduleID(w, r)
	if !ok {
		return
	}
	enabled, definition, ok := decodeSyncScheduleRequest(w, r)
	if !ok {
		return
	}
	saved, err := a.schedules.Update(r.Context(), target, id, enabled, definition)
	a.writeSyncScheduleMutation(w, saved, err, http.StatusOK)
}

func (a *adminAPI) deleteSyncSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedules == nil {
		writeSyncScheduleUnavailable(w)
		return
	}
	target, ok := syncScheduleTarget(w, r)
	if !ok {
		return
	}
	id, ok := syncScheduleID(w, r)
	if !ok {
		return
	}
	if !emptyAdminBody(w, r) {
		writeInvalidSyncSchedule(w)
		return
	}
	err := a.schedules.Delete(r.Context(), target, id)
	if errors.Is(err, syncschedule.ErrScheduleMissing) {
		writeSyncScheduleNotFound(w)
		return
	}
	if errors.Is(err, syncschedule.ErrInvalidSchedule) {
		writeInvalidSyncSchedule(w)
		return
	}
	if err != nil {
		writeSyncScheduleFailure(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *adminAPI) writeSyncScheduleMutation(w http.ResponseWriter, saved syncschedule.Schedule, err error, status int) {
	switch {
	case errors.Is(err, syncschedule.ErrInvalidSchedule):
		writeInvalidSyncSchedule(w)
		return
	case errors.Is(err, syncschedule.ErrScheduleMissing):
		writeSyncScheduleNotFound(w)
		return
	case errors.Is(err, syncschedule.ErrTargetUnavailable):
		writeSyncScheduleTargetUnavailable(w)
		return
	case err != nil:
		writeSyncScheduleFailure(w)
		return
	}
	item, itemErr := a.syncScheduleItem(saved)
	if itemErr != nil {
		writeSyncScheduleFailure(w)
		return
	}
	writeJSON(w, status, item)
}

func syncScheduleTarget(w http.ResponseWriter, r *http.Request) (syncschedule.Target, bool) {
	target := syncschedule.Target(chi.URLParam(r, "target"))
	if !syncschedule.ValidTarget(target) {
		writeInvalidSyncSchedule(w)
		return "", false
	}
	return target, true
}

func syncScheduleID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		writeInvalidSyncSchedule(w)
		return 0, false
	}
	return id, true
}

func decodeSyncScheduleRequest(w http.ResponseWriter, r *http.Request) (bool, syncschedule.Definition, bool) {
	var request saveSyncScheduleRequest
	if err := decodeAdminJSON(w, r, &request); err != nil {
		writeInvalidSyncSchedule(w)
		return false, syncschedule.Definition{}, false
	}
	enabled, definition, err := parseSyncScheduleRequest(request)
	if err != nil {
		writeInvalidSyncSchedule(w)
		return false, syncschedule.Definition{}, false
	}
	return enabled, definition, true
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
		left, right := syncschedule.TargetOrder(items[i].Target), syncschedule.TargetOrder(items[j].Target)
		leftID, _ := strconv.ParseInt(items[i].ID, 10, 64)
		rightID, _ := strconv.ParseInt(items[j].ID, 10, 64)
		return left < right || left == right && leftID < rightID
	})
	return items, nil
}

func (a *adminAPI) syncScheduleItem(schedule syncschedule.Schedule) (syncScheduleItem, error) {
	nextRuns, err := a.schedules.NextRuns(schedule.Definition)
	if err != nil || len(nextRuns) != 5 || schedule.ID <= 0 {
		return syncScheduleItem{}, errors.New("sync schedule preview failed")
	}
	return syncScheduleItem{
		ID: strconv.FormatInt(schedule.ID, 10), Target: schedule.Target, Revision: schedule.Revision, Enabled: schedule.Enabled,
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

func writeInvalidSyncSchedule(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, "invalid_sync_schedule", "Configuration de synchronisation invalide.")
}

func writeSyncScheduleNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "sync_schedule_not_found", "Planification de synchronisation introuvable.")
}

func writeSyncScheduleTargetUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "sync_schedule_target_unavailable", "Cette synchronisation n'est pas disponible.")
}

func writeSyncScheduleUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "sync_schedule_unavailable", "Planification des synchronisations indisponible.")
}

func writeSyncScheduleFailure(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "sync_schedule_failed", "La planification des synchronisations n'a pas pu être traitée.")
}
