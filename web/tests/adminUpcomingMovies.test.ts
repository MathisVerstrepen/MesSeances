import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { useAdminUpcomingMovies } from '../app/composables/useAdminUpcomingMovies.ts'
import { getFrenchAdminApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type { AdminSetUpcomingDecisionRequest, AdminUpcomingMovie, AdminUpcomingMoviesQuery, AdminUpcomingMoviesResponse, UpcomingReviewDecision } from '../app/types/api.ts'
import { frenchReleaseTypeLabels, normalizeUpcomingReviewSearch, parseUpcomingReviewRoute, upcomingReviewApiQuery, upcomingReviewFilters, upcomingReviewReasonLabels, upcomingReviewRouteQuery, upcomingReviewVisibility } from '../app/utils/adminUpcomingMovies.ts'

const state = { filter: 'needs_review', q: '', page: 1 } as const
function movie(overrides: Partial<AdminUpcomingMovie> = {}): AdminUpcomingMovie {
  return {
    tmdb_id: 42, public_movie_id: '7', slug: 'film-7', title: 'Film test', poster_url: null, french_release_date: '2026-10-07',
    active: true, in_window: true, publicly_visible: true, assessment_status: 'assessed', assessed_at: '2026-09-13T12:00:00Z',
    french_releases: [{ type: 2, date: '2026-10-07', note: '<img src=x onerror=alert(1)> Séance unique' }],
    reason_codes: ['limited_only', 'single_screening_note'], decision: 'unreviewed', revision: 1, ...overrides
  }
}
function response(items = [movie()], total = items.length): AdminUpcomingMoviesResponse {
  return { items, total, limit: 50, offset: 0 }
}
function failure(status: number, code = 'unknown') {
  return { status, data: { error: { code, message: 'private diagnostic' } } }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

test('route defaults, five filters, canonical search and bounded pagination match the frozen contract', () => {
  assert.deepEqual(parseUpcomingReviewRoute({}), state)
  assert.deepEqual(upcomingReviewFilters.map(filter => filter.value), ['needs_review', 'pending_assessment', 'approved', 'excluded', 'all'])
  for (const { value } of upcomingReviewFilters) {
    const parsed = parseUpcomingReviewRoute({ filter: value, q: '  42  ', page: '2' })
    assert.deepEqual(upcomingReviewApiQuery(parsed), { filter: value, search: '42', limit: 50, offset: 50 })
    assert.deepEqual(parseUpcomingReviewRoute(upcomingReviewRouteQuery(parsed)), parsed)
  }
  assert.deepEqual(parseUpcomingReviewRoute({ filter: ['all', 'approved'], q: ['x', 'y'], page: '-2' }), state)
  for (const page of ['1e2', '0', '9007199254740992', '42949674']) assert.equal(parseUpcomingReviewRoute({ page }).page, 1)
  assert.equal(upcomingReviewApiQuery(parseUpcomingReviewRoute({ page: '42949673' })).offset, 2147483600)
  assert.equal(upcomingReviewApiQuery(parseUpcomingReviewRoute({ q: '0042' })).search, '0042', 'never reinterpret noncanonical ID as canonical')
  assert.equal(upcomingReviewApiQuery(parseUpcomingReviewRoute({ q: '%_\\' })).search, '%_\\', 'literal search is server-owned')
  assert.equal(Array.from(normalizeUpcomingReviewSearch(' 😀'.repeat(1200))).length, 1024)
  assert.deepEqual(upcomingReviewRouteQuery(state, { filter: 'all', q: 'x', page: '2', other: 'kept' }), { other: 'kept' })
})

test('labels preserve evidence meaning and visibility never infers exclusion from reasons', () => {
  assert.equal(Object.keys(upcomingReviewReasonLabels).length, 4)
  assert.equal(frenchReleaseTypeLabels[1], 'Première')
  assert.equal(frenchReleaseTypeLabels[6], 'Télévision')
  assert.equal(upcomingReviewVisibility(movie()), 'Visible')
  assert.equal(upcomingReviewVisibility(movie({ assessment_status: 'pending', reason_codes: [] })), 'Visible')
  assert.equal(upcomingReviewVisibility(movie({ decision: 'excluded', publicly_visible: false, in_window: false })), 'Exclu')
  assert.equal(upcomingReviewVisibility(movie({ decision: 'approved', publicly_visible: false, in_window: false })), 'Hors de la liste')
})

test('GET and PATCH use exact routes, credentials, abortable reads and no retry or extra fields', async () => {
  interface FetchOptions {
    method?: string
    credentials: string
    query?: AdminUpcomingMoviesQuery
    signal?: AbortSignal
    body?: AdminSetUpcomingDecisionRequest
    retry: false
  }
  const calls: Array<{ url: string; options: FetchOptions }> = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: FetchOptions) => { calls.push({ url, options }); return Promise.resolve(response()) }
  })
  const api = useMesSeancesApi()
  const signal = new AbortController().signal
  await api.adminUpcomingMovies({ filter: 'all', search: undefined, limit: 50, offset: 0 }, signal)
  await api.adminSetUpcomingDecision(42, { decision: 'excluded', expected_revision: 7 })
  assert.deepEqual(calls, [
    { url: 'http://localhost:8080/api/v1/admin/tmdb-upcoming-movies', options: { credentials: 'include', query: { filter: 'all', limit: 50, offset: 0 }, signal, retry: false } },
    { url: 'http://localhost:8080/api/v1/admin/tmdb-upcoming-movies/42/decision', options: { method: 'PATCH', credentials: 'include', body: { decision: 'excluded', expected_revision: 7 }, retry: false } }
  ])
})

