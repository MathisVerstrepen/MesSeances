package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/synccontrol"
)

type syncResponse struct {
	Job *synccontrol.Status `json:"job"`
}

func (a *adminAPI) syncStatus(w http.ResponseWriter, _ *http.Request) {
	if a.syncs == nil {
		writeError(w, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
		return
	}
	status := a.syncs.Status()
	if status.ID == "" {
		writeJSON(w, http.StatusOK, syncResponse{})
		return
	}
	writeJSON(w, http.StatusOK, syncResponse{Job: &status})
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
		writeJSON(w, http.StatusAccepted, syncResponse{Job: &status})
	}
}
