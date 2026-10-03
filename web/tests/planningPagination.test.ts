import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { LocationQuery } from 'vue-router'
import type { TimelineQuery, TimelineResponse } from '../app/types/api.ts'
import { isBroadTheaterSelection } from '../app/utils/cinemaSelection.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'
import * as filters from '../app/utils/showtimeFilters.ts'

const TODAY = '2027-06-27'
const TOMORROW = '2027-06-28'
const source = await readFile(
  new URL('../app/pages/planning.vue', import.meta.url),
  'utf8',
)
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile(
  'planning.ts',
  script,
  ts.ScriptTarget.Latest,
  true,
)
const compiled = ts.transpileModule(
  parsed.statements
    .filter((node) => !ts.isImportDeclaration(node))
    .map((node) => node.getFullText(parsed))
    .join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText

function response(
  query: TimelineQuery,
  ids = query.theaters?.split(',') ?? [],
) {
  return {
    date: query.date,
    timezone: 'Europe/Paris',
    window_start_time: `${query.date}T06:00:00Z`,
    window_end_time: `${query.date}T23:59:00Z`,
    theaters: ids.map((id) => ({
      id,
      slug: id,
      name: id,
      city: 'City',
      provider: 'ugc',
      accepted_passes: [],
      showtimes: [
        {
          id: `${id}-session`,
          provider: 'ugc',
          movie: {
            slug: 'film',
            title: 'Film',
            original_language: 'fr',
            runtime_minutes: 90,
            updated_at: `${query.date}T00:00:00Z`,
            poster_url: null,
            backdrop_url: null,
            tmdb_id: null,
            imdb_id: null,
            overview: null,
            release_date: null,
            french_release_date: null,
            genres: [],
          },
          start_time: `${query.date}T16:00:00Z`,
          end_time: `${query.date}T17:30:00Z`,
          estimated_end_time: null,
          estimated_end_ads_minutes: null,
          language: 'VF',
          format: '2D',
          room: '',
          booking_url: null,
          start_offset_minutes: 600,
          duration_minutes: 90,
          poster_url: null,
          backdrop_url: null,
        },
      ],
    })),
  } satisfies TimelineResponse
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

async function settle() {
  for (let index = 0; index < 10; index++) await nextTick()
}

function harness(
  ids: string[] = [],
  fetch: (query: TimelineQuery) => Promise<TimelineResponse> = async (query) =>
    response(query),
  catalogSize = 23,
) {
  const scope = effectScope()
  const route = reactive<{ query: LocationQuery }>({ query: {} })
  const catalog = Array.from({ length: catalogSize }, (_, index) => ({
    id: `cinema-${index + 1}`,
    available_dates: [TODAY, TOMORROW],
  }))
  const preferences = {
    activeTheaterIds: ref(ids),
    activeTheaters: ref(catalog.filter((theater) => ids.includes(theater.id))),
    theaters: ref(catalog),
    selectionScopeKey: ref(0),
    isInitialized: ref(true),
    error: ref<string | null>(null),
    initialize: async () => {},
    retrySynchronization: async () => {},
  }
  const calls: TimelineQuery[] = []
  let unmount = () => {}
  let mount = async () => {}
  let day = TODAY
  const bindings = {
    ...routeQuery,
    ...filters,
    ref,
    computed,
    watch: (...args: Parameters<typeof watch>) =>
      scope.run(() => watch(...args)),
    isBroadTheaterSelection,
    useRoute: () => route,
    useRouter: () => ({
      replace: async ({ query }: { query: LocationQuery }) => {
        route.query = query
      },
    }),
    usePageCinemaSelection: () => preferences,
    useMesSeancesApi: () => ({
      timeline: (query: TimelineQuery) => {
        calls.push(query)
        return fetch(query)
      },
    }),
    todayInParis: () => day,
    getFrenchApiError: () => 'Service indisponible. Réessayez.',
    onMounted: (callback: () => Promise<void>) => {
      mount = callback
    },
    onBeforeUnmount: (callback: () => void) => {
      unmount = callback
    },
    document: { addEventListener: () => {}, removeEventListener: () => {} },
    window: { clearTimeout: () => {}, setTimeout: () => 1 },
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    absoluteSiteUrl: () => 'https://messeances.fr/planning',
    useSeoMeta: () => {},
    useHead: () => {},
  }
  // SAFETY: Execute actual page setup with explicit Vue/API/lifecycle bindings only.
  const page = new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { timeline, pending, errorMessage, appendPending, appendError, fallbackScope, hasMoreTheaters, queriedTheaterCount, rawShowtimeCount, showtimeCount, loadTimeline, loadMore, retryTimeline, applyRoute, refreshPlanningDay, start: () => { isMounted = true } }`,
  )(...Object.values(bindings)) as {
    timeline: { value: TimelineResponse | null }
    pending: { value: boolean }
    errorMessage: { value: string }
    appendPending: { value: boolean }
    appendError: { value: string }
    fallbackScope: { value: boolean }
    hasMoreTheaters: { value: boolean }
    queriedTheaterCount: { value: number }
    rawShowtimeCount: { value: number }
    showtimeCount: { value: number }
    loadTimeline: () => Promise<void>
    loadMore: () => Promise<void>
    retryTimeline: () => Promise<void>
    applyRoute: () => Promise<void>
    refreshPlanningDay: () => Promise<void>
    start: () => void
  }
  page.start()
  return {
    page,
    route,
    preferences,
    calls,
    catalog,
    mount: () => mount(),
    setDay: () => {
      day = TOMORROW
    },
    close: () => {
      unmount()
      scope.stop()
    },
  }
}

test('empty selection queries ordered explicit batches of 10, 10, remainder only on click', async (t) => {
  const h = harness()
  t.after(h.close)
  await h.page.applyRoute()
  await settle()
  assert.equal(h.calls.length, 1)
  assert.equal(h.page.timeline.value?.theaters.length, 10)
  assert.equal(h.page.hasMoreTheaters.value, true)
  await h.page.loadMore()
  assert.equal(h.page.timeline.value?.theaters.length, 20)
  await h.page.loadMore()
  assert.equal(h.page.timeline.value?.theaters.length, 23)
  assert.equal(h.page.queriedTheaterCount.value, 23)
  assert.equal(h.page.hasMoreTheaters.value, false)
  await h.page.loadMore()
  await h.page.applyRoute()
  assert.deepEqual(
    h.calls.map((query) => query.theaters?.split(',')),
    [h.catalog.slice(0, 10), h.catalog.slice(10, 20), h.catalog.slice(20)].map(
      (batch) => batch.map((theater) => theater.id),
    ),
  )
  assert.deepEqual(h.preferences.activeTheaterIds.value, [])
  assert.deepEqual(h.route.query, {})
})

test('nonempty configured/shared IDs stay complete; configured full catalog keeps omission', async (t) => {
  for (const ids of [
    ['cinema-23'],
    Array.from({ length: 12 }, (_, index) => `cinema-${index + 1}`),
    Array.from({ length: 23 }, (_, index) => `cinema-${index + 1}`),
  ]) {
    const h = harness(ids)
    t.after(h.close)
    await h.page.applyRoute()
    await h.page.loadMore()
    assert.equal(h.calls.length, 1)
    assert.equal(
      h.calls[0]?.theaters,
      ids.length === 23 ? undefined : ids.join(','),
    )
    assert.equal(h.page.fallbackScope.value, false)
    assert.equal(h.page.hasMoreTheaters.value, false)
    assert.deepEqual(h.preferences.activeTheaterIds.value, ids)
  }
})

test('empty catalog never sends an unfiltered request and remains empty without loading', async (t) => {
  const h = harness([], undefined, 0)
  t.after(h.close)
  await h.page.applyRoute()
  await h.page.loadMore()
  assert.equal(h.calls.length, 0)
  assert.equal(h.page.pending.value, false)
  assert.equal(h.page.errorMessage.value, '')
  assert.equal(h.page.timeline.value, null)
  assert.equal(h.page.hasMoreTheaters.value, false)
  assert.equal(h.page.fallbackScope.value, true)
})

test('empty and sparse returned batches advance by queried IDs, not visible rows', async (t) => {
  const h = harness([], async (query) =>
    response(
      query,
      query.theaters?.startsWith('cinema-1,') ? [] : ['cinema-20'],
    ),
  )
  t.after(h.close)
  await h.page.applyRoute()
  assert.equal(h.page.rawShowtimeCount.value, 0)
  assert.equal(h.page.hasMoreTheaters.value, true)
  assert.equal(h.page.queriedTheaterCount.value, 10)
  await h.page.loadMore()
  assert.equal(h.page.queriedTheaterCount.value, 20)
  await h.page.loadMore()
  assert.equal(h.page.queriedTheaterCount.value, 23)
  assert.equal(h.page.timeline.value?.theaters.length, 1)
  assert.equal(h.page.hasMoreTheaters.value, false)
})

test('append single-flight, failure and retry preserve rows/window and retry same IDs', async (t) => {
  const next = deferred<TimelineResponse>()
  let fail = true
  const h = harness([], async (query) =>
    query.theaters?.startsWith('cinema-11,') && fail
      ? next.promise
      : response(query),
  )
  t.after(h.close)
  await h.page.applyRoute()
  const before = JSON.stringify(h.page.timeline.value)
  const append = h.page.loadMore()
  await h.page.loadMore()
  assert.equal(h.calls.length, 2)
  assert.equal(h.page.pending.value, false)
  assert.equal(h.page.appendPending.value, true)
  assert.equal(JSON.stringify(h.page.timeline.value), before)
  next.reject(new Error('offline'))
  await append
  assert.equal(JSON.stringify(h.page.timeline.value), before)
  assert.equal(h.page.queriedTheaterCount.value, 10)
  assert.equal(h.page.appendPending.value, false)
  assert.ok(h.page.appendError.value)
  fail = false
  await h.page.loadMore()
  assert.equal(h.page.timeline.value?.theaters.length, 20)
  assert.equal(h.page.appendError.value, '')
  assert.deepEqual(h.calls[1], h.calls[2])
})

for (const field of [
  'date',
  'timezone',
  'window_start_time',
  'window_end_time',
]) {
  test(`append rejects incompatible ${field}, retaining coherent existing offsets and window`, async (t) => {
    const h = harness([], async (query) => {
      const result = response(query)
      if (query.theaters?.startsWith('cinema-11,'))
        Object.assign(result, { [field]: 'different' })
      return result
    })
    t.after(h.close)
    await h.page.applyRoute()
    const before = JSON.stringify(h.page.timeline.value)
    await h.page.loadMore()
    assert.equal(JSON.stringify(h.page.timeline.value), before)
    assert.equal(h.page.queriedTheaterCount.value, 10)
    assert.ok(h.page.appendError.value)
  })
}

test('format, mode, zoom and unrelated query changes remain local after scope resets', async (t) => {
  const h = harness()
  t.after(h.close)
  await h.page.applyRoute()
  h.preferences.selectionScopeKey.value++
  await settle()
  await h.page.loadMore()
  const before = JSON.stringify(h.page.timeline.value)
  const count = h.calls.length
  h.route.query = {
    format: 'IMAX',
    mode: 'movie',
    zoom: '15',
    campaign: 'local',
  }
  await settle()
  assert.equal(h.calls.length, count)
  assert.equal(JSON.stringify(h.page.timeline.value), before)
  assert.equal(h.page.showtimeCount.value, 0)
  assert.equal(h.page.queriedTheaterCount.value, 20)
})

for (const stage of ['initial', 'append']) {
  for (const transition of [
    'date',
    'language',
    'selection',
    'scope',
    'catalog',
    'unresolved',
    'error',
    'midnight',
    'unmount',
  ]) {
    for (const outcome of ['success', 'error']) {
      test(`${stage} fences stale ${outcome} after ${transition}, including loading/error/cursor state`, async () => {
        const delayed = deferred<TimelineResponse>()
        let calls = 0
        const h = harness([], async (query) => {
          calls++
          if (calls === (stage === 'initial' ? 1 : 2)) return delayed.promise
          return response(query)
        })
        try {
          const initial = h.page.applyRoute()
          if (stage === 'append') await initial
          const loading = stage === 'append' ? h.page.loadMore() : initial
          const staleQuery = h.calls.at(-1)!
          if (transition === 'date') h.route.query = { date: TOMORROW }
          if (transition === 'language') h.route.query = { language: 'VOSTFR' }
          if (transition === 'selection')
            h.preferences.activeTheaterIds.value = ['cinema-23']
          if (transition === 'scope') h.preferences.selectionScopeKey.value++
          if (transition === 'catalog')
            h.preferences.theaters.value = h.catalog.slice(10)
          if (transition === 'unresolved')
            h.preferences.isInitialized.value = false
          if (transition === 'error')
            h.preferences.error.value = 'Session indisponible'
          if (transition === 'midnight') {
            h.setDay()
            await h.page.refreshPlanningDay()
          }
          if (transition === 'unmount') h.close()
          await settle()
          const snapshot = () =>
            JSON.stringify({
              timeline: h.page.timeline.value,
              pending: h.page.pending.value,
              error: h.page.errorMessage.value,
              appendPending: h.page.appendPending.value,
              appendError: h.page.appendError.value,
              cursor: h.page.queriedTheaterCount.value,
              hasMore: h.page.hasMoreTheaters.value,
            })
          const before = snapshot()
          if (outcome === 'success') delayed.resolve(response(staleQuery))
          else delayed.reject(new Error('late'))
          await loading
          await settle()
          assert.equal(snapshot(), before)
          if (
            !['unmount', 'unresolved', 'error', 'selection'].includes(
              transition,
            )
          )
            assert.equal(h.page.queriedTheaterCount.value, 10)
        } finally {
          h.close()
        }
      })
    }
  }
}

test('stale append finally cannot clear loading on replacement append', async (t) => {
  const old = deferred<TimelineResponse>()
  const fresh = deferred<TimelineResponse>()
  const h = harness([], async (query) => {
    if (!query.theaters?.startsWith('cinema-11,')) return response(query)
    return query.date === TODAY ? old.promise : fresh.promise
  })
  t.after(h.close)
  await h.page.applyRoute()
  const oldAppend = h.page.loadMore()
  h.route.query = { date: TOMORROW }
  await settle()
  const freshAppend = h.page.loadMore()
  old.reject(new Error('late'))
  await oldAppend
  assert.equal(h.page.appendPending.value, true)
  assert.equal(h.page.appendError.value, '')
  assert.equal(h.page.timeline.value?.date, TOMORROW)
  fresh.resolve(response(h.calls.at(-1)!))
  await freshAppend
  assert.equal(h.page.appendPending.value, false)
  assert.equal(h.page.timeline.value?.theaters.length, 20)
})

test('initial failure retries bounded first batch and unresolved preferences never fetch', async (t) => {
  let fail = true
  const h = harness([], async (query) => {
    if (fail) throw new Error('offline')
    return response(query)
  })
  t.after(h.close)
  h.preferences.isInitialized.value = false
  await h.page.loadTimeline()
  assert.equal(h.calls.length, 0)
  assert.equal(h.page.pending.value, true)
  h.preferences.isInitialized.value = true
  await settle()
  assert.ok(h.page.errorMessage.value)
  assert.equal(h.page.pending.value, false)
  fail = false
  await h.page.retryTimeline()
  assert.equal(h.page.timeline.value?.theaters.length, 10)
  assert.equal(h.page.errorMessage.value, '')
  assert.deepEqual(h.calls[0], h.calls[1])
})

test('preference initialization and retry completing after unmount cannot start new requests', async () => {
  for (const operation of ['mount', 'retry']) {
    const wait = deferred<void>()
    const h = harness()
    try {
      if (operation === 'mount') h.preferences.initialize = () => wait.promise
      else h.preferences.retrySynchronization = () => wait.promise
      const pending = operation === 'mount' ? h.mount() : h.page.retryTimeline()
      h.close()
      wait.resolve()
      await pending
      assert.equal(h.calls.length, 0)
    } finally {
      h.close()
    }
  }
})

test('footer is independent of timeline/empty rows, only for fallback, with accessible append and retry', () => {
  const footer = source.slice(source.lastIndexOf('<TimelineMatrix'))
  assert.match(
    footer,
    /v-if="!pending && !errorMessage && preferences\.isInitialized\.value && fallbackScope"/,
  )
  assert.match(footer, /v-if="hasMoreTheaters"/)
  assert.match(footer, /v-if="appendError"\s+role="alert"/)
  assert.match(footer, /:disabled="appendPending"/)
  assert.match(footer, /:aria-busy="appendPending"/)
  assert.match(footer, /@click="loadMore"/)
  assert.match(footer, /Charger plus/)
  assert.match(footer, /Réessayer/)
  assert.match(footer, /to="\/cinemas"[\s\S]*Configurer mes cinémas/)
  assert.doesNotMatch(source, /prefetch|fetchAll|setFavoriteTheaterIds/)
})
