package httpapi

import (
	"errors"
	"net/http"

	"messeances/api/internal/enrichment"
)

func (a *adminAPI) rerunTMDBMatches(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.tmdbReruns == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_rerun_unavailable", "Service de relance TMDB indisponible.")
		return
	}
	summary, err := a.tmdbReruns.Rerun(r.Context())
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
	writeJSON(w, http.StatusOK, summary)
}
