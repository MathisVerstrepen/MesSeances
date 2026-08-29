import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { getFrenchAdminApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type { AdminSaveSyncScheduleRequest, AdminSyncJob, AdminSyncProviderState, AdminSyncScheduleItem, AdminSyncTarget, Provider } from '../app/types/api.ts'
import {
  adminSyncRunDurationMilliseconds,
  adminSyncScheduleDraftFingerprint,
  adminSyncScheduleDraftFromItem,
  blankAdminSyncScheduleDraft,
  buildAdminSyncScheduleRequest,
  isAdminSyncScheduleTargetAvailable,
  selectLatestProviderRun,
  validateAdminSyncScheduleDraft,
  type AdminSyncScheduleDraft
} from '../app/utils/adminSyncSchedules.ts'

interface AdminScheduleFetchOptions {
  method?: 'POST' | 'PUT' | 'DELETE'
  credentials: 'include'
  body?: AdminSaveSyncScheduleRequest
}

function draft(overrides: Partial<AdminSyncScheduleDraft> = {}): AdminSyncScheduleDraft {
  return { enabled: false, kind: 'daily', time: '', weekdays: [], expression: '', ...overrides }
}

function job(
  id: string,
  target: AdminSyncTarget,
  startedAt: string,
  states: Partial<Record<Provider, AdminSyncProviderState>> = {},
  state: AdminSyncJob['state'] = 'succeeded'
): AdminSyncJob {
  return {
    id,
    target,
    state,
    trigger: 'manual',
    started_at: startedAt,
    finished_at: state === 'running' ? null : '2026-08-24T12:10:00Z',
    from: '2026-08-24',
    through: '2026-08-30',
    providers: {
      ugc: { state: states.ugc ?? (target === 'ugc' || target === 'all' ? 'succeeded' : 'not_requested') },
      kinepolis: { state: states.kinepolis ?? (target === 'kinepolis' || target === 'all' ? 'succeeded' : 'not_requested') },
      pathe: { state: states.pathe ?? (target === 'pathe' || target === 'all' ? 'succeeded' : 'not_requested') },
      cgr: { state: states.cgr ?? (target === 'cgr' || target === 'all' ? 'succeeded' : 'not_requested') }
    }
  }
}

test('validates daily and weekly drafts without requiring inactive fields', () => {
  assert.deepEqual(validateAdminSyncScheduleDraft(draft()), {
    valid: false,
    errors: { time: 'Indiquez une heure.' }
  })
  assert.equal(validateAdminSyncScheduleDraft(draft({ time: '23:59' })).valid, true)
  assert.equal(validateAdminSyncScheduleDraft(draft({ time: '24:00' })).errors.time, 'Utilisez une heure valide au format HH:MM.')

  const weekly = draft({ kind: 'weekly', time: '07:45' })
  assert.equal(validateAdminSyncScheduleDraft(weekly).errors.weekdays, 'Sélectionnez au moins un jour.')
  assert.equal(validateAdminSyncScheduleDraft({ ...weekly, weekdays: ['fri'] }).valid, true)
})

test('validates and canonicalizes five-field cron drafts', () => {
  assert.equal(validateAdminSyncScheduleDraft(draft({ kind: 'cron' })).errors.expression, 'Indiquez une expression cron.')
  assert.equal(validateAdminSyncScheduleDraft(draft({ kind: 'cron', expression: '30 9 * *' })).errors.expression, 'L’expression cron doit contenir exactement cinq champs.')
  assert.equal(validateAdminSyncScheduleDraft(draft({ kind: 'cron', expression: '30 9 * * 1 extra' })).valid, false)
  assert.equal(validateAdminSyncScheduleDraft(draft({ kind: 'cron', expression: `${'é'.repeat(128)} * * * *` })).errors.expression, 'L’expression cron ne peut pas dépasser 255 octets.')
  assert.equal(validateAdminSyncScheduleDraft(draft({ kind: 'cron', expression: '  30\t9  * * 1  ' })).valid, true)
  assert.deepEqual(buildAdminSyncScheduleRequest(draft({ enabled: true, kind: 'cron', expression: '  30\t9  * * 1  ' })), {
    enabled: true,
    schedule: { kind: 'cron', expression: '30 9 * * 1' }
  })
})

test('builds weekly requests in canonical weekday order without duplicates', () => {
  assert.deepEqual(buildAdminSyncScheduleRequest(draft({ kind: 'weekly', time: '08:15', weekdays: ['fri', 'mon', 'fri'] })), {
    enabled: false,
    schedule: { kind: 'weekly', time: '08:15', weekdays: ['mon', 'fri'] }
  })
})

test('creates independent blank and persisted drafts for repeated targets', () => {
  const first = blankAdminSyncScheduleDraft()
  const second = blankAdminSyncScheduleDraft()
  first.weekdays.push('mon')
  assert.deepEqual(second.weekdays, [])

  const item: AdminSyncScheduleItem = {
    id: '9007199254740993',
    target: 'ugc',
    revision: 4,
    enabled: true,
    schedule: { kind: 'weekly', time: '08:15', weekdays: ['mon', 'fri'] },
    next_runs: [],
    updated_at: '2026-08-29T12:00:00Z'
  }
  const persisted = adminSyncScheduleDraftFromItem(item)
  persisted.weekdays.push('sun')

  assert.deepEqual(item.schedule.weekdays, ['mon', 'fri'])
  assert.notEqual(adminSyncScheduleDraftFingerprint(persisted), adminSyncScheduleDraftFingerprint(adminSyncScheduleDraftFromItem(item)))
})

test('reports target availability without treating disabled configuration as unavailable data', () => {
  const available = ['ugc', 'tmdb_metadata_refresh'] as const
  assert.equal(isAdminSyncScheduleTargetAvailable('ugc', available), true)
  assert.equal(isAdminSyncScheduleTargetAvailable('tmdb_metadata_refresh', available), true)
  assert.equal(isAdminSyncScheduleTargetAvailable('cgr', available), false)
})

test('maps schedule CRUD and availability failures to safe French recovery messages', () => {
  const failure = (code: string) => ({ data: { error: { code } } })

  assert.equal(getFrenchAdminApiError(failure('sync_schedule_not_found')), 'Cette planification n’existe plus. Actualisez la page.')
  assert.equal(getFrenchAdminApiError(failure('sync_schedule_target_unavailable')), 'Cette synchronisation est temporairement indisponible. Désactivez la planification ou réessayez plus tard.')
  assert.equal(getFrenchAdminApiError(failure('sync_schedule_failed')), 'La planification n’a pas pu être enregistrée. Vos modifications sont conservées, réessayez plus tard.')
})

test('uses create, update, and delete schedule endpoints with credentials and decimal-string IDs', async () => {
  const calls: Array<{ url: string, options: AdminScheduleFetchOptions }> = []
  const input: AdminSaveSyncScheduleRequest = { enabled: false, schedule: { kind: 'daily', time: '09:30' } }
  const saved: AdminSyncScheduleItem = {
    id: '9007199254740993',
    target: 'tmdb_metadata_refresh',
    revision: 1,
    enabled: false,
    schedule: input.schedule,
    next_runs: [
      '2026-08-30T07:30:00Z',
      '2026-08-31T07:30:00Z',
      '2026-09-01T07:30:00Z',
      '2026-09-02T07:30:00Z',
      '2026-09-03T07:30:00Z'
    ],
    updated_at: '2026-08-29T12:00:00Z'
  }
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminScheduleFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve(saved)
    }
  })

  const api = useMesSeancesApi()
  assert.equal((await api.adminCreateSyncSchedule('tmdb_metadata_refresh', input)).id, '9007199254740993')
  assert.equal((await api.adminUpdateSyncSchedule('tmdb_metadata_refresh', saved.id, input)).id, '9007199254740993')
  await api.adminDeleteSyncSchedule('tmdb_metadata_refresh', saved.id)

  assert.deepEqual(calls, [
    {
      url: 'http://localhost:8080/api/v1/admin/sync-schedules/tmdb_metadata_refresh',
      options: { method: 'POST', credentials: 'include', body: input }
    },
    {
      url: 'http://localhost:8080/api/v1/admin/sync-schedules/tmdb_metadata_refresh/9007199254740993',
      options: { method: 'PUT', credentials: 'include', body: input }
    },
    {
      url: 'http://localhost:8080/api/v1/admin/sync-schedules/tmdb_metadata_refresh/9007199254740993',
      options: { method: 'DELETE', credentials: 'include' }
    }
  ])
})

