import assert from 'node:assert/strict'
import test from 'node:test'
import type { AdminSyncJob, AdminSyncProviderState, AdminSyncTarget, Provider } from '../app/types/api.ts'
import {
  adminSyncRunDurationMilliseconds,
  buildAdminSyncScheduleRequest,
  selectLatestProviderRun,
  validateAdminSyncScheduleDraft,
  type AdminSyncScheduleDraft
} from '../app/utils/adminSyncSchedules.ts'

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
