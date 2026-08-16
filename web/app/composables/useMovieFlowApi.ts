import type {
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
    }
  }
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
