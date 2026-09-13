import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { getFrenchAdminApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import {
  adminPendingMatchesForFilter,
  adminReplacementTMDBId,
  adminTMDBMatchedSearch,
  adminTMDBMetadataRefreshPresentation,
  adminTMDBMatchesTab,
  adminTMDBSearchQuery,
  adminTMDBSearchURL,
  ADMIN_TMDB_MATCH_SEARCH_DEBOUNCE_MS,
  shouldRefreshAdminTMDBMatchLists
} from '../app/utils/adminTmdbMatches.ts'
import type { AdminPendingMatchesQuery, AdminTMDBMetadataRefreshJob } from '../app/types/api.ts'

const matches = [
  { id: 'review', status: 'review_required' as const },
  { id: 'unmatched', status: 'unmatched' as const },
  { id: 'rejected', status: 'rejected' as const },
  { id: 'matched', status: 'matched' as const }
]

interface AdminFetchOptions {
  method?: 'POST'
  credentials: 'include'
  body?: unknown
  query?: AdminPendingMatchesQuery
}

test('keeps rejected matches out of the unresolved section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'unresolved').map(match => match.id), ['review', 'unmatched'])
})

test('keeps only rejected matches in the Non-TMDB section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'rejected').map(match => match.id), ['rejected'])
})

test('keeps only matched movies in the associated TMDB section', () => {
  assert.deepEqual(adminPendingMatchesForFilter(matches, 'matched').map(match => match.id), ['matched'])
})

test('normalizes match tabs and defaults invalid values to unresolved', () => {
  assert.equal(adminTMDBMatchesTab('rejected'), 'rejected')
  assert.equal(adminTMDBMatchesTab('matched'), 'matched')
  assert.equal(adminTMDBMatchesTab('invalid'), 'unresolved')
  assert.equal(adminTMDBMatchesTab(undefined), 'unresolved')
})

test('trims matched search without truncating or changing literal text', () => {
  const overlong = ` ${'é'.repeat(1025)} `
  assert.equal(adminTMDBMatchedSearch('  Alien_%  '), 'Alien_%')
  assert.equal(adminTMDBMatchedSearch('   '), '')
  assert.equal(adminTMDBMatchedSearch(undefined), '')
  assert.equal(adminTMDBMatchedSearch(overlong), 'é'.repeat(1025))
  assert.equal(ADMIN_TMDB_MATCH_SEARCH_DEBOUNCE_MS, 350)
})

test('normalizes manual TMDB search titles by removing balanced annotations only', () => {
  const cases = [
    ['America America 2 (Version Kannada)', 'America America 2'],
    ['  America\tAmerica 2 (Version Kannada) (VOSTFR)  ', 'America America 2'],
    ['America (Version (Kannada)) America 2', 'America America 2'],
    ['America(Version Kannada)America 2', 'America America 2'],
    ['Alien', 'Alien'],
    ['  Le\u00a0\u00a0Film\n2 ', 'Le Film 2'],
    ['Film (unfinished', 'Film (unfinished'],
    ['Film ) title (VO)', 'Film ) title'],
    ['Film (unfinished (VO)', 'Film (unfinished'],
    ['(Version (Kannada)) ()', ''],
    ['   ', '']
  ] as const

  for (const [title, expected] of cases) {
    assert.equal(adminTMDBSearchQuery(title), expected, title)
  }
})

test('encodes normalized TMDB queries on a fixed HTTPS movie-search URL', () => {
  assert.equal(adminTMDBSearchURL('America America 2 (Version Kannada)'), 'https://www.themoviedb.org/search/movie?query=America+America+2')
  const title = 'Été & nuit + #1 ? / 100% "映画" (VOSTFR)'
  const url = new URL(adminTMDBSearchURL(title)!)
  assert.equal(url.origin, 'https://www.themoviedb.org')
  assert.equal(url.pathname, '/search/movie')
  assert.equal(url.hash, '')
  assert.deepEqual([...url.searchParams], [['query', 'Été & nuit + #1 ? / 100% "映画"']])
  assert.equal(adminTMDBSearchURL('(Version Kannada)'), undefined)
  assert.equal(adminTMDBSearchURL(''), undefined)
  assert.doesNotThrow(() => adminTMDBSearchURL('Film \ud800'))
})