test('page keeps five target sections and per-entry CRUD state without component tooling', async () => {
  const page = await readFile(new URL('../app/pages/admin/sync-schedules.vue', import.meta.url), 'utf8')

  assert.match(page, /const targets = \[\.\.\.providers, 'tmdb_metadata_refresh'\] as const/)
  assert.match(page, /item\.target === section\.target/)
  assert.match(page, /api\.adminCreateSyncSchedule\(entry\.target, request\)/)
  assert.match(page, /api\.adminUpdateSyncSchedule\(entry\.target, entry\.persisted\.id, request\)/)
  assert.match(page, /api\.adminDeleteSyncSchedule\(entry\.target, entry\.persisted\.id\)/)
  assert.match(page, /entry\.draft\.enabled && !targetAvailable\(entry\.target\)/)
  assert.match(page, /v-if="isProvider\(section\.target\)"/)
})

test('selects newest terminal requested provider run across direct and all targets', () => {
  const directOld = job('direct-old', 'ugc', '2026-08-24T08:00:00Z')
  const allNewest = job('all-newest', 'all', '2026-08-24T11:00:00Z')
  const otherProvider = job('other', 'kinepolis', '2026-08-24T12:00:00Z')
  const allNotRequested = job('not-requested', 'all', '2026-08-24T13:00:00Z', { ugc: 'not_requested' })
  const running = job('running', 'ugc', '2026-08-24T14:00:00Z', {}, 'running')

  assert.equal(selectLatestProviderRun('ugc', running, [otherProvider, directOld, allNotRequested, allNewest])?.id, 'all-newest')
  assert.equal(selectLatestProviderRun('kinepolis', null, [directOld, otherProvider])?.id, 'other')
  assert.equal(selectLatestProviderRun('kinepolis', null, [allNewest])?.id, 'all-newest')
  assert.equal(selectLatestProviderRun('pathe', null, [allNewest])?.id, 'all-newest')
  assert.equal(selectLatestProviderRun('pathe', null, [job('pathe-only', 'pathe', '2026-08-24T15:00:00Z')])?.id, 'pathe-only')
  assert.equal(selectLatestProviderRun('cgr', null, [allNewest])?.id, 'all-newest')
  assert.equal(selectLatestProviderRun('cgr', null, [job('cgr-only', 'cgr', '2026-08-24T15:30:00Z')])?.id, 'cgr-only')
  assert.equal(selectLatestProviderRun('kinepolis', null, [job('ugc-only', 'ugc', '2026-08-24T15:00:00Z')]), null)
  assert.equal(selectLatestProviderRun('kinepolis', null, [job('failed-all', 'all', '2026-08-24T16:00:00Z', { kinepolis: 'failed' }, 'failed')])?.id, 'failed-all')
})

