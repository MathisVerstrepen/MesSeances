import assert from 'node:assert/strict'
import test from 'node:test'
import type { AdminSyncSource, Provider } from '../app/types/api.ts'
import {
  adminSyncSourceProviderLabel,
  groupAdminSyncSources,
} from '../app/utils/adminSyncSources.ts'

function source(
  provider: Provider,
  key: string,
  name: string,
  enabled: boolean,
  cnc_id?: string,
): AdminSyncSource {
  return {
    provider,
    key,
    name,
    enabled,
    cnc_id,
    updated_at: '2026-11-25T08:00:00Z',
  }
}

const sources = [
  source('scare', '1854', 'Café des Images (Hérouville)', true, '195741'),
  source('scare', '1000035', 'Le Balzac (Paris)', false, '35'),
  source('ticketingcine', 'EMS0963', 'Amphi (Vienne)', false, '308102'),
]

test('groups sources by provider and counts before filtering', () => {
  const groups = groupAdminSyncSources(sources, '', 'all')
  assert.deepEqual(
    groups.map((g) => [
      g.provider,
      g.label,
      g.enabled,
      g.total,
      g.sources.length,
    ]),
    [
      ['scare', 'SCARE (open data)', 1, 2, 2],
      ['ticketingcine', 'TicketingCiné', 0, 1, 1],
    ],
  )
  assert.equal(adminSyncSourceProviderLabel('webedia'), 'Webedia Movies Pro')
})

test('filters by accent-insensitive search on name, key and CNC number, and by status', () => {
  const byName = groupAdminSyncSources(sources, 'cafe des', 'all')
  assert.deepEqual(
    byName[0]?.sources.map((s) => s.key),
    ['1854'],
  )
  assert.deepEqual(byName[1]?.sources, [])
  assert.equal(byName[1]?.total, 1)
  assert.deepEqual(
    groupAdminSyncSources(sources, 'ems0963', 'all')[1]?.sources.map(
      (s) => s.key,
    ),
    ['EMS0963'],
  )
  assert.deepEqual(
    groupAdminSyncSources(sources, '35', 'all')[0]?.sources.map((s) => s.key),
    ['1000035'],
  )
  assert.deepEqual(
    groupAdminSyncSources(sources, '', 'enabled').flatMap((g) =>
      g.sources.map((s) => s.key),
    ),
    ['1854'],
  )
  assert.deepEqual(
    groupAdminSyncSources(sources, '', 'disabled').flatMap((g) =>
      g.sources.map((s) => s.key),
    ),
    ['1000035', 'EMS0963'],
  )
})
