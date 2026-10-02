import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { LocationQuery } from 'vue-router'
import type {
  MovieShowtimesQuery,
  MovieShowtimesResponse,
} from '../app/types/api.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'
import * as filters from '../app/utils/showtimeFilters.ts'
import { isBroadTheaterSelection } from '../app/utils/cinemaSelection.ts'
import { isShowtimeFormat } from '../app/utils/formats.ts'
import { resolveShowtimeEnd } from '../app/utils/showtimeEnd.ts'

const TODAY = '2027-06-27'
const TOMORROW = '2027-06-28'
const source = await readFile(
  new URL('../app/pages/film/[slug].vue', import.meta.url),
  'utf8',
)
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile(
  'film.ts',
  script,
  ts.ScriptTarget.Latest,
  true,
)
const prefix = script.split('\nhydrateRoute()\n')[0]!
const selectedStatements = parsed.statements.filter((node) => {
  if (!ts.isExpressionStatement(node) || !ts.isCallExpression(node.expression))
    return false
  const name = node.expression.expression.getText(parsed)
  if (name === 'onBeforeUnmount') return true
  if (name !== 'watch') return false
  const arg = node.expression.arguments[0]?.getText(parsed) ?? ''
  return (
    arg.includes('selectionScopeKey') ||
    arg.includes('route.query') ||
    arg === 'slug'
  )
})
const raw = `${prefix}\n${selectedStatements.map((node) => node.getFullText(parsed)).join('\n')}`
const withoutImports = ts.createSourceFile(
  'slice.ts',
  raw,
  ts.ScriptTarget.Latest,
  true,
)
const compiled = ts.transpileModule(
  withoutImports.statements
    .filter((node) => !ts.isImportDeclaration(node))
    .map((node) => node.getFullText(withoutImports))
    .join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText

function response(
  slug: string,
  query: MovieShowtimesQuery,
  revision = 'r1',
): MovieShowtimesResponse {
  const movie = {
    slug,
    title: 'Film',
    original_language: 'en',
    runtime_minutes: 90,
    updated_at: `${TODAY}T00:00:00Z`,
    poster_url: null,
    backdrop_url: null,
    tmdb_id: null,
    imdb_id: null,
    overview: null,
    release_date: null,
    french_release_date: null,
    genres: [],
  }
  const page = query.page ?? 1
  const ids =
    query.theaters?.split(',') ??
    Array.from({ length: 23 }, (_, index) => `cinema-${index + 1}`).slice(
      (page - 1) * 10,
      page * 10,
    )
  return {
    movie,
    date: query.date,
    release_status: 'showing',
    currently_screened: true,
    backdrop_url: null,
    available_dates: [TODAY, TOMORROW],
    catalog_revision: revision,
    available_languages: ['VOSTFR', 'VF'],
    available_formats: ['2D', 'IMAX'],
    pagination: query.theaters
      ? null
      : { page, page_size: 10, total: 23, has_more: page < 3 },
    theaters: ids.map((id) => ({
      id,
      slug: id,
      name: id,
      city: 'City',
      city_slug: 'city',
      provider: 'ugc',
      showtimes: [
        {
          id: `${id}-session`,
          provider: 'ugc',
          movie,
          start_time: `${query.date}T18:00:00+02:00`,
          end_time: null,
          estimated_end_time: null,
          estimated_end_ads_minutes: null,
          language: 'VF',
          format: '2D',
          room: '',
          booking_url: null,
        },
      ],
    })),
  }
}

function emptyConfiguredResponse(slug: string, query: MovieShowtimesQuery) {
  const result = response(slug, query)
  return query.theaters
    ? { ...result, available_dates: [], theaters: [] }
    : result
}

async function settle() {
  for (let index = 0; index < 20; index++) await nextTick()
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

function harness(
  ids: string[] = [],
  fetch: (
    slug: string,
    query: MovieShowtimesQuery,
  ) => Promise<MovieShowtimesResponse> = async (slug, query) =>
    response(slug, query),
) {
  const scope = effectScope()
  const query: LocationQuery = {}
  const route = reactive({ params: { slug: 'film-1' }, query })
  const catalog = Array.from({ length: 24 }, (_, index) => ({
    id: `cinema-${index + 1}`,
  }))
  const preferences = {
    activeTheaterIds: ref(ids),
    theaters: ref(catalog),
    selectionScopeKey: ref(0),
    isInitialized: ref(true),
    error: ref<string | null>(null),
    initialize: async () => {},
    retrySynchronization: async () => {},
  }
  const calls: Array<{ slug: string; query: MovieShowtimesQuery }> = []
  const evictedNuxtKeys: string[] = []
  let unmount = () => {}
  let day = TODAY
  const bindings = {
    ...routeQuery,
    ...filters,
    computed,
    ref,
    nextTick,
    watch: (...args: Parameters<typeof watch>) =>
      scope.run(() => watch(...args)),
    isBroadTheaterSelection,
    isShowtimeFormat,
    resolveShowtimeEnd,
    useRoute: () => route,
    useRouter: () => ({
      replace: async ({ query }: { query: LocationQuery }) => {
        route.query = query
      },
    }),
    usePageCinemaSelection: () => preferences,
    useMesSeancesApi: () => ({
      movieShowtimes: (slug: string, query: MovieShowtimesQuery) => {
        calls.push({ slug, query })
        return fetch(slug, query)
      },
    }),
    todayInParis: () => day,
    getFrenchApiError: () => 'Service indisponible. Réessayez.',
    isNotFoundError: () => false,
    navigateTo: async () => {},
    clearNuxtData: (matches: (key: string) => boolean) => {
      const keys = [
        `film-schedule:film-1|${TODAY}|ALL|ALL|catalog`,
        `film-schedule:film-1|${TOMORROW}|ALL|ALL|catalog`,
        `film-schedule:film-2|${TODAY}|ALL|ALL|catalog`,
      ]
      evictedNuxtKeys.push(...keys.filter(matches))
    },
    onBeforeUnmount: (callback: () => void) => {
      unmount = callback
    },
    document: { removeEventListener: () => {} },
    window: {},
  }
  // SAFETY: Actual page setup explicitly returns these refs/methods; synchronous/batched watchers are unchanged.
  const page = new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { schedule, pending, errorMessage, appendPending, appendError, languageOptions, technologyOptions, visibleTheaters, broadScope, canBroadenSearch, selectedDate, broadenSearch, loadMore, applyRoute, retryLoad, resetFilters, refreshFilmDay, start: () => { isReady = true } }`,
  )(...Object.values(bindings)) as {
    schedule: { value: MovieShowtimesResponse | null }
    pending: { value: boolean }
    errorMessage: { value: string }
    appendPending: { value: boolean }
    appendError: { value: string }
    languageOptions: { value: Array<{ value: string }> }
    technologyOptions: { value: Array<{ value: string }> }
    visibleTheaters: { value: Array<{ id: string }> }
    broadScope: { value: boolean }
    canBroadenSearch: { value: boolean }
    selectedDate: { value: string }
    broadenSearch: () => Promise<void>
    loadMore: () => Promise<void>
    applyRoute: () => Promise<void>
    retryLoad: () => Promise<void>
    resetFilters: () => void
    refreshFilmDay: () => Promise<void>
    start: () => void
  }
  page.start()
  return {
    page,
    route,
    preferences,
    calls,
    evictedNuxtKeys,
    catalog,
    setDay: () => {
      day = TOMORROW
    },
    close: () => {
      unmount()
      scope.stop()
    },
  }
}

test('broad pages append 10, 10, remainder only on click, full facets survive and server order is retained', async (t) => {
  const h = harness()
  t.after(h.close)
  await h.page.applyRoute()
  assert.equal(h.calls.length, 1)
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.ok(
    h.page.languageOptions.value.some((option) => option.value === 'VOSTFR'),
  )
  assert.ok(
    h.page.technologyOptions.value.some((option) => option.value === 'IMAX'),
  )
  await h.page.loadMore()
  assert.equal(h.page.schedule.value?.theaters.length, 20)
  await h.page.loadMore()
  assert.equal(h.page.schedule.value?.theaters.length, 23)
  assert.equal(h.page.schedule.value?.pagination?.has_more, false)
  await h.page.loadMore()
  assert.deepEqual(
    h.calls.map((call) => call.query.page),
    [1, 2, 3],
  )
  assert.equal(
    h.calls.every((call) => call.query.theaters === undefined),
    true,
  )
  assert.deepEqual(
    h.page.visibleTheaters.value.map((row) => row.id),
    h.page.schedule.value?.theaters.map((row) => row.id),
  )
})

test('configured no-dates fallback reuses nationwide page 1 then appends 10 without changing selection or shared query', async (t) => {
  const ids = ['cinema-23', 'cinema-24']
  const h = harness(ids, async (slug, query) =>
    emptyConfiguredResponse(slug, query),
  )
  t.after(h.close)
  h.route.query = { shared_theaters: ids.join(',') }
  await h.page.applyRoute()
  await settle()
  assert.equal(h.page.canBroadenSearch.value, true)
  assert.equal(h.page.broadScope.value, false)
  assert.deepEqual(h.page.schedule.value?.available_dates, [])
  const before = h.calls.length
  const savedSelection = h.preferences.activeTheaterIds.value
  const savedCatalog = h.preferences.theaters.value
  await h.page.broadenSearch()
  assert.equal(h.calls.length, before, 'reuse cached public evidence')
  assert.equal(h.page.broadScope.value, true)
  assert.equal(h.page.canBroadenSearch.value, false)
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.equal(h.page.schedule.value?.pagination?.page_size, 10)
  await h.page.loadMore()
  assert.equal(h.page.schedule.value?.theaters.length, 20)
  assert.deepEqual(
    h.calls.slice(before).map(({ query }) => query),
    [{ date: TODAY, page: 2, language: 'ALL', format: 'ALL', sort: 'catalog' }],
  )
  assert.deepEqual(
    h.page.visibleTheaters.value.map(({ id }) => id),
    Array.from({ length: 20 }, (_, index) => `cinema-${index + 1}`),
  )
  assert.equal(h.preferences.activeTheaterIds.value, savedSelection)
  assert.deepEqual(h.preferences.activeTheaterIds.value, ids)
  assert.equal(h.preferences.theaters.value, savedCatalog)
  assert.equal(h.preferences.selectionScopeKey.value, 0)
  assert.deepEqual(h.route.query, { shared_theaters: ids.join(',') })
  await h.page.broadenSearch()
  await h.page.applyRoute()
  assert.equal(h.page.schedule.value?.theaters.length, 20)
  assert.equal(h.calls.length, before + 1)
})

test('fallback keeps filters and sort, date/filter reloads stay nationwide, reset filters retains override', async (t) => {
  const h = harness(['cinema-24'], async (slug, query) =>
    emptyConfiguredResponse(slug, query),
  )
  t.after(h.close)
  h.route.query = {
    shared_theaters: 'cinema-24',
    language: 'ORIGINAL',
    format: 'IMAX',
    sort: 'next',
  }
  await h.page.applyRoute()
  await settle()
  const before = h.calls.length
  await h.page.broadenSearch()
  assert.deepEqual(h.calls.at(-1)?.query, {
    date: TODAY,
    page: 1,
    language: 'ORIGINAL',
    format: 'IMAX',
    sort: 'next',
  })
  await h.page.loadMore()
  h.route.query = { ...h.route.query, date: TOMORROW, language: 'VF' }
  assert.equal(h.page.schedule.value?.theaters.length, 0)
  await settle()
  assert.equal(h.page.broadScope.value, true)
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.deepEqual(h.calls.at(-1)?.query, {
    date: TOMORROW,
    page: 1,
    language: 'VF',
    format: 'IMAX',
    sort: 'next',
  })
  h.page.resetFilters()
  await settle()
  assert.equal(h.page.broadScope.value, true)
  assert.deepEqual(h.calls.at(-1)?.query, {
    date: TOMORROW,
    page: 1,
    language: 'ALL',
    format: 'ALL',
    sort: 'next',
  })
  assert.ok(h.calls.slice(before).every(({ query }) => !query.theaters))
  assert.deepEqual(h.preferences.activeTheaterIds.value, ['cinema-24'])
  assert.equal(h.route.query.shared_theaters, 'cinema-24')
})

test('fallback resolves first nationwide available date without duplicate canonical reloads', async (t) => {
  const h = harness(['cinema-24'], async (slug, query) => ({
    ...emptyConfiguredResponse(slug, query),
    available_dates: query.theaters ? [] : [TOMORROW],
    theaters:
      query.theaters || query.date === TODAY
        ? []
        : response(slug, query).theaters,
  }))
  t.after(h.close)
  await h.page.applyRoute()
  await settle()
  await h.page.broadenSearch()
  await settle()
  assert.equal(h.page.selectedDate.value, TOMORROW)
  assert.equal(h.page.schedule.value?.date, TOMORROW)
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.equal(h.route.query.date, undefined)
  assert.deepEqual(
    h.calls.map(({ query }) => query),
    [
      { date: TODAY, page: 1 },
      { date: TODAY, theaters: 'cinema-24' },
      {
        date: TOMORROW,
        page: 1,
        language: 'ALL',
        format: 'ALL',
        sort: 'catalog',
      },
    ],
  )
})

test('fallback page 1 and append failures stay nationwide and support explicit retry', async (t) => {
  let failFirst = true
  let failAppend = true
  const h = harness(['cinema-24'], async (slug, query) => {
    if (!query.theaters && query.language === 'ORIGINAL') {
      if (query.page === 1 && failFirst) throw new Error('offline')
      if (query.page === 2 && failAppend) throw new Error('offline')
    }
    return emptyConfiguredResponse(slug, query)
  })
  t.after(h.close)
  h.route.query = { language: 'ORIGINAL' }
  await h.page.applyRoute()
  await settle()
  const before = h.calls.length
  await h.page.broadenSearch()
  assert.equal(h.page.pending.value, false)
  assert.ok(h.page.errorMessage.value)
  assert.equal(h.page.canBroadenSearch.value, false)
  failFirst = false
  await h.page.retryLoad()
  assert.equal(h.page.errorMessage.value, '')
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  await h.page.loadMore()
  assert.ok(h.page.appendError.value)
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.equal(h.page.schedule.value?.pagination?.page, 1)
  failAppend = false
  await h.page.loadMore()
  assert.equal(h.page.appendError.value, '')
  assert.equal(h.page.schedule.value?.theaters.length, 20)
  assert.deepEqual(
    h.calls.slice(before).map(({ query }) => query.page),
    [1, 1, 2, 2],
  )
  assert.ok(h.calls.slice(before).every(({ query }) => !query.theaters))
})

test('fallback CTA is limited to loaded configured no-dates state', async (t) => {
  for (const state of [
    'dates',
    'nationwide-empty',
    'ended',
    'error',
    'loading',
  ]) {
    const waiting = deferred<MovieShowtimesResponse>()
    const h = harness(
      state === 'nationwide-empty' ? [] : ['cinema-24'],
      async (slug, query) => {
        if (state === 'error') throw new Error('offline')
        if (state === 'loading') return waiting.promise
        const result = response(slug, query)
        return state === 'dates'
          ? result
          : {
              ...result,
              available_dates: [],
              theaters: [],
              currently_screened: state !== 'ended',
              release_status: state === 'ended' ? 'ended' : 'showing',
            }
      },
    )
    t.after(h.close)
    const loading = h.page.applyRoute()
    if (state !== 'loading') await loading
    const before = h.calls.length
    assert.equal(h.page.canBroadenSearch.value, false, state)
    await h.page.broadenSearch()
    assert.equal(h.calls.length, before, state)
    if (state === 'loading') {
      h.close()
      waiting.resolve(response('film-1', { date: TODAY }))
      await loading
    }
  }
  const panel = source.match(
    /<EditorialStatePanel\s+v-else-if="!hasAvailableDates"[\s\S]*?<\/EditorialStatePanel>/,
  )?.[0]
  assert.ok(panel)
  assert.match(panel, /<template v-if="canBroadenSearch" #actions/)
  assert.match(panel, /type="button"/)
  assert.match(panel, /@click="broadenSearch"/)
  assert.match(panel, /Rechercher dans toute la France/)
  assert.match(panel, /min-h-11/)
  assert.match(panel, /focus-visible:ring-2/)
})

test('same IDs and preference readiness/error transitions retain override; actual selection or scope changes reset it', async (t) => {
  for (const transition of ['ids', 'scope', 'movie']) {
    const h = harness(['cinema-24'], async (slug, query) =>
      emptyConfiguredResponse(slug, query),
    )
    t.after(h.close)
    await h.page.applyRoute()
    await h.page.broadenSearch()
    h.preferences.activeTheaterIds.value = ['cinema-24']
    await settle()
    assert.equal(h.page.broadScope.value, true)
    h.preferences.isInitialized.value = false
    h.preferences.error.value = 'Synchronisation indisponible'
    await settle()
    assert.equal(h.page.broadScope.value, true)
    h.preferences.error.value = null
    h.preferences.isInitialized.value = true
    await settle()
    assert.equal(h.page.broadScope.value, true)
    assert.equal(h.page.schedule.value?.theaters.length, 10)
    if (transition === 'ids')
      h.preferences.activeTheaterIds.value = ['cinema-23']
    if (transition === 'scope') h.preferences.selectionScopeKey.value++
    if (transition === 'movie') h.route.params.slug = 'film-2'
    await settle()
    assert.equal(h.page.broadScope.value, false, transition)
    assert.equal(h.page.canBroadenSearch.value, true, transition)
    assert.equal(h.page.schedule.value?.pagination, null)
    assert.equal(
      h.calls.at(-1)?.query.theaters,
      transition === 'ids' ? 'cinema-23' : 'cinema-24',
    )
    if (transition === 'movie')
      assert.equal(h.page.schedule.value?.movie.slug, 'film-2')
  }
})

for (const transition of [
  'ids',
  'scope',
  'movie',
  'date',
  'language',
  'format',
  'sort',
  'unmount',
]) {
  for (const outcome of ['success', 'error']) {
    test(`fallback append ignores stale ${outcome} after ${transition}`, async () => {
      const next = deferred<MovieShowtimesResponse>()
      const h = harness(['cinema-24'], async (slug, query) =>
        query.page === 2 ? next.promise : emptyConfiguredResponse(slug, query),
      )
      try {
        await h.page.applyRoute()
        await h.page.broadenSearch()
        const append = h.page.loadMore()
        if (transition === 'ids')
          h.preferences.activeTheaterIds.value = ['cinema-23']
        if (transition === 'scope') h.preferences.selectionScopeKey.value++
        if (transition === 'movie') h.route.params.slug = 'film-2'
        if (transition === 'date') h.route.query = { date: TOMORROW }
        if (transition === 'language') h.route.query = { language: 'ORIGINAL' }
        if (transition === 'format') h.route.query = { format: 'IMAX' }
        if (transition === 'sort') h.route.query = { sort: 'next' }
        if (transition === 'unmount') h.close()
        await settle()
        const before = JSON.stringify(h.page.schedule.value)
        const appendPendingBefore = h.page.appendPending.value
        if (outcome === 'success')
          next.resolve(response('film-1', { date: TODAY, page: 2 }))
        else next.reject(new Error('late'))
        await append
        assert.equal(JSON.stringify(h.page.schedule.value), before)
        assert.equal(h.page.appendError.value, '')
        assert.equal(h.page.appendPending.value, appendPendingBefore)
      } finally {
        h.close()
      }
    })
  }
}

for (const transition of ['ids', 'scope', 'movie']) {
  for (const outcome of ['success', 'error']) {
    test(`fallback page 1 ignores stale ${outcome} after ${transition}`, async () => {
      const first = deferred<MovieShowtimesResponse>()
      const h = harness(['cinema-24'], async (slug, query) =>
        !query.theaters && query.language === 'ORIGINAL'
          ? first.promise
          : emptyConfiguredResponse(slug, query),
      )
      try {
        h.route.query = { language: 'ORIGINAL' }
        await h.page.applyRoute()
        await settle()
        const loading = h.page.broadenSearch()
        await settle()
        if (transition === 'ids')
          h.preferences.activeTheaterIds.value = ['cinema-23']
        if (transition === 'scope') h.preferences.selectionScopeKey.value++
        if (transition === 'movie') h.route.params.slug = 'film-2'
        await settle()
        const before = JSON.stringify(h.page.schedule.value)
        if (outcome === 'success')
          first.resolve(
            response('film-1', { date: TODAY, page: 1, language: 'ORIGINAL' }),
          )
        else first.reject(new Error('late'))
        await loading
        assert.equal(JSON.stringify(h.page.schedule.value), before)
        assert.equal(h.page.broadScope.value, false)
        assert.equal(h.page.errorMessage.value, '')
        assert.equal(h.page.pending.value, false)
      } finally {
        h.close()
      }
    })
  }
}

test('append single-flight and inline retry retain rows, page number, and no duplicates', async (t) => {
  const next = deferred<MovieShowtimesResponse>()
  let fail = true
  const h = harness([], async (slug, query) =>
    query.page === 2 && fail ? next.promise : response(slug, query),
  )
  t.after(h.close)
  await h.page.applyRoute()
  const append = h.page.loadMore()
  await h.page.loadMore()
  assert.equal(h.calls.length, 2)
  next.reject(new Error('offline'))
  await append
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.equal(h.page.schedule.value?.pagination?.page, 1)
  assert.ok(h.page.appendError.value)
  assert.equal(h.page.pending.value, false)
  fail = false
  await h.page.loadMore()
  assert.equal(h.page.schedule.value?.theaters.length, 20)
  assert.equal(h.page.appendError.value, '')
  assert.equal(h.calls.at(-1)?.query.page, 2)
})

test('catalog revision drift discards append and forces fresh uncached page 1 once', async (t) => {
  let revision = 'r1'
  const h = harness([], async (slug, query) => response(slug, query, revision))
  t.after(h.close)
  await h.page.applyRoute()
  revision = 'r2'
  await h.page.loadMore()
  assert.deepEqual(
    h.calls.map((call) => call.query.page),
    [1, 2, 1],
  )
  assert.equal(h.page.schedule.value?.catalog_revision, 'r2')
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  await h.page.loadMore()
  assert.equal(h.page.schedule.value?.theaters.length, 20)
  assert.equal(h.evictedNuxtKeys.length, 2)
  assert.equal(
    h.evictedNuxtKeys.some((key) => key.includes('film-2')),
    false,
  )
})

test('server filter/sort changes reset broad page 1, full-catalog IDs normalize to omission', async (t) => {
  const h = harness()
  t.after(h.close)
  h.preferences.activeTheaterIds.value = h.catalog.map((theater) => theater.id)
  await h.page.applyRoute()
  await h.page.loadMore()
  h.route.query = { language: 'VOSTFR', format: 'IMAX', sort: 'next' }
  assert.equal(h.page.schedule.value?.theaters.length, 0)
  await settle()
  assert.equal(h.page.schedule.value?.theaters.length, 10)
  assert.deepEqual(h.calls.at(-1)?.query, {
    date: TODAY,
    page: 1,
    language: 'VOSTFR',
    format: 'IMAX',
    sort: 'next',
  })
})

for (const transition of [
  'date',
  'movie',
  'scope',
  'language',
  'format',
  'sort',
  'midnight',
  'unmount',
]) {
  for (const outcome of ['success', 'error']) {
    test(`pending append ignores stale ${outcome} after ${transition}, including finally`, async () => {
      const next = deferred<MovieShowtimesResponse>()
      const h = harness([], async (slug, query) =>
        query.page === 2 ? next.promise : response(slug, query),
      )
      try {
        await h.page.applyRoute()
        const append = h.page.loadMore()
        if (transition === 'date') h.route.query = { date: TOMORROW }
        if (transition === 'movie') h.route.params.slug = 'film-2'
        if (transition === 'scope') {
          h.preferences.selectionScopeKey.value++
          h.preferences.activeTheaterIds.value = ['cinema-24']
        }
        if (['language', 'format', 'sort'].includes(transition))
          h.route.query = {
            [transition]:
              transition === 'language'
                ? 'ORIGINAL'
                : transition === 'format'
                  ? 'IMAX'
                  : 'next',
          }
        if (transition === 'midnight') {
          h.setDay()
          await h.page.refreshFilmDay()
        }
        if (transition === 'unmount') h.close()
        await settle()
        const before = JSON.stringify(h.page.schedule.value)
        if (outcome === 'success')
          next.resolve(response('film-1', { date: TODAY, page: 2 }))
        else next.reject(new Error('late'))
        await append
        assert.equal(JSON.stringify(h.page.schedule.value), before)
        assert.equal(h.page.appendError.value, '')
      } finally {
        h.close()
      }
    })
  }
}

test('partial scope remains complete, filters local even while request pending, unchanged refresh preserves rows', async (t) => {
  const complete = deferred<MovieShowtimesResponse>()
  const h = harness(
    Array.from({ length: 12 }, (_, index) => `cinema-${index + 1}`),
    async (slug, query) =>
      query.theaters ? complete.promise : response(slug, query),
  )
  t.after(h.close)
  const loading = h.page.applyRoute()
  await settle()
  h.route.query = { language: 'ORIGINAL', sort: 'next' }
  await settle()
  assert.equal(h.calls.length, 2)
  const personalQuery = h.calls[1]!.query
  assert.equal(personalQuery.page, undefined)
  assert.equal(personalQuery.language, undefined)
  complete.resolve(response('film-1', personalQuery))
  await loading
  assert.equal(h.page.schedule.value?.theaters.length, 12)
  assert.equal(h.page.pending.value, false)
  assert.equal(h.page.schedule.value?.pagination, null)
  await h.page.loadMore()
  await h.page.applyRoute()
  assert.equal(h.calls.length, 2)
})

for (const transition of ['date', 'movie', 'scope', 'unmount']) {
  for (const outcome of ['success', 'error']) {
    test(`pending page 1 ignores stale ${outcome} after ${transition}`, async () => {
      const first = deferred<MovieShowtimesResponse>()
      let started = false
      const h = harness([], async (slug, query) => {
        if (!started) {
          started = true
          return first.promise
        }
        return response(slug, query)
      })
      try {
        const loading = h.page.applyRoute()
        await settle()
        if (transition === 'date') h.route.query = { date: TOMORROW }
        if (transition === 'movie') h.route.params.slug = 'film-2'
        if (transition === 'scope') {
          h.preferences.selectionScopeKey.value++
          h.preferences.isInitialized.value = false
          h.preferences.activeTheaterIds.value = ['cinema-24']
        }
        if (transition === 'unmount') h.close()
        await settle()
        const before = JSON.stringify(h.page.schedule.value)
        if (outcome === 'success')
          first.resolve(response('film-1', { date: TODAY, page: 1 }))
        else first.reject(new Error('late'))
        await loading
        await settle()
        assert.equal(JSON.stringify(h.page.schedule.value), before)
        assert.equal(h.page.appendError.value, '')
      } finally {
        h.close()
      }
    })
  }
}

test('footer keeps configure CTA for exhaustion/zero, filtered zero reset, inline retry and no fetch-all', () => {
  assert.match(source, /Charger plus/)
  assert.match(source, /Configurer mes cinémas/)
  assert.match(source, /v-if="schedule\.pagination\.has_more"/)
  assert.match(source, /v-if="appendError"\s+role="alert"/)
  assert.match(source, /@click="resetFilters"/)
  assert.doesNotMatch(source, /page_size:|fetchAll|prefetch/)
})
