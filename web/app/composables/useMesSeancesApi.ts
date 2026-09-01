import type {
  AdminAcceptTheaterLocationSuggestionRequest,
  AdminAddLocalMovieMembersRequest,
  AdminAddLocalMovieMembersResponse,
  AdminCreateLocalMovieGroupRequest,
  AdminCorrectMatchRequest,
  AdminLocalMovieGroup,
  AdminLocalMovieGroupsResponse,
  AdminMatchDecisionResponse,
  AdminMovieItem,
  AdminMoviePatchRequest,
  AdminMoviesQuery,
  AdminMoviesResponse,
  AdminPendingMatchesFilter,
  AdminPendingMatchesQuery,
  AdminPendingMatchesResponse,
  AdminSaveSyncScheduleRequest,
  AdminSessionResponse,
  AdminSyncScheduleItem,
  AdminSyncScheduleTarget,
  AdminSyncSchedulesResponse,
  AdminSyncResponse,
  AdminSyncTarget,
  AdminSetManualTheaterLocationRequest,
  AdminTheaterGeocodingResponse,
  AdminTheaterLocationResolutionResponse,
  AdminTheaterLocationsResponse,
  AdminTMDBMetadataRefreshResponse,
  AdminTMDBRerunSummary,
  AdminUnmergeLocalMovieResponse,
  ApiErrorResponse,
  CitiesResponse,
  CityDetailResponse,
  MoviesQuery,
  MoviesResponse,
  MovieShowtimesBundleResponse,
  MovieShowtimesQuery,
  MovieShowtimesResponse,
  Provider,
  SlotQuery,
  SlotResult,
  ShortLinkResponse,
  Theater,
  TheaterShowtimesResponse,
  TheaterQuery,
  TimelineQuery,
  TimelineResponse
} from '~/types/api'

function queryValues<T extends object>(query: T) {
  return Object.fromEntries(Object.entries(query).filter((entry): entry is [string, string | number | boolean] => entry[1] !== undefined))
}