test('shows safe new-tab TMDB search alongside generic provider links in shared movie cards', async () => {
  const page = await readFile(new URL('../app/pages/admin/tmdb-matches.vue', import.meta.url), 'utf8')
  const sourceSection = page.match(/<section class="min-w-0" :aria-labelledby="`source-title-\$\{domKey\(match\)\}`">[\s\S]*?<\/section>/)?.[0]
  assert.ok(sourceSection)
  const searchLink = sourceSection.match(/<a v-if="adminTMDBSearchURL\(match.source_title\)"[^>]*>[\s\S]*?<\/a>/)?.[0]
  assert.ok(searchLink)
  assert.match(searchLink, /:href="adminTMDBSearchURL\(match.source_title\)"/)
  assert.match(searchLink, /target="_blank" rel="noopener noreferrer"/)
  assert.match(searchLink, /aria-label="TMDB, ouverture dans un nouvel onglet"/)
  assert.match(sourceSection, /<a v-if="match.source_detail_url" :href="match.source_detail_url" target="_blank" rel="noopener noreferrer"/)
  assert.match(sourceSection, /Voir sur \$\{providerLabel\(match.source_provider\)\}/)
  assert.doesNotMatch(sourceSection, /activeTab|match\.status|source_provider ===/)
  for (const [provider, label] of Object.entries({ ugc: 'UGC', kinepolis: 'Kinepolis', pathe: 'Pathé', cgr: 'CGR', megarama: 'Megarama' })) {
    assert.ok(page.includes(`${provider}: '${label}'`))
  }
})

test('sends trimmed search only in non-empty matched-list query objects', async () => {
  const calls: Array<{ url: string, options: AdminFetchOptions }> = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve({ items: [], limit: 20, offset: 0 })
    }
  })

  const api = useMesSeancesApi()
  await api.adminPendingMatches('unresolved', 20, 0, 'ignored')
  await api.adminPendingMatches('matched', 20, 20, '   ')
  await api.adminPendingMatches('matched', 20, 40, '  Alien_%  ')

  assert.deepEqual(calls, [
    {
      url: 'http://localhost:8080/api/v1/admin/tmdb-matches',
      options: { credentials: 'include', query: { status: 'unresolved', limit: 20, offset: 0 } }
    },
    {
      url: 'http://localhost:8080/api/v1/admin/tmdb-matches',
      options: { credentials: 'include', query: { status: 'matched', limit: 20, offset: 20 } }
    },
    {
      url: 'http://localhost:8080/api/v1/admin/tmdb-matches',
      options: { credentials: 'include', query: { status: 'matched', limit: 20, offset: 40, search: 'Alien_%' } }
    }
  ])
})

test('accepts only a positive safe replacement TMDB ID different from the current ID', () => {
  assert.equal(adminReplacementTMDBId(' 456 ', 123), 456)
  assert.equal(adminReplacementTMDBId('123', 123), null)
  assert.equal(adminReplacementTMDBId('0', 123), null)
  assert.equal(adminReplacementTMDBId('-1', 123), null)
  assert.equal(adminReplacementTMDBId('1.5', 123), null)
  assert.equal(adminReplacementTMDBId('9007199254740992', 123), null)
})

