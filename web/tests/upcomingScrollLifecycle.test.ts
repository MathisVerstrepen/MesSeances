import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { Ref } from 'vue'
import type {
  LocationQuery,
  NavigationFailure,
  RouteLocationNormalized,
} from 'vue-router'
import type {
  UpcomingMoviesQuery,
  UpcomingMoviesResponse,
} from '../app/types/api.ts'
import { queriesEqual } from '../app/utils/routeQuery.ts'
import * as upcoming from '../app/utils/upcomingMovies.ts'

// Exercise the real page setup, following searchSelectionLifecycle.test.ts.
const source = await readFile(
  new URL('../app/pages/films/prochainement.vue', import.meta.url),
  'utf8',
)
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile(
  'upcoming.ts',
  script,
  ts.ScriptTarget.Latest,
  true,
)
const withoutImports = parsed.statements
  .filter((statement) => !ts.isImportDeclaration(statement))
  .map((statement) => statement.getFullText(parsed))
  .join('\n')
  .replaceAll('import.meta.server', 'false')
const compiled = ts.transpileModule(withoutImports, {
  compilerOptions: { target: ts.ScriptTarget.ESNext },
}).outputText

function response(page: number): UpcomingMoviesResponse {
  return {
    view: 'upcoming',
    year: null,
    month: null,
    available_years: [],
    available_months: [],
    generated_at: '2026-09-13T12:00:00Z',
    catalog_revision: 'fixture',
    timezone: 'Europe/Paris',
    window: { from: '2026-09-16', through: '2027-09-13' },
    items: [],
    page,
    total: 20,
    total_weeks: 9,
    total_pages: 3,
  }
}

