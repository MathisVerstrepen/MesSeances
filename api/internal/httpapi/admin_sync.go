package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/synccontrol"
)

type syncResponse struct {
	Job  *synccontrol.Status  `json:"job"`
	Runs []synccontrol.Status `json:"runs"`
}

func (a *adminAPI) syncStatus(w http.ResponseWriter, _ *http.Request) {
	if a.syncs == nil {
		writeError(w, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
		return
	}
	status := a.syncs.Status()
	runs := syncRuns(a.syncs)
	if status.ID == "" {
		writeJSON(w, http.StatusOK, syncResponse{Runs: runs})
		return
	}
	writeJSON(w, http.StatusOK, syncResponse{Job: &status, Runs: runs})
}

func (a *adminAPI) startSync(w http.ResponseWriter, r *http.Request) {
	if a.syncs == nil {
		writeError(w, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
		return
	}
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	status, err := a.syncs.Start(synccontrol.Target(chi.URLParam(r, "target")))
	switch {
	case errors.Is(err, synccontrol.ErrInvalidTarget):
		writeError(w, http.StatusBadRequest, "invalid_sync_target", "Cible de synchronisation invalide.")
	case errors.Is(err, synccontrol.ErrInProgress):
		writeError(w, http.StatusConflict, "sync_in_progress", "Une synchronisation est déjà en cours.")
	case err != nil:
		writeError(w, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
	default:
		writeJSON(w, http.StatusAccepted, syncResponse{Job: &status, Runs: syncRuns(a.syncs)})
	}
}

func syncRuns(controller SyncController) []synccontrol.Status {
	runs := controller.Runs()
	if runs == nil {
		return []synccontrol.Status{}
	}
	return runs
}
