import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { computed, effectScope, nextTick, readonly, ref, watch } from 'vue'
import {
  collectPublicPages,
  localMoviePage,
  localUpcomingPage,
  projectTimeline,
  showWatched,
  withoutWatched,
} from '../app/utils/watchedDiscovery.ts'
import { watchedSections } from '../app/types/watched.ts'
import type {
  WatchedPreferences,
  WatchedSnapshot,
  SaveWatched,
  SaveWatchedPreferences,
} from '../app/types/watched.ts'
import type { AccountSession } from '../app/types/account.ts'
import type { AccountWatchlist } from '../app/types/watchlist.ts'
import type {
  MoviesQuery,
  MoviesResponse,
  CatalogMovie,
  TimelineResponse,
  TimelineTheater,
  TimelineShowtime,
  UpcomingMoviesResponse,
} from '../app/types/api.ts'
import type { useWatched } from '../app/composables/useWatched.ts'
import type { useWatchlist } from '../app/composables/useWatchlist.ts'
import type { useAccountSession } from '../app/composables/useAccountSession.ts'
import type { useCollectionWrite } from '../app/composables/useCollectionWrite.ts'
import type { useWatchedCatalog } from '../app/composables/useWatchedCatalog.ts'
import type { useWatchedDiscovery } from '../app/composables/useWatchedDiscovery.ts'
import * as errors from '../app/utils/accountState.ts'
import {
  filterCompatibleShowtimeResults,
  validShowtimeSelectionKeys,
} from '../app/utils/showtimeResults.ts'
import { partitionWatchlistResults } from '../app/utils/watchlistResults.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'

const defaults: WatchedPreferences = {
  default_visibility: 'hide',
  auto_remove_watchlist: false,
  search_visibility: 'inherit',
  planning_visibility: 'inherit',
  films_visibility: 'inherit',
  upcoming_visibility: 'inherit',
  home_visibility: 'inherit',
  cinema_city_visibility: 'inherit',
}
for (const section of watchedSections)
  test(`${section}: explicit override, inherited default and temporary true/false precedence`, () => {
    for (const global of ['hide', 'show'] as const)
      for (const override of ['inherit', 'show', 'hide'] as const) {
        const preferences = {
          ...defaults,
          default_visibility: global,
          [`${section}_visibility`]: override,
        }
        assert.equal(
          showWatched(preferences, section),
          (override === 'inherit' ? global : override) === 'show',
        )
        assert.equal(showWatched(preferences, section, true), true)
        assert.equal(showWatched(preferences, section, false), false)
        for (const other of watchedSections.filter((key) => key !== section))
          assert.equal(showWatched(preferences, other), global === 'show')
      }
  })
const films = (slugs: string[]) =>
  slugs.map(
    (slug): CatalogMovie => ({
      slug,
      title: 'Homonym',
      runtime_minutes: 90,
      poster_url: null,
      original_language: null,
      updated_at: '2026-10-15T12:00:00Z',
      tmdb_id: null,
      imdb_id: null,
      metacritic_id: null,
      overview: null,
      release_date: null,
      french_release_date: null,
      genres: [],
    }),
  )
interface CatalogModule {
  useWatchedCatalog?: typeof useWatchedCatalog
}
interface DiscoveryModule {
  useWatchedDiscovery?: typeof useWatchedDiscovery
}
interface ComposableModules {
  useAccountSession: { useAccountSession: typeof useAccountSession }
  useCollectionWrite: { useCollectionWrite: typeof useCollectionWrite }
  useWatchlist: { useWatchlist: typeof useWatchlist }
  useWatched: { useWatched: typeof useWatched }
}
interface ComposableDependencies {
  useAccountSession?: typeof useAccountSession
  useCollectionWrite?: typeof useCollectionWrite
  useWatchlist?: typeof useWatchlist
}
type ComposableExports = ComposableModules['useAccountSession'] &
  ComposableModules['useCollectionWrite'] &
  ComposableModules['useWatchlist'] &
  ComposableModules['useWatched']