test('stale GET success or error cannot replace newer rows and disposal aborts reads', async () => {
  for (const reject of [false, true]) {
    const old = deferred<AdminUpcomingMoviesResponse>()
    let calls = 0
    let oldSignal: AbortSignal | undefined
    const review = useAdminUpcomingMovies({
      adminUpcomingMovies: (_query, signal) => { if (++calls === 1) { oldSignal = signal; return old.promise } return Promise.resolve(response([movie({ title: 'Latest' })])) },
      adminSetUpcomingDecision: async () => { assert.fail('no edit expected') }
    }, async () => { assert.fail('no clamp expected') })
    const first = review.load(state)
    await review.load({ ...state, q: 'Latest' })
    assert.equal(oldSignal?.aborted, true)
    if (reject) old.reject(failure(500))
    else old.resolve(response([movie({ title: 'Old' })]))
    await first
    assert.equal(review.items.value[0]?.title, 'Latest')
    assert.equal(review.error.value, '')
    review.dispose()
  }
  const pending = deferred<AdminUpcomingMoviesResponse>()
  let signal: AbortSignal | undefined
  const review = useAdminUpcomingMovies({ adminUpcomingMovies: (_q, s) => { signal = s; return pending.promise }, adminSetUpcomingDecision: async () => movie() }, async () => {})
  const loading = review.load()
  review.dispose()
  pending.resolve(response())
  await loading
  assert.equal(signal?.aborted, true)
  assert.equal(review.loaded.value, false)
})

test('canonical poster values survive list and decision refresh without extra movie reads', async () => {
  for (const poster_url of [null, 'https://image.tmdb.org/t/p/w500/poster.jpg', 'https://messeances.fr/uploads/manual-poster.webp']) {
    let reads = 0
    const review = useAdminUpcomingMovies({
      adminUpcomingMovies: async () => { reads += 1; return response([movie({ poster_url, decision: reads > 1 ? 'approved' : 'unreviewed' })]) },
      adminSetUpcomingDecision: async () => movie({ poster_url, decision: 'approved', revision: 2 })
    }, async () => {})
    await review.load({ ...state, filter: 'all' })
    assert.equal(review.items.value[0]?.poster_url, poster_url)
    await review.decide(review.items.value[0]!, 'approved')
    assert.equal(reads, 2)
    assert.equal(review.items.value[0]?.poster_url, poster_url)
    review.dispose()
  }
})

test('all three decisions send displayed revision, suppress duplicates/no-ops and reload totals', async () => {
  for (const decision of ['approved', 'excluded', 'unreviewed'] as const) {
    const edit = deferred<AdminUpcomingMovie>()
    let reads = 0
    let edits = 0
    const initial = movie({ revision: 8, decision: decision === 'unreviewed' ? 'excluded' : 'unreviewed' })
    const review = useAdminUpcomingMovies({
      adminUpcomingMovies: async () => { reads += 1; return response(reads === 1 ? [initial] : []) },
      adminSetUpcomingDecision: (id, input) => { edits += 1; assert.equal(id, 42); assert.deepEqual(input, { decision, expected_revision: 8 }); return edit.promise }
    }, async () => {})
    await review.load()
    await review.decide(initial, initial.decision)
    assert.equal(edits, 0)
    const saving = review.decide(initial, decision)
    await review.decide(initial, decision)
    assert.equal(review.canMutate.value, false)
    assert.equal(edits, 1)
    edit.resolve(movie({ decision, revision: 9 }))
    await saving
    assert.equal(reads, 2)
    assert.equal(review.total.value, 0)
    assert.equal(review.items.value.length, 0)
    assert.notEqual(review.message.value, '')
    review.dispose()
  }
})

test('409 and 404 reload once, never replay mutation and retain exact safe feedback', async () => {
  for (const status of [409, 404]) {
    let reads = 0
    let edits = 0
    const review = useAdminUpcomingMovies({
      adminUpcomingMovies: async () => { reads += 1; return response([movie({ revision: reads })]) },
      adminSetUpcomingDecision: async () => { edits += 1; throw failure(status) }
    }, async () => {})
    await review.load()
    await review.decide(movie(), 'approved')
    assert.equal(edits, 1)
    assert.equal(reads, 2)
    assert.equal(review.items.value[0]?.revision, 2)
    assert.equal(review.message.value, status === 409 ? 'Cette évaluation a changé. La liste a été actualisée.' : 'Cette sortie n’existe plus. La liste a été actualisée.')
    review.dispose()
  }
})

