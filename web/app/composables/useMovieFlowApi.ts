import type {
  AdminCreateLocalMovieGroupRequest,
  AdminLocalMovieGroup,
  AdminLocalMovieGroupsResponse,
  AdminMatchDecisionResponse,
  AdminPendingMatchesResponse,
  AdminSessionResponse,
  AdminSyncResponse,
  AdminSyncTarget,
  AdminUnmergeLocalMovieResponse,
  ApiErrorResponse,
  MoviesQuery,
  MoviesResponse,
  MovieShowtimesQuery,
  MovieShowtimesResponse,
  Provider,
  SlotQuery,
  SlotResult,
  Theater,
  TheaterQuery,
  TimelineQuery,
  TimelineResponse
} from '~/types/api'

function queryValues<T extends object>(query: T) {
  return Object.fromEntries(Object.entries(query).filter((entry): entry is [string, string | number | boolean] => entry[1] !== undefined))
}

export function useMovieFlowApi() {
  const config = useRuntimeConfig()
  const apiBase = config.public.apiBase.replace(/\/$/, '')

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
    timeline(query: TimelineQuery) {
      return $fetch<TimelineResponse>(`${apiBase}/api/v1/timeline`, { query: queryValues(query) })
    },
    searchSlot(query: SlotQuery) {
      return $fetch<SlotResult[]>(`${apiBase}/api/v1/search/slot`, { query: queryValues(query) })
    },
    theaters(query: TheaterQuery = {}) {
      return $fetch<Theater[]>(`${apiBase}/api/v1/theaters`, { query: queryValues(query) })
    },
    movies(query: MoviesQuery = {}) {
      return $fetch<MoviesResponse>(`${apiBase}/api/v1/movies`, { query: queryValues(query) })
    },
    movieShowtimes(slug: string, query: MovieShowtimesQuery) {
      return $fetch<MovieShowtimesResponse>(`${apiBase}/api/v1/movies/${encodeURIComponent(slug)}/showtimes`, { query: queryValues(query) })
    },
    adminSession() {
      return $fetch<AdminSessionResponse>(`${apiBase}/api/v1/admin/session`, { credentials: 'include' })
    },
    adminLogin(password: string) {
      return $fetch<AdminSessionResponse>(`${apiBase}/api/v1/admin/login`, {
        method: 'POST',
        credentials: 'include',
        body: { password }
      })
    },
    adminLogout() {
      return withAdminRedirect($fetch<AdminSessionResponse>(`${apiBase}/api/v1/admin/logout`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminPendingMatches(limit: number, offset: number) {
      return withAdminRedirect($fetch<AdminPendingMatchesResponse>(`${apiBase}/api/v1/admin/tmdb-matches`, {
        credentials: 'include',
        query: { limit, offset }
      }))
    },
    adminApproveMatch(sourceProvider: Provider, sourceMovieId: string, tmdbId: number) {
      return withAdminRedirect($fetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceProvider)}/${encodeURIComponent(sourceMovieId)}/approve`, {
        method: 'POST',
        credentials: 'include',
        body: { tmdb_id: tmdbId }
      }))
    },
    adminRejectMatch(sourceProvider: Provider, sourceMovieId: string) {
      return withAdminRedirect($fetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceProvider)}/${encodeURIComponent(sourceMovieId)}/reject`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminLocalMovieGroups(limit: number, offset: number) {
      return withAdminRedirect($fetch<AdminLocalMovieGroupsResponse>(`${apiBase}/api/v1/admin/local-movie-groups`, {
        credentials: 'include',
        query: { limit, offset }
      }))
    },
    adminCreateLocalMovieGroup(input: AdminCreateLocalMovieGroupRequest) {
      return withAdminRedirect($fetch<AdminLocalMovieGroup>(`${apiBase}/api/v1/admin/local-movie-groups`, {
        method: 'POST',
        credentials: 'include',
        body: input
      }))
    },
    adminUnmergeLocalMovie(localMovieId: string) {
      return withAdminRedirect($fetch<AdminUnmergeLocalMovieResponse>(`${apiBase}/api/v1/admin/local-movie-groups/${encodeURIComponent(localMovieId)}/unmerge`, {
        method: 'POST',
        credentials: 'include'
      }))
    },
    adminSyncStatus() {
      return withAdminRedirect($fetch<AdminSyncResponse>(`${apiBase}/api/v1/admin/syncs`, {
        credentials: 'include'
      }))
    },
    adminStartSync(target: AdminSyncTarget) {
      return withAdminRedirect($fetch<AdminSyncResponse>(`${apiBase}/api/v1/admin/syncs/${encodeURIComponent(target)}`, {
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

export function getFrenchApiError(cause: unknown): string {
  const message = parseApiFailure(cause)?.data?.error?.message
  if (message !== undefined) return message
  return 'Impossible de joindre le service. Vérifiez que l’API est démarrée, puis réessayez.'
}

export function getFrenchAdminApiError(cause: unknown): string {
  const code = getApiErrorCode(cause)
  if (code === 'admin_unavailable') return 'L’administration est désactivée sur ce service.'
  if (code === 'review_unavailable') return 'Le service de validation TMDB est temporairement indisponible.'
  if (code === 'local_movie_conflict') return 'Ces films ont changé et ne peuvent plus être regroupés. La liste a été actualisée.'
  if (code === 'local_movie_failed') return 'Le regroupement des films est temporairement indisponible.'
  if (code === 'sync_unavailable') return 'Le service de synchronisation est temporairement indisponible.'
  if (code === 'sync_in_progress') return 'Une synchronisation est déjà en cours.'
  if (code === 'sync_failed') return 'La synchronisation n’a pas pu démarrer. Réessayez plus tard.'
  if (code === 'review_failed' || code === 'internal_error' || getApiErrorStatus(cause) === 502) {
    return 'Le service de validation a rencontré une erreur. Réessayez plus tard.'
  }
  return getFrenchApiError(cause)
}
