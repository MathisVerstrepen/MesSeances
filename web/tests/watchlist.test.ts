import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import {
  type Component,
  computed,
  createSSRApp,
  effectScope,
  h,
  nextTick,
  readonly,
  ref,
  watch,
} from 'vue'
import type { useWatchlist } from '../app/composables/useWatchlist.ts'
import type { useAccountSession } from '../app/composables/useAccountSession.ts'
import type { AccountSession } from '../app/types/account.ts'
import type {
  AccountWatchlist,
  ImportedWatchlist,
  SaveWatchlist,
  SaveWatchlistSort,
  WatchlistSortOrder,
  WatchlistSearch,
} from '../app/types/watchlist.ts'
import * as errors from '../app/utils/accountState.ts'
import * as dates from '../app/utils/date.ts'
import * as images from '../app/utils/safeImageUrl.ts'
import * as upcoming from '../app/utils/upcomingMovies.ts'
import * as sorting from '../app/utils/watchlistSort.ts'

const require = createRequire(import.meta.url)
const rowSource = await readFile(
  new URL('../app/components/WatchlistMovieRow.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(rowSource)
interface RowModule {
  default?: Component
}
const rowModule: RowModule = {}
runInNewContext(
  ts.transpileModule(
    compileScript(descriptor, {
      id: 'WatchlistMovieRow',
      inlineTemplate: true,
    }).content,
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText,
  {
    exports: rowModule,
    computed,
    require: (id: string) => {
      if (id === '~/utils/date') return dates
      if (id === '~/utils/safeImageUrl') return images
      if (id === '~/utils/upcomingMovies') return upcoming
      return require(id)
    },
  },
)
assert.ok(rowModule.default)
const WatchlistMovieRow = rowModule.default

interface RowDateProps {
  releaseDate?: string | null
  frenchReleaseDate?: string | null
}

async function renderRow(props: RowDateProps) {
  const app = createSSRApp(WatchlistMovieRow, {
    title: 'Film sauvegardé',
    slug: 'film-1',
    ...props,
  })
  app.component('NuxtLink', {
    props: ['to'],
    setup:
      (props, { slots }) =>
      () =>
        h('a', { href: props.to }, slots.default?.()),
  })
  return renderToString(app)
}

test('saved movie row renders a full French theatrical date in semantic time, not the general year', async () => {
  const html = await renderRow({
    frenchReleaseDate: '1998-10-14',
    releaseDate: '1997-07-24',
  })
  assert.match(html, /<time datetime="1998-10-14"[^>]*>14 octobre 1998<\/time>/)
  assert.doesNotMatch(html, /1997/)
  assert.match(html, /href="\/film\/film-1"/)
  assert.match(html, /Film sauvegardé/)
  assert.match(html, /aria-hidden="true"/)
})

test('saved row omits missing, negative and invalid evidence without date placeholder or inferred year', async () => {
  for (const frenchReleaseDate of [
    undefined,
    null,
    '',
    '1998',
    '1998-02-30',
    '2025-02-29',
    '1998-13-14',
    '1998-10-14T00:00:00Z',
    'invalid',
  ]) {
    const html = await renderRow({ frenchReleaseDate })
    assert.doesNotMatch(html, /<time|text-muted|1998|2025|Invalid Date/)
    assert.match(html, /Film sauvegardé/)
  }
  assert.match(
    await renderRow({ frenchReleaseDate: '2000-02-29' }),
    />29 février 2000<\/time>/,
  )
})

test('search rows retain general-date years and saved loop receives no fallback release date', async () => {
  const html = await renderRow({ releaseDate: '1997-07-24' })
  assert.match(html, /<p[^>]*>1997<\/p>/)
  assert.doesNotMatch(html, /<time|juillet/)
  const page = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  const rows = [...page.matchAll(/<WatchlistMovieRow\b[^>]*>/g)].map(
    (match) => match[0],
  )
  assert.equal(rows.length, 3)
  for (const row of rows.slice(0, 2)) {
    assert.match(row, /:release-date="movie.release_date"/)
    assert.doesNotMatch(row, /french-release-date/)
  }
  assert.match(rows[2]!, /:french-release-date="movie.french_release_date"/)
  assert.doesNotMatch(rows[2]!, /\s:release-date=/)
})

function session(username = 'alice'): AccountSession {
  return {
    enabled: true,
    state: 'complete',
    account: {
      username,
      email: `${username}@example.test`,
      has_password: true,
      google_linked: false,
    },
  }
}
function value(
  revision = '1',
  slugs = ['film-1'],
  username = 'alice',
  sortOrder: WatchlistSortOrder = 'added_desc',
): AccountWatchlist {
  return {
    username,
    revision,
    sort_order: sortOrder,
    items: slugs.map((slug) => ({
      slug,
      title: slug,
      release_date: '1997-07-24',
      french_release_date: '1998-10-14',
      added_at: '2026-09-01T00:00:00Z',
    })),
    external_search_available: true,
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
async function settle() {
  await nextTick()
  await new Promise<void>((resolve) => setImmediate(resolve))
}

async function fixture(client = true) {
  const states = new Map()
  const messages: string[] = []
  const app = {
    _accountChannel: {
      postMessage: (message: string) => messages.push(message),
    },
  }
  const posts: SaveWatchlist[] = []
  const sortPosts: SaveWatchlistSort[] = []
  const scopes: ReturnType<typeof effectScope>[] = []
  let response = value()
  let admitted = session()
  let gets = 0
  let read = async () => response
  let write = async (input: SaveWatchlist) => {
    response = value(
      String(BigInt(input.expected_revision) + 1n),
      input.saved === 'true' ? [input.movie_slug] : [],
      input.expected_username,
      response.sort_order,
    )
    return response
  }
  let sort = async (input: SaveWatchlistSort) => {
    response = {
      ...response,
      revision: String(BigInt(input.expected_revision) + 1n),
      sort_order: input.sort_order,
    }
    return response
  }
  let search = async (): Promise<WatchlistSearch> => ({
    username: 'alice',
    catalog: [],
    external: [{ tmdb_id: '12', title: 'External' }],
    external_status: 'ready',
    catalog_has_more: false,
  })
  let imported = async (): Promise<ImportedWatchlist> => ({
    watchlist: value('2', ['film-12']),
    movie_slug: 'film-12',
  })
  const context = {
    ref,
    computed,
    readonly,
    watch,
    AbortController,
    effectScope: () => {
      const scope = effectScope(true)
      scopes.push(scope)
      return scope
    },
    useState: <T>(key: string, init: () => T) => {
      if (!states.has(key)) states.set(key, ref(init()))
      return states.get(key)
    },
    useNuxtApp: () => app,
    useAccountApi: () => ({
      session: async () => admitted,
      watchlist: async () => {
        gets++
        return read()
      },
      saveWatchlist: (input: SaveWatchlist) => {
        posts.push(input)
        return write(input)
      },
      saveWatchlistSort: (input: SaveWatchlistSort) => {
        sortPosts.push(input)
        return sort(input)
      },
      searchWatchlist: () => search(),
      importWatchlist: () => imported(),
    }),
    require: () => errors,
  }
  async function compile<T>(name: string, extra = {}): Promise<T> {
    const source = await readFile(
      new URL(`../app/composables/${name}.ts`, import.meta.url),
      'utf8',
    )
    const exports = {}
    runInNewContext(
      ts.transpileModule(
        source.replaceAll('import.meta.client', String(client)),
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      { ...context, ...extra, exports },
    )
    // SAFETY: Export type matches the compiled module supplied by each caller.
    return exports as T
  }
  const account = (
    await compile<{ useAccountSession: typeof useAccountSession }>(
      'useAccountSession',
    )
  ).useAccountSession()
  const module = await compile<{ useWatchlist: typeof useWatchlist }>(
    'useWatchlist',
    { useAccountSession: () => account },
  )
  const list = module.useWatchlist()
  list.startSynchronization()
  return {
    list,
    account,
    posts,
    sortPosts,
    messages,
    states,
    another: () => module.useWatchlist(),
    get gets() {
      return gets
    },
    setRead: (fn: typeof read) => {
      read = fn
    },
    setWrite: (fn: typeof write) => {
      write = fn
    },
    setSort: (fn: typeof sort) => {
      sort = fn
    },
    setSearch: (fn: typeof search) => {
      search = fn
    },
    setImport: (fn: typeof imported) => {
      imported = fn
    },
    setResponse: (next: AccountWatchlist) => {
      response = next
    },
    admit: (next = session()) => {
      admitted = next
      account.accept(next)
    },
    stop: () => scopes.forEach((scope) => scope.stop()),
  }
}

test('watchlist is app scoped, client-only, absent from serialized state and guest storage', async () => {
  const f = await fixture(false)
  try {
    f.admit()
    await settle()
    assert.equal(f.gets, 0)
    assert.equal(f.another(), f.list)
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.save('film-1', true), false)
    assert.ok([...f.states.keys()].every((key) => !key.includes('watchlist')))
    const source = await readFile(
      new URL('../app/composables/useWatchlist.ts', import.meta.url),
      'utf8',
    )
    assert.doesNotMatch(
      source,
      /useState|useAsyncData|localStorage|sessionStorage/,
    )
  } finally {
    f.stop()
  }
})

test('revalidation acquires, replaces and removes French evidence at equal membership revision in private memory only', async () => {
  const f = await fixture()
  try {
    const initial = value()
    delete initial.items[0]!.french_release_date
    f.setResponse(initial)
    f.admit()
    await settle()
    assert.equal(f.list.items.value[0]?.french_release_date, undefined)
    for (const date of ['1998-10-14', '1998-10-07', undefined]) {
      const updated = value()
      if (date) updated.items[0]!.french_release_date = date
      else delete updated.items[0]!.french_release_date
      f.setResponse(updated)
      await f.account.revalidate()
      assert.equal(f.list.ready.value, true)
      assert.equal(f.list.items.value[0]?.french_release_date, date)
      assert.equal(f.list.items.value[0]?.release_date, '1997-07-24')
      assert.equal(f.list.items.value[0]?.added_at, initial.items[0]?.added_at)
      assert.deepEqual([...f.list.slugs.value], ['film-1'])
      assert.doesNotMatch(
        JSON.stringify([...f.states].map(([key, state]) => [key, state.value])),
        /french_release_date|1998-10|film-1|added_at/,
      )
    }
    assert.equal(f.gets, 4)
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.messages, [])
    // Metadata refresh keeps membership CAS unchanged for the next real write.
    await f.list.save('film-2', true)
    assert.equal(f.posts[0]?.expected_revision, '1')
    assert.deepEqual(f.messages, ['watchlist-changed'])
    f.account.clear()
    assert.equal(f.list.items.value.length, 0)
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner'] as const) {
  test(`late French evidence revalidation is discarded after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<AccountWatchlist>()
      f.setRead(() => pending.promise)
      const refresh = f.account.revalidate()
      await settle()
      f.setRead(async () => value('1', [], 'bob'))
      if (transition === 'logout') f.account.clear()
      else f.admit(session('bob'))
      await settle()
      pending.resolve(value())
      await refresh
      await settle()
      assert.equal(f.list.items.value.length, 0)
      assert.equal(f.list.owner.value, transition === 'logout' ? '' : 'bob')
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

test('committed snapshots alone change saved status and writes are serialized', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const save = f.list.save('film-2', true)
    assert.equal(f.list.saving.value, true)
    assert.equal(f.list.slugs.value.has('film-2'), false)
    assert.equal(await f.list.save('film-3', true), false)
    pending.resolve(value('2', ['film-1', 'film-2']))
    assert.equal(await save, true)
    assert.equal(f.list.slugs.value.has('film-2'), true)
    assert.deepEqual(f.messages, ['watchlist-changed'])
    assert.equal(f.posts.length, 1)
    assert.equal(f.posts[0]?.saved, 'true')
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner', 'same-owner'] as const) {
  test(`late searches cannot resurrect state after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<WatchlistSearch>()
      f.setSearch(() => pending.promise)
      f.list.query.value = 'Private query'
      const search = f.list.search()
      f.setResponse(value('3', [], transition === 'owner' ? 'bob' : 'alice'))
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve({
        username: 'alice',
        catalog: [],
        external: [],
        external_status: 'ready',
        catalog_has_more: false,
      })
      await search
      assert.equal(f.list.query.value, '')
      assert.equal(f.list.searchResults.value, null)
      assert.deepEqual([...f.list.items.value], [])
      assert.equal(f.list.searching.value, false)
    } finally {
      f.stop()
    }
  })

  test(`late initial reads cannot resurrect state after ${transition}`, async () => {
    const f = await fixture()
    try {
      const pending = deferred<AccountWatchlist>()
      f.setRead(() => pending.promise)
      f.admit()
      await settle()
      assert.equal(f.list.loading.value, true)
      f.setRead(async () =>
        value('3', [], transition === 'owner' ? 'bob' : 'alice'),
      )
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve(value('99', ['private-old-film']))
      await settle()
      assert.deepEqual([...f.list.items.value], [])
      assert.equal(f.list.loading.value, false)
      assert.equal(f.list.ready.value, transition !== 'logout')
    } finally {
      f.stop()
    }
  })
}

test('late mutations are discarded across owners and do not unblock a newer write', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const old = deferred<AccountWatchlist>()
    f.setWrite(() => old.promise)
    const first = f.list.save('film-2', true)
    f.setResponse(value('0', [], 'bob'))
    f.admit(session('bob'))
    await settle()
    const next = deferred<AccountWatchlist>()
    f.setWrite(() => next.promise)
    const second = f.list.save('film-3', true)
    old.resolve(value('2', ['film-2']))
    await first
    assert.equal(f.list.saving.value, true)
    assert.deepEqual([...f.list.items.value], [])
    next.resolve(value('1', ['film-3'], 'bob'))
    await second
    assert.deepEqual([...f.list.slugs.value], ['film-3'])
  } finally {
    f.stop()
  }
})

test('conflicts and ambiguous writes reconcile without replay, including committed timeout', async () => {
  for (const code of [
    new errors.AccountApiError(409, 'watchlist_changed'),
    new errors.AccountApiError(),
  ]) {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      f.setWrite(async () => {
        f.setResponse(value('2', ['film-2']))
        throw code
      })
      assert.equal(await f.list.save('film-2', true), false)
      assert.deepEqual([...f.list.slugs.value], ['film-2'])
      assert.equal(f.posts.length, 1)
      assert.equal(f.list.ready.value, true)
      assert.ok(f.list.error.value)
    } finally {
      f.stop()
    }
  }
})

test('failed reconciliation gates results and writes until explicit retry succeeds', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.setWrite(async () => {
      throw new errors.AccountApiError()
    })
    f.setRead(async () => {
      throw new errors.AccountApiError(503, 'watchlist_unavailable')
    })
    await f.list.save('film-2', true)
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.save('film-2', true), false)
    f.setRead(async () => value('2', ['film-2']))
    await f.list.retry()
    assert.equal(f.list.ready.value, true)
    assert.equal(f.posts.length, 1)
  } finally {
    f.stop()
  }
})