test('list and uncertain mutation errors preserve rows/search and require explicit read recovery', async () => {
  for (const cause of [new Error('private'), failure(500, 'upcoming_review_update_failed')]) {
    let reads = 0
    let edits = 0
    const review = useAdminUpcomingMovies({
      adminUpcomingMovies: async query => { reads += 1; assert.equal(query.search, 'Film'); if (reads === 2) throw failure(500, 'upcoming_review_list_failed'); return response() },
      adminSetUpcomingDecision: async () => { edits += 1; throw cause }
    }, async () => {})
    await review.load({ ...state, q: 'Film' })
    await review.load()
    assert.equal(review.items.value.length, 1)
    assert.match(review.error.value, /Impossible de charger/)
    assert.equal(review.canMutate.value, false)
    await review.load()
    await review.decide(movie(), 'excluded')
    await review.decide(movie(), 'excluded')
    assert.equal(edits, 1)
    assert.equal(reads, 3)
    assert.equal(review.items.value.length, 1)
    assert.doesNotMatch(review.error.value, /private/)
    await review.load()
    assert.equal(review.canMutate.value, true)
    review.dispose()
  }
})

test('last page clamps after decision removes its only result', async () => {
  let reads = 0
  const pages: number[] = []
  const review = useAdminUpcomingMovies({
    adminUpcomingMovies: async () => { reads += 1; return response(reads === 1 ? [movie()] : [], reads === 1 ? 51 : 50) },
    adminSetUpcomingDecision: async () => movie({ decision: 'approved' })
  }, async page => { pages.push(page) })
  await review.load({ ...state, page: 2 })
  await review.decide(movie(), 'approved')
  assert.deepEqual(pages, [1])
  assert.equal(review.canMutate.value, false)
  review.dispose()
})

test('late mutation completion after unmount cannot reload or announce', async () => {
  const edit = deferred<AdminUpcomingMovie>()
  let reads = 0
  const review = useAdminUpcomingMovies({ adminUpcomingMovies: async () => { reads += 1; return response() }, adminSetUpcomingDecision: () => edit.promise }, async () => {})
  await review.load()
  const saving = review.decide(movie(), 'approved')
  review.dispose()
  edit.resolve(movie({ decision: 'approved' }))
  await saving
  assert.equal(reads, 1)
  assert.equal(review.message.value, '')
})

test('French error mappings never display diagnostic details', () => {
  for (const code of ['invalid_upcoming_review_query', 'invalid_upcoming_review_id', 'invalid_upcoming_review_update', 'upcoming_review_not_found', 'upcoming_review_conflict', 'upcoming_review_list_failed', 'upcoming_review_update_failed']) {
    assert.doesNotMatch(getFrenchAdminApiError(failure(400, code)), /private/)
  }
})

test('page is client-only authenticated, accessible, escaped and preserves dashboard sync/history', async () => {
  const page = await readFile(new URL('../app/pages/admin/upcoming-movies.vue', import.meta.url), 'utf8')
  const dashboard = await readFile(new URL('../app/pages/admin/index.vue', import.meta.url), 'utf8')
  assert.match(page, /definePageMeta\(\{ middleware: 'admin-auth' \}\)/)
  assert.match(page, /onMounted\(/)
  assert.match(page, /onBeforeUnmount\(.*cancelSearch\(\); dispose\(\)/)
  assert.match(page, /router\.push\(/)
  assert.match(page, /router\.replace\(/)
  assert.match(page, /watch\(\(\) => route\.query/)
  assert.match(page, /UPCOMING_REVIEW_SEARCH_DELAY/)
  assert.match(page, /<label for="review-search"[^>]*>Titre ou ID TMDB/)
  assert.match(page, /<details v-if="movie.assessment_status === 'assessed'"/)
  assert.match(page, /<summary[^>]+min-h-11/)
  assert.match(page, /wrap-anywhere text-muted">\{\{ release.note \}\}/)
  assert.match(page, /movie.assessment_status === 'pending'/)
  assert.match(page, /Évaluation après la prochaine synchronisation réussie/)
  assert.match(page, /Les signalements restent visibles jusqu’à leur exclusion\./)
  assert.match(page, /role="status" aria-live="polite"/)
  assert.match(page, /role="alert"/)
  assert.match(page, /<PosterImage\s+:src="movie.poster_url"/)
  assert.match(page, /sizes="\(min-width: 640px\) 96px, 64px"/)
  assert.match(page, /aspect-2\/3 w-16/)
  assert.match(page, /fallback-marker="upcoming-review"/)
  assert.match(page, /sm:grid-cols-3/)
  assert.match(page, /inline-flex h-6 items-center whitespace-nowrap/)
  assert.match(page, /border border-line bg-surface px-3 py-2/)
  assert.doesNotMatch(page, /button-secondary|image\.tmdb\.org/)
  assert.doesNotMatch(page, /v-html|localStorage|sessionStorage|adminStartUpcomingSync|useAsyncData/)
  for (const decision of ['approved', 'excluded', 'unreviewed'] satisfies UpcomingReviewDecision[]) assert.ok(page.includes(`decide(movie, '${decision}')`))
  assert.match(dashboard, /to="\/admin\/upcoming-movies"/)
  assert.match(dashboard, /@click="startUpcomingSync"/)
})
