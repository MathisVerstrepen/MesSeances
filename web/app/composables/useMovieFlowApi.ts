import type {
  AdminMatchDecisionResponse,
  AdminPendingMatchesResponse,
  AdminSessionResponse,
  AdminSyncResponse,
  AdminSyncTarget,
  ApiErrorResponse,
  MoviesQuery,
  MoviesResponse,
  MovieShowtimesQuery,
  MovieShowtimesResponse,
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
    adminApproveMatch(sourceMovieId: string, tmdbId: number) {
      return withAdminRedirect($fetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceMovieId)}/approve`, {
        method: 'POST',
        credentials: 'include',
        body: { tmdb_id: tmdbId }
      }))
    },
    adminRejectMatch(sourceMovieId: string) {
      return withAdminRedirect($fetch<AdminMatchDecisionResponse>(`${apiBase}/api/v1/admin/tmdb-matches/${encodeURIComponent(sourceMovieId)}/reject`, {
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

export function getApiErrorStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  if ('status' in error && typeof error.status === 'number') return error.status
  if ('statusCode' in error && typeof error.statusCode === 'number') return error.statusCode
  return undefined
}

function getApiErrorCode(error: unknown): string | undefined {
  if (typeof error !== 'object' || error === null || !('data' in error)) return undefined
  return (error as { data?: ApiErrorResponse }).data?.error?.code
}

export function getFrenchApiError(error: unknown): string {
  if (typeof error === 'object' && error !== null && 'data' in error) {
    const data = (error as { data?: ApiErrorResponse }).data
    if (typeof data?.error?.message === 'string') {
      return data.error.message
    }
  }
  return 'Impossible de joindre le service. Vérifiez que l’API est démarrée, puis réessayez.'
}

export function getFrenchAdminApiError(error: unknown): string {
  const code = getApiErrorCode(error)
  if (code === 'admin_unavailable') return 'L’administration est désactivée sur ce service.'
  if (code === 'review_unavailable') return 'Le service de validation TMDB est temporairement indisponible.'
  if (code === 'sync_unavailable') return 'Le service de synchronisation est temporairement indisponible.'
  if (code === 'sync_in_progress') return 'Une synchronisation est déjà en cours.'
  if (code === 'sync_failed') return 'La synchronisation n’a pas pu démarrer. Réessayez plus tard.'
  if (code === 'review_failed' || code === 'internal_error' || getApiErrorStatus(error) === 502) {
    return 'Le service de validation a rencontré une erreur. Réessayez plus tard.'
  }
  return getFrenchApiError(error)
}
