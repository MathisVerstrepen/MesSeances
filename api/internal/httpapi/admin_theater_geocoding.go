package httpapi

import (
	"errors"
	"net/http"

	"messeances/api/internal/geocoding"
)

type theaterGeocodingResponse struct {
	Job *geocoding.RunStatus `json:"job"`
}

func (a *adminAPI) theaterGeocodingStatus(w http.ResponseWriter, r *http.Request) {
	if a.geocoding == nil {
		writeError(w, http.StatusServiceUnavailable, "theater_geocoding_unavailable", "Service de géocodage indisponible.")
		return
	}
	status, err := a.geocoding.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "theater_geocoding_failed", "L'état du géocodage n'a pas pu être chargé.")
		return
	}
	writeJSON(w, http.StatusOK, theaterGeocodingResponse{Job: status})
}

func (a *adminAPI) startTheaterGeocoding(w http.ResponseWriter, r *http.Request) {
	if a.geocoding == nil {
		writeError(w, http.StatusServiceUnavailable, "theater_geocoding_unavailable", "Service de géocodage indisponible.")
		return
	}
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	status, err := a.geocoding.Start()
	switch {
	case errors.Is(err, geocoding.ErrRunInProgress):
		writeError(w, http.StatusConflict, "theater_geocoding_in_progress", "Un géocodage est déjà en cours.")
	case err != nil:
		writeError(w, http.StatusBadGateway, "theater_geocoding_failed", "Le géocodage n'a pas pu démarrer.")
	default:
		writeJSON(w, http.StatusAccepted, theaterGeocodingResponse{Job: &status})
	}
}
