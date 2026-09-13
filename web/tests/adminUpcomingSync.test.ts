import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { useAdminUpcomingSync, UPCOMING_SYNC_MAX_POLLS, UPCOMING_SYNC_POLL_DELAY } from '../app/composables/useAdminUpcomingSync.ts'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type { AdminUpcomingSyncResponse } from '../app/types/api.ts'

function response(state: 'running' | 'succeeded' | 'failed' | null): AdminUpcomingSyncResponse {
  if (state === null) return { job: null }
  const job: NonNullable<AdminUpcomingSyncResponse['job']> = {
    state,
    started_at: '2026-09-13T12:00:00Z',
    finished_at: state === 'running' ? null : '2026-09-13T12:01:00Z'
  }
  if (state === 'failed') job.error_code = 'sync_failed'
  return { job }
}

function failure(status: number, code = 'unknown') {
  return { status, data: { error: { code, message: 'private upstream details' } } }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: ReturnType<typeof failure>) => void
  const promise = new Promise<T>((onResolve, onReject) => { resolve = onResolve; reject = onReject })
  return { promise, resolve, reject }
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

test('loads initial status before allowing a start and guards duplicate status/start clicks', async (t) => {
  const initial = deferred<AdminUpcomingSyncResponse>()
  const accepted = deferred<AdminUpcomingSyncResponse>()
  let reads = 0
  let starts = 0
  const sync = useAdminUpcomingSync({
    adminUpcomingSyncStatus: () => { reads += 1; return initial.promise },
    adminStartUpcomingSync: () => { starts += 1; return accepted.promise }
  })
  t.after(sync.dispose)
  assert.equal(sync.canStart.value, false)
  await sync.start()
  assert.equal(starts, 0)
  const loading = sync.checkStatus()
  await sync.checkStatus()
  await sync.start()
  assert.equal(reads, 1)
  assert.equal(sync.pending.value, 'status')
  initial.resolve(response(null))
  await loading
  assert.equal(sync.canStart.value, true)
  assert.equal(sync.message.value, '')
  const starting = sync.start()
  await sync.start()
  await sync.checkStatus()
  assert.equal(starts, 1)
  assert.equal(reads, 1)
  assert.equal(sync.pending.value, 'start')
  accepted.resolve(response('running'))
  await starting
  assert.equal(sync.running.value, true)
  assert.equal(sync.canStart.value, false)
})

test('polls only running jobs, stops on completion, and does not poll idle or historical success', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let reads = 0
  let current = response(null)
  const sync = useAdminUpcomingSync({
    adminUpcomingSyncStatus: async () => { reads += 1; return current },
    adminStartUpcomingSync: async () => response('running')
  })
  t.after(sync.dispose)
  await sync.checkStatus()
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  assert.equal(reads, 1)
  await sync.start()
  current = response('succeeded')
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY)
  await flush()
  assert.equal(reads, 2)
  assert.match(sync.message.value, /terminée/)
  assert.equal(sync.canStart.value, true)
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  assert.equal(reads, 2)
  await sync.checkStatus()
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  assert.equal(reads, 3)
})

test('initial running job resumes polling with at most one GET in flight', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const poll = deferred<AdminUpcomingSyncResponse>()
  let reads = 0
  const sync = useAdminUpcomingSync({
    adminUpcomingSyncStatus: () => ++reads === 1 ? Promise.resolve(response('running')) : poll.promise,
    adminStartUpcomingSync: async () => { assert.fail('must not start a running job') }
  })
  t.after(sync.dispose)
  await sync.checkStatus()
  await sync.start()
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY)
  assert.equal(reads, 2)
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  await sync.checkStatus()
  assert.equal(reads, 2)
  poll.resolve(response('failed'))
  await flush()
  assert.match(sync.error.value, /a échoué/)
  assert.equal(sync.message.value, '')
  assert.equal(sync.canStart.value, true)
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  assert.equal(reads, 2)
})

test('bounds automatic polling and requires explicit status check to resume without starting a job', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let reads = 0
  const sync = useAdminUpcomingSync({
    adminUpcomingSyncStatus: async () => { reads += 1; return response('running') },
    adminStartUpcomingSync: async () => { assert.fail('status check must not start a job') }
  })
  t.after(sync.dispose)
  await sync.checkStatus()
  for (let count = 0; count < UPCOMING_SYNC_MAX_POLLS; count += 1) {
    t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY)
    await flush()
  }
  assert.equal(reads, UPCOMING_SYNC_MAX_POLLS + 1)
  assert.match(sync.message.value, /en pause/)
  assert.equal(sync.needsCheck.value, true)
  assert.equal(sync.canStart.value, false)
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  assert.equal(reads, UPCOMING_SYNC_MAX_POLLS + 1)
  await sync.checkStatus()
  assert.equal(sync.needsCheck.value, false)
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY)
  await flush()
  assert.equal(reads, UPCOMING_SYNC_MAX_POLLS + 3)
})

