import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { isBroadTheaterSelection } from '../app/utils/cinemaSelection.ts'
import ts from 'typescript'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import type { Ref, WatchStopHandle } from 'vue'
import type { MoviesQuery } from '../app/types/api.ts'
import * as dates from '../app/utils/date.ts'
import * as filters from '../app/utils/movieCatalogFilters.ts'
import * as presentation from '../app/utils/movieCatalogPresentation.ts'
import * as queries from '../app/utils/routeQuery.ts'
import { serializeJsonLd } from '../app/utils/jsonLd.ts'
import { absoluteSiteUrl } from '../app/utils/siteUrl.ts'

type Catalog = {
  total: number
  items: { slug: string; showtime_count?: number }[]
}
type InitialState = {
  kind: 'success' | 'upstream-error'
  catalog: Catalog | null
  errorMessage: string
}

async function filmsSetup(server: boolean) {
  const source = await readFile(
    new URL('../app/pages/films/index.vue', import.meta.url),
    'utf8',
  )
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )![1]!
  const parsed = ts.createSourceFile(
    'page.ts',
    script,
    ts.ScriptTarget.Latest,
    true,
  )
  // Execute the complete setup, including the awaited initial loader and mount hooks.
  const body = parsed.statements
    .filter((node) => !ts.isImportDeclaration(node))
    .map((node) => node.getFullText(parsed))
    .join('\n')
    .replaceAll('import.meta.server', String(server))
  return ts.transpileModule(
    `async function setup() { ${body}
      return { catalog, pending, errorMessage, isIndexable, filmsJsonLd, submitSearch, retryMovies }
    }`,
    { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
  ).outputText
}

async function fixture(
  options: {
    server?: boolean
    payload?: Record<string, InitialState>
    query?: Record<string, string>
    fail?: boolean
    initialize?: () => Promise<void>
    retrySynchronization?: () => Promise<void>
    theaterIds?: string[]
    catalog?: (query: MoviesQuery) => Catalog | Promise<Catalog>
  } = {},
) {
  const server = options.server ?? false
  const requests: MoviesQuery[] = []
  const mounted: (() => Promise<void>)[] = []
  const beforeUnmount: (() => void)[] = []
  const watchers: WatchStopHandle[] = []
  const payload = { ...options.payload }
  const event = { status: 200 }
  const response: Catalog = { total: 48, items: [{ slug: 'test-film' }] }
  const route = reactive({ query: options.query ?? {} })
  const preferences = {
    theaters: ref([{ id: 'ugc-46' }, { id: 'ugc-45' }, { id: 'other' }]),
    favoriteTheaterIds: ref(options.theaterIds ?? ['ugc-46', 'ugc-45']),
    selectionScopeKey: ref(0),
    isInitialized: ref(!options.initialize),
    error: ref(''),
    initialize: async () => {
      await options.initialize?.()
      preferences.isInitialized.value = true
    },
    retrySynchronization: async () => {
      await options.retrySynchronization?.()
    },
  }
  const bindings = {
    useWatchedCatalog: () => ({
      key: ref('public'),
      blocked: ref(false),
      movies: async (query: MoviesQuery) => {
        requests.push(query)
        if (options.fail) throw new Error('upstream unavailable')
        return options.catalog?.(query) ?? response
      },
    }),
    isBroadTheaterSelection,
    ...dates,
    ...filters,
    ...presentation,
    ...queries,
    serializeJsonLd,
    absoluteSiteUrl,
    ref,
    computed,
    nextTick,
    watch: (...args: Parameters<typeof watch>) => {
      const stop = watch(...args)
      watchers.push(stop)
      return stop
    },
    useMesSeancesApi: () => ({
      movies: async (query: MoviesQuery) => {
        requests.push(query)
        if (options.fail) throw new Error('upstream unavailable')
        return options.catalog?.(query) ?? response
      },
    }),
    useRoute: () => route,
    useRouter: () => ({
      replace: async ({ query }: { query: Record<string, string> }) => {
        route.query = query
      },
    }),
    useCinemaPreferences: () => preferences,
    // Model Nuxt's documented immediate/server defaults and hydration cache.
    // Installed Nuxt 4.5 loads cached data even when immediate is false.
    useAsyncData: async (
      key: string,
      handler: () => Promise<InitialState>,
      asyncOptions: { immediate?: boolean; server?: boolean } = {},
    ) => {
      let value = options.payload?.[key]
      if (
        value === undefined &&
        asyncOptions.immediate !== false &&
        (!server || asyncOptions.server !== false)
      ) {
        value = await handler()
        payload[key] = value
      }
      return { data: ref(value) }
    },
    getFrenchApiError: () => 'Catalogue indisponible',
    useRequestEvent: () => event,
    setResponseStatus: (requestEvent: typeof event, status: number) => {
      requestEvent.status = status
    },
    onMounted: (callback: () => Promise<void>) => mounted.push(callback),
    onBeforeUnmount: (callback: () => void) => beforeUnmount.push(callback),
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    useSeoMeta: () => {},
    useHead: () => {},
  }
  // SAFETY: Compile only the checked-in setup with explicit dependencies and returned refs.
  const setup = new Function(
    ...Object.keys(bindings),
    `${await filmsSetup(server)}\nreturn setup`,
  )(...Object.values(bindings)) as () => Promise<{
    catalog: Ref<Catalog | null>
    pending: Ref<boolean>
    errorMessage: Ref<string>
    isIndexable: Ref<boolean>
    filmsJsonLd: Ref<string | null>
    submitSearch: (search: string) => void
    retryMovies: () => Promise<void>
  }>
  const page = await setup()
  return {
    page,
    requests,
    payload,
    event,
    response,
    preferences,
    navigate: async (query: Record<string, string>) => {
      route.query = query
      await nextTick()
    },
    search: async (search: string) => {
      page.submitSearch(search)
      await nextTick()
    },
    mount: async () => {
      assert.equal(server, false, 'SSR must not run mounted hooks')
      for (const callback of mounted) await callback()
    },
    dispose: () => {
      for (const callback of beforeUnmount) callback()
      for (const stop of watchers) stop()
    },
  }
}