test('focus revalidation waits for writes and reads committed state under new session revision', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const save = f.list.save('film-2', true)
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.list.ready.value, false)
    f.setResponse(value('2', ['film-2']))
    pending.resolve(value('2', ['film-2']))
    await save
    await refresh
    assert.deepEqual([...f.list.slugs.value], ['film-2'])
    assert.equal(f.list.writesBlocked.value, false)
  } finally {
    f.stop()
  }
})

test('external import replaces candidate with canonical link only after committed success', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'External'
    await f.list.search()
    const pending = deferred<ImportedWatchlist>()
    f.setImport(() => pending.promise)
    const save = f.list.importMovie('12')
    assert.equal(f.list.searchResults.value?.catalog.length, 0)
    pending.resolve({
      watchlist: value('2', ['film-12']),
      movie_slug: 'film-12',
    })
    assert.equal(await save, 'film-12')
    assert.equal(f.list.searchResults.value?.catalog[0]?.slug, 'film-12')
    assert.equal(f.list.searchResults.value?.external.length, 0)
  } finally {
    f.stop()
  }
})

test('route departure clears query and fences pending search/import result replacement', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<WatchlistSearch>()
    f.setSearch(() => pending.promise)
    f.list.query.value = 'External'
    const search = f.list.search()
    f.list.clearSearch()
    pending.resolve({
      username: 'alice',
      catalog: [],
      external: [{ tmdb_id: '12', title: 'External' }],
      external_status: 'ready',
      catalog_has_more: false,
    })
    await search
    assert.equal(f.list.searchResults.value, null)
    assert.equal(f.list.query.value, '')
  } finally {
    f.stop()
  }
})

