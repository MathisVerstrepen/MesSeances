package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/shortlink"
	"messeances/api/internal/synccontrol"
)

type API struct {
	schedule   *schedule.Service
	admin      *adminAPI
	shortlinks ShortlinkService
	origin     string
}

type ShortlinkService interface {
	Create(context.Context, string) (shortlink.Link, error)
	Resolve(context.Context, string) (shortlink.Link, error)
}

type HandlerOptions struct {
	Admin      AdminOptions
	Shortlinks ShortlinkService
}

type AdminOptions struct {
	Password      string
	SessionSecret string
	Reviews       *enrichment.ReviewService
	TMDBReruns    TMDBRerunner
	LocalMovies   *enrichment.LocalMovieService
	Syncs         SyncController
	Now           func() time.Time
	Logger        *slog.Logger
	Metrics       *observability.Metrics
}

type TMDBRerunner interface {
	Rerun(context.Context) (enrichment.RerunSummary, error)
}

type SyncController interface {
	Start(synccontrol.Target) (synccontrol.Status, error)
	Status() synccontrol.Status
	Runs() []synccontrol.Status
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type probeResponse struct {
	Status string `json:"status"`
}

func NewHandler(service *schedule.Service, webOrigin string) http.Handler {
	return NewHandlerWithOptions(service, webOrigin, HandlerOptions{})
}

func NewHandlerWithAdmin(service *schedule.Service, webOrigin string, options AdminOptions) http.Handler {
	return NewHandlerWithOptions(service, webOrigin, HandlerOptions{Admin: options})
}

func NewHandlerWithOptions(service *schedule.Service, webOrigin string, options HandlerOptions) http.Handler {
	if options.Admin.Logger == nil {
		options.Admin.Logger = observability.NewLogger(io.Discard)
	}
	if options.Admin.Metrics == nil {
		options.Admin.Metrics = observability.NewMetrics()
	}
	api := &API{schedule: service, admin: newAdminAPI(webOrigin, options.Admin), shortlinks: options.Shortlinks, origin: webOrigin}
	router := chi.NewRouter()
	router.Use(observability.HTTPMiddleware(options.Admin.Logger, options.Admin.Metrics))
	router.Use(jsonContentType)
	router.Use(recoverJSON(options.Admin.Logger))
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{webOrigin},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, probeResponse{Status: "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, probeResponse{Status: "ready"})
	})
	router.Get("/metrics", options.Admin.Metrics.Handler().ServeHTTP)
	router.Get("/api/v1/timeline", api.timeline)
	router.Get("/api/v1/theaters", api.theaters)
	router.Get("/api/v1/theaters/{slug}/showtimes", api.theaterShowtimes)
	router.Get("/api/v1/cities", api.cities)
	router.Get("/api/v1/cities/{slug}", api.city)
	router.Get("/api/v1/movies", api.movies)
	router.Get("/api/v1/movies/{slug}/showtimes", api.movieShowtimes)
	router.Get("/api/v1/search/slot", api.searchSlot)
	router.With(api.noStoreShortlink, api.requireShortlinkOrigin).Post("/api/v1/shortlinks", api.createShortlink)
	router.Get("/api/v1/shortlinks/{code}", api.resolveShortlink)
	router.Route("/api/v1/admin", func(router chi.Router) {
		router.Use(api.admin.noStore)
		router.With(api.admin.requireOrigin).Post("/login", api.admin.login)
		router.Get("/session", api.admin.session)
		router.Group(func(router chi.Router) {
			router.Use(api.admin.authorize)
			router.With(api.admin.requireOrigin).Post("/logout", api.admin.logout)
			router.Get("/tmdb-matches", api.admin.pendingMatches)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/rerun", api.admin.rerunTMDBMatches)
			router.Get("/local-movie-groups", api.admin.localMovieGroups)
			router.With(api.admin.requireOrigin).Post("/local-movie-groups", api.admin.mergeLocalMovies)
			router.With(api.admin.requireOrigin).Post("/local-movie-groups/{localMovieID}/unmerge", api.admin.unmergeLocalMovie)
			router.Get("/syncs", api.admin.syncStatus)
			router.With(api.admin.requireOrigin).Post("/syncs/{target}", api.admin.startSync)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/approve", api.admin.approveMatch)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/reject", api.admin.rejectMatch)
		})
	})
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
		language = schedule.Language(query.Get("language"))
	}

	result, err := api.schedule.Timeline(schedule.TimelineQuery{
		Date:       query.Get("date"),
		TheaterIDs: parseCSVQuery(query, "theaters"),
		Language:   language,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) theaters(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	writeJSON(w, http.StatusOK, api.schedule.Theaters(schedule.TheaterCatalogQuery{
		City:  query.Get("city"),
		Chain: schedule.Provider(query.Get("chain")),
	}))
}

func (api *API) theaterShowtimes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := api.schedule.TheaterShowtimes(schedule.TheaterShowtimesQuery{Slug: chi.URLParam(r, "slug"), Date: query.Get("date"), DateProvided: query.Has("date")})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) cities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.schedule.Cities())
}

