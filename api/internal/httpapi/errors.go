package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"messeances/api/internal/schedule"
)

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeServiceError(w http.ResponseWriter, err error) {
	var validation *schedule.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, "invalid_query", validation.Message)
		return
	}
	var notFound *schedule.NotFoundError
	if errors.As(err, &notFound) {
		writeError(w, http.StatusNotFound, "not_found", notFound.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
