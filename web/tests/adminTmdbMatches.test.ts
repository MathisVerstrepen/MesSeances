import assert from 'node:assert/strict'
import test from 'node:test'
import { getFrenchAdminApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import {
  adminPendingMatchesForFilter,
  adminTMDBMetadataRefreshPresentation,
  shouldRefreshAdminTMDBMatchLists
} from '../app/utils/adminTmdbMatches.ts'
import type { AdminTMDBMetadataRefreshJob } from '../app/types/api.ts'

const matches = [
  { id: 'review', status: 'review_required' as const },
  { id: 'unmatched', status: 'unmatched' as const },
  { id: 'rejected', status: 'rejected' as const }
]

interface AdminFetchOptions {
  method?: 'POST'
  credentials: 'include'
}

test('keeps rejected matches out of the unresolved section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'unresolved').map(match => match.id), ['review', 'unmatched'])
})

test('keeps only rejected matches in the Non-TMDB section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'rejected').map(match => match.id), ['rejected'])
})

test('posts the matched-metadata refresh with admin credentials and no body', async () => {
  const calls: Array<{ url: string, options: AdminFetchOptions }> = []
  const runningJob = {
    state: 'running' as const,
    started_at: '2026-08-28T12:00:00Z',
    finished_at: null,
    summary: null
  }
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve({ job: runningJob })
    }
  })

  const response = await useMesSeancesApi().adminRefreshTMDBMetadata()

  assert.deepEqual(response, { job: runningJob })
  assert.deepEqual(calls, [{
    url: 'http://localhost:8080/api/v1/admin/tmdb-matches/refresh-metadata',
    options: { method: 'POST', credentials: 'include' }
  }])
})

test('loads the matched-metadata refresh status with admin credentials', async () => {
  const calls: Array<{ url: string, options: AdminFetchOptions }> = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve({ job: null })
    }
  })

  const response = await useMesSeancesApi().adminTMDBMetadataRefreshStatus()

  assert.deepEqual(response, { job: null })
  assert.deepEqual(calls, [{
    url: 'http://localhost:8080/api/v1/admin/tmdb-matches/refresh-metadata',
    options: { credentials: 'include' }
  }])
})

test('maps matched-metadata refresh failures to safe French messages', () => {
  const failure = (code: string) => ({ data: { error: { code } } })

  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_in_progress')), 'Une autre opération TMDB est déjà en cours.')
  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_unavailable')), 'Le service d’actualisation des métadonnées TMDB est temporairement indisponible.')
  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_failed')), 'L’actualisation des métadonnées TMDB a échoué. Réessayez plus tard.')
})

test('maps matched-metadata job states to loading, counters, and safe failure UI', () => {
  const startedAt = '2026-08-28T12:00:00Z'
  const summary = { processed: 4, updated: 2, unchanged: 1, failed: 1 }
  const jobs = {
    running: { state: 'running', started_at: startedAt, finished_at: null, summary: null },
    succeeded: { state: 'succeeded', started_at: startedAt, finished_at: '2026-08-28T12:03:00Z', summary },
    failed: { state: 'failed', started_at: startedAt, finished_at: '2026-08-28T12:01:00Z', summary: null, error_code: 'refresh_failed' }
  } satisfies Record<'running' | 'succeeded' | 'failed', AdminTMDBMetadataRefreshJob>

  assert.deepEqual(adminTMDBMetadataRefreshPresentation(null), { running: false, summary: null, error: '' })
  assert.deepEqual(adminTMDBMetadataRefreshPresentation(jobs.running), { running: true, summary: null, error: '' })
  assert.deepEqual(adminTMDBMetadataRefreshPresentation(jobs.succeeded), { running: false, summary, error: '' })
  assert.deepEqual(adminTMDBMetadataRefreshPresentation(jobs.failed), {
    running: false,
    summary: null,
    error: 'L’actualisation des métadonnées TMDB a échoué. Réessayez plus tard.'
  })
})

test('refreshes match lists once only after an observed running job succeeds', () => {
  const startedAt = '2026-08-28T12:00:00Z'
  const running: AdminTMDBMetadataRefreshJob = { state: 'running', started_at: startedAt, finished_at: null, summary: null }
  const succeeded: AdminTMDBMetadataRefreshJob = {
    state: 'succeeded',
    started_at: startedAt,
    finished_at: '2026-08-28T12:03:00Z',
    summary: { processed: 4, updated: 2, unchanged: 1, failed: 1 }
  }

  assert.equal(shouldRefreshAdminTMDBMatchLists(null, succeeded, new Set()), false)
  assert.equal(shouldRefreshAdminTMDBMatchLists(startedAt, running, new Set()), false)
  assert.equal(shouldRefreshAdminTMDBMatchLists(startedAt, succeeded, new Set()), true)
  assert.equal(shouldRefreshAdminTMDBMatchLists(startedAt, succeeded, new Set([startedAt])), false)
})
