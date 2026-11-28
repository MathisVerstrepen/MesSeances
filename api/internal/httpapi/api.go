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

	"messeances/api/internal/cinemaimage"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/shortlink"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
	"messeances/api/internal/tmdb"
)

type API struct {
	schedule            *schedule.Service
	admin               *adminAPI
	shortlinks          ShortlinkService
	history             HistoryReader
	historyCache        *HistoryCache
	activity            ActivityReader
	publicTheaterImages PublicTheaterImageController
	sitemap             SitemapObserver
	origin              string
	openAPI             []byte
}

type ShortlinkService interface {
	Create(context.Context, string) (shortlink.Link, error)
	Resolve(context.Context, string) (shortlink.Link, error)
}

type HandlerOptions struct {
	Accounts             AccountOptions
	Admin                AdminOptions
	Readiness            ReadinessOptions
	Shortlinks           ShortlinkService
	History              HistoryReader
	HistoryCache         *HistoryCache
	Activity             ActivityReader
	PublicTheaterImages  PublicTheaterImageController
	Sitemap              SitemapObserver
	TrustedProxyCIDRs    []netip.Prefix
	InternalSharedSecret string
	RateLimitClock       func() time.Time
}

type AdminOptions struct {
	Password      string
	SessionSecret string
	// APIToken enables non-browser admin clients through an Authorization
	// bearer header. It is optional and only effective when admin is configured.
	APIToken         string
	TMDBSearch       AdminTMDBSearcher
	Reviews          *enrichment.ReviewService
	TMDBReruns       TMDBRerunner
	TMDBRefreshes    TMDBMetadataRefresher
	TMDBUpcoming     TMDBUpcomingSyncer
	UpcomingReviews  *enrichment.UpcomingReviewService
	LocalMovies      *enrichment.LocalMovieService
	Syncs            SyncController
	SyncSchedules    SyncScheduleController
	SyncSources      SyncSourceController
	TheaterLocations TheaterLocationController
	TheaterGeocoding TheaterGeocodingController
	TheaterImages    TheaterImageController
	Movies           *enrichment.AdminMovieService
	Accounts         AdminAccountsLister
	Now              func() time.Time
	Logger           *slog.Logger
	Metrics          *observability.Metrics
}

type AdminTMDBSearcher interface {
	Search(context.Context, string) ([]tmdb.Candidate, error)
	Details(context.Context, int64) (tmdb.Details, error)
}

type TMDBRerunner interface {
	Start() (enrichment.RerunStatus, error)
	Snapshot() *enrichment.RerunStatus
}

type TMDBMetadataRefresher interface {
	Start() (enrichment.MetadataRefreshStatus, error)
	Snapshot() *enrichment.MetadataRefreshStatus
}

type TMDBUpcomingSyncer interface {
	Start() (enrichment.UpcomingStatus, error)
	Snapshot() *enrichment.UpcomingStatus
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

type TheaterImageController interface {
	List(context.Context, cinemaimage.ListQuery) (cinemaimage.Inventory, error)
	Upload(context.Context, cinemaimage.Identity, func() (int64, []byte, string, error)) (cinemaimage.Result, error)
	Import(context.Context, cinemaimage.Identity, int64, string) (cinemaimage.Result, error)
	Remove(context.Context, cinemaimage.Identity, int64) (cinemaimage.Result, error)
	Read(context.Context, cinemaimage.Identity, int64) ([]byte, error)
}

type PublicTheaterImageController interface {
	PublicImage(context.Context, cinemaimage.Identity) (*cinemaimage.PublicImage, error)
	PublicImages(context.Context) (map[string]cinemaimage.PublicImage, error)
	Read(context.Context, cinemaimage.Identity, int64) ([]byte, error)
}

func NewHandler(service *schedule.Service, webOrigin string) http.Handler {
	return NewHandlerWithOptions(service, webOrigin, HandlerOptions{})
}

func NewHandlerWithAdmin(service *schedule.Service, webOrigin string, options AdminOptions) http.Handler {
	return NewHandlerWithOptions(service, webOrigin, HandlerOptions{Admin: options})
}

func NewHandlerWithOptions(service *schedule.Service, webOrigin string, options HandlerOptions) http.Handler {
	return accountBoundary(newRouter(service, webOrigin, options))
}

// newRouter registers every route; NewHandlerWithOptions adds the account boundary.
func newRouter(service *schedule.Service, webOrigin string, options HandlerOptions) *chi.Mux {
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
	openAPI, err := renderPublicOpenAPI(webOrigin)
	if err != nil {
		// The spec is embedded at build time and parsed by tests.
		panic(err)
	}
	api := &API{schedule: service, admin: newAdminAPI(webOrigin, options.Admin), shortlinks: options.Shortlinks, history: options.History, historyCache: options.HistoryCache, activity: options.Activity, publicTheaterImages: options.PublicTheaterImages, sitemap: options.Sitemap, origin: webOrigin, openAPI: openAPI}
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
	router.Use(accountAwareCORS(cors.Handler(cors.Options{
		AllowedOrigins:   []string{webOrigin},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	})))
	options.Accounts.schedule = service
	registerAccountRoutes(router, options.Accounts)

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
	router.Get("/api/v1/openapi.json", api.publicOpenAPI)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/timeline", api.timeline)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/statistics", api.statistics)
	router.With(noStoreHistory, expensiveReads).Get("/api/v1/statistics/history", api.historyStatistics)
	router.With(noStoreHistory, expensiveReads).Get("/api/v1/statistics/history/options", api.historyOptions)
	router.With(noStoreHistory, expensiveReads).Get("/api/v1/theaters/{slug}/activity", api.theaterActivity)
	router.With(noStoreHistory, expensiveReads).Post("/api/v1/theaters/activity-preview", api.theaterActivityPreview)
	router.With(noStorePublicTheaterImages, api.requireSchedule).Get("/api/v1/theaters", api.theaters)
	router.With(noStorePublicTheaterImages, api.requireSchedule, expensiveReads).Get("/api/v1/theaters/{slug}/showtimes", api.theaterShowtimes)
	router.With(noStorePublicTheaterImages, expensiveReads).Get("/api/v1/theaters/{provider}/{providerTheaterID}/image/{revision}", api.publicTheaterImage)
	router.With(api.requireSchedule).Get("/api/v1/cities", api.cities)
	router.With(api.requireSchedule).Get("/api/v1/cities/{slug}", api.city)
	router.With(api.requireCatalog, expensiveReads).Get("/api/v1/movies", api.movies)
	router.With(expensiveReads).Get("/api/v1/sitemap-data", api.sitemapData)
	router.With(api.requireCatalog, expensiveReads).Get("/api/v1/movies/upcoming", api.upcomingMovies)
	router.With(expensiveReads, api.refreshMissingMovie, api.requireCatalog).Get("/api/v1/movies/{slug}/showtimes", api.movieShowtimes)
	router.With(api.requireInternalService, expensiveReads, api.refreshMissingMovie, api.requireCatalog).Get("/api/v1/internal/movies/{slug}/showtimes-bundle", api.movieShowtimesBundle)
	router.With(api.requireSchedule, expensiveReads).Get("/api/v1/search/slot", api.searchSlot)
	router.With(api.noStoreShortlink, api.requireShortlinkOrigin, shortlinkCreations).Post("/api/v1/shortlinks", api.createShortlink)
	router.Get("/api/v1/shortlinks/{code}", api.resolveShortlink)
	router.Route("/api/v1/admin", api.admin.routes)
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
