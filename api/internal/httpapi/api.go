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
	"messeances/api/internal/geocoding"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/shortlink"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
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
	Readiness  ReadinessOptions
	Shortlinks ShortlinkService
}

type ReadinessOptions struct {
	Schedule  schedule.Source
	Database  DatabasePinger
	Revisions RevisionReader
	Now       func() time.Time
}

type DatabasePinger interface {
	Ping(context.Context) error
}

type RevisionReader interface {
	CurrentRevision(context.Context) (schedule.SnapshotRevision, error)
}

type AdminOptions struct {
	Password         string
	SessionSecret    string
	Reviews          *enrichment.ReviewService
	TMDBReruns       TMDBRerunner
	LocalMovies      *enrichment.LocalMovieService
	Syncs            SyncController
	SyncSchedules    SyncScheduleController
	TheaterLocations TheaterLocationController
	TheaterGeocoding TheaterGeocodingController
	Now              func() time.Time
	Logger           *slog.Logger
	Metrics          *observability.Metrics
}

type TMDBRerunner interface {
	Rerun(context.Context) (enrichment.RerunSummary, error)
}

type SyncController interface {
	Start(synccontrol.Target) (synccontrol.Status, error)
	Snapshot(context.Context) (synccontrol.Snapshot, error)
}

type SyncScheduleController interface {
	List(context.Context) ([]syncschedule.Schedule, error)
	Save(context.Context, synccontrol.Target, bool, syncschedule.Definition) (syncschedule.Schedule, error)
	NextRuns(syncschedule.Definition) ([]time.Time, error)
}

type TheaterLocationController interface {
	Pending(context.Context, int, int) ([]geocoding.PendingLocation, error)
	AcceptSuggestion(context.Context, string, string, time.Time) error
	SetManual(context.Context, string, string, time.Time, float64, float64) error
}

type TheaterGeocodingController interface {
	Start() (geocoding.RunStatus, error)
	Snapshot(context.Context) (*geocoding.RunStatus, error)
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

const readinessDatabaseTimeout = 250 * time.Millisecond

type readinessChecker struct {
	schedule  schedule.Source
	database  DatabasePinger
	revisions RevisionReader
	now       func() time.Time
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
	readiness := newReadinessChecker(options.Readiness)
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
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !readiness.ready(r.Context()) {
			writeJSON(w, http.StatusServiceUnavailable, probeResponse{Status: "not_ready"})
			return
		}
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
			router.Get("/sync-schedules", api.admin.syncSchedules)
			router.With(api.admin.requireOrigin).Post("/sync-schedules/{provider}", api.admin.saveSyncSchedule)
			router.Get("/theater-locations", api.admin.pendingTheaterLocations)
			router.Get("/theater-locations/geocoding-runs", api.admin.theaterGeocodingStatus)
			router.With(api.admin.requireOrigin).Post("/theater-locations/geocoding-runs", api.admin.startTheaterGeocoding)
			router.With(api.admin.requireOrigin).Post("/theater-locations/{provider}/{providerTheaterID}/accept-suggestion", api.admin.acceptTheaterLocationSuggestion)
			router.With(api.admin.requireOrigin).Post("/theater-locations/{provider}/{providerTheaterID}/manual", api.admin.setManualTheaterLocation)
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

func newReadinessChecker(options ReadinessOptions) readinessChecker {
	if options.Now == nil {
		options.Now = time.Now
	}
	return readinessChecker{
		schedule:  options.Schedule,
		database:  options.Database,
		revisions: options.Revisions,
		now:       options.Now,
	}
}

func (c readinessChecker) ready(ctx context.Context) bool {
	if c.schedule == nil || c.database == nil || c.revisions == nil {
		return false
	}
	view := c.schedule.Snapshot()
	if !view.ReadyAt(c.now()) {
		return false
	}
	databaseCtx, cancel := context.WithTimeout(ctx, readinessDatabaseTimeout)
	defer cancel()
	if c.database.Ping(databaseCtx) != nil {
		return false
	}
	revision, err := c.revisions.CurrentRevision(databaseCtx)
	if err != nil {
		return false
	}
	return revision.ScheduleVersion > 0 && revision.EnrichmentVersion >= 0 && revision.TheaterLocationVersion >= 0
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
	includeEnded := false
	if query.Has("include_ended") {
		rawValue := query.Get("include_ended")
		if !strings.EqualFold(rawValue, "true") && !strings.EqualFold(rawValue, "false") {
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre include_ended doit être true ou false.")
			return
		}
		includeEnded = strings.EqualFold(rawValue, "true")
	}
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
	var duration *schedule.MovieCatalogDuration
	if query.Has("duration") {
		value := schedule.MovieCatalogDuration(query.Get("duration"))
		duration = &value
	}
	var date *string
	if query.Has("date") {
		value := query.Get("date")
		date = &value
	}
	var dateTo *string
	if query.Has("date_to") {
		value := query.Get("date_to")
		dateTo = &value
	}

	result, err := api.schedule.Movies(schedule.MovieCatalogQuery{
		CurrentlyScreened: currentlyScreened,
		IncludeEnded:      includeEnded,
		Search:            query.Get("search"),
		Sort:              schedule.MovieCatalogSort(query.Get("sort")),
		TheaterIDs:        parseCSVQuery(query, "theaters"),
		Genres:            parseCSVQuery(query, "genres"),
		Duration:          duration,
		Date:              date,
		DateTo:            dateTo,
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
			writeError(w, http.StatusBadRequest, "invalid_query", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA, 4DX ou ICE.")
			return
		}
	}

	buffer := 15
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