test('films SSR fetches nationwide data for SEO; hydration reuses payload before personalized mount', async (t) => {
  const ssr = await fixture({ server: true })
  t.after(ssr.dispose)
  assert.equal(ssr.requests.length, 1)
  assert.equal(ssr.requests[0]!.theaters, undefined)
  assert.equal(ssr.requests[0]!.currently_screened, true)
  assert.deepEqual(ssr.page.catalog.value, ssr.response)
  assert.equal(ssr.page.pending.value, false)
  assert.equal(ssr.page.isIndexable.value, true)
  assert.match(ssr.page.filmsJsonLd.value!, /test-film/)
  assert.equal(ssr.event.status, 200)

  const client = await fixture({ payload: ssr.payload })
  t.after(client.dispose)
  assert.equal(client.requests.length, 0)
  assert.deepEqual(client.page.catalog.value, ssr.page.catalog.value)
  assert.equal(client.page.pending.value, false)
  assert.equal(client.page.filmsJsonLd.value, ssr.page.filmsJsonLd.value)
  await client.mount()
  assert.equal(client.requests.length, 1)
  assert.equal(client.requests[0]!.theaters, 'ugc-46,ugc-45')
})

test('films client navigation skips nationwide fetch and waits for preferences before one filtered request', async (t) => {
  let resolve!: () => void
  const ready = new Promise<void>((yes) => {
    resolve = yes
  })
  const client = await fixture({ initialize: () => ready })
  t.after(client.dispose)
  assert.equal(client.requests.length, 0)
  assert.equal(client.page.catalog.value, null)
  assert.equal(client.page.pending.value, true)
  const mounting = client.mount()
  assert.equal(client.requests.length, 0)
  assert.equal(client.page.pending.value, true)
  resolve()
  await mounting
  assert.equal(client.requests.length, 1)
  assert.equal(client.requests[0]!.theaters, 'ugc-46,ugc-45')
  assert.deepEqual(client.page.catalog.value, client.response)
  assert.equal(client.page.pending.value, false)
})

