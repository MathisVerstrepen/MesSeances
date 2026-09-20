import { computed, ref } from 'vue'
import type {
  AdminSetUpcomingDecisionRequest,
  AdminUpcomingMovie,
  AdminUpcomingMoviesQuery,
  AdminUpcomingMoviesResponse,
  UpcomingReviewDecision,
} from '../types/api.ts'
import {
  upcomingReviewApiQuery,
  UPCOMING_REVIEW_PAGE_SIZE,
  type UpcomingReviewRouteState,
} from '../utils/adminUpcomingMovies.ts'
import {
  getApiErrorCode,
  getApiErrorStatus,
  getFrenchAdminApiError,
} from './useMesSeancesApi.ts'

interface UpcomingReviewApi {
  adminUpcomingMovies: (
    query: AdminUpcomingMoviesQuery,
    signal?: AbortSignal,
  ) => Promise<AdminUpcomingMoviesResponse>
  adminSetUpcomingDecision: (
    tmdbID: number,
    input: AdminSetUpcomingDecisionRequest,
  ) => Promise<AdminUpcomingMovie>
}

export function useAdminUpcomingMovies(
  api: UpcomingReviewApi,
  clampPage: (page: number) => Promise<void>,
) {
  const items = ref<AdminUpcomingMovie[]>([])
  const total = ref(0)
  const loading = ref(true)
  const loaded = ref(false)
  const error = ref('')
  const message = ref('')
  const mutationID = ref<number | null>(null)
  const current = ref(false)
  const canMutate = computed(
    () => current.value && !loading.value && mutationID.value === null,
  )
  let state: UpcomingReviewRouteState = {
    filter: 'needs_review',
    q: '',
    page: 1,
  }
  let disposed = false
  let request: AbortController | undefined

  function invalidate() {
    request?.abort()
    current.value = false
    loading.value = true
  }

  async function load(next: UpcomingReviewRouteState = state) {
    if (disposed) return
    state = { ...next }
    invalidate()
    const controller = new AbortController()
    request = controller
    error.value = ''
    try {
      const result = await api.adminUpcomingMovies(
        upcomingReviewApiQuery(next),
        controller.signal,
      )
      if (disposed || controller.signal.aborted || request !== controller)
        return
      const lastPage = Math.max(
        1,
        Math.ceil(result.total / UPCOMING_REVIEW_PAGE_SIZE),
      )
      if (next.page > lastPage) {
        await clampPage(lastPage)
        return
      }
      items.value = result.items
      total.value = result.total
      loaded.value = true
      current.value = true
    } catch (cause) {
      if (disposed || controller.signal.aborted || request !== controller)
        return
      error.value =
        getApiErrorStatus(cause) === 401
          ? 'Session expirée. Reconnectez-vous à l’administration.'
          : getFrenchAdminApiError(cause)
    } finally {
      if (!disposed && !controller.signal.aborted && request === controller)
        loading.value = false
    }
  }

  async function decide(
    movie: AdminUpcomingMovie,
    decision: UpcomingReviewDecision,
  ) {
    if (disposed || !canMutate.value || movie.decision === decision) return
    mutationID.value = movie.tmdb_id
    error.value = ''
    message.value = ''
    try {
      await api.adminSetUpcomingDecision(movie.tmdb_id, {
        decision,
        expected_revision: movie.revision,
      })
      if (disposed) return
      message.value =
        decision === 'approved'
          ? 'Sortie approuvée.'
          : decision === 'excluded'
            ? 'Sortie exclue de Prochainement.'
            : 'Décision réinitialisée.'
      await load()
    } catch (cause) {
      if (disposed) return
      const status = getApiErrorStatus(cause)
      if (status === 409 || status === 404) {
        message.value =
          status === 409
            ? 'Cette évaluation a changé. La liste a été actualisée.'
            : 'Cette sortie n’existe plus. La liste a été actualisée.'
        await load()
      } else {
        // An uncertain response may follow a committed edit. Reload before any new decision.
        current.value = false
        error.value =
          getApiErrorCode(cause) === 'upcoming_review_update_failed' ||
          status === 403 ||
          status === 503
            ? getFrenchAdminApiError(cause)
            : status === 401
              ? 'Session expirée. Reconnectez-vous à l’administration.'
              : 'Impossible de confirmer la décision. Actualisez la liste avant de réessayer.'
      }
    } finally {
      if (!disposed) mutationID.value = null
    }
  }

  function dispose() {
    disposed = true
    request?.abort()
  }

  return {
    items,
    total,
    loading,
    loaded,
    error,
    message,
    mutationID,
    canMutate,
    load,
    invalidate,
    decide,
    dispose,
  }
}
