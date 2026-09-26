import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import {
  computed,
  effectScope,
  nextTick,
  reactive,
  ref,
  watch,
  type Ref,
} from 'vue'
import type { LocationQuery } from 'vue-router'
import type {
  MovieShowtimesQuery,
  MovieShowtimesResponse,
} from '../app/types/api.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'
import * as filters from '../app/utils/showtimeFilters.ts'
import * as initialSchedule from '../app/utils/filmInitialSchedule.ts'
import { isShowtimeFormat } from '../app/utils/formats.ts'
import { buildFilmJsonLd } from '../app/utils/filmJsonLd.ts'
import { serializeJsonLd } from '../app/utils/jsonLd.ts'
import { absoluteSiteUrl } from '../app/utils/siteUrl.ts'
import { buildMovieExternalLinks } from '../app/utils/movieExternalLinks.ts'
import { isIndexableMovie } from '../app/utils/movieIndexability.ts'
import { safeBackdropUrl, safePosterUrl } from '../app/utils/safeImageUrl.ts'

const TODAY = '2027-06-27'
const TOMORROW = '2027-06-28'
const source = await readFile(
  new URL('../app/pages/film/[slug].vue', import.meta.url),
  'utf8',
)
const parsed = ts.createSourceFile(
  'film.ts',
  source
    .match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
    .replaceAll('import.meta.server', 'isServer'),
  ts.ScriptTarget.Latest,
  true,
)
const compiled = ts.transpileModule(
  parsed.statements
    .filter((statement) => !ts.isImportDeclaration(statement))
    .map((statement) => statement.getFullText(parsed))
    .join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ESNext } },
).outputText

function response(
  slug = 'film-1',
  query: MovieShowtimesQuery = { date: TODAY },
): MovieShowtimesResponse {
  return {
    movie: {
      slug,
      title: 'Film',
      original_language: 'en',
      runtime_minutes: 100,
      updated_at: `${TODAY}T00:00:00Z`,
      poster_url: null,
      tmdb_id: null,
      imdb_id: null,
      overview: null,
      release_date: null,
      french_release_date: null,
      genres: [],
    },
    date: query.date,
    available_dates: [TODAY, TOMORROW],
    currently_screened: true,
    release_status: 'showing',
    backdrop_url: null,
    theaters: (
      query.theaters?.split(',') ?? [query.city ? 'paris' : 'nationwide']
    ).map((id) => ({
      id,
      slug: id,
      provider: 'ugc',
      name: id,
      city: 'City',
      city_slug: 'city',
      showtimes: [],
    })),
  }
}

function publicPayload() {
  return {
    kind: 'success',
    schedule: response('film-1', { date: TODAY, city: 'Paris' }),
    nationwide: response(),
    selectedDate: TODAY,
    errorMessage: '',
  }
}

interface Page {
  schedule: Ref<MovieShowtimesResponse | null>
  pending: Ref<boolean>
  notFound: Ref<boolean>
  errorMessage: Ref<string>
  isPersonalizedSchedule: Ref<boolean>
  selectedDate: Ref<string>
  robots: Ref<string>
  applyRoute: () => Promise<void>
  retryLoad: () => Promise<void>
}

interface Options {
  initialized?: boolean
  ids?: string[]
  payload?: ReturnType<typeof publicPayload>
  server?: boolean
  bundle?: boolean
  query?: LocationQuery
  fetch?: (
    slug: string,
    query: MovieShowtimesQuery,
  ) => Promise<MovieShowtimesResponse>
}

async function settle() {
  // Flush Vue's actual watcher queue and chained API/router promises.
  for (let i = 0; i < 20; i++) await nextTick()
}