test('films SSR upstream failure keeps 502 and hydrated error without a client setup retry', async (t) => {
  const ssr = await fixture({ server: true, fail: true })
  t.after(ssr.dispose)
  assert.equal(ssr.requests.length, 1)
  assert.equal(ssr.event.status, 502)
  assert.equal(ssr.page.catalog.value, null)
  assert.equal(ssr.page.pending.value, false)
  assert.equal(ssr.page.isIndexable.value, false)
  assert.equal(ssr.page.errorMessage.value, 'Catalogue indisponible')
  const client = await fixture({ payload: ssr.payload })
  t.after(client.dispose)
  assert.equal(client.requests.length, 0)
  assert.equal(client.page.errorMessage.value, ssr.page.errorMessage.value)
  await client.mount()
  assert.equal(client.requests.length, 1)
  assert.equal(client.requests[0]!.theaters, 'ugc-46,ugc-45')
  assert.equal(client.page.errorMessage.value, '')
})

test('films client navigation preserves explicit all-theater search and route filters', async (t) => {
  const client = await fixture({
    query: {
      all_theaters: '1',
      q: 'Test',
      genres: 'Drame',
      duration: 'short',
      date: 'today',
      sort: 'title_asc',
      page: '2',
    },
    theaterIds: [],
  })
  t.after(client.dispose)
  assert.equal(client.requests.length, 0)
  await client.mount()
  assert.deepEqual(client.requests, [
    {
      include_ended: true,
      theaters: undefined,
      search: 'Test',
      genres: 'Drame',
      duration: 'short',
      date: 'today',
      date_to: undefined,
      sort: 'title_asc',
      page: 2,
      page_size: 24,
    },
  ])
  assert.equal(client.page.pending.value, false)
})

test('films client navigation with an empty selection browses nationwide movies', async (t) => {
  const client = await fixture({ theaterIds: [] })
  t.after(client.dispose)
  await client.mount()
  assert.equal(client.requests.length, 1)
  assert.equal(client.requests[0]?.theaters, undefined)
  assert.deepEqual(client.page.catalog.value, client.response)
  assert.equal(client.page.pending.value, false)
})

test('films full-catalog selection normalizes to nationwide without huge CSV', async (t) => {
  const client = await fixture({ theaterIds: ['ugc-46', 'ugc-45', 'other'] })
  t.after(client.dispose)
  await client.mount()
  assert.equal(client.requests.length, 1)
  assert.equal(client.requests[0]?.theaters, undefined)
})

test('films SSR and client searches include zero-count movies with effective cinema scope and catalog filters', async (t) => {
  const cases = [
    { name: 'SSR', server: true },
    { name: 'saved subset favorites', theaterIds: ['ugc-46'] },
    { name: 'empty selection', theaterIds: [] },
    { name: 'full selection', theaterIds: ['ugc-46', 'ugc-45', 'other'] },
    { name: 'explicit all theaters', all_theaters: '1' },
  ]
  for (const scenario of cases) {
    await t.test(scenario.name, async (t) => {
      const query = {
        q: '  Film  ',
        genres: 'Drame',
        duration: 'short',
        sort: 'title_asc',
        page: '2',
      }
      const page = await fixture({
        ...scenario,
        query: scenario.all_theaters ? { ...query, all_theaters: '1' } : query,
        catalog: (query) => ({
          total: 25,
          items: query.include_ended
            ? [{ slug: 'film-never-screened' }, { slug: 'film-past-only' }]
            : [],
        }),
      })
      t.after(page.dispose)
      if (!scenario.server) await page.mount()
      assert.deepEqual(page.requests, [
        {
          include_ended: true,
          theaters:
            scenario.name === 'saved subset favorites' ? 'ugc-46' : undefined,
          search: 'Film',
          genres: 'Drame',
          duration: 'short',
          date: undefined,
          date_to: undefined,
          sort: 'title_asc',
          page: 2,
          page_size: 24,
        },
      ])
      assert.deepEqual(page.page.catalog.value?.items, [
        { slug: 'film-never-screened' },
        { slug: 'film-past-only' },
      ])
      assert.equal(page.page.pending.value, false)
    })
  }
})

test('films blank searches remain current-only in SSR and client loaders', async (t) => {
  for (const server of [true, false]) {
    const page = await fixture({ server, theaterIds: [], query: { q: '  ' } })
    t.after(page.dispose)
    if (!server) await page.mount()
    assert.equal(page.requests.length, 1)
    assert.equal(page.requests[0]!.currently_screened, true)
    assert.equal(page.requests[0]!.include_ended, undefined)
    assert.equal(page.requests[0]!.search, undefined)
  }
})