test('recoverable search failure preserves query; unavailable provider preserves local results', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'Private query'
    f.setSearch(async () => {
      throw new errors.AccountApiError(503)
    })
    await f.list.search()
    assert.equal(f.list.query.value, 'Private query')
    assert.ok(f.list.searchError.value)
    f.setSearch(async () => ({
      username: 'alice',
      catalog: [{ slug: 'film-1', title: 'Local' }],
      external: [],
      external_status: 'unavailable',
      catalog_has_more: false,
    }))
    await f.list.search()
    assert.equal(f.list.searchResults.value?.catalog.length, 1)
    assert.equal(f.list.searchResults.value?.external_status, 'unavailable')
  } finally {
    f.stop()
  }
})

test('dismissal keeps query but fences pending results without ending a newer search', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const old = deferred<WatchlistSearch>()
    const newer = deferred<WatchlistSearch>()
    f.setSearch(() => old.promise)
    f.list.query.value = 'Private query'
    const first = f.list.search()
    f.list.dismissSearch()
    assert.equal(f.list.query.value, 'Private query')
    assert.equal(f.list.searching.value, false)
    f.setSearch(() => newer.promise)
    const second = f.list.search()
    old.resolve({
      username: 'alice',
      catalog: [],
      external: [],
      external_status: 'ready',
      catalog_has_more: false,
    })
    await first
    assert.equal(f.list.searchResults.value, null)
    assert.equal(f.list.searching.value, true)
    newer.resolve({
      username: 'alice',
      catalog: [{ slug: 'film-2', title: 'New' }],
      external: [],
      external_status: 'ready',
      catalog_has_more: false,
    })
    await second
    assert.equal(f.list.searchResults.value?.catalog[0]?.slug, 'film-2')
  } finally {
    f.stop()
  }
})

