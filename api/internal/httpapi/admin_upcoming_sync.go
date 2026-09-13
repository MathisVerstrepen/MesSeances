package httpapi

import (
	"errors"
	"net/http"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/syncschedule"
)

type upcomingSyncResponse struct {
	Job *enrichment.UpcomingStatus `json:"job"`
}

func (a *adminAPI) tmdbUpcomingStatus(w http.ResponseWriter, _ *http.Request) {
	if a.tmdbUpcoming == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_upcoming_sync_unavailable", "Service de synchronisation TMDB Prochainement indisponible.")
		return
	}
	writeJSON(w, http.StatusOK, upcomingSyncResponse{Job: a.tmdbUpcoming.Snapshot()})
}

func (a *adminAPI) syncTMDBUpcoming(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.tmdbUpcoming == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_upcoming_sync_unavailable", "Service de synchronisation TMDB Prochainement indisponible.")
		return
	}
	status, err := a.tmdbUpcoming.Start()
	if err != nil {
		switch {
		case errors.Is(err, syncschedule.ErrInProgress):
			writeError(w, http.StatusConflict, "tmdb_upcoming_sync_in_progress", "Une opération TMDB est déjà en cours.")
		case errors.Is(err, syncschedule.ErrTargetUnavailable):
			writeError(w, http.StatusServiceUnavailable, "tmdb_upcoming_sync_unavailable", "Service de synchronisation TMDB Prochainement indisponible.")
		default:
			writeError(w, http.StatusBadGateway, "tmdb_upcoming_sync_failed", "La synchronisation TMDB Prochainement a échoué.")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, upcomingSyncResponse{Job: &status})
}