test('films dated title searches include full inventory with public SSR and personalized client counts', async (t) => {
  const today = dates.todayInParis()
  const cases = [true, false].flatMap((server) => [
    {
      name: `${server ? 'SSR' : 'client'} date`,
      server,
      query: { date: 'today' },
    },
    {
      name: `${server ? 'SSR' : 'client'} all theaters date range`,
      server,
      query: {
        all_theaters: '1',
        date: today,
        date_to: dates.addCalendarDays(today, 1),
      },
    },
    {
      name: `${server ? 'SSR' : 'client'} date range`,
      server,
      query: { date: today, date_to: dates.addCalendarDays(today, 1) },
    },
  ])
  for (const scenario of cases) {
    await t.test(scenario.name, async (t) => {
      const page = await fixture({
        server: scenario.server,
        query: { q: 'Film', ...scenario.query },
        theaterIds: ['ugc-46'],
      })
      t.after(page.dispose)
      if (!scenario.server) await page.mount()
      assert.equal(page.requests.length, 1)
      const request = page.requests[0]!
      assert.equal(request.currently_screened, undefined)
      assert.equal(request.include_ended, true)
      assert.equal(request.search, 'Film')
      assert.equal(request.date, scenario.query.date)
      assert.equal(request.date_to, scenario.query.date_to)
      assert.equal(
        request.theaters,
        scenario.server || scenario.query.all_theaters ? undefined : 'ugc-46',
      )
    })
  }
})

test('films search eligibility uses normalized effective dates, not invalid route parameters', async (t) => {
  for (const server of [true, false]) {
    const page = await fixture({
      server,
      theaterIds: [],
      query: { q: 'Film', date: 'invalid', date_to: 'invalid' },
    })
    t.after(page.dispose)
    if (!server) await page.mount()
    assert.equal(page.requests.length, 1)
    assert.equal(page.requests[0]!.include_ended, true)
    assert.equal(page.requests[0]!.currently_screened, undefined)
    assert.equal(page.requests[0]!.date, undefined)
    assert.equal(page.requests[0]!.date_to, undefined)
  }
})

test('films client search pagination retains full inventory and clearing search restores current browsing at page one', async (t) => {
  const allMovies = [
    { slug: 'z-positive', showtime_count: 2 },
    ...Array.from({ length: 24 }, (_, index) => ({
      slug: `a-without-seances-${index + 1}`,
    })),
  ]
  const currentMovies = [{ slug: 'currently-screened-film' }]
  const client = await fixture({
    theaterIds: ['ugc-46'],
    query: { q: 'Film' },
    catalog: (query) => {
      const movies = query.include_ended ? allMovies : currentMovies
      const offset = ((query.page ?? 1) - 1) * 24
      return { total: movies.length, items: movies.slice(offset, offset + 24) }
    },
  })
  t.after(client.dispose)
  await client.mount()
  assert.equal(client.page.catalog.value?.total, 25)
  assert.equal(client.page.catalog.value?.items.length, 24)
  assert.deepEqual(client.page.catalog.value?.items, allMovies.slice(0, 24))
  await client.navigate({ q: 'Film', page: '2' })
  assert.equal(client.requests[1]!.include_ended, true)
  assert.equal(client.requests[1]!.currently_screened, undefined)
  assert.equal(client.requests[1]!.page, 2)
  assert.equal(client.requests[1]!.theaters, 'ugc-46')
  assert.deepEqual(client.page.catalog.value?.items, [allMovies[24]])
  await client.search('')
  assert.equal(client.requests.length, 3)
  assert.equal(client.requests[2]!.currently_screened, true)
  assert.equal(client.requests[2]!.include_ended, undefined)
  assert.equal(client.requests[2]!.search, undefined)
  assert.equal(client.requests[2]!.page, 1)
  assert.equal(client.requests[2]!.theaters, 'ugc-46')
  assert.deepEqual(client.page.catalog.value?.items, currentMovies)
})