class TestSelect {
  value: string
  isConnected = true
  disabled = false
  focusCalls = 0
  constructor(value: string) {
    this.value = value
  }
  focus() {
    this.focusCalls++
  }
}

interface PageInteractions {
  sortedItems: ReturnType<typeof computed<AccountWatchlist['items']>>
  changeSort: (event: { target: unknown }) => Promise<void>
  panelOpen: ReturnType<typeof ref<boolean>>
  activeTab: ReturnType<typeof ref<string>>
  panelHeight: ReturnType<typeof ref<number>>
  submitSearch: () => Promise<void>
  selectTab: (tab: string) => void
  tabKeydown: (event: { key: string; preventDefault: () => void }) => void
  addMovie: (movie: { slug: string } | { tmdb_id: string }) => Promise<void>
  dismiss: (restoreFocus?: boolean) => void
  clearPageSearch: () => void
}

interface PageDocument {
  activeElement: unknown
  body: object
  removeEventListener: () => void
}

// Exercise the page's interaction handlers with the real fenced composable.
async function pageFixture() {
  const f = await fixture()
  f.admit()
  await settle()
  const source = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  const elements = new Map<string, ReturnType<typeof ref>>()
  let focused = ''
  const scope = effectScope()
  const callbacks: (() => void)[] = []
  const pageDocument: PageDocument = {
    activeElement: null,
    body: {},
    removeEventListener: () => {},
  }
  // SAFETY: The transpiled page below exports exactly this interaction contract before use.
  const exports = {} as PageInteractions
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )?.[1]
  assert.ok(script)
  scope.run(() =>
    runInNewContext(
      ts.transpileModule(
        `${script}\nexport { sortedItems, changeSort, panelOpen, activeTab, panelHeight, submitSearch, selectTab, tabKeydown, addMovie, dismiss, clearPageSearch }`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        require: (id: string) =>
          id === '~/utils/watchlistSort' ? sorting : errors,
        computed,
        HTMLSelectElement: TestSelect,
        ref,
        watch,
        nextTick,
        useWatchlist: () => f.list,
        useAccountSession: () => f.account,
        useHead: () => {},
        definePageMeta: () => {},
        onMounted: () => {},
        onBeforeUnmount: (fn: () => void) => callbacks.push(fn),
        onBeforeRouteLeave: () => {},
        useTemplateRef: (name: string) => {
          const element = ref({
            focus: () => {
              focused = name
            },
            scrollTop: 50,
            getBoundingClientRect: () => ({ top: 500 }),
          })
          elements.set(name, element)
          return element
        },
        window: { innerHeight: 844, removeEventListener: () => {} },
        document: pageDocument,
      },
    ),
  )
  return {
    ...f,
    page: exports,
    pageDocument,
    get gets() {
      return f.gets
    },
    elements,
    get focused() {
      return focused
    },
    stop() {
      callbacks.forEach((fn) => fn())
      scope.stop()
      f.stop()
    },
  }
}