async function harness(options: Options = {}) {
  const scope = effectScope()
  const query: LocationQuery = options.query ?? {}
  const route = reactive({ params: { slug: 'film-1' }, query })
  const preferences = {
    activeTheaterIds: ref(options.ids ?? ['ugc-25']),
    selectionScopeKey: ref(0),
    isInitialized: ref(options.initialized ?? true),
    error: ref<string | null>(null),
    initialize: async () => {},
    retrySynchronization: async () => {},
  }
  const calls: Array<{ slug: string; query: MovieShowtimesQuery }> = []
  const bundleCalls: string[] = []
  const redirects: unknown[] = []
  const heads: unknown[] = []
  const statuses: number[] = []
  const mounted: Array<() => void> = []
  const unmounted: Array<() => void> = []
  let payload: unknown
  let loaderCalls = 0
  const bindings = {
    ...routeQuery,
    ...filters,
    ...initialSchedule,
    ref,
    computed,
    nextTick,
    watch: (...args: Parameters<typeof watch>) =>
      scope.run(() => watch(...args)),
    isServer: options.server ?? false,
    todayInParis: () => TODAY,
    isShowtimeFormat,
    buildFilmJsonLd,
    serializeJsonLd,
    absoluteSiteUrl,
    buildMovieExternalLinks,
    isIndexableMovie,
    safeBackdropUrl,
    safePosterUrl,
    useRoute: () => route,
    useRouter: () => ({
      replace: async ({ query }: { query: LocationQuery }) => {
        route.query = query
      },
    }),
    usePageCinemaSelection: () => preferences,
    useMesSeancesApi: () => ({
      hasInternalApiIdentity: options.bundle ?? false,
      movieShowtimes: async (slug: string, query: MovieShowtimesQuery) => {
        calls.push({ slug, query })
        return options.fetch
          ? options.fetch(slug, query)
          : response(slug, query)
      },
      movieShowtimesBundle: async (slug: string, date: string) => {
        bundleCalls.push(date)
        return {
          scoped: response(slug, { date, city: 'Paris' }),
          nationwide: response(slug, { date }),
        }
      },
    }),
    useAsyncData: async (
      _key: string,
      loader: () => Promise<ReturnType<typeof publicPayload>>,
    ) => {
      if (options.payload) payload = options.payload
      else {
        loaderCalls++
        payload = await loader()
      }
      return { data: ref(payload) }
    },
    navigateTo: async (...args: unknown[]) => {
      redirects.push(args)
    },
    isNotFoundError: (error: { status?: number }) => error?.status === 404,
    getFrenchApiError: () => 'Service indisponible',
    useRequestEvent: () => ({}),
    setResponseStatus: (_event: Record<string, never>, status: number) => {
      statuses.push(status)
    },
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://example.test' } }),
    useSeoMeta: () => {},
    useHead: (
      head:
        | { script: Array<{ innerHTML: string }> }
        | (() => { link: Array<{ rel: string; href: string }> }),
    ) => {
      heads.push(head)
    },
    onMounted: (callback: () => void) => {
      mounted.push(callback)
    },
    onBeforeUnmount: (callback: () => void) => {
      unmounted.push(callback)
    },
    window: {
      setInterval: () => 1,
      setTimeout: () => 1,
      clearInterval: () => {},
      clearTimeout: () => {},
    },
    document: { addEventListener: () => {}, removeEventListener: () => {} },
  }
  // Execute the complete actual setup, including awaited initial loader, real
  // Vue watchers, lifecycle registration and SEO. Only Nuxt/browser IO is mocked.
  // SAFETY: The wrapper explicitly returns the listed refs and methods from the page setup.
  const page = (await new Function(
    ...Object.keys(bindings),
    `return (async () => { ${compiled}\nreturn { schedule, pending, notFound, errorMessage, isPersonalizedSchedule, selectedDate, robots, applyRoute, retryLoad }; })()`,
  )(...Object.values(bindings))) as Page
  return {
    page,
    route,
    preferences,
    calls,
    bundleCalls,
    redirects,
    heads,
    statuses,
    payload,
    loaderCalls,
    mount: async () => {
      for (const callback of mounted) callback()
      await settle()
    },
    close: () => {
      for (const callback of unmounted) callback()
      scope.stop()
    },
  }
}