test('films title search hydrates public SSR then waits for personalized counts before reloading', async (t) => {
  for (const query of [{ q: 'Film' }, { q: 'Film', date: 'today' }]) {
    const catalog = (query: MoviesQuery): Catalog => ({
      total: 2,
      items: [
        { slug: 'z-positive', showtime_count: query.theaters ? 2 : 10 },
        { slug: 'a-zero' },
      ],
    })
    const ssr = await fixture({
      server: true,
      query,
      theaterIds: ['ugc-46'],
      catalog,
    })
    t.after(ssr.dispose)
    assert.equal(ssr.requests[0]!.theaters, undefined)
    let resolve!: () => void
    const ready = new Promise<void>((yes) => {
      resolve = yes
    })
    const client = await fixture({
      query,
      theaterIds: ['ugc-46'],
      initialize: () => ready,
      payload: ssr.payload,
      catalog,
    })
    t.after(client.dispose)
    assert.deepEqual(client.page.catalog.value, ssr.page.catalog.value)
    assert.equal(client.requests.length, 0)
    const mounting = client.mount()
    await nextTick()
    assert.equal(client.preferences.isInitialized.value, false)
    assert.equal(client.requests.length, 0)
    assert.equal(client.page.catalog.value, null)
    assert.equal(client.page.pending.value, true)
    resolve()
    await mounting
    assert.equal(client.requests.length, 1)
    assert.deepEqual(client.requests[0], {
      ...ssr.requests[0],
      theaters: 'ugc-46',
    })
    assert.deepEqual(client.page.catalog.value, catalog(client.requests[0]!))
    assert.equal(client.page.pending.value, false)
  }
})

test('films title searches surface preference errors and retry synchronization without nationwide fallback', async (t) => {
  for (const query of [{ q: 'Film' }, { q: 'Film', date: 'today' }]) {
    let retries = 0
    const client = await fixture({
      query,
      theaterIds: ['ugc-46'],
      retrySynchronization: async () => {
        retries++
        if (retries > 1) {
          client.preferences.error.value = ''
          client.preferences.isInitialized.value = true
        }
      },
    })
    t.after(client.dispose)
    client.preferences.isInitialized.value = false
    client.preferences.error.value = 'Préférences indisponibles'
    await client.mount()
    assert.equal(client.requests.length, 0)
    assert.equal(client.page.catalog.value, null)
    assert.equal(client.page.pending.value, false)
    assert.equal(client.page.errorMessage.value, 'Préférences indisponibles')
    client.preferences.isInitialized.value = false
    await client.page.retryMovies()
    assert.equal(retries, 1)
    assert.equal(client.requests.length, 0)
    assert.equal(client.page.pending.value, false)
    await client.page.retryMovies()
    assert.equal(retries, 2)
    assert.ok(client.requests.length > 0)
    assert.ok(
      client.requests.every(
        (request) =>
          request.theaters === 'ugc-46' &&
          request.include_ended &&
          !request.currently_screened &&
          request.date === query.date,
      ),
    )
    assert.equal(client.page.errorMessage.value, '')
    assert.deepEqual(client.page.catalog.value, client.response)
  }
})

test('films search changes still wait for favorite cinemas; all_theaters bypasses unresolved and failed preferences', async (t) => {
  let resolve!: () => void
  const ready = new Promise<void>((yes) => {
    resolve = yes
  })
  const client = await fixture({
    initialize: () => ready,
    theaterIds: ['ugc-46'],
    retrySynchronization: async () => {
      throw new Error('all_theaters must not retry preferences')
    },
  })
  t.after(client.dispose)
  const mounting = client.mount()
  await nextTick()
  assert.equal(client.requests.length, 0)
  await client.search('Film')
  assert.equal(client.requests.length, 0)
  assert.equal(client.page.pending.value, true)
  client.preferences.error.value = 'Préférences indisponibles'
  await client.navigate({ q: 'Film', all_theaters: '1', date: 'today' })
  assert.equal(client.requests.length, 1)
  assert.equal(client.requests[0]!.include_ended, true)
  assert.equal(client.requests[0]!.theaters, undefined)
  assert.equal(client.page.pending.value, false)
  assert.equal(client.page.errorMessage.value, '')
  client.preferences.favoriteTheaterIds.value = ['ugc-45']
  client.preferences.selectionScopeKey.value++
  await nextTick()
  assert.equal(client.requests.length, 1)
  await client.page.retryMovies()
  assert.equal(client.requests.length, 2)
  resolve()
  await mounting
  assert.equal(client.requests.length, 2)
  assert.deepEqual(client.page.catalog.value, client.response)
})

