package httpapi

import (
	_ "embed"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// adminOpenAPISpec documents every route registered by routes. A test keeps
// both in sync; update the spec in the same change as any admin route.
//
//go:embed admin_openapi.yaml
var adminOpenAPISpec []byte

// routes registers the admin API under /api/v1/admin.
func (a *adminAPI) routes(router chi.Router) {
	router.Use(a.noStore)
	router.With(a.requireOrigin).Post("/login", a.login)
	router.Get("/session", a.session)
	router.With(adminAccountsPrivacy, a.authorize).Get("/accounts", a.adminAccounts)
	router.Group(func(router chi.Router) {
		router.Use(a.authorize)
		router.Get("/openapi.yaml", a.openAPISpec)
		router.Get("/theaters", a.adminTheaters)
		router.With(a.requireOrigin).Post("/theaters/{provider}/{providerTheaterID}/image", a.uploadTheaterImage)
		router.With(a.requireOrigin).Post("/theaters/{provider}/{providerTheaterID}/image/import", a.importTheaterImage)
		router.With(a.requireOrigin).Delete("/theaters/{provider}/{providerTheaterID}/image", a.removeTheaterImage)
		router.Get("/theaters/{provider}/{providerTheaterID}/image/{revision}", a.theaterImage)
		router.With(a.requireOrigin).Post("/logout", a.logout)
		router.Get("/tmdb-matches", a.pendingMatches)
		router.Get("/tmdb-search", a.searchTMDB)
		router.Get("/tmdb-search/{tmdbID}", a.tmdbMovieDetails)
		router.Get("/tmdb-matches/rerun", a.tmdbRerunStatus)
		router.With(a.requireOrigin).Post("/tmdb-matches/rerun", a.rerunTMDBMatches)
		router.Get("/tmdb-matches/refresh-metadata", a.tmdbMetadataRefreshStatus)
		router.With(a.requireOrigin).Post("/tmdb-matches/refresh-metadata", a.refreshTMDBMetadata)
		router.Get("/tmdb-upcoming-movies/sync", a.tmdbUpcomingStatus)
		router.Get("/tmdb-upcoming-movies", a.upcomingReviews)
		router.With(a.requireOrigin).Patch("/tmdb-upcoming-movies/{tmdbID}/decision", a.setUpcomingDecision)
		router.With(a.requireOrigin).Post("/tmdb-upcoming-movies/sync", a.syncTMDBUpcoming)
		router.Get("/local-movie-groups", a.localMovieGroups)
		router.Get("/movies", a.adminMovies)
		router.Get("/movies/{id}/posters", a.adminMoviePosters)
		router.With(a.requireOrigin).Patch("/movies/{id}", a.updateAdminMovie)
		router.With(a.requireOrigin).Post("/local-movie-groups", a.mergeLocalMovies)
		router.With(a.requireOrigin).Post("/local-movie-groups/{localMovieID}/members", a.addLocalMovieMembers)
		router.With(a.requireOrigin).Post("/local-movie-groups/{localMovieID}/unmerge", a.unmergeLocalMovie)
		router.With(a.requireOrigin).Post("/local-movie-groups/{localMovieID}/merge-groups", a.mergeLocalMovieGroups)
		router.Get("/syncs", a.syncStatus)
		router.With(a.requireOrigin).Post("/syncs/{target}", a.startSync)
		router.Get("/sync-sources", a.syncSources)
		router.With(a.requireOrigin).Post("/sync-sources/enabled", a.setSyncSources)
		router.With(a.requireOrigin).Patch("/sync-sources/{provider}/{key}", a.setSyncSource)
		router.Get("/sync-schedules", a.syncSchedules)
		router.With(a.requireOrigin).Post("/sync-schedules/{target}", a.createSyncSchedule)
		router.With(a.requireOrigin).Put("/sync-schedules/{target}/{id}", a.updateSyncSchedule)
		router.With(a.requireOrigin).Delete("/sync-schedules/{target}/{id}", a.deleteSyncSchedule)
		router.Get("/theater-locations", a.pendingTheaterLocations)
		router.Get("/theater-locations/geocoding-runs", a.theaterGeocodingStatus)
		router.With(a.requireOrigin).Post("/theater-locations/geocoding-runs", a.startTheaterGeocoding)
		router.With(a.requireOrigin).Post("/theater-locations/{provider}/{providerTheaterID}/accept-suggestion", a.acceptTheaterLocationSuggestion)
		router.With(a.requireOrigin).Post("/theater-locations/{provider}/{providerTheaterID}/manual", a.setManualTheaterLocation)
		router.With(a.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/approve", a.approveMatch)
		router.With(a.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/reject", a.rejectMatch)
		router.With(a.requireOrigin).Post("/tmdb-matches/{sourceProvider}/{sourceMovieID}/correct", a.correctMatch)
	})
}

func (a *adminAPI) openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(adminOpenAPISpec)
}