test('deduplicates current and history entries and returns null without eligible terminal runs', () => {
  const current = job('same', 'all', '2026-08-24T10:00:00Z')
  const duplicate = { ...current, started_at: '2026-08-24T15:00:00Z' }
  assert.equal(selectLatestProviderRun('ugc', current, [duplicate])?.started_at, '2026-08-24T10:00:00Z')
  assert.equal(selectLatestProviderRun('ugc', null, [job('other', 'kinepolis', '2026-08-24T12:00:00Z')]), null)
})

test('calculates whole-run duration from finished or supplied current time', () => {
  const completed = job('completed', 'ugc', '2026-08-24T12:00:00Z')
  assert.equal(adminSyncRunDurationMilliseconds(completed, Date.parse('2026-08-24T13:00:00Z')), 600_000)

  const running = job('running', 'ugc', '2026-08-24T12:00:00Z', {}, 'running')
  assert.equal(adminSyncRunDurationMilliseconds(running, Date.parse('2026-08-24T12:00:30Z')), 30_000)
  assert.equal(adminSyncRunDurationMilliseconds({ ...completed, finished_at: '2026-08-24T11:00:00Z' }, Date.now()), 0)
  assert.equal(adminSyncRunDurationMilliseconds({ ...completed, started_at: 'invalid' }, Date.now()), 0)
})
