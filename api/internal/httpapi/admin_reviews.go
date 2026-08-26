package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/enrichment"
)

func (a *adminAPI) pendingMatches(w http.ResponseWriter, r *http.Request) {
	filter := enrichment.PendingMatchFilterUnresolved
	if values, exists := r.URL.Query()["status"]; exists {
		if len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_query", "Filtre de statut invalide.")
			return
		}
		switch values[0] {
		case string(enrichment.PendingMatchFilterUnresolved):
			filter = enrichment.PendingMatchFilterUnresolved
		case string(enrichment.PendingMatchFilterRejected):
			filter = enrichment.PendingMatchFilterRejected
		default:
			writeError(w, http.StatusBadRequest, "invalid_query", "Filtre de statut invalide.")
			return
		}
	}
	limit, offset := 50, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := r.URL.Query().Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
		}
	}
	if err != nil || limit < 1 || limit > 100 || offset < 0 {
		writeError(w, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
		return
	}
	items, err := a.reviews.Pending(r.Context(), filter, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items  []enrichment.PendingMatch `json:"items"`
		Limit  int                       `json:"limit"`
		Offset int                       `json:"offset"`
	}{Items: items, Limit: limit, Offset: offset})
}

func (a *adminAPI) approveMatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TMDBID int64 `json:"tmdb_id"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil || input.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	err := a.reviews.Approve(r.Context(), chi.URLParam(r, "sourceProvider"), chi.URLParam(r, "sourceMovieID"), input.TMDBID)
	if err != nil {
		a.writeReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": enrichment.StatusMatched})
}

func (a *adminAPI) rejectMatch(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	err := a.reviews.Reject(r.Context(), chi.URLParam(r, "sourceProvider"), chi.URLParam(r, "sourceMovieID"))
	if err != nil {
		a.writeReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": enrichment.StatusRejected})
}

func (a *adminAPI) writeReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enrichment.ErrReviewNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Correspondance introuvable.")
	case errors.Is(err, enrichment.ErrReviewConflict):
		writeError(w, http.StatusConflict, "review_conflict", "Cette correspondance ne peut plus être modifiée.")
	case errors.Is(err, enrichment.ErrReviewUnavailable):
		writeError(w, http.StatusServiceUnavailable, "review_unavailable", "Service de validation indisponible.")
	default:
		writeError(w, http.StatusBadGateway, "review_failed", "La correspondance n'a pas pu être modifiée.")
	}
}
