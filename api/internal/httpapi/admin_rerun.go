package httpapi

import (
	"errors"
	"net/http"

	"messeances/api/internal/enrichment"
)

type rerunResponse struct {
	Job *enrichment.RerunStatus `json:"job"`
}

func (a *adminAPI) tmdbRerunStatus(w http.ResponseWriter, _ *http.Request) {
	if a.tmdbReruns == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_rerun_unavailable", "Service de relance TMDB indisponible.")
		return
	}
	writeJSON(w, http.StatusOK, rerunResponse{Job: a.tmdbReruns.Snapshot()})
}

func (a *adminAPI) rerunTMDBMatches(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.tmdbReruns == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_rerun_unavailable", "Service de relance TMDB indisponible.")
		return
	}
	status, err := a.tmdbReruns.Start()
	if err != nil {
		switch {
		case errors.Is(err, enrichment.ErrRerunInProgress):
			writeError(w, http.StatusConflict, "tmdb_rerun_in_progress", "Une relance TMDB est déjà en cours.")
		case errors.Is(err, enrichment.ErrRerunUnavailable):
			writeError(w, http.StatusServiceUnavailable, "tmdb_rerun_unavailable", "Service de relance TMDB indisponible.")
		default:
			writeError(w, http.StatusBadGateway, "tmdb_rerun_failed", "La relance TMDB a échoué.")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, rerunResponse{Job: &status})
}