test('ready client navigation sends one nationwide and one selected request, never Paris', async (t) => {
  const h = await harness()
  t.after(h.close)
  await h.mount()
  assert.deepEqual(
    h.calls.map(({ query }) => query),
    [{ date: TODAY }, { date: TODAY, theaters: 'ugc-25' }],
  )
  assert.deepEqual(
    h.page.schedule.value?.theaters.map(({ id }) => id),
    ['ugc-25'],
  )
  assert.equal(h.page.pending.value, false)
  assert.equal(h.page.isPersonalizedSchedule.value, true)
  h.preferences.activeTheaterIds.value = ['ugc-25']
  await settle()
  await h.page.applyRoute()
  assert.equal(h.calls.length, 2)
})

test('hydration reuses SSR public evidence and waits across account initialization transitions', async (t) => {
  const h = await harness({ payload: publicPayload(), initialized: false })
  t.after(h.close)
  assert.deepEqual(
    h.page.schedule.value?.theaters.map(({ id }) => id),
    ['paris'],
  )
  await h.mount()
  for (let i = 0; i < 3; i++) {
    h.preferences.selectionScopeKey.value++
    h.preferences.activeTheaterIds.value = []
    await settle()
  }
  assert.equal(h.loaderCalls, 0)
  assert.equal(h.calls.length, 0)
  h.preferences.activeTheaterIds.value = ['ugc-46', 'ugc-45', 'ugc-25']
  h.preferences.isInitialized.value = true
  await settle()
  assert.deepEqual(
    h.calls.map(({ query }) => query),
    [{ date: TODAY, theaters: 'ugc-46,ugc-45,ugc-25' }],
  )
  assert.equal(h.page.pending.value, false)
})

test('SSR stays Paris scoped with nationwide SEO and never serializes saved selection', async (t) => {
  for (const bundle of [true, false]) {
    const h = await harness({ server: true, bundle, ids: ['private-theater'] })
    t.after(h.close)
    assert.deepEqual(
      h.page.schedule.value?.theaters.map(({ id }) => id),
      ['paris'],
    )
    assert.equal(JSON.stringify(h.payload).includes('private-theater'), false)
    assert.deepEqual(JSON.parse(JSON.stringify(h.payload)).nationwide, {
      ...response(),
      theaters: [],
    })
    assert.equal(h.heads.length, 2)
    assert.equal(h.page.robots.value, 'index,follow')
    assert.deepEqual(
      h.calls.map(({ query }) => query),
      bundle ? [] : [{ date: TODAY }, { date: TODAY, city: 'Paris' }],
    )
    assert.deepEqual(h.bundleCalls, bundle ? [TODAY] : [])
  }
})

test('selection and date changes reuse only matching nationwide evidence; filters make no requests', async (t) => {
  const h = await harness()
  t.after(h.close)
  await h.mount()
  h.preferences.activeTheaterIds.value = ['ugc-26']
  await settle()
  assert.deepEqual(h.calls.at(-1)?.query, { date: TODAY, theaters: 'ugc-26' })
  assert.equal(h.calls.length, 3)
  h.route.query = { date: TOMORROW, language: 'ORIGINAL' }
  await settle()
  assert.deepEqual(
    h.calls.slice(3).map(({ query }) => query),
    [{ date: TOMORROW }, { date: TOMORROW, theaters: 'ugc-26' }],
  )
  h.route.query = { ...h.route.query, sort: 'next' }
  await settle()
  assert.equal(h.calls.length, 5)
  h.route.params.slug = 'film-2'
  await settle()
  assert.deepEqual(
    h.calls.slice(5).map(({ slug, query }) => [slug, query]),
    [
      ['film-2', { date: TOMORROW }],
      ['film-2', { date: TOMORROW, theaters: 'ugc-26' }],
    ],
  )
})

