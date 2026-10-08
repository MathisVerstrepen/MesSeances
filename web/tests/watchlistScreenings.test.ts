import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, ref, watch } from 'vue'
import type {
  useWatchlistScreenings,
  screeningAverage,
} from '../app/composables/useWatchlistScreenings.ts'
import type { useAccountLifetime } from '../app/composables/useAccountLifetime.ts'
import type { MoviesQuery, MoviesResponse } from '../app/types/api.ts'

const source = await readFile(
  new URL('../app/composables/useWatchlistScreenings.ts', import.meta.url),
  'utf8',
)
const lifetimeSource = await readFile(
  new URL('../app/composables/useAccountLifetime.ts', import.meta.url),
  'utf8',
)
const window = {
  as_of: '2026-10-08T12:00:00+02:00',
  timezone: 'Europe/Paris' as const,
  from: '2026-10-08',
  through: '2026-10-13',
  day_count: 6,
}
function catalog(page = 1, total = 1): MoviesResponse {
  return {
    page,
    page_size: 100,
    total,
    generated_at: '2026-10-08T08:00:00Z',
    catalog_revision: '1',
    available_genres: [],
    screening_window: { ...window },
    items: Array.from(
      { length: Math.min(100, Math.max(0, total - (page - 1) * 100)) },
      (_, index) => ({
        slug: `film-${(page - 1) * 100 + index}`,
        title: 'Film',
        runtime_minutes: 90,
        showtime_count: 10,
        remaining_showtime_count: 8,
        next_7_days_showtime_count: 9,
      }),
    ),
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
async function settle() {
  for (let i = 0; i < 8; i++) {
    await Promise.resolve()
    await nextTick()
  }
}
async function fixture(client = true) {
  const scope = effectScope()
  const mounts: (() => void)[] = []
  const unmounts: (() => void)[] = []
  const starts = new Set<() => void>()
  const arrivals = new Set<() => void>()
  const listeners = new Map<string, () => void>()
  const account = { revision: ref(1), revalidating: ref(false) }
  const list = {
    owner: ref('alice'),
    scopeKey: ref(1),
    ready: ref(true),
    items: ref([{ slug: 'film-0' }]),
    slugs: computed(() => new Set(list.items.value.map((item) => item.slug))),
  }
  const preferences = {
    isInitialized: ref(true),
    favoriteTheaterIds: ref(['cinema-a']),
    selectionScopeKey: ref(1),
    error: ref<string | null>(null),
    initialize: async () => {},
    retrySynchronization: async () => {
      preferences.error.value = null
      preferences.isInitialized.value = true
    },
  }
  const calls: { query: MoviesQuery; signal?: AbortSignal }[] = []
  let read = async (query: MoviesQuery) => catalog(query.page)
  const context = {
    ref,
    computed,
    watch,
    AbortController,
    useAccountSession: () => account,
    useWatchlist: () => list,
    useCinemaPreferences: () => preferences,
    useMesSeancesApi: () => ({
      movies: (query: MoviesQuery, signal?: AbortSignal) => {
        calls.push({ query, signal })
        return read(query)
      },
    }),
    useAccountNavigation: () => ({ starts, arrivals }),
    useRoute: () => ({ path: '/compte/watchlist' }),
    useRouter: () => ({ currentRoute: ref({ path: '/compte/watchlist' }) }),
    window: {
      addEventListener: (event: string, fn: () => void) =>
        listeners.set(event, fn),
      removeEventListener: (event: string) => listeners.delete(event),
    },
    onMounted: (fn: () => void) => mounts.push(fn),
    onBeforeUnmount: (fn: () => void) => unmounts.push(fn),
  }
  function compile<T>(code: string, extra = {}): T {
    const exports = {}
    runInNewContext(
      ts.transpileModule(
        code.replaceAll('import.meta.client', String(client)),
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      { ...context, ...extra, exports },
    )
    // SAFETY: Callers supply the exact exports of these two inspected modules.
    return exports as T
  }
  const lifetime = compile<{ useAccountLifetime: typeof useAccountLifetime }>(
    lifetimeSource,
  )
  const module = compile<{
    useWatchlistScreenings: typeof useWatchlistScreenings
    screeningAverage: typeof screeningAverage
  }>(source, lifetime)
  const page = scope.run(() => module.useWatchlistScreenings())!
  return {
    account,
    list,
    preferences,
    calls,
    page,
    starts,
    arrivals,
    listeners,
    average: module.screeningAverage,
    setRead(fn: typeof read) {
      read = fn
    },
    async mount() {
      mounts.forEach((fn) => fn())
      await settle()
    },
    stop() {
      unmounts.forEach((fn) => fn())
      scope.stop()
    },
  }
}

test('1000 saved slugs join one 205-item catalog with exactly three serialized bulk requests', async () => {
  const f = await fixture()
  try {
    f.list.items.value = Array.from({ length: 1000 }, (_, i) => ({
      slug: `film-${i}`,
    }))
    let inFlight = 0
    f.setRead(async (query) => {
      assert.equal(++inFlight, 1)
      await Promise.resolve()
      inFlight--
      return catalog(query.page, 205)
    })
    await f.mount()
    assert.deepEqual(
      f.calls.map((call) => call.query.page),
      [1, 2, 3],
    )
    for (const { query, signal } of f.calls) {
      assert.equal(query.currently_screened, true)
      assert.equal(query.screening_summary, true)
      assert.equal(query.page_size, 100)
      assert.equal(query.sort, 'title_asc')
      assert.equal(query.theaters, 'cinema-a')
      assert.equal(Object.keys(query).length, 6)
      assert.ok(signal)
    }
    assert.equal(f.page.forMovie('film-0')?.average.text, '1,3 séances/j')
    assert.equal(f.page.forMovie('film-999')?.average.text, '0 séances/j')
    assert.equal(f.page.forMovie('film-999')?.inTheaters, false)
    f.list.items.value = [...f.list.items.value].reverse()
    await settle()
    assert.equal(
      f.calls.length,
      3,
      'sort/group/tag-only snapshots preserve slug set',
    )
    f.list.items.value.push({ slug: 'imported-film' })
    await settle()
    assert.equal(f.calls.length, 6, 'committed import refreshes')
  } finally {
    f.stop()
  }
})

test('uninitialized selection never means nationwide; initialized empty selection does', async () => {
  const f = await fixture()
  try {
    f.preferences.isInitialized.value = false
    f.preferences.favoriteTheaterIds.value = []
    await f.mount()
    assert.equal(f.calls.length, 0)
    assert.equal(f.page.forMovie('absent'), null)
    assert.equal(f.page.loading.value, true)
    f.preferences.isInitialized.value = true
    await settle()
    assert.equal(f.calls.length, 1)
    assert.equal(f.calls[0]?.query.theaters, undefined)
  } finally {
    f.stop()
  }
})

test('complete empty catalog establishes zero; French integers have no trailing decimal', async () => {
  const f = await fixture()
  try {
    f.setRead(async () => catalog(1, 0))
    await f.mount()
    assert.equal(f.page.forMovie('imported')?.average.text, '0 séances/j')
    assert.equal(f.average(12, window).text, '2 séances/j')
    assert.match(
      f.average(8, window).label,
      /restantes.*mardi 13 octobre 2026 inclus/,
    )
    assert.doesNotMatch(
      source,
      /useState|localStorage|sessionStorage|setInterval|movieShowtimes/,
    )
  } finally {
    f.stop()
  }
})

for (const defect of [
  'error',
  'truncated',
  'missing-window',
  'missing-count',
  'negative',
  'fraction',
  'next7-missing',
  'next7-negative',
  'next7-fraction',
  'next7-string',
  'next7-null',
  'next7-unsafe',
  'next7-infinite',
  'duplicate',
  'revision',
  'generated-at',
  'total',
  'day-count',
  'dates',
  'timezone',
  'page',
  'page-size',
]) {
  test(`${defect} rejects entire walk without inferred absence/zero; explicit retry recovers`, async () => {
    const f = await fixture()
    try {
      const total =
        [
          'duplicate',
          'revision',
          'generated-at',
          'total',
          'day-count',
          'dates',
          'timezone',
        ].includes(defect) || defect.startsWith('next7-')
          ? 101
          : 1
      f.setRead(async (query) => {
        const value = catalog(query.page, total)
        if (defect === 'error') throw new Error('Unavailable')
        if (defect === 'truncated') value.items = []
        if (defect === 'missing-window') delete value.screening_window
        if (defect === 'missing-count')
          delete value.items[0]!.remaining_showtime_count
        if (defect === 'negative') value.items[0]!.remaining_showtime_count = -1
        if (defect === 'fraction')
          value.items[0]!.remaining_showtime_count = 0.5
        if (defect === 'page') value.page = 2
        if (defect === 'page-size') value.page_size = 20
        if (query.page === 2) {
          if (defect === 'next7-missing')
            delete value.items[0]!.next_7_days_showtime_count
          if (defect === 'next7-negative')
            value.items[0]!.next_7_days_showtime_count = -1
          if (defect === 'next7-fraction')
            value.items[0]!.next_7_days_showtime_count = 0.5
          if (defect === 'next7-string')
            Object.assign(value.items[0]!, { next_7_days_showtime_count: '1' })
          if (defect === 'next7-null')
            Object.assign(value.items[0]!, { next_7_days_showtime_count: null })
          if (defect === 'next7-unsafe')
            value.items[0]!.next_7_days_showtime_count =
              Number.MAX_SAFE_INTEGER + 1
          if (defect === 'next7-infinite')
            value.items[0]!.next_7_days_showtime_count =
              Number.POSITIVE_INFINITY
          if (defect === 'duplicate') value.items[0]!.slug = 'film-0'
          if (defect === 'revision') value.catalog_revision = '2'
          if (defect === 'generated-at')
            value.generated_at = '2026-10-08T09:00:00Z'
          if (defect === 'total') value.total = 102
          if (defect === 'day-count') value.screening_window!.day_count = 7
          if (defect === 'dates') value.screening_window!.from = '2026-10-09'
          if (defect === 'timezone')
            Object.assign(value.screening_window!, { timezone: 'UTC' })
        }
        return value
      })
      await f.mount()
      assert.equal(f.page.error.value, true)
      assert.equal(f.page.loading.value, false)
      assert.equal(f.page.forMovie('absent'), null)
      assert.equal(f.page.forMovie('film-0'), null)
      const count = f.calls.length
      await settle()
      assert.equal(f.calls.length, count, 'no automatic restart loop')
      f.setRead(async () => catalog())
      await f.page.retry()
      assert.equal(f.page.error.value, false)
      assert.equal(f.page.forMovie('film-0')?.inTheaters, true)
    } finally {
      f.stop()
    }
  })
}

test('only server next-seven-days count controls En salle; Tuesday average is independent', async () => {
  const f = await fixture()
  try {
    const cases = [
      {
        slug: 'distant-two-months',
        showtime_count: 4,
        remaining_showtime_count: 0,
        next_7_days_showtime_count: 0,
      },
      {
        slug: 'past-grace-only',
        showtime_count: 1,
        remaining_showtime_count: 0,
        next_7_days_showtime_count: 0,
      },
      {
        slug: 'within-seven-after-tuesday',
        showtime_count: 1,
        remaining_showtime_count: 0,
        next_7_days_showtime_count: 1,
      },
      {
        slug: 'within-seven-before-tuesday',
        showtime_count: 12,
        remaining_showtime_count: 12,
        next_7_days_showtime_count: 12,
      },
    ]
    f.list.items.value = cases.map(({ slug }) => ({ slug }))
    f.setRead(async () => ({
      ...catalog(1, cases.length),
      items: cases.map((item) => ({ ...catalog().items[0]!, ...item })),
    }))
    await f.mount()
    assert.equal(f.calls.length, 1)
    for (const slug of ['distant-two-months', 'past-grace-only']) {
      assert.equal(f.page.forMovie(slug)?.inTheaters, false)
      assert.equal(f.page.forMovie(slug)?.average.text, '0 séances/j')
    }
    assert.equal(
      f.page.forMovie('within-seven-after-tuesday')?.inTheaters,
      true,
    )
    assert.equal(
      f.page.forMovie('within-seven-after-tuesday')?.average.text,
      '0 séances/j',
    )
    assert.equal(
      f.page.forMovie('within-seven-before-tuesday')?.inTheaters,
      true,
    )
    assert.equal(
      f.page.forMovie('within-seven-before-tuesday')?.average.text,
      '2 séances/j',
    )
  } finally {
    f.stop()
  }
})

test('selected cinema excludes qualifying screenings elsewhere; empty initialized selection includes them', async () => {
  const f = await fixture()
  try {
    f.setRead(async (query) => ({
      ...catalog(),
      items: [
        {
          ...catalog().items[0]!,
          remaining_showtime_count: 0,
          next_7_days_showtime_count: query.theaters === 'cinema-a' ? 0 : 1,
        },
      ],
    }))
    await f.mount()
    assert.equal(f.page.forMovie('film-0')?.inTheaters, false)
    f.preferences.favoriteTheaterIds.value = ['cinema-b']
    assert.equal(f.page.forMovie('film-0'), null)
    await settle()
    assert.equal(f.page.forMovie('film-0')?.inTheaters, true)
    assert.equal(f.page.forMovie('film-0')?.average.text, '0 séances/j')
    f.preferences.favoriteTheaterIds.value = []
    await settle()
    assert.equal(f.page.forMovie('film-0')?.inTheaters, true)
    assert.equal(f.page.forMovie('film-0')?.average.text, '0 séances/j')
    assert.deepEqual(
      f.calls.map(({ query }) => query.theaters),
      ['cinema-a', 'cinema-b', undefined],
    )
  } finally {
    f.stop()
  }
})

test('as_of may change across pages without pretending clock is frozen', async () => {
  const f = await fixture()
  try {
    f.setRead(async (query) => {
      const value = catalog(query.page, 101)
      if (query.page === 2)
        value.screening_window!.as_of = '2026-10-08T12:01:00+02:00'
      return value
    })
    await f.mount()
    assert.equal(f.page.error.value, false)
    assert.equal(f.page.forMovie('film-100')?.inTheaters, true)
  } finally {
    f.stop()
  }
})

for (const outcome of ['success', 'error']) {
  test(`A -> B -> A fences late ${outcome} even when scope returns to original selection`, async () => {
    const f = await fixture()
    try {
      const held = deferred<MoviesResponse>()
      f.setRead(() => held.promise)
      await f.mount()
      f.preferences.favoriteTheaterIds.value = ['cinema-b']
      assert.equal(f.calls[0]?.signal?.aborted, true)
      f.preferences.favoriteTheaterIds.value = ['cinema-a']
      f.setRead(async () => ({
        ...catalog(),
        items: [{ ...catalog().items[0]!, remaining_showtime_count: 12 }],
      }))
      await settle()
      if (outcome === 'success') held.resolve(catalog())
      else held.reject(new Error('Old failure'))
      await settle()
      assert.equal(f.page.forMovie('film-0')?.average.text, '2 séances/j')
      assert.equal(f.page.error.value, false)
    } finally {
      f.stop()
    }
  })
}

for (const boundary of [
  'owner',
  'session',
  'departure',
  'unmount',
  'offline',
  'pagehide',
  'selection-invalidation',
]) {
  test(`${boundary} aborts pending collection and ignores late response`, async () => {
    const f = await fixture()
    try {
      const held = deferred<MoviesResponse>()
      f.setRead(() => held.promise)
      await f.mount()
      if (boundary === 'owner') f.list.owner.value = ''
      if (boundary === 'session') {
        f.account.revision.value++
        f.account.revalidating.value = true
      }
      if (boundary === 'departure') f.starts.forEach((fn) => fn())
      if (boundary === 'unmount') f.stop()
      if (boundary === 'offline' || boundary === 'pagehide')
        f.listeners.get(boundary)?.()
      if (boundary === 'selection-invalidation')
        f.preferences.isInitialized.value = false
      assert.equal(f.calls[0]?.signal?.aborted, true)
      held.resolve(catalog())
      await settle()
      assert.equal(f.page.forMovie('film-0'), null)
      assert.equal(f.page.error.value, false)
    } finally {
      f.stop()
    }
  })
}

test('deferred A and B responses cannot replace committed cinema C or replacement account', async () => {
  const f = await fixture()
  try {
    const a = deferred<MoviesResponse>()
    const b = deferred<MoviesResponse>()
    f.setRead((query) => {
      if (query.theaters === 'cinema-a') return a.promise
      if (query.theaters === 'cinema-b') return b.promise
      return Promise.resolve({
        ...catalog(),
        items: [{ ...catalog().items[0]!, remaining_showtime_count: 18 }],
      })
    })
    await f.mount()
    f.preferences.favoriteTheaterIds.value = ['cinema-b']
    await settle()
    assert.equal(f.calls.length, 2)
    assert.equal(f.page.forMovie('film-0'), null)
    f.list.owner.value = 'bob'
    f.list.scopeKey.value++
    f.preferences.selectionScopeKey.value++
    f.preferences.favoriteTheaterIds.value = ['cinema-c']
    await settle()
    assert.equal(f.page.forMovie('film-0')?.average.text, '3 séances/j')
    b.resolve(catalog())
    a.reject(new Error('Previous account failure'))
    await settle()
    assert.equal(f.calls[0]?.signal?.aborted, true)
    assert.equal(f.calls[1]?.signal?.aborted, true)
    assert.equal(f.page.forMovie('film-0')?.average.text, '3 séances/j')
    assert.equal(f.page.error.value, false)
  } finally {
    f.stop()
  }
})

test('same-owner revalidation completion refreshes once; navigation arrival resumes and unmount unregisters', async () => {
  const f = await fixture()
  try {
    await f.mount()
    f.account.revision.value++
    f.account.revalidating.value = true
    await settle()
    assert.equal(f.page.forMovie('film-0'), null)
    assert.equal(f.calls.length, 1)
    f.account.revalidating.value = false
    await settle()
    assert.equal(f.calls.length, 2)
    f.starts.forEach((fn) => fn())
    f.account.revision.value++
    await settle()
    assert.equal(f.calls.length, 2)
    f.arrivals.forEach((fn) => fn())
    await settle()
    assert.equal(f.calls.length, 3)
  } finally {
    f.stop()
  }
  assert.equal(f.starts.size, 0)
  assert.equal(f.arrivals.size, 0)
  assert.equal(f.listeners.size, 0)
})

test('preference failure stays unknown and retry uses existing synchronization path', async () => {
  const f = await fixture()
  try {
    f.preferences.error.value = 'Unavailable'
    f.preferences.isInitialized.value = false
    await f.mount()
    assert.equal(f.page.error.value, true)
    assert.equal(f.calls.length, 0)
    assert.equal(f.page.forMovie('film-0'), null)
    await f.page.retry()
    await settle()
    assert.equal(f.calls.length, 1)
    assert.equal(f.page.forMovie('film-0')?.inTheaters, true)
  } finally {
    f.stop()
  }
})

test('SSR setup performs no public collection', async () => {
  const f = await fixture(false)
  try {
    await f.mount()
    assert.equal(f.calls.length, 0)
  } finally {
    f.stop()
  }
})

test('chips appear only in saved content slot before tags; both search loops remain unchanged', async () => {
  const page = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  const saved = page.slice(page.indexOf('v-for="section in savedSections"'))
  assert.match(
    saved,
    /#content>[\s\S]*data-watchlist-screenings[\s\S]*En salle[\s\S]*average\.text[\s\S]*<WatchlistItemTags/,
  )
  assert.doesNotMatch(
    page.slice(0, page.indexOf('<section aria-labelledby="saved-heading">')),
    /data-watchlist-screenings|average\.text/,
  )
  assert.equal((page.match(/Séances indisponibles\./g) ?? []).length, 1)
})