const page = (items: string[], number = 1, total = 5): MoviesResponse => ({
  items: films(items),
  page: number,
  page_size: 2,
  total,
  available_genres: [],
  generated_at: '2026-10-15T12:00:00Z',
  catalog_revision: 'r1',
})
test('canonical identity, not title or poster; stable ordering, local totals and clamp without mutating public snapshots', () => {
  const value = page(['canonical', 'other', 'third', 'fourth', 'last'])
  const before = JSON.stringify(value)
  const hidden = new Set(['canonical', 'last'])
  assert.deepEqual(
    withoutWatched(value.items, hidden, (film) => film.slug).map(
      (film) => film.slug,
    ),
    ['other', 'third', 'fourth'],
  )
  const result = localMoviePage(value, hidden, 9, 2)
  assert.equal(result.total, 3)
  assert.equal(result.page, 2)
  assert.deepEqual(
    result.items.map((film) => film.slug),
    ['fourth'],
  )
  assert.equal(JSON.stringify(value), before)
  assert.equal(
    localMoviePage(value, new Set(value.items.map((item) => item.slug)), 4, 24)
      .page,
    1,
  )
})
for (const section of watchedSections) {
  test(`${section}: discovery ignores metadata, ordering, revision and unrelated preferences, but tracks filtering and scope`, async (t) => {
    const scope = effectScope()
    t.after(() => scope.stop())
    const snapshot = ref<WatchedSnapshot>({
      username: 'alice',
      revision: '1',
      preferences: { ...defaults },
      items: ['first', 'second'].map((slug) => ({
        slug,
        title: slug,
        watched_at: '2026-10-01T00:00:00Z',
      })),
    })
    const watched = {
      owner: ref('alice'),
      ready: ref(true),
      scopeKey: ref(0),
      snapshot,
      slugs: computed(
        () => new Set(snapshot.value.items.map((item) => item.slug)),
      ),
      preferences: computed(() => snapshot.value.preferences),
    }
    const exports: DiscoveryModule = {}
    const mounts: (() => void)[] = []
    const source = await readFile(
      new URL('../app/composables/useWatchedDiscovery.ts', import.meta.url),
      'utf8',
    )
    runInNewContext(
      ts.transpileModule(source, {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      }).outputText,
      {
        exports,
        ref,
        computed,
        watch,
        useWatched: () => watched,
        onMounted: (fn: () => void) => mounts.push(fn),
        require: () => ({ showWatched, withoutWatched }),
      },
    )
    const discovery = scope.run(() => exports.useWatchedDiscovery!(section))!
    const beforeMount = discovery.key.value
    watched.scopeKey.value++
    assert.equal(
      discovery.key.value,
      beforeMount,
      'private scope resets cannot invalidate SSR discovery',
    )
    mounts.forEach((fn) => fn())
    const initial = discovery.key.value
    const unrelated = section === 'home' ? 'films' : 'home'
    snapshot.value = {
      ...snapshot.value,
      revision: '2',
      items: [...snapshot.value.items].reverse().map((item) => ({
        ...item,
        title: 'Refreshed metadata',
        poster_url: '/updated.webp',
      })),
      preferences: {
        ...defaults,
        auto_remove_watchlist: true,
        [`${unrelated}_visibility`]: 'show',
      },
    }
    assert.equal(discovery.key.value, initial)
    assert.deepEqual(
      discovery.filter(['first', 'visible'], (slug) => slug),
      ['visible'],
    )
    watched.preferences.value[`${section}_visibility`] = 'show'
    assert.notEqual(discovery.key.value, initial)
    const showing = discovery.key.value
    snapshot.value.items.push({
      slug: 'third',
      title: 'Third',
      watched_at: '2026-10-02T00:00:00Z',
    })
    assert.equal(
      discovery.key.value,
      showing,
      'membership cannot change an unfiltered catalog',
    )
    discovery.temporary.value = false
    const hiding = discovery.key.value
    snapshot.value.items = snapshot.value.items.filter(
      (item) => item.slug !== 'third',
    )
    assert.notEqual(discovery.key.value, hiding)
    const ready = discovery.key.value
    watched.ready.value = false
    assert.notEqual(discovery.key.value, ready)
    assert.equal(discovery.filter(['visible'], (slug) => slug).length, 0)
    watched.ready.value = true
    watched.scopeKey.value++
    assert.equal(discovery.temporary.value, undefined)
    assert.notEqual(discovery.key.value, ready)
    watched.owner.value = ''
    const anonymous = discovery.key.value
    watched.scopeKey.value++
    watched.ready.value = false
    assert.equal(
      discovery.key.value,
      anonymous,
      'guest session checks cannot reload public discovery',
    )
  })
}