test('search panel opens only on submit, bounds height and supports roving tabs', async () => {
  const f = await pageFixture()
  try {
    f.list.query.value = 'External'
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.list.searchResults.value, null)
    await f.page.submitSearch()
    assert.equal(f.page.panelOpen.value, true)
    assert.equal(f.focused, 'catalogTab')
    assert.equal(f.page.panelHeight.value, 328)
    for (const [key, tab] of [
      ['ArrowRight', 'external'],
      ['ArrowRight', 'catalog'],
      ['End', 'external'],
      ['Home', 'catalog'],
      ['ArrowLeft', 'external'],
    ]) {
      let prevented = false
      f.page.tabKeydown({
        key: key!,
        preventDefault: () => {
          prevented = true
        },
      })
      assert.equal(prevented, true)
      assert.equal(f.page.activeTab.value, tab)
      assert.equal(f.focused, `${tab}Tab`)
      assert.equal(f.elements.get('resultsScroll')?.value.scrollTop, 0)
    }
    f.page.dismiss(true)
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.focused, 'searchInput')
    assert.equal(f.list.query.value, 'External')
  } finally {
    f.stop()
  }
})

for (const imported of [false, true]) {
  test(`${imported ? 'external import' : 'catalog save'} dismisses only on confirmed success`, async () => {
    const f = await pageFixture()
    try {
      f.list.query.value = 'External'
      await f.page.submitSearch()
      const candidate = imported ? { tmdb_id: '12' } : { slug: 'film-1' }
      const failed = async () => {
        throw new errors.AccountApiError()
      }
      if (imported) f.setImport(failed)
      else f.setWrite(failed)
      await f.page.addMovie(candidate)
      assert.equal(f.page.panelOpen.value, true)
      assert.equal(f.list.query.value, 'External')
      assert.ok(f.list.searchResults.value)
      assert.ok(f.list.error.value)
      if (imported)
        f.setImport(async () => ({
          watchlist: value('2', ['film-12']),
          movie_slug: 'film-12',
        }))
      else f.setWrite(async () => value('2'))
      await f.page.addMovie(candidate)
      assert.equal(f.page.panelOpen.value, false)
      assert.equal(f.list.query.value, '')
      assert.equal(f.list.searchResults.value, null)
      assert.equal(f.focused, 'searchInput')
      if (!imported) assert.ok(f.posts.every((post) => post.saved === 'true'))
    } finally {
      f.stop()
    }
  })
}

