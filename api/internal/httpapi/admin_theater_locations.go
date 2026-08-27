package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/geocoding"
)

type theaterLocationSuggestionResponse struct {
	Label      string   `json:"label"`
	Score      float64  `json:"score"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
	PostalCode *string  `json:"postal_code"`
	City       *string  `json:"city"`
	Type       *string  `json:"type"`
}

type theaterLocationResponse struct {
	Provider            string                             `json:"provider"`
	ProviderTheaterID   string                             `json:"provider_theater_id"`
	TheaterID           string                             `json:"theater_id"`
	Name                string                             `json:"name"`
	Address             string                             `json:"address"`
	PostalCode          string                             `json:"postal_code"`
	City                string                             `json:"city"`
	Status              geocoding.Status                   `json:"status"`
	UpdatedAt           time.Time                          `json:"updated_at"`
	Suggestion          *theaterLocationSuggestionResponse `json:"suggestion"`
	CanAcceptSuggestion bool                               `json:"can_accept_suggestion"`
}

type theaterLocationsResponse struct {
	Items  []theaterLocationResponse `json:"items"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

type acceptTheaterLocationRequest struct {
	ExpectedUpdatedAt string `json:"expected_updated_at"`
}

type manualTheaterLocationRequest struct {
	ExpectedUpdatedAt string   `json:"expected_updated_at"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
}

func (a *adminAPI) pendingTheaterLocations(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parseTheaterLocationPagination(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
		return
	}
	if a.locations == nil {
		a.writeTheaterLocationError(w, geocoding.ErrResolutionUnavailable)
		return
	}
	items, err := a.locations.Pending(r.Context(), limit, offset)
	if err != nil {
		a.writeTheaterLocationError(w, err)
		return
	}
	responseItems := make([]theaterLocationResponse, len(items))
	for index, item := range items {
		responseItems[index] = theaterLocationResponse{
			Provider: item.Provider, ProviderTheaterID: item.ProviderTheaterID, TheaterID: item.TheaterID,
			Name: item.Name, Address: item.Address, PostalCode: item.PostalCode, City: item.City,
			Status: item.Status, UpdatedAt: item.UpdatedAt, CanAcceptSuggestion: item.CanAcceptSuggestion,
		}
		if item.Suggestion != nil {
			responseItems[index].Suggestion = &theaterLocationSuggestionResponse{
				Label: item.Suggestion.Label, Score: item.Suggestion.Score,
				Latitude: item.Suggestion.Latitude, Longitude: item.Suggestion.Longitude,
				PostalCode: item.Suggestion.PostalCode, City: item.Suggestion.City, Type: item.Suggestion.Type,
			}
		}
	}
	writeJSON(w, http.StatusOK, theaterLocationsResponse{Items: responseItems, Limit: limit, Offset: offset})
}

func (a *adminAPI) acceptTheaterLocationSuggestion(w http.ResponseWriter, r *http.Request) {
	provider, providerTheaterID, ok := theaterLocationPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	var input acceptTheaterLocationRequest
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	expectedUpdatedAt, err := time.Parse(time.RFC3339Nano, input.ExpectedUpdatedAt)
	if err != nil || expectedUpdatedAt.IsZero() {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.locations == nil {
		a.writeTheaterLocationError(w, geocoding.ErrResolutionUnavailable)
		return
	}
	if err := a.locations.AcceptSuggestion(r.Context(), provider, providerTheaterID, expectedUpdatedAt); err != nil {
		a.writeTheaterLocationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(geocoding.StatusManual)})
}

func (a *adminAPI) setManualTheaterLocation(w http.ResponseWriter, r *http.Request) {
	provider, providerTheaterID, ok := theaterLocationPath(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	var input manualTheaterLocationRequest
	if err := decodeAdminJSON(w, r, &input); err != nil || input.Latitude == nil || input.Longitude == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	expectedUpdatedAt, err := time.Parse(time.RFC3339Nano, input.ExpectedUpdatedAt)
	if err != nil || expectedUpdatedAt.IsZero() || !validAdminCoordinates(*input.Latitude, *input.Longitude) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.locations == nil {
		a.writeTheaterLocationError(w, geocoding.ErrResolutionUnavailable)
		return
	}
	if err := a.locations.SetManual(r.Context(), provider, providerTheaterID, expectedUpdatedAt, *input.Latitude, *input.Longitude); err != nil {
		a.writeTheaterLocationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(geocoding.StatusManual)})
}

func parseTheaterLocationPagination(r *http.Request) (int, int, bool) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "limit" && key != "offset") || len(values) != 1 || values[0] == "" {
			return 0, 0, false
		}
	}
	limit, offset := 20, 0
	var err error
	if raw, exists := query["limit"]; exists {
		limit, err = strconv.Atoi(raw[0])
	}
	if err == nil {
		if raw, exists := query["offset"]; exists {
			offset, err = strconv.Atoi(raw[0])
		}
	}
	return limit, offset, err == nil && limit >= 1 && limit <= 100 && offset >= 0
}

func theaterLocationPath(r *http.Request) (string, string, bool) {
	provider, providerTheaterID := chi.URLParam(r, "provider"), chi.URLParam(r, "providerTheaterID")
	return provider, providerTheaterID, geocoding.ValidProviderTheaterID(provider, providerTheaterID)
}

func validAdminCoordinates(latitude, longitude float64) bool {
	return latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func (a *adminAPI) writeTheaterLocationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, geocoding.ErrResolutionInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	case errors.Is(err, geocoding.ErrResolutionNotFound):
		writeError(w, http.StatusNotFound, "theater_location_not_found", "Localisation de cinéma introuvable.")
	case errors.Is(err, geocoding.ErrResolutionConflict):
		writeError(w, http.StatusConflict, "theater_location_conflict", "Cette localisation a changé ou la suggestion n'est plus disponible.")
	case errors.Is(err, geocoding.ErrResolutionUnavailable):
		writeError(w, http.StatusServiceUnavailable, "theater_location_unavailable", "Service de localisation indisponible.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
	}
}