test('scope-local full-query cache reprojects hide/show/membership without refetch and is discarded on owner invalidation', async (t) => {
  const hide = ref(true)
  const scopeKey = ref(0)
  const slugs = ref(new Set(['hidden']))
  const calls: number[] = []
  const exports: CatalogModule = {}
  const source = await readFile(
    new URL('../app/composables/useWatchedCatalog.ts', import.meta.url),
    'utf8',
  )
  const scope = effectScope(true)
  t.after(() => scope.stop())
  const discovery = {
    hide,
    personal: ref(true),
    blocked: ref(false),
    watched: { scopeKey, slugs, refreshCatalog: async () => {} },
  }
  runInNewContext(
    ts.transpileModule(source.replaceAll('import.meta.client', 'true'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    {
      exports,
      watch,
      AbortController,
      onBeforeUnmount() {},
      useWatchedDiscovery: () => discovery,
      useMesSeancesApi: () => ({
        movies: async (query: { page: number }) => {
          calls.push(query.page)
          return {
            ...page(['hidden', 'visible'], query.page, 2),
            page_size: 100,
          }
        },
      }),
      require: () => ({
        collectPublicPages,
        localMoviePage,
        localUpcomingPage,
      }),
    },
  )
  const catalog = scope.run(() => exports.useWatchedCatalog!('films'))!
  const query = { page: 1, page_size: 24 }
  assert.equal((await catalog.movies(query)).total, 1)
  hide.value = false
  assert.equal((await catalog.movies(query)).total, 2)
  hide.value = true
  slugs.value = new Set(['visible'])
  assert.deepEqual(
    (await catalog.movies(query)).items.map((item) => item.slug),
    ['hidden'],
  )
  assert.deepEqual(calls, [1])
  scopeKey.value++
  await catalog.movies(query)
  assert.deepEqual(calls, [1, 1])
})
test('guest scope resets cannot cancel an in-flight public catalog request', async (t) => {
  const scope = effectScope()
  t.after(() => scope.stop())
  const held = deferred<MoviesResponse>()
  const scopeKey = ref(0)
  let signal: AbortSignal | undefined
  const exports: CatalogModule = {}
  const source = await readFile(
    new URL('../app/composables/useWatchedCatalog.ts', import.meta.url),
    'utf8',
  )
  runInNewContext(
    ts.transpileModule(source.replaceAll('import.meta.client', 'true'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    {
      exports,
      watch,
      AbortController,
      onBeforeUnmount() {},
      useWatchedDiscovery: () => ({
        hide: ref(false),
        personal: ref(false),
        blocked: ref(false),
        watched: { scopeKey },
      }),
      useMesSeancesApi: () => ({
        movies: (_query: MoviesQuery, requestSignal: AbortSignal) => {
          signal = requestSignal
          return held.promise
        },
      }),
      require: () => ({
        collectPublicPages,
        localMoviePage,
        localUpcomingPage,
      }),
    },
  )
  const catalog = scope.run(() => exports.useWatchedCatalog!('upcoming'))!
  const pending = catalog.movies({ page: 1, page_size: 24 })
  scopeKey.value++
  held.resolve(page(['visible'], 1, 1))
  await assert.doesNotReject(pending)
  assert.equal(signal?.aborted, false)
})

test('sequential complete walk deduplicates canonical slugs and rejects drift, missing pages and cancellation', async () => {
  const calls: number[] = []
  const value = await collectPublicPages(async (number) => {
    calls.push(number)
    return page(
      number === 1 ? ['a', 'b'] : number === 2 ? ['b', 'c'] : ['d'],
      number,
    )
  }, new AbortController().signal)
  assert.deepEqual(calls, [1, 2, 3])
  assert.deepEqual(
    value.items.map((item) => item.slug),
    ['a', 'b', 'c', 'd'],
  )
  const drifts = [
    { catalog_revision: 'r2' },
    { total: 6 },
    {
      screening_window: {
        from: 'changed',
        through: '2026-10-21',
        as_of: '2026-10-15T12:00:00Z',
        timezone: 'Europe/Paris',
        day_count: 7,
      },
    },
    { available_genres: ['Drama'] },
    { page: 8 },
    { items: [] },
  ] satisfies Partial<MoviesResponse>[]
  for (const drift of drifts) {
    await assert.rejects(
      collectPublicPages(
        async (number) =>
          number === 1
            ? page(['a', 'b'])
            : { ...page(['c'], number), ...drift },
        new AbortController().signal,
      ),
    )
  }
  const controller = new AbortController()
  await assert.rejects(
    collectPublicPages(async (number) => {
      controller.abort()
      return page(['a'], number)
    }, controller.signal),
    /cancelled/,
  )
})
test('six-film refill crosses hidden-only public pages and never uses server totals as visible counts', async () => {
  const collected = await collectPublicPages(
    async (number) => ({
      ...page(
        number === 1
          ? ['h1', 'h2']
          : number === 2
            ? ['h3', 'a']
            : number === 3
              ? ['b', 'c']
              : number === 4
                ? ['d', 'e']
                : ['f', 'h4'],
        number,
        10,
      ),
    }),
    new AbortController().signal,
  )
  const result = localMoviePage(
    collected,
    new Set(['h1', 'h2', 'h3', 'h4']),
    1,
    6,
  )
  assert.deepEqual(
    result.items.map((item) => item.slug),
    ['a', 'b', 'c', 'd', 'e', 'f'],
  )
  assert.equal(result.total, 6)
})
for (const view of ['upcoming', 'history'] as const)
  test(`${view}: filter whole weeks, Wednesday-Tuesday boundaries, four nonempty weeks, empty totals`, () => {
    const dates = [
      '2026-10-14',
      '2026-10-20',
      '2026-10-21',
      '2026-10-28',
      '2026-11-04',
      '2026-11-11',
      '2026-11-18',
    ]
    const period =
      view === 'history'
        ? { view, window: null, year: 2026, month: null }
        : {
            view,
            window: { from: '2026-10-14', through: '2027-10-13' },
            year: null,
            month: null,
          }
    const value = {
      ...page(dates.map((_, index) => `f${index}`)),
      items: films(dates.map((_, index) => `f${index}`)).map((film, index) => ({
        ...film,
        french_release_date: dates[index]!,
      })),
      ...period,
      timezone: 'Europe/Paris',
      total_weeks: 6,
      total_pages: 2,
      available_years: [2026],
      available_months: [10, 11],
    } satisfies UpcomingMoviesResponse
    const result = localUpcomingPage(value, new Set(['f2']), 1)
    assert.equal(result.total, 6)
    assert.equal(result.total_weeks, 5)
    assert.equal(result.total_pages, 2)
    assert.equal(localUpcomingPage(value, new Set(['f2']), 9).page, 2)
    assert.equal(
      localUpcomingPage(value, new Set(value.items.map((item) => item.slug)), 1)
        .total_pages,
      0,
    )
    assert.deepEqual(result.available_years, [2026])
  })
test('timeline filters showtimes before either row projection, drops empty theaters and keeps public window', () => {
  const showtime = (slug: string): TimelineShowtime => ({
    provider: 'ugc',
    id: `session-${slug}`,
    movie: films([slug])[0]!,
    start_time: '2026-10-15T14:00:00Z',
    end_time: '2026-10-15T16:00:00Z',
    estimated_end_time: null,
    estimated_end_ads_minutes: null,
    language: 'VF',
    format: '2D',
    room: '',
    booking_url: null,
    start_offset_minutes: 360,
    duration_minutes: 120,
    poster_url: null,
    backdrop_url: null,
  })
  const value = {
    date: '2026-10-15',
    timezone: 'Europe/Paris',
    window_start_time: 'start',
    window_end_time: 'end',
    theaters: [
      { id: 'a', slugs: ['hidden'] },
      { id: 'b', slugs: ['visible', 'hidden'] },
    ].map(
      (row): TimelineTheater => ({
        provider: 'ugc',
        id: row.id,
        slug: row.id,
        name: row.id,
        city: 'Lille',
        accepted_passes: [],
        showtimes: row.slugs.map(showtime),
      }),
    ),
  } satisfies TimelineResponse
  const projected = projectTimeline(value, new Set(['hidden']))
  assert.equal(projected.theaters.length, 1)
  assert.equal(projected.theaters[0]!.showtimes.length, 1)
  assert.equal(projected.window_start_time, 'start')
  assert.equal(value.theaters.length, 2)
})
test('search hides before compatibility and watchlist-only partition, ignoring hidden selected constraints without mutating shared selection', () => {
  const raw = ['hidden', 'visible'].map(
    (slug, index): ShowtimeResultViewModel => ({
      key: `key${index}`,
      movieSlug: slug,
      effectiveStartTime: '2026-10-15T14:00:00Z',
      end: { time: '2026-10-15T16:00:00Z', estimated: false, adsMinutes: null },
      showtimeId: `session-${index}`,
      provider: 'ugc',
      movieKey: slug,
      movieTitle: slug,
      movieOriginalLanguage: null,
      movieRuntimeMinutes: 120,
      theaterId: 'theater',
      theaterName: 'Cinéma',
      advertisedStartTime: '2026-10-15T14:00:00Z',
      language: 'VF',
      format: '2D',
      room: '',
      bookingUrl: null,
      posterUrl: null,
      backdropUrl: null,
    }),
  )
  const routeSelection = ['key0']
  const policy = withoutWatched(
    raw,
    new Set(['hidden']),
    (item) => item.movieSlug,
  )
  const selected = validShowtimeSelectionKeys(policy, routeSelection)
  const compatible = filterCompatibleShowtimeResults(policy, selected)
  assert.deepEqual(selected, [])
  assert.deepEqual(
    partitionWatchlistResults(compatible, new Set(['visible', 'hidden']), true)
      .flatMap((section) => section.results)
      .map((item) => item.movieSlug),
    ['visible'],
  )
  assert.deepEqual(validShowtimeSelectionKeys(raw, routeSelection), ['key0'])
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}
async function settle() {
  await nextTick()
  await new Promise<void>((resolve) => setImmediate(resolve))
}
async function fixture() {
  const app = {
    _accountChannel: {
      postMessage: (message: string) => messages.push(message),
    },
  }
  const messages: string[] = []
  const states = {
    'account-session': ref<AccountSession | null>(null),
    'account-session-status': ref<'idle' | 'loading' | 'ready' | 'error'>(
      'idle',
    ),
    'account-session-error': ref(''),
    'account-session-request': ref(0),
    'account-revalidating': ref(false),
  }
  const scopes: ReturnType<typeof effectScope>[] = []
  let watched: WatchedSnapshot = {
    username: 'alice',
    revision: '0',
    items: [],
    preferences: { ...defaults },
  }
  let saved: AccountWatchlist = {
    username: 'alice',
    revision: '1',
    items: [
      {
        slug: 'film-1',
        title: 'Film',
        added_at: '2026-10-15T12:00:00Z',
        tag_ids: ['1'],
      },
    ],
    tags: [{ id: '1', name: 'Private', color: 'blue' }],
    sort_order: 'title_asc',
    view_mode: 'tags',
    filter_tag_id: '1',
    external_search_available: false,
  }
  const posts: SaveWatched[] = []
  let gets = 0
  let savedGets = 0
  let read = async () => watched
  let write = async (input: SaveWatched) => {
    watched = {
      ...watched,
      revision: String(BigInt(watched.revision) + 1n),
      items:
        input.watched === 'true'
          ? [
              {
                slug: input.movie_slug,
                title: 'Film',
                watched_at: '2026-10-15T12:00:00Z',
              },
            ]
          : [],
    }
    if (watched.preferences.auto_remove_watchlist && input.watched === 'true')
      saved = { ...saved, revision: '2', items: [] }
    return {
      watched,
      watchlist:
        watched.preferences.auto_remove_watchlist && input.watched === 'true'
          ? saved
          : null,
    }
  }
  const api = {
    session: async () =>
      ({
        enabled: true,
        state: 'complete',
        account: {
          username: watched.username,
          email: 'fixture@example.test',
          has_password: true,
          google_linked: false,
        },
      }) satisfies AccountSession,
    watched: () => {
      gets++
      return read()
    },
    watchlist: async () => {
      savedGets++
      return saved
    },
    saveWatched: (input: SaveWatched) => {
      posts.push(input)
      return write(input)
    },
    saveWatchedPreferences: async (input: SaveWatchedPreferences) => {
      const {
        expected_username: _owner,
        expected_revision: _revision,
        ...preferences
      } = input
      watched = {
        ...watched,
        revision: String(BigInt(watched.revision) + 1n),
        preferences: {
          ...preferences,
          auto_remove_watchlist: input.auto_remove_watchlist === 'true',
        },
      }
      return watched
    },
    saveWatchlist: async () => saved,
  }
  const context = {
    ref,
    computed,
    readonly,
    watch,
    AbortController,
    useNuxtApp: () => app,
    useAccountApi: () => api,
    useState: (key: keyof typeof states) => states[key],
    effectScope: () => {
      const scope = effectScope(true)
      scopes.push(scope)
      return scope
    },
    require: () => errors,
  }
  function load(
    name: 'useAccountSession',
    dependencies?: ComposableDependencies,
  ): Promise<ComposableModules['useAccountSession']>
  function load(
    name: 'useCollectionWrite',
    dependencies?: ComposableDependencies,
  ): Promise<ComposableModules['useCollectionWrite']>
  function load(
    name: 'useWatchlist',
    dependencies?: ComposableDependencies,
  ): Promise<ComposableModules['useWatchlist']>
  function load(
    name: 'useWatched',
    dependencies?: ComposableDependencies,
  ): Promise<ComposableModules['useWatched']>
  async function load(
    name: keyof ComposableModules,
    dependencies: ComposableDependencies = {},
  ): Promise<ComposableModules[keyof ComposableModules]> {
    const exports: Partial<ComposableExports> = {}
    const source = await readFile(
      new URL(`../app/composables/${name}.ts`, import.meta.url),
      'utf8',
    )
    runInNewContext(
      ts.transpileModule(source.replaceAll('import.meta.client', 'true'), {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      }).outputText,
      { ...context, ...dependencies, exports },
    )
    switch (name) {
      case 'useAccountSession':
        assert.ok(exports.useAccountSession)
        return { useAccountSession: exports.useAccountSession }
      case 'useCollectionWrite':
        assert.ok(exports.useCollectionWrite)
        return { useCollectionWrite: exports.useCollectionWrite }
      case 'useWatchlist':
        assert.ok(exports.useWatchlist)
        return { useWatchlist: exports.useWatchlist }
      case 'useWatched':
        assert.ok(exports.useWatched)
        return { useWatched: exports.useWatched }
    }
  }
  const account = (await load('useAccountSession')).useAccountSession()
  const { useCollectionWrite: collectionWrite } =
    await load('useCollectionWrite')
  const dependencies = {
    useAccountSession: () => account,
    useCollectionWrite: collectionWrite,
  }
  const watchlist = (await load('useWatchlist', dependencies)).useWatchlist()
  const collection = (
    await load('useWatched', { ...dependencies, useWatchlist: () => watchlist })
  ).useWatched()
  watchlist.startSynchronization()
  collection.startSynchronization()
  account.accept(await api.session())
  await settle()
  return {
    account,
    watchlist,
    collection,
    posts,
    messages,
    api,
    get gets() {
      return gets
    },
    get savedGets() {
      return savedGets
    },
    setRead(fn: typeof read) {
      read = fn
    },
    setWrite(fn: typeof write) {
      write = fn
    },
    setWatched(value: WatchedSnapshot) {
      watched = value
    },
    setSaved(value: AccountWatchlist) {
      saved = value
    },
    switchOwner(username: string) {
      watched = { ...watched, username }
      saved = { ...saved, username }
    },
    stop() {
      for (const scope of scopes) scope.stop()
    },
  }
}
test('committed-only membership and conditional pair commit preserve unrelated tags/preferences, then allow explicit watchlist saves', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  assert.equal(f.collection.ready.value, true)
  const oldTags = JSON.stringify(f.watchlist.tags.value)
  await f.collection.save('film-1', true)
  assert.equal(f.watchlist.items.value.length, 1)
  await f.collection.savePreferences({
    ...defaults,
    auto_remove_watchlist: true,
  })
  assert.equal(f.watchlist.items.value.length, 1)
  await f.collection.save('film-1', false)
  const wait = deferred<Awaited<ReturnType<typeof f.api.saveWatched>>>()
  f.setWrite(() => wait.promise)
  const saving = f.collection.save('film-1', true)
  assert.equal(f.collection.slugs.value.has('film-1'), false)
  assert.equal(f.watchlist.writesBlocked.value, true)
  assert.equal(await f.watchlist.save('film-2', true), false)
  const next = {
    ...f.collection.snapshot.value!,
    revision: '4',
    items: [{ slug: 'film-1', title: 'Film', watched_at: 'now' }],
  }
  wait.resolve({
    watched: next,
    watchlist: { ...f.watchlist.snapshot.value!, revision: '2', items: [] },
  })
  assert.equal(await saving, true)
  assert.equal(f.watchlist.items.value.length, 0)
  assert.equal(JSON.stringify(f.watchlist.tags.value), oldTags)
  assert.equal(f.watchlist.viewMode.value, 'tags')
  assert.equal(f.watchlist.filterTagId.value, '1')
  assert.equal(await f.watchlist.save('film-1', true), true)
  assert.equal(f.posts.at(-1)?.expected_watchlist_revision, '1')
  assert.ok(
    f.messages.every((message) =>
      ['watched-changed', 'watchlist-changed'].includes(message),
    ),
  )
})
for (const code of [
  'watched_changed',
  'watchlist_changed',
  'watched_unavailable',
])
  test(`${code}: reconcile both snapshots, no replay, require explicit next submit`, async (t) => {
    const f = await fixture()
    t.after(f.stop)
    const before = [f.gets, f.savedGets]
    f.setWrite(async () => {
      throw new errors.AccountApiError(
        code.endsWith('unavailable') ? 503 : 409,
        code,
      )
    })
    assert.equal(await f.collection.save('film-1', true), false)
    assert.equal(f.posts.length, 1)
    assert.equal(f.gets, before[0]! + 1)
    assert.equal(f.savedGets, before[1]! + 1)
    assert.ok(f.collection.error.value)
    assert.equal(f.collection.slugs.value.size, 0)
  })
test('equal revision refreshes canonical metadata; read error remains unknown and revalidation blocks writes', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  f.setWatched({
    ...f.collection.snapshot.value!,
    items: [{ slug: 'canonical', title: 'Updated', watched_at: 'now' }],
  })
  await f.account.revalidate()
  assert.equal(f.collection.slugs.value.has('canonical'), true)
  const wait = deferred<WatchedSnapshot>()
  f.setRead(() => wait.promise)
  const revalidating = f.account.revalidate()
  await settle()
  assert.equal(f.collection.writesBlocked.value, true)
  assert.equal(await f.collection.save('other', true), false)
  wait.resolve({ ...f.collection.snapshot.value! })
  await revalidating
  f.setRead(async () => {
    throw new errors.AccountApiError(503, 'watched_unavailable')
  })
  await f.account.revalidate()
  assert.equal(f.collection.ready.value, false)
  assert.equal(f.collection.writesBlocked.value, true)
})
test('session revalidation waits the shared write lane before reading either member of an atomic watched/watchlist pair', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  await f.collection.savePreferences({
    ...defaults,
    auto_remove_watchlist: true,
  })
  const held = deferred<Awaited<ReturnType<typeof f.api.saveWatched>>>()
  f.setWrite(() => held.promise)
  const writing = f.collection.save('film-1', true)
  const before = [f.gets, f.savedGets]
  const revalidating = f.account.revalidate()
  await settle()
  assert.deepEqual([f.gets, f.savedGets], before)
  const watched = {
    ...f.collection.snapshot.value!,
    revision: '2',
    items: [{ slug: 'film-1', title: 'Film', watched_at: 'now' }],
  }
  const saved = { ...f.watchlist.snapshot.value!, revision: '2', items: [] }
  f.setWatched(watched)
  f.setSaved(saved)
  held.resolve({ watched, watchlist: saved })
  await writing
  await revalidating
  assert.equal(f.collection.slugs.value.has('film-1'), true)
  assert.equal(f.watchlist.items.value.length, 0)
  assert.equal(f.collection.ready.value, true)
  assert.equal(f.watchlist.ready.value, true)
})
test('logout/owner replacement fences delayed reads', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  const wait = deferred<WatchedSnapshot>()
  f.setRead(() => wait.promise)
  const reading = f.collection.refreshCatalog()
  f.account.clear()
  assert.equal(f.collection.snapshot.value, null)
  wait.resolve({
    username: 'alice',
    revision: '99',
    items: [{ slug: 'secret', title: 'Secret', watched_at: 'now' }],
    preferences: defaults,
  })
  await reading
  assert.equal(f.collection.snapshot.value, null)
  f.setRead(async () => ({
    username: 'bob',
    revision: '0',
    items: [],
    preferences: defaults,
  }))
  f.switchOwner('bob')
  f.account.accept({
    enabled: true,
    state: 'complete',
    account: {
      username: 'bob',
      email: 'bob@example.test',
      has_password: true,
      google_linked: false,
    },
  })
  await settle()
  assert.equal(f.collection.owner.value, 'bob')
  assert.equal(f.collection.slugs.value.size, 0)
})
test('old pair response cannot enter a replacement owner or unlock its active write lease', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  const old = deferred<Awaited<ReturnType<typeof f.api.saveWatched>>>()
  const alice = {
    ...f.collection.snapshot.value!,
    revision: '90',
    items: [{ slug: 'secret', title: 'Secret', watched_at: 'now' }],
  }
  f.setWrite(() => old.promise)
  const oldWrite = f.collection.save('secret', true)
  f.account.clear()
  f.switchOwner('bob')
  f.account.accept(await f.api.session())
  await settle()
  const current = deferred<Awaited<ReturnType<typeof f.api.saveWatched>>>()
  f.setWrite(() => current.promise)
  const currentWrite = f.collection.save('other', true)
  old.resolve({
    watched: alice,
    watchlist: {
      ...f.watchlist.snapshot.value!,
      username: 'alice',
      revision: '90',
      items: [],
    },
  })
  assert.equal(await oldWrite, false)
  assert.equal(f.collection.snapshot.value!.username, 'bob')
  assert.equal(f.collection.slugs.value.has('secret'), false)
  assert.equal(f.collection.writesBlocked.value, true)
  assert.equal(f.watchlist.writesBlocked.value, true)
  current.resolve({
    watched: {
      ...f.collection.snapshot.value!,
      revision: '1',
      items: [{ slug: 'other', title: 'Other', watched_at: 'now' }],
    },
    watchlist: null,
  })
  assert.equal(await currentWrite, true)
  assert.deepEqual([...f.collection.slugs.value], ['other'])
})