test('empty selection shows no nationwide theaters and shared query is preserved', async (t) => {
  const h = await harness({ ids: [], query: { shared_theaters: '' } })
  t.after(h.close)
  await h.mount()
  assert.equal(h.calls.length, 1)
  assert.deepEqual(h.page.schedule.value?.theaters, [])
  h.preferences.activeTheaterIds.value = ['ugc-shared']
  h.route.query = { shared_theaters: 'ugc-shared', language: 'ORIGINAL' }
  await settle()
  assert.deepEqual(h.calls.at(-1)?.query, {
    date: TODAY,
    theaters: 'ugc-shared',
  })
  assert.equal(h.route.query.shared_theaters, 'ugc-shared')
  assert.equal(h.route.query.language, 'ORIGINAL')
})

test('scope transitions clear private theaters synchronously and reject late old-account results', async (t) => {
  let release: ((value: MovieShowtimesResponse) => void) | undefined
  const h = await harness({
    fetch: async (slug, query) =>
      query.theaters === 'old'
        ? new Promise((resolve) => {
            release = resolve
          })
        : response(slug, query),
  })
  t.after(h.close)
  await h.mount()
  h.preferences.activeTheaterIds.value = ['old']
  await settle()
  h.preferences.selectionScopeKey.value++
  h.preferences.isInitialized.value = false
  assert.deepEqual(h.page.schedule.value?.theaters, [])
  await settle()
  const before = h.calls.length
  release!(response('film-1', { date: TODAY, theaters: 'old' }))
  await settle()
  assert.deepEqual(h.page.schedule.value?.theaters, [])
  assert.equal(h.calls.length, before)
  h.preferences.activeTheaterIds.value = ['new']
  h.preferences.isInitialized.value = true
  await settle()
  assert.deepEqual(
    h.page.schedule.value?.theaters.map(({ id }) => id),
    ['new'],
  )
  assert.equal(h.calls.filter(({ query }) => !query.theaters).length, 1)
})

test('initial errors and not-found do not silently refetch on mount; explicit retry recovers', async (t) => {
  for (const status of [404, 502]) {
    let failing = true
    const h = await harness({
      fetch: async (slug, query) => {
        if (failing) throw { status }
        return response(slug, query)
      },
    })
    t.after(h.close)
    await h.mount()
    h.preferences.selectionScopeKey.value++
    await settle()
    assert.equal(h.calls.length, 1)
    assert.equal(h.page.notFound.value, status === 404)
    assert.equal(h.page.pending.value, false)
    failing = false
    await h.page.retryLoad()
    assert.equal(h.calls.length, 3)
    assert.equal(h.page.notFound.value, false)
    assert.equal(h.page.errorMessage.value, '')
  }
})

test('scoped error retry reuses nationwide evidence', async (t) => {
  let failing = true
  const h = await harness({
    fetch: async (slug, query) => {
      if (query.theaters && failing) throw new Error('unavailable')
      return response(slug, query)
    },
  })
  t.after(h.close)
  await h.mount()
  assert.equal(h.page.errorMessage.value, 'Service indisponible')
  assert.equal(h.page.pending.value, false)
  failing = false
  await h.page.retryLoad()
  assert.equal(h.calls.length, 3)
  assert.equal(h.page.errorMessage.value, '')
})

test('uninitialized client navigation keeps public Paris fallback until selection settles', async (t) => {
  const h = await harness({ initialized: false })
  t.after(h.close)
  await h.mount()
  assert.deepEqual(
    h.calls.map(({ query }) => query),
    [{ date: TODAY }, { date: TODAY, city: 'Paris' }],
  )
  h.preferences.error.value = 'Synchronisation indisponible'
  await settle()
  assert.equal(h.page.pending.value, false)
  assert.equal(h.page.errorMessage.value, 'Synchronisation indisponible')
  h.preferences.retrySynchronization = async () => {
    h.preferences.error.value = null
    h.preferences.activeTheaterIds.value = ['ugc-26']
    h.preferences.isInitialized.value = true
    await settle()
  }
  await h.page.retryLoad()
  await settle()
  assert.deepEqual(
    h.calls.slice(2).map(({ query }) => query),
    [{ date: TODAY, theaters: 'ugc-26' }],
  )
})