func (api *API) city(w http.ResponseWriter, r *http.Request) {
	result, err := api.schedule.City(chi.URLParam(r, "slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) movies(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var currentlyScreened *bool
	if query.Has("currently_screened") {
		rawValue := query.Get("currently_screened")
		if !strings.EqualFold(rawValue, "true") && !strings.EqualFold(rawValue, "false") {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre currently_screened doit être true ou false.")
			return
		}
		value := strings.EqualFold(rawValue, "true")
		currentlyScreened = &value
	}

	page, ok := parsePositiveInteger(w, query, "page", "Le paramètre page doit être un entier supérieur ou égal à 1.")
	if !ok {
		return
	}
	pageSize, ok := parsePositiveInteger(w, query, "page_size", "Le paramètre page_size doit être un entier compris entre 1 et 100.")
	if !ok {
		return
	}

	result, err := api.schedule.Movies(schedule.MovieCatalogQuery{
		CurrentlyScreened: currentlyScreened,
		Search:            query.Get("search"),
		Sort:              schedule.MovieCatalogSort(query.Get("sort")),
		TheaterIDs:        parseCSVQuery(query, "theaters"),
		Page:              page,
		PageSize:          pageSize,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) movieShowtimes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := api.schedule.MovieShowtimes(schedule.MovieShowtimesQuery{
		Slug:       chi.URLParam(r, "slug"),
		Date:       query.Get("date"),
		City:       query.Get("city"),
		TheaterIDs: parseCSVQuery(query, "theaters"),
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
		language = schedule.Language(query.Get("language"))
	}
	format := schedule.FormatAll
	if query.Has("format") {
		format = schedule.Format(query.Get("format"))
		if format == "" {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA ou 4DX.")
			return
		}
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
	includeAds := true
	if query.Has("include_ads") {
		rawValue := query.Get("include_ads")
		if !strings.EqualFold(rawValue, "true") && !strings.EqualFold(rawValue, "false") {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre include_ads doit être true ou false.")
			return
		}
		includeAds = strings.EqualFold(rawValue, "true")
	}

	result, err := api.schedule.SearchSlot(schedule.SlotQuery{
		City:         query.Get("city"),
		TheaterIDs:   parseCSVQuery(query, "theaters"),
		Date:         query.Get("date"),
		StartAfter:   query.Get("start_after"),
		FinishBefore: query.Get("finish_before"),
		BufferAds:    buffer,
		IncludeAds:   includeAds,
		Language:     language,
		Format:       format,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseCSVQuery(query mapQuery, key string) []string {
	if !query.Has(key) {
		return nil
	}
	parts := strings.Split(query.Get(key), ",")
	values := make([]string, len(parts))
	for i, part := range parts {
		values[i] = strings.TrimSpace(part)
	}
	return values
}

type mapQuery interface {
	Get(string) string
	Has(string) bool
}

func parsePositiveInteger(w http.ResponseWriter, query mapQuery, key, message string) (int, bool) {
	if !query.Has(key) {
		return 0, true
	}
	value, err := strconv.Atoi(query.Get(key))
	if err != nil || value < 1 {
		writeError(w, http.StatusBadRequest, "invalid_query", message)
		return 0, false
	}
	return value, true
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

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func recoverJSON(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recover() != nil {
					logger.ErrorContext(r.Context(), "http_panic_recovered", "component", "http", "error_code", "internal_failure")
					writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