test('status failures stop polling, hide stale success, and require a successful status check before starting', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let reads = 0
  const sync = useAdminUpcomingSync({
    adminUpcomingSyncStatus: async () => {
      reads += 1
      if (reads === 2) throw failure(503)
      return response(reads === 1 ? 'running' : 'succeeded')
    },
    adminStartUpcomingSync: async () => { assert.fail('cannot start with unknown status') }
  })
  t.after(sync.dispose)
  await sync.checkStatus()
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY)
  await flush()
  assert.match(sync.error.value, /Impossible de vérifier/)
  assert.doesNotMatch(sync.error.value, /private/)
  assert.equal(sync.message.value, '')
  assert.equal(sync.canStart.value, false)
  assert.equal(sync.needsCheck.value, true)
  await sync.start()
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  assert.equal(reads, 2)
  await sync.checkStatus()
  assert.equal(sync.error.value, '')
  assert.match(sync.message.value, /terminée/)
})

test('unavailable and authentication errors use safe feedback without retry loops', async (t) => {
  for (const [status, code, message] of [
    [503, 'tmdb_upcoming_sync_unavailable', /indisponible/],
    [503, 'admin_unavailable', /indisponible/],
    [401, 'unauthorized', /Session expirée/]
  ] as const) {
    let reads = 0
    const sync = useAdminUpcomingSync({
      adminUpcomingSyncStatus: async () => { reads += 1; throw failure(status, code) },
      adminStartUpcomingSync: async () => { assert.fail('cannot start') }
    })
    t.after(sync.dispose)
    await sync.checkStatus()
    assert.match(sync.error.value, message)
    assert.doesNotMatch(sync.error.value, /private/)
    assert.equal(sync.canStart.value, false)
    if (status === 401) {
      assert.equal(sync.canCheck.value, false)
      await sync.checkStatus()
      assert.equal(reads, 1)
    }
  }
})

test('conflict reloads local status, polls a local running job, but never invents another replica job', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  for (const localState of ['running', 'succeeded', null] as const) {
    let reads = 0
    let starts = 0
    const sync = useAdminUpcomingSync({
      adminUpcomingSyncStatus: async () => response(++reads === 1 ? null : localState),
      adminStartUpcomingSync: async () => { starts += 1; throw failure(409, 'tmdb_upcoming_sync_in_progress') }
    })
    t.after(sync.dispose)
    await sync.checkStatus()
    await sync.start()
    assert.equal(starts, 1)
    assert.equal(reads, 2)
    assert.equal(sync.running.value, localState === 'running')
    if (localState === 'running') {
      assert.equal(sync.error.value, '')
      assert.equal(sync.canStart.value, false)
    } else {
      assert.match(sync.error.value, /opération TMDB est déjà en cours/)
      assert.equal(sync.message.value, '')
    }
    t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY)
    await flush()
    assert.equal(reads, localState === 'running' ? 3 : 2)
    sync.dispose()
  }
})

test('failed or uncertain POST admission requires status recovery and never retries the mutation', async (t) => {
  for (const cause of [failure(502, 'tmdb_upcoming_sync_failed'), failure(403, 'origin_forbidden'), new Error('private timeout')]) {
    let starts = 0
    const sync = useAdminUpcomingSync({
      adminUpcomingSyncStatus: async () => response(null),
      adminStartUpcomingSync: async () => { starts += 1; throw cause }
    })
    t.after(sync.dispose)
    await sync.checkStatus()
    await sync.start()
    assert.match(sync.error.value, /Vérifiez le statut avant de réessayer/)
    assert.doesNotMatch(sync.error.value, /private/)
    assert.equal(sync.canStart.value, false)
    await sync.start()
    assert.equal(starts, 1)
    await sync.checkStatus()
    assert.equal(sync.canStart.value, true)
  }
})

test('dispose aborts GET and POST and ignores late completion or failure even if transport ignores cancellation', async () => {
  for (const kind of ['status', 'start'] as const) {
    for (const lateFailure of [false, true]) {
      const pending = deferred<AdminUpcomingSyncResponse>()
      let signal: AbortSignal | undefined
      const capture = (requestSignal?: AbortSignal) => { signal = requestSignal; return pending.promise }
      const sync = useAdminUpcomingSync({
        adminUpcomingSyncStatus: kind === 'status' ? capture : async () => response(null),
        adminStartUpcomingSync: capture
      })
      if (kind === 'start') await sync.checkStatus()
      const loading = kind === 'start' ? sync.start() : sync.checkStatus()
      const previousMessage = sync.message.value
      sync.dispose()
      assert.equal(signal?.aborted, true)
      if (lateFailure) pending.reject(failure(503))
      else pending.resolve(response('running'))
      await loading
      assert.equal(sync.message.value, previousMessage)
      assert.equal(sync.error.value, '')
      assert.equal(sync.running.value, false)
    }
  }
})