test('selected scope resolves its own fallback date without duplicate requests from canonical replace', async (t) => {
  const h = await harness({
    fetch: async (slug, query) => ({
      ...response(slug, query),
      available_dates: query.theaters ? [TOMORROW] : [TODAY, TOMORROW],
    }),
  })
  t.after(h.close)
  await h.mount()
  assert.deepEqual(
    h.calls.map(({ query }) => query),
    [
      { date: TODAY },
      { date: TODAY, theaters: 'ugc-25' },
      { date: TOMORROW, theaters: 'ugc-25' },
    ],
  )
  assert.equal(h.page.selectedDate.value, TOMORROW)
  assert.equal(h.page.schedule.value?.date, TOMORROW)
  assert.equal(h.route.query.date, undefined)
})

test('ended films remain public without waiting for account preferences', async (t) => {
  const h = await harness({
    initialized: false,
    fetch: async (slug, query) => ({
      ...response(slug, query),
      currently_screened: false,
      release_status: 'ended',
      available_dates: [],
      theaters: [],
    }),
  })
  t.after(h.close)
  await h.mount()
  h.preferences.selectionScopeKey.value++
  await settle()
  assert.equal(h.calls.length, 2)
  assert.equal(h.page.pending.value, false)
  assert.equal(h.page.schedule.value?.release_status, 'ended')
})

test('SSR failures preserve HTTP status and canonical slugs preserve shared query redirects', async (t) => {
  for (const status of [404, 502]) {
    const h = await harness({
      server: true,
      fetch: async () => {
        throw { status }
      },
    })
    t.after(h.close)
    assert.deepEqual(h.statuses, [status])
    assert.equal(h.page.robots.value, 'noindex,follow')
  }
  const query = { shared_theaters: 'ugc-25', language: 'ORIGINAL' }
  const h = await harness({
    query,
    fetch: async (_slug, query) => response('canonical-film', query),
  })
  t.after(h.close)
  assert.deepEqual(h.redirects, [
    [
      { path: '/film/canonical-film', query },
      { redirectCode: 308, replace: true },
    ],
  ])
})

test('concurrent selection changes share an in-flight nationwide date request and admit only latest selection', async (t) => {
  let release: ((value: MovieShowtimesResponse) => void) | undefined
  const h = await harness({
    fetch: async (slug, query) =>
      query.date === TOMORROW && !query.theaters
        ? new Promise((resolve) => {
            release = resolve
          })
        : response(slug, query),
  })
  t.after(h.close)
  await h.mount()
  h.route.query = { date: TOMORROW }
  await settle()
  h.preferences.activeTheaterIds.value = ['ugc-26']
  await settle()
  assert.equal(h.calls.length, 3)
  release!(response('film-1', { date: TOMORROW }))
  await settle()
  assert.deepEqual(
    h.calls.slice(2).map(({ query }) => query),
    [{ date: TOMORROW }, { date: TOMORROW, theaters: 'ugc-26' }],
  )
})

test('slug changes still load public movie content during failed preference synchronization', async (t) => {
  const h = await harness()
  t.after(h.close)
  await h.mount()
  h.preferences.isInitialized.value = false
  h.preferences.error.value = 'Synchronisation indisponible'
  await settle()
  h.route.params.slug = 'film-2'
  await settle()
  assert.deepEqual(h.calls.slice(2), [
    { slug: 'film-2', query: { date: TODAY } },
  ])
  assert.equal(h.page.schedule.value?.movie.slug, 'film-2')
  assert.deepEqual(h.page.schedule.value?.theaters, [])
  assert.equal(h.page.errorMessage.value, 'Synchronisation indisponible')
  assert.equal(h.page.pending.value, false)
})