test('late successful add cannot dismiss a newer search; owner invalidation closes panel', async () => {
  const f = await pageFixture()
  try {
    f.list.query.value = 'External'
    await f.page.submitSearch()
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const add = f.page.addMovie({ slug: 'film-1' })
    f.page.dismiss()
    f.list.query.value = 'Another search draft'
    pending.resolve(value('2'))
    await add
    assert.equal(f.list.query.value, 'Another search draft')
    assert.equal(f.focused, 'catalogTab')
    await f.page.submitSearch()
    f.account.clear()
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.list.query.value, '')
    assert.equal(f.list.searchResults.value, null)
  } finally {
    f.stop()
  }
})

test('uncertain committed add preserves search despite read-back membership; unmount fences completion', async () => {
  const f = await pageFixture()
  try {
    f.list.query.value = 'External'
    await f.page.submitSearch()
    f.setWrite(async () => {
      f.setResponse(value('2', ['film-2']))
      throw new errors.AccountApiError()
    })
    await f.page.addMovie({ slug: 'film-2' })
    assert.equal(f.list.slugs.value.has('film-2'), true)
    assert.equal(f.page.panelOpen.value, true)
    assert.equal(f.list.query.value, 'External')
    assert.ok(f.list.searchResults.value)
    assert.ok(f.list.error.value)
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const add = f.page.addMovie({ slug: 'film-3' })
    f.page.clearPageSearch()
    f.list.query.value = 'New page draft'
    pending.resolve(value('3', ['film-3']))
    await add
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.list.query.value, 'New page draft')
    assert.equal(f.focused, 'catalogTab')
  } finally {
    f.stop()
  }
})

test('owner mismatch fails closed and anonymous login never queues a save', async () => {
  const f = await fixture()
  try {
    f.admit({ enabled: true, state: 'anonymous', account: null })
    await settle()
    assert.equal(await f.list.save('film-1', true), false)
    f.setResponse(value('1', ['film-2'], 'bob'))
    f.admit()
    await settle()
    assert.equal(f.account.status.value, 'error')
    assert.equal(f.list.items.value.length, 0)
    assert.equal(f.posts.length, 0)
  } finally {
    f.stop()
  }
})

