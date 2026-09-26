import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, nextTick, ref, watch } from 'vue'
import type { Ref, WatchStopHandle } from 'vue'
import type { MoviesQuery } from '../app/types/api.ts'
import * as dates from '../app/utils/date.ts'
import * as filters from '../app/utils/movieCatalogFilters.ts'
import * as presentation from '../app/utils/movieCatalogPresentation.ts'
import * as queries from '../app/utils/routeQuery.ts'
import { serializeJsonLd } from '../app/utils/jsonLd.ts'
import { absoluteSiteUrl } from '../app/utils/siteUrl.ts'

type Catalog = { total: number; items: { slug: string }[] }
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
      return { catalog, pending, errorMessage, isIndexable, filmsJsonLd }
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
    theaterIds?: string[]
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
  const route = { query: options.query ?? {} }
  const preferences = {
    favoriteTheaterIds: ref(options.theaterIds ?? ['ugc-46', 'ugc-45']),
    selectionScopeKey: ref(0),
    isInitialized: ref(!options.initialize),
    error: ref(''),
    initialize: async () => {
      await options.initialize?.()
      preferences.isInitialized.value = true
    },
  }
  const bindings = {
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
        return response
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
  }>
  const page = await setup()
  return {
    page,
    requests,
    payload,
    event,
    response,
    preferences,
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

test('films client navigation preserves explicit all-theater browsing and route filters', async (t) => {
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
      currently_screened: true,
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

test('films client navigation with an empty selection never falls back to nationwide movies', async (t) => {
  const client = await fixture({ theaterIds: [] })
  t.after(client.dispose)
  await client.mount()
  assert.equal(client.requests.length, 0)
  assert.equal(client.page.catalog.value, null)
  assert.equal(client.page.pending.value, false)
})
