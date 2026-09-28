import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, readonly, ref, watch } from 'vue'
import type { useWatchlist } from '../app/composables/useWatchlist.ts'
import type { useAccountSession } from '../app/composables/useAccountSession.ts'
import type { AccountSession } from '../app/types/account.ts'
import type {
  AccountWatchlist,
  ImportedWatchlist,
  SaveWatchlist,
  WatchlistSearch,
} from '../app/types/watchlist.ts'
import * as errors from '../app/utils/accountState.ts'

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
): AccountWatchlist {
  return {
    username,
    revision,
    items: slugs.map((slug) => ({
      slug,
      title: slug,
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
    )
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

interface PageInteractions {
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
  // SAFETY: The transpiled page below exports exactly this interaction contract before use.
  const exports = {} as PageInteractions
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )?.[1]
  assert.ok(script)
  scope.run(() =>
    runInNewContext(
      ts.transpileModule(
        `${script}\nexport { panelOpen, activeTab, panelHeight, submitSearch, selectTab, tabKeydown, addMovie, dismiss, clearPageSearch }`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        require: () => errors,
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
        document: { removeEventListener: () => {} },
      },
    ),
  )
  return {
    ...f,
    page: exports,
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