test('posts a matched correction with credentials, encoded identity, and optimistic token', async () => {
  const calls: Array<{ url: string, options: AdminFetchOptions }> = []
  const input = { tmdb_id: 456, expected_updated_at: '2026-08-30T12:34:56.123456Z' }
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve({ status: 'matched' })
    }
  })

  const response = await useMesSeancesApi().adminCorrectMatch('pathe', 'film/123', input)

  assert.deepEqual(response, { status: 'matched' })
  assert.deepEqual(calls, [{
    url: 'http://localhost:8080/api/v1/admin/tmdb-matches/pathe/film%2F123/correct',
    options: { method: 'POST', credentials: 'include', body: input }
  }])
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

test('posts selected sources to the encoded existing local group endpoint', async () => {
  const calls: Array<{ url: string, options: AdminFetchOptions }> = []
  const input = {
    members: [
      { source_provider: 'ugc' as const, source_movie_id: '123' },
      { source_provider: 'pathe' as const, source_movie_id: 'film/456' }
    ]
  }
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve({ status: 'members_added', local_movie_id: 'local-film/7' })
    }
  })

  const response = await useMesSeancesApi().adminAddLocalMovieMembers('local-film/7', input)

  assert.deepEqual(response, { status: 'members_added', local_movie_id: 'local-film/7' })
  assert.deepEqual(calls, [{
    url: 'http://localhost:8080/api/v1/admin/local-movie-groups/local-film%2F7/members',
    options: { method: 'POST', credentials: 'include', body: input }
  }])
})

test('maps matched-metadata refresh failures to safe French messages', () => {
  const failure = (code: string) => ({ data: { error: { code } } })

  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_in_progress')), 'Une autre opération TMDB est déjà en cours.')
  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_unavailable')), 'Le service d’actualisation des métadonnées TMDB est temporairement indisponible.')
  assert.equal(getFrenchAdminApiError(failure('tmdb_metadata_refresh_failed')), 'L’actualisation des métadonnées TMDB a échoué. Réessayez plus tard.')
})

test('maps stale matched corrections to the list-refresh conflict message', () => {
  assert.equal(
    getFrenchAdminApiError({ status: 409, data: { error: { code: 'review_conflict' } } }),
    'Cette correspondance a changé. Les listes ont été actualisées.'
  )
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

test('keeps matched search route-owned, debounced, server-side, and cleaned up', async () => {
  const page = await readFile(new URL('../app/pages/admin/tmdb-matches.vue', import.meta.url), 'utf8')

  assert.match(page, /const OWNED_QUERY_KEYS = \['tab', 'q', 'page', 'rejected_page', 'matched_page', 'groups_page'\] as const/)
  assert.match(page, /q: nextSearch \|\| undefined/)
  assert.match(page, /const requestedSearch = adminTMDBMatchedSearch\(singularQueryValue\(route\.query\.q\)\)/)
  assert.match(page, /matchedSearchInput\.value = requestedSearch/)
  assert.match(page, /matchedPage\.value !== lastLoadMatchedPage \|\| matchedSearch\.value !== lastLoadMatchedSearch/)
  assert.match(page, /api\.adminPendingMatches\('matched', PAGE_SIZE, matchedOffset\.value, matchedSearch\.value \|\| undefined\)/)

  assert.match(page, /function updateMatchedSearch\(\) \{\s*clearMatchedSearchTimer\(\)/)
  assert.match(page, /}, ADMIN_TMDB_MATCH_SEARCH_DEBOUNCE_MS\)/)
  assert.match(page, /adminQuery\(page\.value, rejectedPage\.value, 1, groupsPage\.value, activeTab\.value, search\)/)
  assert.match(page, /onBeforeUnmount\(\(\) => \{[\s\S]*clearMatchedSearchTimer\(\)[\s\S]*clearMetadataRefreshPolling\(\)/)

  assert.match(page, /v-if="activeTab === 'matched'" for="matched-search"/)
  assert.match(page, /id="matched-search"[\s\S]*maxlength="1024"[\s\S]*@input="updateMatchedSearch"/)
  assert.match(page, /Aucun film associé ne correspond à cette recherche\./)
  assert.doesNotMatch(page, /\.filter\([\s\S]{0,120}matchedSearch/)
})
