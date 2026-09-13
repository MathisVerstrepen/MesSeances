import { computed, readonly, ref } from 'vue'
import type { AdminUpcomingSyncResponse } from '../types/api.ts'
import { getApiErrorCode, getApiErrorStatus } from './useMesSeancesApi.ts'

export const UPCOMING_SYNC_POLL_DELAY = 2000
export const UPCOMING_SYNC_MAX_POLLS = 300

interface UpcomingSyncApi {
  adminUpcomingSyncStatus: (signal?: AbortSignal) => Promise<AdminUpcomingSyncResponse>
  adminStartUpcomingSync: (signal?: AbortSignal) => Promise<AdminUpcomingSyncResponse>
}

export function useAdminUpcomingSync(api: UpcomingSyncApi) {
  const job = ref<AdminUpcomingSyncResponse['job']>(null)
  const pending = ref<'status' | 'start' | null>(null)
  const ready = ref(false)
  const paused = ref(false)
  const expired = ref(false)
  const error = ref('')
  const running = computed(() => job.value?.state === 'running')
  const canStart = computed(() => ready.value && !pending.value && !running.value && !paused.value && !expired.value)
  const canCheck = computed(() => !pending.value && !expired.value)
  const needsCheck = computed(() => !ready.value || paused.value)
  const message = computed(() => {
    if (pending.value === 'start') return 'Démarrage de la synchronisation TMDB - Prochainement…'
    if (pending.value === 'status' && needsCheck.value) return 'Vérification du statut TMDB - Prochainement…'
    if (error.value) return ''
    if (paused.value) return 'Suivi automatique en pause. Vérifiez le statut pour reprendre.'
    if (running.value) return 'Synchronisation TMDB - Prochainement en cours…'
    if (job.value?.state === 'succeeded') return 'Synchronisation TMDB - Prochainement terminée.'
    return ''
  })
  let disposed = false
  let activeRequest: AbortController | null = null
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let remainingPolls = UPCOMING_SYNC_MAX_POLLS

  function clearPolling() {
    if (pollTimer !== undefined) clearTimeout(pollTimer)
    pollTimer = undefined
  }

  function schedulePolling() {
    clearPolling()
    if (disposed || !ready.value || !running.value || pending.value || error.value) return
    if (remainingPolls === 0) {
      paused.value = true
      return
    }
    pollTimer = setTimeout(() => {
      pollTimer = undefined
      remainingPolls -= 1
      void request('status')
    }, UPCOMING_SYNC_POLL_DELAY)
  }

  async function request(kind: 'status' | 'start', conflictMessage = '') {
    if (disposed || pending.value || expired.value) return
    clearPolling()
    const controller = new AbortController()
    activeRequest = controller
    pending.value = kind
    error.value = ''
    let conflict = false
    try {
      const response = await (kind === 'start' ? api.adminStartUpcomingSync(controller.signal) : api.adminUpcomingSyncStatus(controller.signal))
      if (disposed || controller.signal.aborted || activeRequest !== controller) return
      job.value = response.job
      ready.value = true
      paused.value = false
      if (response.job?.state === 'failed') error.value = 'La synchronisation TMDB - Prochainement a échoué. Réessayez plus tard.'
      if (!running.value && conflictMessage) error.value = conflictMessage
    } catch (cause) {
      if (disposed || controller.signal.aborted || activeRequest !== controller) return
      ready.value = false
      const code = getApiErrorCode(cause)
      if (getApiErrorStatus(cause) === 401) {
        expired.value = true
        error.value = 'Session expirée. Reconnectez-vous à l’administration.'
      } else if (code === 'tmdb_upcoming_sync_unavailable' || code === 'admin_unavailable') {
        error.value = 'Service de synchronisation TMDB - Prochainement indisponible. Réessayez plus tard.'
      } else if (kind === 'start' && getApiErrorStatus(cause) === 409) {
        conflict = true
      } else if (code === 'tmdb_upcoming_sync_failed') {
        error.value = 'La synchronisation TMDB - Prochainement n’a pas pu démarrer. Vérifiez le statut avant de réessayer.'
      } else {
        error.value = kind === 'start'
          ? 'Impossible de confirmer le démarrage. Vérifiez le statut avant de réessayer.'
          : 'Impossible de vérifier le statut TMDB - Prochainement. Réessayez.'
      }
    } finally {
      if (!disposed && activeRequest === controller) {
        activeRequest = null
        pending.value = null
        schedulePolling()
      }
    }
    // A shared gate or another replica may be busy without a local running job.
    if (conflict) await request('status', 'Une opération TMDB est déjà en cours. Réessayez plus tard.')
  }

  async function checkStatus() {
    if (!canCheck.value || disposed) return
    remainingPolls = UPCOMING_SYNC_MAX_POLLS
    await request('status')
  }

  async function start() {
    if (!canStart.value || disposed) return
    remainingPolls = UPCOMING_SYNC_MAX_POLLS
    await request('start')
  }

  function dispose() {
    disposed = true
    clearPolling()
    activeRequest?.abort()
    activeRequest = null
  }

  return { pending: readonly(pending), running, canStart, canCheck, needsCheck, error: readonly(error), message, checkStatus, start, dispose }
}
