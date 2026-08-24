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

func (a *adminAPI) syncStatus(w http.ResponseWriter, r *http.Request) {
	if a.syncs == nil {
		writeError(w, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
		return
	}
	snapshot, err := a.syncs.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "sync_failed", "L'état des synchronisations n'a pas pu être chargé.")
		return
	}
	writeJSON(w, http.StatusOK, syncResponse{Job: snapshot.Job, Runs: nonNilSyncRuns(snapshot.Runs)})
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
	target := synccontrol.Target(chi.URLParam(r, "target"))
	if !synccontrol.ValidTarget(target) {
		writeError(w, http.StatusBadRequest, "invalid_sync_target", "Cible de synchronisation invalide.")
		return
	}
	snapshot, err := a.syncs.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
		return
	}
	status, err := a.syncs.Start(target)
	switch {
	case errors.Is(err, synccontrol.ErrInvalidTarget):
		writeError(w, http.StatusBadRequest, "invalid_sync_target", "Cible de synchronisation invalide.")
	case errors.Is(err, synccontrol.ErrInProgress):
		writeError(w, http.StatusConflict, "sync_in_progress", "Une synchronisation est déjà en cours.")
	case err != nil:
		writeError(w, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
	default:
		writeJSON(w, http.StatusAccepted, syncResponse{Job: &status, Runs: nonNilSyncRuns(snapshot.Runs)})
	}
}

func nonNilSyncRuns(runs []synccontrol.Status) []synccontrol.Status {
	if runs == nil {
		return []synccontrol.Status{}
	}
	return runs
}