function historyResponse(
  page = 1,
  year: number | null = 2025,
  month: number | null = null,
): UpcomingMoviesResponse {
  return {
    ...response(page),
    view: 'history',
    window: null,
    year,
    month,
    available_years: [2025, 2024],
    available_months: year === 2025 ? [1, 9, 10] : [12],
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

interface Page {
  pagination: Ref<upcoming.UpcomingRouteState>
  pending: Ref<boolean>
  catalog: Ref<UpcomingMoviesResponse | null>
  errorMessage: Ref<string>
  followPageLink: (event: ReturnType<typeof click>, page: number) => void
  loadCatalog: () => Promise<void>
  changeYear: (event: Event) => void
  changeMonth: (event: Event) => void
}

function click(overrides = {}) {
  return {
    button: 0,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true
    },
    ...overrides,
  }
}

class SyntheticSelect extends EventTarget {
  value = ''
}

function selectChange(value: string): Event {
  const select = new SyntheticSelect()
  select.value = value
  const event = new Event('change')
  select.dispatchEvent(event)
  return event
}

async function settle() {
  for (let i = 0; i < 8; i++) await nextTick()
}

async function harness(
  reducedMotion = false,
  query: LocationQuery = {},
  first?: UpcomingMoviesResponse,
) {
  const scope = effectScope()
  const route = reactive<{ query: LocationQuery }>({ query })
  const calls: Array<
    { page: number; query: UpcomingMoviesQuery } & ReturnType<
      typeof deferred<UpcomingMoviesResponse>
    >
  > = []
  const scrolls: Array<{ top: number; behavior: string }> = []
  let mounted = async () => {}
  let unmount = () => {}
  let initial = true
  let navigationFailure = false
  let afterNavigation = (
    _to: Pick<RouteLocationNormalized, 'query'>,
    _from: Pick<RouteLocationNormalized, 'query'>,
    _failure?: Pick<NavigationFailure, 'type'>,
  ) => {}
  let navigationError = () => {}
  let renderTick: Promise<void> | null = null
  let page!: Page
  const pushes: LocationQuery[] = []
  const initialQueries: UpcomingMoviesQuery[] = []
  let cacheKey = ''
  const bindings = {
    HTMLSelectElement: SyntheticSelect,
    ...upcoming,
    queriesEqual,
    ref,
    computed,
    watch: (...args: Parameters<typeof watch>) =>
      scope.run(() => watch(...args)),
    nextTick: () => renderTick ?? nextTick(),
    useRoute: () => route,
    useRouter: () => ({
      push: async ({ query }: { query: LocationQuery }) => {
        pushes.push(query)
        route.query = query
      },
      replace: async ({ query }: { query: LocationQuery }) => {
        route.query = query
      },
      afterEach: (callback: typeof afterNavigation) => {
        afterNavigation = callback
        return () => {
          afterNavigation = () => {}
        }
      },
      onError: (callback: typeof navigationError) => {
        navigationError = callback
        return () => {
          navigationError = () => {}
        }
      },
    }),
    useMesSeancesApi: () => ({
      upcomingMovies: (query: UpcomingMoviesQuery) => {
        const requested = query.page ?? 1
        if (initial) {
          initial = false
          initialQueries.push(query)
          return Promise.resolve(first ?? response(requested))
        }
        const request = {
          page: requested,
          query,
          ...deferred<UpcomingMoviesResponse>(),
        }
        calls.push(request)
        return request.promise
      },
    }),
    useAsyncData: async (
      _key: string,
      load: () => Promise<{
        catalog: UpcomingMoviesResponse | null
        errorMessage: string
      }>,
    ) => {
      cacheKey = _key
      return { data: ref(await load()) }
    },
    onMounted: (callback: typeof mounted) => {
      mounted = callback
    },
    onBeforeUnmount: (callback: typeof unmount) => {
      unmount = callback
    },
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    absoluteSiteUrl: () => 'https://messeances.fr/films/prochainement',
    useSeoMeta: () => {},
    useHead: () => {},
    getFrenchApiError: () => 'Échec du chargement',
    getApiErrorCode: (error: { data?: { error?: { code?: string } } }) =>
      error.data?.error?.code,
    window: {
      matchMedia: () => ({ matches: reducedMotion }),
      scrollTo: (options: { top: number; behavior: string }) => {
        assert.equal(
          page.pending.value,
          false,
          'scroll cannot run while the loading panel is displayed',
        )
        scrolls.push(options)
      },
    },
  }
  // SAFETY: The wrapper explicitly returns these actual setup bindings; no production code is replaced.
  page = (await new Function(
    ...Object.keys(bindings),
    `return (async () => { ${compiled}\nreturn { pagination, pending, catalog, errorMessage, followPageLink, loadCatalog, changeYear, changeMonth } })()`,
  )(...Object.values(bindings))) as Page
  await mounted()
  const followPageLink = page.followPageLink
  // Model RouterLink's event order: push starts, preventDefault, emitted handler, async route commit.
  page.followPageLink = (event, target) => {
    const next = { ...page.pagination.value, page: target }
    const navigates =
      event.button === 0 &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.shiftKey &&
      !event.altKey &&
      target >= 1 &&
      target <= 3 &&
      !page.pending.value
    if (navigates) event.preventDefault()
    followPageLink(event, target)
    if (navigates)
      queueMicrotask(() => {
        if (navigationFailure)
          afterNavigation(
            { query: upcoming.upcomingRouteQuery(next) },
            { query: route.query },
            { type: 4 },
          )
        else route.query = upcoming.upcomingRouteQuery(next)
      })
  }
  return {
    page,
    route,
    calls,
    scrolls,
    pushes,
    initialQueries,
    cacheKey,
    stop: () => {
      unmount()
      scope.stop()
    },
    failNavigation: (fail: boolean) => {
      navigationFailure = fail
    },
    navigationError: () => navigationError(),
    holdRender: () => {
      const tick = deferred<void>()
      renderTick = tick.promise
      return tick
    },
  }
}

test('next and prev scroll only after winning response, pending false and render tick; history never resets', async (context) => {
  const h = await harness()
  context.after(h.stop)
  assert.deepEqual(h.scrolls, [])
  for (const target of [2, 1]) {
    // NuxtLink has called preventDefault before its emitted navigate reaches this page.
    await h.page.followPageLink(click({ defaultPrevented: true }), target)
    await settle()
    assert.equal(h.page.pending.value, true)
    const tick = h.holdRender()
    const previousScrolls = h.scrolls.length
    h.calls.at(-1)!.resolve(response(target))
    await settle()
    assert.equal(h.page.pending.value, false)
    assert.equal(h.page.catalog.value?.page, target)
    assert.equal(h.scrolls.length, previousScrolls)
    tick.resolve()
    await settle()
    assert.deepEqual(h.scrolls.at(-1), { top: 0, behavior: 'smooth' })
  }
  h.route.query = { page: '2' }
  await settle()
  h.calls.at(-1)!.resolve(response(2))
  await settle()
  assert.equal(h.scrolls.length, 2)
  assert.deepEqual(h.pushes, [], 'NuxtLink owns navigation; no second push')
})

test('modified, middle and invalid clicks never arm a later scroll; pending double click is blocked', async (context) => {
  const h = await harness()
  context.after(h.stop)
  for (const options of [
    { ctrlKey: true },
    { metaKey: true },
    { shiftKey: true },
    { altKey: true },
    { button: 1 },
  ]) {
    const event = click(options)
    await h.page.followPageLink(event, 2)
    assert.equal(event.defaultPrevented, false)
  }
  for (const target of [0, 1, 4]) await h.page.followPageLink(click(), target)
  assert.equal(h.calls.length, 0)
  assert.deepEqual(h.route.query, {})
  await h.page.followPageLink(click(), 2)
  const second = click()
  await h.page.followPageLink(second, 2)
  await settle()
  assert.equal(second.defaultPrevented, true)
  assert.equal(h.calls.length, 1)
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.equal(h.scrolls.length, 1)
})

test('failed pagination retains intent for explicit retry, with reduced motion instant scrolling', async (context) => {
  const h = await harness(true)
  context.after(h.stop)
  await h.page.followPageLink(click(), 2)
  await settle()
  h.calls[0]!.reject(new Error('offline'))
  await settle()
  assert.equal(h.page.errorMessage.value, 'Échec du chargement')
  assert.deepEqual(h.scrolls, [])
  const retry = h.page.loadCatalog()
  h.calls[1]!.resolve(response(2))
  await retry
  assert.deepEqual(h.scrolls, [{ top: 0, behavior: 'instant' }])
})

test('superseded response, failed navigation and unmounted page never leak scroll intent', async (context) => {
  const h = await harness()
  context.after(h.stop)
  h.failNavigation(true)
  await h.page.followPageLink(click(), 2)
  h.failNavigation(false)
  h.route.query = { page: '2' }
  await settle()
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.deepEqual(h.scrolls, [])
  await h.page.followPageLink(click(), 3)
  await settle()
  h.route.query = { page: '1' }
  await settle()
  h.calls.at(-1)!.resolve(response(1))
  h.calls[1]!.resolve(response(3))
  await settle()
  assert.equal(h.page.catalog.value?.page, 1)
  assert.deepEqual(h.scrolls, [])
  await h.page.followPageLink(click(), 2)
  await settle()
  const tick = h.holdRender()
  h.calls.at(-1)!.resolve(response(2))
  await settle()
  h.stop()
  tick.resolve()
  await settle()
  assert.deepEqual(h.scrolls, [])
})

test('winning clamped page keeps pagination scroll intent through query replacement', async (context) => {
  const h = await harness()
  context.after(h.stop)
  await h.page.followPageLink(click(), 3)
  await settle()
  h.calls[0]!.resolve({ ...response(3), total_pages: 2 })
  await settle()
  assert.equal(h.calls[1]!.page, 2)
  h.calls[1]!.resolve({ ...response(2), total_pages: 2 })
  await settle()
  assert.deepEqual(h.route.query, { page: '2' })
  assert.equal(h.page.catalog.value?.page, 2)
  assert.deepEqual(h.scrolls, [{ top: 0, behavior: 'smooth' }])
})

test('router errors and unmount during fetch discard pending scroll', async (context) => {
  const h = await harness()
  context.after(h.stop)
  await h.page.followPageLink(click(), 2)
  await settle()
  h.navigationError()
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.deepEqual(h.scrolls, [])
  await h.page.followPageLink(click(), 3)
  await settle()
  h.stop()
  h.calls[1]!.resolve(response(3))
  await settle()
  assert.equal(
    h.page.catalog.value,
    null,
    'unmounted request cannot restore cleared stale cards',
  )
  assert.deepEqual(h.scrolls, [])
})

test('initial SSR-shaped history resolves API year and normalizes full cache/URL identity', async (context) => {
  const h = await harness(
    false,
    { vue: 'historique', mois: '09', unknown: 'x' },
    historyResponse(1, 2025, 9),
  )
  context.after(h.stop)
  assert.deepEqual(h.initialQueries, [{ view: 'history', month: 9, page: 1 }])
  assert.equal(h.cacheKey, 'upcoming:{"vue":"historique","mois":"9"}')
  assert.deepEqual(h.route.query, {
    vue: 'historique',
    annee: '2025',
    mois: '9',
  })
  assert.deepEqual(h.page.pagination.value, {
    view: 'history',
    year: 2025,
    month: 9,
    page: 1,
  })
  assert.equal(
    h.calls.length,
    0,
    'mounted default normalization does not refetch',
  )
  assert.deepEqual(h.scrolls, [])
})

test('history last-page and empty corrections preserve resolved view/year/month and retry scroll', async (context) => {
  const h = await harness(
    false,
    { vue: 'historique', annee: '2025', mois: '9' },
    historyResponse(1, 2025, 9),
  )
  context.after(h.stop)
  h.page.followPageLink(click(), 3)
  await settle()
  h.calls[0]!.resolve({ ...historyResponse(3, 2025, 9), total_pages: 2 })
  await settle()
  assert.deepEqual(h.calls[1]!.query, {
    view: 'history',
    year: 2025,
    month: 9,
    page: 2,
  })
  h.calls[1]!.resolve({ ...historyResponse(2, 2025, 9), total_pages: 2 })
  await settle()
  assert.deepEqual(h.route.query, {
    vue: 'historique',
    annee: '2025',
    mois: '9',
    page: '2',
  })
  assert.equal(h.scrolls.length, 1)
  h.route.query = { vue: 'historique', annee: '2024', mois: '11', page: '99' }
  await settle()
  h.calls[2]!.resolve({ ...historyResponse(99, 2024, 11), total_pages: 0 })
  await settle()
  assert.deepEqual(h.calls[3]!.query, {
    view: 'history',
    year: 2024,
    month: 11,
    page: 1,
  })
  h.calls[3]!.resolve({ ...historyResponse(1, 2024, 11), total_pages: 0 })
  await settle()
  assert.deepEqual(h.route.query, {
    vue: 'historique',
    annee: '2024',
    mois: '11',
  })
  assert.equal(h.scrolls.length, 1)
})

test('filter handlers push copies, reset month/page, and back/forward restores entire state', async (context) => {
  const h = await harness(
    false,
    { vue: 'historique', annee: '2025', mois: '9', page: '2' },
    historyResponse(2, 2025, 9),
  )
  context.after(h.stop)
  h.page.changeYear(selectChange('2024'))
  assert.deepEqual(
    h.page.pagination.value,
    { view: 'history', year: 2025, month: 9, page: 2 },
    'no committed state mutation before watcher',
  )
  await settle()
  assert.deepEqual(h.calls[0]!.query, { view: 'history', year: 2024, page: 1 })
  h.calls[0]!.resolve(historyResponse(1, 2024))
  await settle()
  h.page.changeMonth(selectChange('12'))
  await settle()
  assert.deepEqual(h.calls[1]!.query, {
    view: 'history',
    year: 2024,
    month: 12,
    page: 1,
  })
  h.calls[1]!.resolve(historyResponse(1, 2024, 12))
  await settle()
  h.page.changeMonth(selectChange(''))
  await settle()
  assert.deepEqual(h.calls[2]!.query, { view: 'history', year: 2024, page: 1 })
  h.calls[2]!.resolve(historyResponse(1, 2024))
  await settle()
  for (const month of [12, null]) {
    h.route.query = upcoming.upcomingRouteQuery({
      view: 'history',
      year: 2024,
      month,
      page: 1,
    })
    await settle()
    h.calls.at(-1)!.resolve(historyResponse(1, 2024, month))
    await settle()
    assert.equal(h.page.pagination.value.month, month)
  }
  assert.equal(h.pushes.length, 3)
  assert.deepEqual(h.scrolls, [])
})

test('superseded view/year/month responses cannot commit metadata and same-page filter changes clear scroll intent', async (context) => {
  const h = await harness()
  context.after(h.stop)
  h.page.followPageLink(click(), 2)
  await settle()
  const selections: LocationQuery[] = [
    { vue: 'historique', annee: '2025', page: '2' },
    { vue: 'historique', annee: '2024', page: '2' },
    { vue: 'historique', annee: '2024', mois: '12', page: '2' },
  ]
  for (const query of selections) {
    h.route.query = query
    await settle()
    assert.equal(
      h.page.catalog.value,
      null,
      'old catalog never renders under changed heading/filters',
    )
  }
  h.calls[3]!.resolve(historyResponse(2, 2024, 12))
  await settle()
  h.calls[2]!.resolve(historyResponse(2, 2024))
  h.calls[1]!.resolve(historyResponse(2, 2025))
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.equal(h.page.catalog.value?.view, 'history')
  assert.equal(h.page.catalog.value?.year, 2024)
  assert.equal(h.page.catalog.value?.month, 12)
  assert.deepEqual(h.scrolls, [])
})

test('failed history filters retain selection on retry and isolate unavailable message from upcoming', async (context) => {
  const h = await harness()
  context.after(h.stop)
  h.route.query = { vue: 'historique', annee: '2024', mois: '12' }
  await settle()
  const unavailable = Object.assign(new Error('unavailable'), {
    data: { error: { code: 'upcoming_unavailable' } },
  })
  h.calls[0]!.reject(unavailable)
  await settle()
  assert.equal(
    h.page.errorMessage.value,
    'L’historique des sorties n’est pas encore disponible. Réessayez plus tard.',
  )
  const retry = h.page.loadCatalog()
  assert.deepEqual(h.calls[1]!.query, {
    view: 'history',
    year: 2024,
    month: 12,
    page: 1,
  })
  h.calls[1]!.resolve(historyResponse(1, 2024, 12))
  await retry
  assert.equal(h.page.errorMessage.value, '')
  h.route.query = {}
  await settle()
  h.calls[2]!.reject(unavailable)
  await settle()
  assert.equal(h.page.errorMessage.value, 'Échec du chargement')
  assert.deepEqual(h.scrolls, [])
})