test('films search favorite and identity changes refetch scoped counts and fence in-flight old responses', async (t) => {
  let resolveOld!: (catalog: Catalog) => void
  const oldResponse = new Promise<Catalog>((resolve) => {
    resolveOld = resolve
  })
  const scopedResponse: Catalog = {
    total: 2,
    items: [{ slug: 'z-positive', showtime_count: 3 }, { slug: 'a-zero' }],
  }
  const changedResponse: Catalog = {
    total: 2,
    items: [{ slug: 'a-zero', showtime_count: 1 }, { slug: 'z-positive' }],
  }
  let oldScopeRequests = 0
  const client = await fixture({
    query: { q: 'Film', date: 'today', sort: 'title_asc' },
    theaterIds: ['ugc-46'],
    catalog: (query) => {
      if (query.theaters !== 'ugc-46') return scopedResponse
      return oldScopeRequests++ === 0 ? oldResponse : changedResponse
    },
  })
  t.after(client.dispose)
  const mounting = client.mount()
  await nextTick()
  assert.equal(client.requests.length, 1)
  client.preferences.favoriteTheaterIds.value = ['ugc-45']
  assert.equal(client.page.catalog.value, null)
  assert.equal(client.page.pending.value, true)
  await nextTick()
  assert.equal(client.requests.length, 2)
  assert.equal(client.requests[1]!.theaters, 'ugc-45')
  assert.deepEqual(client.page.catalog.value, scopedResponse)
  resolveOld({ total: 1, items: [{ slug: 'stale', showtime_count: 99 }] })
  await mounting
  assert.deepEqual(client.page.catalog.value, scopedResponse)
  assert.equal(client.page.pending.value, false)
  client.preferences.selectionScopeKey.value++
  assert.equal(client.page.catalog.value, null)
  await nextTick()
  assert.equal(client.requests.length, 3)
  assert.deepEqual(client.page.catalog.value, scopedResponse)
  await client.navigate({ q: 'Film', date: 'today', sort: 'title_asc' })
  assert.equal(
    client.requests.length,
    4,
    'selection invalidation clears the previous load key',
  )
  await client.navigate({ q: 'Film', date: 'today', sort: 'title_asc' })
  assert.equal(
    client.requests.length,
    4,
    'identical route and resolved favorites deduplicate',
  )
  client.preferences.favoriteTheaterIds.value = ['ugc-46']
  await nextTick()
  assert.equal(client.requests.length, 5)
  assert.deepEqual(client.page.catalog.value, changedResponse)
  assert.ok(
    client.requests.every(
      (request) =>
        request.include_ended &&
        !request.currently_screened &&
        request.date === 'today',
    ),
  )
})

test('films scoped inventory search clamps out-of-range pages while retaining date range and API results', async (t) => {
  const date = dates.todayInParis()
  const query = {
    q: 'Film',
    date,
    date_to: dates.addCalendarDays(date, 1),
    page: '99',
    sort: 'title_asc',
  }
  const zeroMatch = { slug: 'zero-match' }
  const client = await fixture({
    query,
    theaterIds: ['ugc-46'],
    catalog: (request) => ({
      total: 25,
      items: request.page === 2 ? [zeroMatch] : [],
    }),
  })
  t.after(client.dispose)
  await client.mount()
  await nextTick()
  assert.deepEqual(
    client.requests.map((request) => request.page),
    [99, 2],
  )
  assert.ok(
    client.requests.every(
      (request) =>
        request.include_ended &&
        !request.currently_screened &&
        request.theaters === 'ugc-46' &&
        request.date === date &&
        request.date_to === query.date_to,
    ),
  )
  assert.deepEqual(client.page.catalog.value, { total: 25, items: [zeroMatch] })
  await client.search('')
  assert.equal(client.requests.at(-1)!.currently_screened, true)
  assert.equal(client.requests.at(-1)!.include_ended, undefined)
  assert.equal(client.requests.at(-1)!.page, 1)
  assert.equal(client.requests.at(-1)!.date_to, query.date_to)
})