test('six committed sort choices share owner/revision CAS and preserve search results', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'External'
    await f.list.search()
    const results = JSON.stringify(f.list.searchResults.value)
    for (const [index, option] of sorting.watchlistSortOptions.entries()) {
      assert.equal(await f.list.saveSort(option.value), true)
      assert.equal(f.list.sortOrder.value, option.value)
      assert.deepEqual(
        { ...f.sortPosts[index] },
        {
          expected_username: 'alice',
          expected_revision: String(index + 1),
          sort_order: option.value,
        },
      )
    }
    assert.equal(JSON.stringify(f.list.searchResults.value), results)
    assert.equal(f.list.query.value, 'External')
    assert.equal(f.posts.length, 0)
    assert.equal(f.messages.length, 6)
    assert.ok(f.messages.every((message) => message === 'watchlist-changed'))
    assert.doesNotMatch(
      JSON.stringify([...f.states].map(([key, state]) => [key, state.value])),
      /sort_order|release_asc/,
    )
  } finally {
    f.stop()
  }
})

test('sort serializes membership/import both ways with no optimistic selection or rows', async () => {
  const f = await pageFixture()
  try {
    const next = value('2', ['film-2', 'film-1'], 'alice', 'title_desc')
    const pending = deferred<AccountWatchlist>()
    f.setSort(() => pending.promise)
    const select = new TestSelect('title_desc')
    f.page.changeSort({ target: select })
    assert.equal(select.value, 'added_desc')
    assert.equal(f.list.sortOrder.value, 'added_desc')
    assert.equal(f.list.writesBlocked.value, true)
    assert.deepEqual(
      f.page.sortedItems.value.map((item) => item.slug),
      ['film-1'],
    )
    assert.equal(await f.list.save('film-3', true), false)
    assert.equal(await f.list.importMovie('12'), false)
    assert.equal(await f.list.saveSort('title_asc'), false)
    pending.resolve(next)
    await settle()
    assert.equal(f.list.sortOrder.value, 'title_desc')
    assert.deepEqual(
      f.page.sortedItems.value.map((item) => item.slug),
      ['film-2', 'film-1'],
    )
    for (const operation of ['membership', 'import'] as const) {
      const held = deferred<AccountWatchlist>()
      f.setWrite(() => held.promise)
      f.setImport(async () => ({
        watchlist: await held.promise,
        movie_slug: 'film-12',
      }))
      const write =
        operation === 'membership'
          ? f.list.save('film-12', true)
          : f.list.importMovie('12')
      assert.equal(await f.list.saveSort('title_asc'), false)
      held.resolve(value('3', ['film-12'], 'alice', 'title_desc'))
      await write
      assert.equal(f.list.sortOrder.value, 'title_desc')
    }
    assert.equal(f.sortPosts.length, 1)
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner', 'same-owner'] as const) {
  test(`late sort result is discarded after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<AccountWatchlist>()
      f.setSort(() => pending.promise)
      const save = f.list.saveSort('title_desc')
      f.setResponse(
        value('3', [], transition === 'owner' ? 'bob' : 'alice', 'release_asc'),
      )
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve(value('99', ['old-private-film'], 'alice', 'title_desc'))
      assert.equal(await save, false)
      assert.equal(
        f.list.sortOrder.value,
        transition === 'logout' ? undefined : 'release_asc',
      )
      assert.equal(f.list.items.value.length, 0)
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

test('older snapshots cannot replace selection but equal-revision French evidence reorders saved projection', async () => {
  const f = await pageFixture()
  try {
    const initial = value('5', ['film-1', 'film-2'], 'alice', 'release_asc')
    delete initial.items[0]!.french_release_date
    f.setResponse(initial)
    await f.account.revalidate()
    assert.deepEqual(
      f.page.sortedItems.value.map((item) => item.slug),
      ['film-2', 'film-1'],
    )
    for (const [date, expected] of [
      ['1997-01-01', ['film-1', 'film-2']],
      ['1999-01-01', ['film-2', 'film-1']],
      [undefined, ['film-2', 'film-1']],
    ] as const) {
      const updated = structuredClone(initial)
      updated.items[0]!.french_release_date = date
      f.setResponse(updated)
      await f.account.revalidate()
      assert.deepEqual(
        f.page.sortedItems.value.map((item) => item.slug),
        expected,
      )
    }
    f.setResponse(value('4', [], 'alice', 'title_desc'))
    await f.account.revalidate()
    assert.equal(f.list.sortOrder.value, 'release_asc')
    assert.equal(f.list.items.value.length, 2)
    assert.equal(f.sortPosts.length, 0)
  } finally {
    f.stop()
  }
})

for (const committed of [false, true]) {
  test(`sort failure restores native control and reads authoritative ${committed ? 'committed' : 'unchanged'} choice without replay`, async () => {
    const f = await pageFixture()
    try {
      f.setSort(async () => {
        if (committed)
          f.setResponse(value('2', ['film-1'], 'alice', 'title_desc'))
        throw new errors.AccountApiError(
          committed ? 0 : 409,
          committed ? '' : 'watchlist_changed',
        )
      })
      const select = new TestSelect('title_desc')
      f.page.changeSort({ target: select })
      assert.equal(select.value, 'added_desc')
      await settle()
      assert.equal(
        f.list.sortOrder.value,
        committed ? 'title_desc' : 'added_desc',
      )
      assert.equal(f.list.ready.value, true)
      assert.ok(f.list.error.value)
      assert.equal(f.sortPosts.length, 1)
      assert.equal(f.gets, 2)
    } finally {
      f.stop()
    }
  })
}

test('sort reconciliation failure gates writes until explicit retry, with no replay', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.setSort(async () => {
      throw new errors.AccountApiError()
    })
    f.setRead(async () => {
      throw new errors.AccountApiError(503, 'watchlist_unavailable')
    })
    await f.list.saveSort('title_desc')
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.saveSort('title_desc'), false)
    f.setRead(async () => value('2', ['film-1'], 'alice', 'release_desc'))
    await f.list.retry()
    assert.equal(f.list.sortOrder.value, 'release_desc')
    assert.equal(f.list.ready.value, true)
    assert.equal(f.sortPosts.length, 1)
  } finally {
    f.stop()
  }
})

test('focus revalidation waits for sort and applies other-session authoritative preference', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setSort(() => pending.promise)
    const save = f.list.saveSort('title_desc')
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.gets, 1)
    assert.equal(f.list.writesBlocked.value, true)
    f.setResponse(value('3', ['film-1'], 'alice', 'release_desc'))
    pending.resolve(value('2', ['film-1'], 'alice', 'title_desc'))
    await save
    await refresh
    assert.equal(f.list.sortOrder.value, 'release_desc')
    assert.equal(f.list.writesBlocked.value, false)
  } finally {
    f.stop()
  }
})

test('sort response owner mismatch clears private state instead of applying preference', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.setSort(async () => value('2', [], 'bob', 'title_desc'))
    assert.equal(await f.list.saveSort('title_desc'), false)
    assert.equal(f.list.sortOrder.value, undefined)
    assert.equal(f.list.items.value.length, 0)
    assert.equal(f.account.status.value, 'error')
  } finally {
    f.stop()
  }
})

for (const transition of [
  'same-control',
  'owner',
  'unmount',
  'moved-focus',
] as const) {
  test(`native sort focus restoration respects ${transition}`, async () => {
    const f = await pageFixture()
    try {
      const pending = deferred<AccountWatchlist>()
      f.setSort(() => pending.promise)
      const select = new TestSelect('title_desc')
      f.pageDocument.activeElement = select
      const change = f.page.changeSort({ target: select })
      assert.equal(select.value, 'added_desc')
      // Browser moves focus to body when the select becomes disabled.
      f.pageDocument.activeElement = f.pageDocument.body
      if (transition === 'owner') {
        f.setResponse(value('0', [], 'bob'))
        f.admit(session('bob'))
        await settle()
      } else if (transition === 'unmount') select.isConnected = false
      else if (transition === 'moved-focus') f.pageDocument.activeElement = {}
      pending.resolve(value('2', ['film-1'], 'alice', 'title_desc'))
      await change
      assert.equal(select.focusCalls, transition === 'same-control' ? 1 : 0)
      assert.equal(
        select.value,
        'added_desc',
        'completion never writes native value imperatively',
      )
    } finally {
      f.stop()
    }
  })
}
