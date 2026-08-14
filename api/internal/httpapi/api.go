package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"movieflow/api/internal/schedule"
)

type API struct {
	schedule *schedule.Service
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewHandler(service *schedule.Service, webOrigin string) http.Handler {
	api := &API{schedule: service}
	router := chi.NewRouter()
	router.Use(jsonContentType)
	router.Use(recoverJSON)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{webOrigin},
		AllowedMethods: []string{http.MethodGet, http.MethodOptions},
		AllowedHeaders: []string{"Accept", "Content-Type"},
		MaxAge:         300,
	}))

	router.Get("/api/v1/timeline", api.timeline)
	router.Get("/api/v1/search/slot", api.searchSlot)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Ressource introuvable.")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Méthode non autorisée.")
	})

	return router
}

func (api *API) timeline(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	language := schedule.LanguageAll
	if query.Has("language") {
		language = query.Get("language")
	}

	var theaterIDs []string
	if query.Has("theaters") {
		theaters := query.Get("theaters")
		parts := strings.Split(theaters, ",")
		theaterIDs = make([]string, len(parts))
		for i, part := range parts {
			theaterIDs[i] = strings.TrimSpace(part)
		}
	}

	result, err := api.schedule.Timeline(schedule.TimelineQuery{
		Date:       query.Get("date"),
		TheaterIDs: theaterIDs,
		Language:   language,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) searchSlot(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	language := schedule.LanguageAll
	if query.Has("language") {
		language = query.Get("language")
	}

	buffer := 20
	if query.Has("buffer_ads") {
		rawBuffer := query.Get("buffer_ads")
		parsed, err := strconv.Atoi(rawBuffer)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre buffer_ads doit être un entier compris entre 0 et 120.")
			return
		}
		buffer = parsed
	}

	result, err := api.schedule.SearchSlot(schedule.SlotQuery{
		City:         query.Get("city"),
		Date:         query.Get("date"),
		StartAfter:   query.Get("start_after"),
		FinishBefore: query.Get("finish_before"),
		BufferAds:    buffer,
		Language:     language,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeServiceError(w http.ResponseWriter, err error) {
	var validation *schedule.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, "invalid_query", validation.Message)
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

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func recoverJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
