package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
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
	Admin                AdminOptions
	Readiness            ReadinessOptions
	Shortlinks           ShortlinkService
	TrustedProxyCIDRs    []netip.Prefix
	InternalSharedSecret string
	RateLimitClock       func() time.Time
}

type AdminOptions struct {
	Password         string
	SessionSecret    string
	Reviews          *enrichment.ReviewService
	TMDBReruns       TMDBRerunner
	TMDBRefreshes    TMDBMetadataRefresher
	LocalMovies      *enrichment.LocalMovieService
	Syncs            SyncController
	SyncSchedules    SyncScheduleController
	TheaterLocations TheaterLocationController
	TheaterGeocoding TheaterGeocodingController
	Movies           *enrichment.AdminMovieService
	Now              func() time.Time
	Logger           *slog.Logger
	Metrics          *observability.Metrics
}

type TMDBRerunner interface {
	Rerun(context.Context) (enrichment.RerunSummary, error)
}

type TMDBMetadataRefresher interface {
	Start() (enrichment.MetadataRefreshStatus, error)
	Snapshot() *enrichment.MetadataRefreshStatus
}

type SyncController interface {
	Start(synccontrol.Target) (synccontrol.Status, error)
	Snapshot(context.Context) (synccontrol.Snapshot, error)
}

type SyncScheduleController interface {
	List(context.Context) ([]syncschedule.Schedule, error)
	AvailableTargets() []syncschedule.Target
	Create(context.Context, syncschedule.Target, bool, syncschedule.Definition) (syncschedule.Schedule, error)
	Update(context.Context, syncschedule.Target, int64, bool, syncschedule.Definition) (syncschedule.Schedule, error)
	Delete(context.Context, syncschedule.Target, int64) error
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
	if options.RateLimitClock == nil {
		options.RateLimitClock = time.Now
	}
	api := &API{schedule: service, admin: newAdminAPI(webOrigin, options.Admin), shortlinks: options.Shortlinks, origin: webOrigin}
	clients := newClientIdentifier(options.TrustedProxyCIDRs)
	authenticator := newInternalServiceAuthenticator(options.InternalSharedSecret)
	publicExpensiveReads := newTokenBucketLimiter(expensiveReadBurst, expensiveReadRefillRate, expensiveReadIdleHorizon, maxRateLimitClients, options.RateLimitClock)
	internalExpensiveReads := newTokenBucketLimiter(internalExpensiveReadBurst, internalExpensiveReadRefillRate, internalExpensiveReadIdleHorizon, internalRateLimitClients, options.RateLimitClock)
	expensiveReads := expensiveReadRateLimit(publicExpensiveReads, internalExpensiveReads)
	shortlinkCreations := rateLimit(newTokenBucketLimiter(shortlinkCreationBurst, shortlinkCreationRefillRate, shortlinkCreationIdleHorizon, maxRateLimitClients, options.RateLimitClock), func(r *http.Request) string {
		return requestIdentityFromContext(r.Context()).publicKey
	})
	router := chi.NewRouter()
	router.Use(requestMetadata(clients, authenticator))
	router.Use(observability.HTTPMiddleware(options.Admin.Logger, options.Admin.Metrics))
	router.Use(jsonContentType)
	router.Use(recoverJSON(options.Admin.Logger))
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{webOrigin},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
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
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/timeline", api.timeline)
	router.With(api.requireSchedule).Get("/api/v1/theaters", api.theaters)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/theaters/{slug}/showtimes", api.theaterShowtimes)
	router.With(api.requireSchedule).Get("/api/v1/cities", api.cities)
	router.With(api.requireSchedule).Get("/api/v1/cities/{slug}", api.city)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/movies", api.movies)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/movies/{slug}/showtimes", api.movieShowtimes)
	router.With(api.requireInternalService, api.requireSchedule, expensiveReads).Get("/api/v1/internal/movies/{slug}/showtimes-bundle", api.movieShowtimesBundle)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/search/slot", api.searchSlot)
	router.With(api.noStoreShortlink, api.requireShortlinkOrigin, shortlinkCreations).Post("/api/v1/shortlinks", api.createShortlink)
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
			router.Get("/tmdb-matches/refresh-metadata", api.admin.tmdbMetadataRefreshStatus)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/refresh-metadata", api.admin.refreshTMDBMetadata)
			router.Get("/local-movie-groups", api.admin.localMovieGroups)
			router.Get("/movies", api.admin.adminMovies)
			router.With(api.admin.requireOrigin).Patch("/movies/{id}", api.admin.updateAdminMovie)
			router.With(api.admin.requireOrigin).Post("/local-movie-groups", api.admin.mergeLocalMovies)
			router.With(api.admin.requireOrigin).Post("/local-movie-groups/{localMovieID}/members", api.admin.addLocalMovieMembers)
			router.With(api.admin.requireOrigin).Post("/local-movie-groups/{localMovieID}/unmerge", api.admin.unmergeLocalMovie)
			router.Get("/syncs", api.admin.syncStatus)
			router.With(api.admin.requireOrigin).Post("/syncs/{target}", api.admin.startSync)
			router.Get("/sync-schedules", api.admin.syncSchedules)
			router.With(api.admin.requireOrigin).Post("/sync-schedules/{target}", api.admin.createSyncSchedule)
			router.With(api.admin.requireOrigin).Put("/sync-schedules/{target}/{id}", api.admin.updateSyncSchedule)
			router.With(api.admin.requireOrigin).Delete("/sync-schedules/{target}/{id}", api.admin.deleteSyncSchedule)
			router.Get("/theater-locations", api.admin.pendingTheaterLocations)
			router.Get("/theater-locations/geocoding-runs", api.admin.theaterGeocodingStatus)
			router.With(api.admin.requireOrigin).Post("/theater-locations/geocoding-runs", api.admin.startTheaterGeocoding)
			router.With(api.admin.requireOrigin).Post("/theater-locations/{provider}/{providerTheaterID}/accept-suggestion", api.admin.acceptTheaterLocationSuggestion)
			router.With(api.admin.requireOrigin).Post("/theater-locations/{provider}/{providerTheaterID}/manual", api.admin.setManualTheaterLocation)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/approve", api.admin.approveMatch)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/reject", api.admin.rejectMatch)
			router.With(api.admin.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/correct", api.admin.correctMatch)
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