test('dispose clears scheduled polling', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let reads = 0
  const sync = useAdminUpcomingSync({
    adminUpcomingSyncStatus: async () => { reads += 1; return response('running') },
    adminStartUpcomingSync: async () => response('running')
  })
  await sync.checkStatus()
  sync.dispose()
  t.mock.timers.tick(UPCOMING_SYNC_POLL_DELAY * 10)
  await sync.checkStatus()
  await sync.start()
  assert.equal(reads, 1)
})

test('GET and empty-body POST use frozen path, credentials, cancellation, timeout and no automatic retries', async () => {
  interface FetchOptions { method?: string, credentials: string, signal?: AbortSignal, retry: false, timeout: number }
  const calls: Array<{ url: string, options: FetchOptions }> = []
  const controller = new AbortController()
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: FetchOptions) => { calls.push({ url, options }); return Promise.resolve(response(null)) }
  })
  const api = useMesSeancesApi()
  await api.adminUpcomingSyncStatus(controller.signal)
  await api.adminStartUpcomingSync(controller.signal)
  const options = { credentials: 'include', signal: controller.signal, retry: false, timeout: 15000 }
  assert.deepEqual(calls, [
    { url: 'http://localhost:8080/api/v1/admin/tmdb-upcoming-movies/sync', options },
    { url: 'http://localhost:8080/api/v1/admin/tmdb-upcoming-movies/sync', options: { method: 'POST', ...options } }
  ])
})

test('upcoming review page owns accessible direct action, status recovery, initial load and teardown without schedule mutation', async () => {
  const page = await readFile(new URL('../app/pages/admin/upcoming-movies.vue', import.meta.url), 'utf8')
  const api = await readFile(new URL('../app/composables/useMesSeancesApi.ts', import.meta.url), 'utf8')
  assert.match(page, /definePageMeta\(\{ middleware: 'admin-auth' \}\)/)
  assert.match(page, /onMounted\(\(\) => \{ void checkUpcomingStatus\(\) \}\)/)
  assert.match(page, /onBeforeUnmount\(disposeUpcomingSync\)/)
  assert.equal([...page.matchAll(/useAdminUpcomingSync\(api\)/g)].length, 1)
  assert.equal([...page.matchAll(/void checkUpcomingStatus\(\)/g)].length, 1)
  assert.match(page, /<button type="button"[^>]+:disabled="!canStartUpcoming"[^>]+aria-describedby="upcoming-sync-status"[^>]+@click="startUpcomingSync"/)
  assert.match(page, /v-if="upcomingPending \|\| upcomingRunning"/)
  assert.match(page, /v-if="upcomingNeedsCheck && !upcomingPending"[^>]+:disabled="!canCheckUpcoming"/)
  assert.match(page, /Synchroniser TMDB - Prochainement/)
  assert.match(page, /role="status" aria-live="polite"/)
  assert.match(page, /v-if="upcomingError"[^>]+role="alert"/)
  assert.match(page, /@click="checkUpcomingStatus">Vérifier le statut/)
  assert.doesNotMatch(page, /adminCreateSyncSchedule|adminUpdateSyncSchedule|adminStartSync|v-html|loggingOut|void startUpcomingSync\(/)
  assert.ok(page.indexOf('Synchroniser TMDB - Prochainement') < page.indexOf('<label for="review-filter"'))
  const buttonClass = page.match(/<button type="button" class="([^"]+)"[^>]+@click="startUpcomingSync"/)?.[1]?.split(' ') ?? []
  for (const token of ['inline-flex', 'min-h-11', 'w-full', 'sm:w-auto', 'bg-primary', 'text-white']) assert.ok(buttonClass.includes(token), token)
  for (const method of ['adminUpcomingSyncStatus', 'adminStartUpcomingSync']) {
    assert.match(api, new RegExp(`${method}\\(signal\\?: AbortSignal\\) \\{\\s+return withAdminRedirect`))
  }
})

test('dashboard retains review navigation and logout without instantiating or querying upcoming sync', async () => {
  const dashboard = await readFile(new URL('../app/pages/admin/index.vue', import.meta.url), 'utf8')
  assert.match(dashboard, /to="\/admin\/upcoming-movies"/)
  assert.match(dashboard, /Revue des sorties à venir/)
  assert.match(dashboard, /@click="logout"/)
  assert.match(dashboard, /await api.adminLogout\(\)/)
  assert.doesNotMatch(dashboard, /useAdminUpcomingSync|adminUpcomingSyncStatus|adminStartUpcomingSync|checkUpcomingStatus|startUpcomingSync|upcoming-sync-status|Synchroniser TMDB - Prochainement/)
})
