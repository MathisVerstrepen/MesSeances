import type { ApiErrorResponse, SlotQuery, SlotResult, TimelineQuery, TimelineResponse } from '~/types/api'

function queryValues<T extends object>(query: T) {
  return Object.fromEntries(Object.entries(query).filter((entry): entry is [string, string | number] => entry[1] !== undefined))
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
    }
  }
}

export function getFrenchApiError(error: unknown): string {
  if (typeof error === 'object' && error !== null && 'data' in error) {
    const data = (error as { data?: ApiErrorResponse }).data
    if (data?.error?.code === 'invalid_query' && typeof data.error.message === 'string') {
      return data.error.message
    }
  }
  return 'Impossible de joindre le service. Vérifiez que l’API est démarrée, puis réessayez.'
}
