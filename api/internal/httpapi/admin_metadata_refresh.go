package httpapi

import (
	"errors"
	"net/http"

	"messeances/api/internal/enrichment"
)

func (a *adminAPI) refreshTMDBMetadata(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.tmdbRefreshes == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_metadata_refresh_unavailable", "Service d'actualisation TMDB indisponible.")
		return
	}
	summary, err := a.tmdbRefreshes.Refresh(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, enrichment.ErrMetadataRefreshInProgress):
			writeError(w, http.StatusConflict, "tmdb_metadata_refresh_in_progress", "Une opération TMDB est déjà en cours.")
		case errors.Is(err, enrichment.ErrMetadataRefreshUnavailable):
			writeError(w, http.StatusServiceUnavailable, "tmdb_metadata_refresh_unavailable", "Service d'actualisation TMDB indisponible.")
		default:
			writeError(w, http.StatusBadGateway, "tmdb_metadata_refresh_failed", "L'actualisation TMDB a échoué.")
		}
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