export function useMesSeancesApi() {
  const config = useRuntimeConfig()
  const apiBase = (import.meta.server ? config.apiBase : config.public.apiBase).replace(/\/$/, '')
  const requestEvent = import.meta.server ? useRequestEvent() : undefined
  const hasInternalApiIdentity = import.meta.server && config.internalApiSharedSecret !== ''
  const apiFetch = import.meta.server
    ? $fetch.create({
        async onRequest({ options }) {
          if (!requestEvent) return
          const { internalApiHeaders } = await import('~~/server/utils/internalApi')
          const headers = new Headers(options.headers)
          for (const [name, value] of Object.entries(internalApiHeaders(requestEvent, config.internalApiSharedSecret))) headers.set(name, value)
          options.headers = headers
        }
      })
    : $fetch

  async function withAdminRedirect<T>(request: Promise<T>): Promise<T> {
    try {
      return await request
    } catch (error) {
      if (getApiErrorStatus(error) === 401 && import.meta.client && useRoute().path !== '/admin/login') {
        await navigateTo('/admin/login')
      }
      throw error
    }
  }

  return {
    hasInternalApiIdentity,
    timeline(query: TimelineQuery) {
      return apiFetch<TimelineResponse>(`${apiBase}/api/v1/timeline`, { query: queryValues(query) })
    },
    searchSlot(query: SlotQuery) {
      return apiFetch<SlotResult[]>(`${apiBase}/api/v1/search/slot`, { query: queryValues(query) })
    },
    theaters(query: TheaterQuery = {}) {
      return apiFetch<Theater[]>(`${apiBase}/api/v1/theaters`, { query: queryValues(query) })
    },
    cities() {
      return apiFetch<CitiesResponse>(`${apiBase}/api/v1/cities`)
    },
    city(slug: string) {
      return apiFetch<CityDetailResponse>(`${apiBase}/api/v1/cities/${encodeURIComponent(slug)}`)
    },
    theaterShowtimes(slug: string, date?: string) {
      return apiFetch<TheaterShowtimesResponse>(`${apiBase}/api/v1/theaters/${encodeURIComponent(slug)}/showtimes`, {
        query: date ? { date } : undefined
      })
    },
    movies(query: MoviesQuery = {}) {
      return apiFetch<MoviesResponse>(`${apiBase}/api/v1/movies`, { query: queryValues(query) })
    },
    movieShowtimes(slug: string, query: MovieShowtimesQuery) {
      return apiFetch<MovieShowtimesResponse>(`${apiBase}/api/v1/movies/${encodeURIComponent(slug)}/showtimes`, { query: queryValues(query) })
    },
    movieShowtimesBundle(slug: string, date: string) {
      if (!hasInternalApiIdentity) throw new Error('Internal API identity unavailable')
      return apiFetch<MovieShowtimesBundleResponse>(`${apiBase}/api/v1/internal/movies/${encodeURIComponent(slug)}/showtimes-bundle`, {
        query: { date, city: 'Paris' },
        retry: false
      })
    },
    createShortLink(target: string) {
      return apiFetch<ShortLinkResponse>(`${apiBase}/api/v1/shortlinks`, {
        method: 'POST',
        body: { target }
      })
    },
    adminSession() {
      return apiFetch<AdminSessionResponse>(`${apiBase}/api/v1/admin/session`, { credentials: 'include' })
    },
    adminMovies(query: AdminMoviesQuery, signal?: AbortSignal) {
      return withAdminRedirect(apiFetch<AdminMoviesResponse>(`${apiBase}/api/v1/admin/movies`, {
        credentials: 'include',
        query: queryValues(query),
        signal
      }))
    },
    adminUpdateMovie(id: string, input: AdminMoviePatchRequest) {
      return withAdminRedirect(apiFetch<AdminMovieItem>(`${apiBase}/api/v1/admin/movies/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        credentials: 'include',
        body: input
      }))
    },
    adminLogin(password: string) {
      return apiFetch<AdminSessionResponse>(`${apiBase}/api/v1/admin/login`, {
        method: 'POST',
        credentials: 'include',
        body: { password }
      })
    },
    adminLogout() {
      return withAdminRedirect(apiFetch<AdminSessionResponse>(`${apiBase}/api/v1/admin/logout`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminPendingMatches(status: AdminPendingMatchesFilter, limit: number, offset: number, search?: string) {
      const normalizedSearch = search?.trim()
      const query: AdminPendingMatchesQuery = {
        status,
        limit,
        offset
      }
      if (status === 'matched' && normalizedSearch) query.search = normalizedSearch
      return withAdminRedirect(apiFetch<AdminPendingMatchesResponse>(`${apiBase}/api/v1/admin/tmdb-matches`, {
        credentials: 'include',
        query
      }))
    },
    adminApproveMatch(sourceProvider: Provider, sourceMovieId: string, tmdbId: number) {
      return withAdminRedirect(apiFetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceProvider)}/${encodeURIComponent(sourceMovieId)}/approve`, {
        method: 'POST',
        credentials: 'include',
        body: { tmdb_id: tmdbId }
      }))
    },
    adminRejectMatch(sourceProvider: Provider, sourceMovieId: string) {
      return withAdminRedirect(apiFetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceProvider)}/${encodeURIComponent(sourceMovieId)}/reject`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminCorrectMatch(sourceProvider: Provider, sourceMovieId: string, input: AdminCorrectMatchRequest) {
      return withAdminRedirect(apiFetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceProvider)}/${encodeURIComponent(sourceMovieId)}/correct`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminRerunTMDBMatches() {
      return withAdminRedirect(apiFetch<AdminTMDBRerunSummary>(`${apiBase}/api/v1/admin/tmdb-matches/rerun`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminRefreshTMDBMetadata() {
      return withAdminRedirect(apiFetch<AdminTMDBMetadataRefreshResponse>(`${apiBase}/api/v1/admin/tmdb-matches/refresh-metadata`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminTMDBMetadataRefreshStatus() {
      return withAdminRedirect(apiFetch<AdminTMDBMetadataRefreshResponse>(`${apiBase}/api/v1/admin/tmdb-matches/refresh-metadata`, {
        credentials: 'include'
      }))
    },
    adminLocalMovieGroups(limit: number, offset: number) {
      return withAdminRedirect(apiFetch<AdminLocalMovieGroupsResponse>(`${apiBase}/api/v1/admin/local-movie-groups`, {
        credentials: 'include',
        query: { limit, offset }
      }))
    },
    adminCreateLocalMovieGroup(input: AdminCreateLocalMovieGroupRequest) {
      return withAdminRedirect(apiFetch<AdminLocalMovieGroup>(`${apiBase}/api/v1/admin/local-movie-groups`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminAddLocalMovieMembers(localMovieId: string, input: AdminAddLocalMovieMembersRequest) {
      return withAdminRedirect(apiFetch<AdminAddLocalMovieMembersResponse>(`${apiBase}/api/v1/admin/local-movie-groups/${encodeURIComponent(localMovieId)}/members`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminUnmergeLocalMovie(localMovieId: string) {
      return withAdminRedirect(apiFetch<AdminUnmergeLocalMovieResponse>(`${apiBase}/api/v1/admin/local-movie-groups/${encodeURIComponent(localMovieId)}/unmerge`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminTheaterLocations(limit: number, offset: number) {
      return withAdminRedirect(apiFetch<AdminTheaterLocationsResponse>(`${apiBase}/api/v1/admin/theater-locations`, {
        credentials: 'include',
        query: { limit, offset }
      }))
    },
    adminAcceptTheaterLocationSuggestion(provider: Provider, providerTheaterId: string, input: AdminAcceptTheaterLocationSuggestionRequest) {
      return withAdminRedirect(apiFetch<AdminTheaterLocationResolutionResponse>(`${apiBase}/api/v1/admin/theater-locations/${encodeURIComponent(provider)}/${encodeURIComponent(providerTheaterId)}/accept-suggestion`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminSetManualTheaterLocation(provider: Provider, providerTheaterId: string, input: AdminSetManualTheaterLocationRequest) {
      return withAdminRedirect(apiFetch<AdminTheaterLocationResolutionResponse>(`${apiBase}/api/v1/admin/theater-locations/${encodeURIComponent(provider)}/${encodeURIComponent(providerTheaterId)}/manual`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminTheaterGeocodingStatus() {
      return withAdminRedirect(apiFetch<AdminTheaterGeocodingResponse>(`${apiBase}/api/v1/admin/theater-locations/geocoding-runs`, {
        credentials: 'include'
      }))
    },
    adminStartTheaterGeocoding() {
      return withAdminRedirect(apiFetch<AdminTheaterGeocodingResponse>(`${apiBase}/api/v1/admin/theater-locations/geocoding-runs`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminSyncStatus() {
      return withAdminRedirect(apiFetch<AdminSyncResponse>(`${apiBase}/api/v1/admin/syncs`, {
        credentials: 'include'
      }))
    },
    adminSyncSchedules() {
      return withAdminRedirect(apiFetch<AdminSyncSchedulesResponse>(`${apiBase}/api/v1/admin/sync-schedules`, {
        credentials: 'include'
      }))
    },
    adminCreateSyncSchedule(target: AdminSyncScheduleTarget, input: AdminSaveSyncScheduleRequest) {
      return withAdminRedirect(apiFetch<AdminSyncScheduleItem>(`${apiBase}/api/v1/admin/sync-schedules/${encodeURIComponent(target)}`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminUpdateSyncSchedule(target: AdminSyncScheduleTarget, id: string, input: AdminSaveSyncScheduleRequest) {
      return withAdminRedirect(apiFetch<AdminSyncScheduleItem>(`${apiBase}/api/v1/admin/sync-schedules/${encodeURIComponent(target)}/${encodeURIComponent(id)}`, {
        method: 'PUT',
        credentials: 'include',
        body: input
      }))
    },
    adminDeleteSyncSchedule(target: AdminSyncScheduleTarget, id: string) {
      return withAdminRedirect(apiFetch<void>(`${apiBase}/api/v1/admin/sync-schedules/${encodeURIComponent(target)}/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        credentials: 'include'
      }))
    },
    adminStartSync(target: AdminSyncTarget) {
      return withAdminRedirect(apiFetch<AdminSyncResponse>(`${apiBase}/api/v1/admin/syncs/${encodeURIComponent(target)}`, {
        method: 'POST',
        credentials: 'include'
      }))
    }
  }
}

interface ApiFailure {
  status?: number
  statusCode?: number
  data?: ApiErrorResponse
}

function parseApiFailure(cause: unknown): ApiFailure | null {
  if (cause === null || Object(cause) !== cause) return null
  // SAFETY: Object identity above establishes a non-null object; every field remains optional.
  return cause as ApiFailure
}

export function getApiErrorStatus(cause: unknown): number | undefined {
  const failure = parseApiFailure(cause)
  const status = failure?.status
  if (status !== undefined && Number.isFinite(status)) return status
  const statusCode = failure?.statusCode
  if (statusCode !== undefined && Number.isFinite(statusCode)) return statusCode
  return undefined
}

export function getApiErrorCode(cause: unknown): string | undefined {
  return parseApiFailure(cause)?.data?.error?.code
}

export function isNotFoundError(cause: unknown): boolean {
  return getApiErrorStatus(cause) === 404 || getApiErrorCode(cause) === 'not_found'
}

export function getFrenchApiError(cause: unknown): string {
  const message = parseApiFailure(cause)?.data?.error?.message
  if (message !== undefined) return message
  return 'Impossible de joindre le service. Vérifiez que l’API est démarrée, puis réessayez.'
}

export function getFrenchShortLinkPreparationError(cause: unknown): string {
  if (getApiErrorCode(cause) === 'rate_limited' || getApiErrorStatus(cause) === 429) {
    return 'Vous avez créé trop de liens trop rapidement. Patientez quelques minutes puis réessayez.'
  }
  return 'Le lien n’a pas pu être préparé. Réessayez.'
}

export function getFrenchAdminApiError(cause: unknown): string {
  const code = getApiErrorCode(cause)
  if (code === 'admin_unavailable') return 'L’administration est désactivée sur ce service.'
  if (code === 'invalid_admin_movie_query') return 'Filtres de films invalides.'
  if (code === 'invalid_admin_movie_id') return 'Identifiant de film invalide.'
  if (code === 'invalid_admin_movie_update') return 'Modifications de film invalides.'
  if (code === 'admin_movie_not_found') return 'Film introuvable.'
  if (code === 'admin_movie_conflict') return 'Ce film a changé. La liste a été actualisée.'
  if (code === 'admin_movie_list_failed') return 'Impossible de charger les films.'
  if (code === 'admin_movie_update_failed') return 'Impossible d’enregistrer le film.'
  if (code === 'review_unavailable') return 'Le service de validation TMDB est temporairement indisponible.'
  if (code === 'review_conflict') return 'Cette correspondance a changé. Les listes ont été actualisées.'
  if (code === 'tmdb_rerun_in_progress') return 'Une relance TMDB est déjà en cours.'
  if (code === 'tmdb_rerun_unavailable') return 'Le service de relance TMDB est temporairement indisponible.'
  if (code === 'tmdb_rerun_failed') return 'La relance TMDB a échoué. La liste a été actualisée, car certains films ont peut-être déjà été traités.'
  if (code === 'tmdb_metadata_refresh_in_progress') return 'Une autre opération TMDB est déjà en cours.'
  if (code === 'tmdb_metadata_refresh_unavailable') return 'Le service d’actualisation des métadonnées TMDB est temporairement indisponible.'
  if (code === 'tmdb_metadata_refresh_failed') return 'L’actualisation des métadonnées TMDB a échoué. Réessayez plus tard.'
  if (code === 'local_movie_conflict') return 'Ces films ont changé et ne peuvent plus être regroupés. La liste a été actualisée.'
  if (code === 'local_movie_failed') return 'Le regroupement des films est temporairement indisponible.'
  if (code === 'sync_unavailable') return 'Le service de synchronisation est temporairement indisponible.'
  if (code === 'sync_in_progress') return 'Une synchronisation est déjà en cours.'
  if (code === 'sync_failed') return 'La synchronisation n’a pas pu démarrer. Réessayez plus tard.'
  if (code === 'invalid_sync_schedule') return 'La configuration est invalide. Vérifiez les champs requis et leur format, puis réessayez.'
  if (code === 'sync_schedule_not_found') return 'Cette planification n’existe plus. Actualisez la page.'
  if (code === 'sync_schedule_target_unavailable') return 'Cette synchronisation est temporairement indisponible. Désactivez la planification ou réessayez plus tard.'
  if (code === 'sync_schedule_unavailable') return 'La planification des synchronisations est temporairement indisponible.'
  if (code === 'sync_schedule_failed') return 'La planification n’a pas pu être enregistrée. Vos modifications sont conservées, réessayez plus tard.'
  if (code === 'theater_location_not_found') return 'Cette localisation n’est plus à traiter. Actualisez la liste.'
  if (code === 'theater_location_conflict') return 'Cette localisation a changé ou la suggestion n’est plus disponible. Actualisez la liste, puis réessayez.'
  if (code === 'theater_location_unavailable') return 'Le service de localisation des cinémas est temporairement indisponible. Réessayez plus tard.'
  if (code === 'theater_geocoding_in_progress') return 'Un géocodage IGN est déjà en cours.'
  if (code === 'theater_geocoding_unavailable') return 'Le service de géocodage IGN est temporairement indisponible.'
  if (code === 'theater_geocoding_failed') return 'Le service de géocodage IGN a rencontré une erreur. Réessayez plus tard.'
  if (code === 'review_failed' || code === 'internal_error' || getApiErrorStatus(cause) === 502) {
    return 'Le service de validation a rencontré une erreur. Réessayez plus tard.'
  }
  return getFrenchApiError(cause)
}
