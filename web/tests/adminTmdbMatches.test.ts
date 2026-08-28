import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { getFrenchAdminApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { adminPendingMatchesForFilter } from '../app/utils/adminTmdbMatches.ts'

const matches = [
  { id: 'review', status: 'review_required' as const },
  { id: 'unmatched', status: 'unmatched' as const },
  { id: 'rejected', status: 'rejected' as const }
]

interface AdminFetchOptions {
  method: 'POST'
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
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve({ processed: 4, updated: 2, unchanged: 1, failed: 1 })
    }
  })

  const summary = await useMesSeancesApi().adminRefreshTMDBMetadata()

  assert.deepEqual(summary, { processed: 4, updated: 2, unchanged: 1, failed: 1 })
  assert.deepEqual(calls, [{
    url: 'http://localhost:8080/api/v1/admin/tmdb-matches/refresh-metadata',
    options: { method: 'POST', credentials: 'include' }
  }])
})

test('maps matched-metadata refresh failures to safe French messages', () => {
  const failure = (code: string) => ({ data: { error: { code } } })

  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_in_progress')), 'Une autre opération TMDB est déjà en cours.')
  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_unavailable')), 'Le service d’actualisation des métadonnées TMDB est temporairement indisponible.')
  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_failed')), 'L’actualisation des métadonnées TMDB a échoué. Réessayez plus tard.')
})

test('exposes distinct matched-metadata loading and summary states under shared mutation gating', async () => {
  const page = await readFile(new URL('../app/pages/admin/tmdb-matches.vue', import.meta.url), 'utf8')

  assert.match(page, /definePageMeta\(\{ middleware: 'admin-auth' \}\)/)
  assert.match(page, />Métadonnées TMDB associées</)
  assert.match(page, /'Relancer les films non résolus'/)
  assert.match(page, /@click="refreshTMDBMetadata"/)
  assert.match(page, /metadataRefreshPending\.value/)
  assert.match(page, /rerunPending\.value \|\| metadataRefreshPending\.value/)
  assert.match(page, /metadataRefreshSummary\.processed/)
  assert.match(page, /metadataRefreshSummary\.updated/)
  assert.match(page, /metadataRefreshSummary\.unchanged/)
  assert.match(page, /metadataRefreshSummary\.failed/)
  assert.match(page, /Promise\.all\(\[loadMatches\(true\), loadRejectedMatches\(true\)\]\)/)
})
