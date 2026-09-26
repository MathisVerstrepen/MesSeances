import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, readonly, ref, watch } from 'vue'
import type { useCinemaPreferences } from '../app/composables/useCinemaPreferences.ts'
import type { useAccountSession } from '../app/composables/useAccountSession.ts'
import type {
  AccountSession,
  AccountTheaterPreferences,
  SaveAccountTheaterPreferences,
} from '../app/types/account.ts'
import * as errors from '../app/utils/accountState.ts'
import * as sharedSelection from '../app/utils/sharedTheaterSelection.ts'
import type { usePageCinemaSelection } from '../app/composables/usePageCinemaSelection.ts'

const catalog = [{ id: 'ugc-1' }, { id: 'ugc-2' }, { id: 'ugc-3' }]
const anonymous: AccountSession = {
  enabled: true,
  state: 'anonymous',
  account: null,
}
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
  theater_ids = ['ugc-2'],
  username = 'alice',
): AccountTheaterPreferences {
  return { username, revision, theater_ids }
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
const settle = async () => {
  await nextTick()
  await new Promise<void>((resolve) => setImmediate(resolve))
}

interface TestApp {
  _accountChannel: { postMessage: (value: string) => void }
  _accountRevalidation?: { details: Set<unknown> }
}

async function fixture(
  options: {
    stored?: string
    client?: boolean
    storageFails?: boolean
    admission?: AccountSession
  } = {},
) {
  const states = new Map()
  const app: TestApp = {
    _accountChannel: { postMessage: (message) => messages.push(message) },
  }
  const messages: string[] = []
  const storageWrites: string[] = []
  const posts: SaveAccountTheaterPreferences[] = []
  const theaterQueries: ({ city: string } | undefined)[] = []
  let gets = 0
  let sessionGets = 0
  let response = value()
  let admitted = options.admission ?? session()
  let read = async () => response
  let readTheaters = async (query?: { city: string }) =>
    query ? [catalog[0]!] : catalog
  let write = async (input: SaveAccountTheaterPreferences) => {
    response = value(
      String(BigInt(input.expected_revision) + 1n),
      input.theater_ids ? input.theater_ids.split(',') : [],
      input.expected_username,
    )
    return response
  }
  const scopes: ReturnType<typeof effectScope>[] = []
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
    useMesSeancesApi: () => ({
      theaters: async (query?: { city: string }) => {
        theaterQueries.push(query)
        return readTheaters(query)
      },
    }),
    useAccountApi: () => ({
      session: async () => {
        sessionGets++
        return { ...admitted }
      },
      theaterPreferences: async () => {
        gets++
        return read()
      },
      saveTheaterPreferences: async (input: SaveAccountTheaterPreferences) => {
        posts.push(input)
        return write(input)
      },
    }),
    getFrenchApiError: () => 'Catalogue indisponible',
    localStorage: {
      getItem: () => {
        if (options.storageFails) throw new Error('blocked')
        return options.stored ?? null
      },
      setItem: (_key: string, raw: string) => {
        if (options.storageFails) throw new Error('blocked')
        storageWrites.push(raw)
      },
    },
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
        source.replaceAll(
          'import.meta.client',
          String(options.client !== false),
        ),
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      { ...context, ...extra, exports },
    )
    // SAFETY: Each call supplies the matching exported composable type.
    return exports as T
  }
  const accountModule = await compile<{
    useAccountSession: typeof useAccountSession
  }>('useAccountSession')
  const account = accountModule.useAccountSession()
  const module = await compile<{
    useCinemaPreferences: typeof useCinemaPreferences
  }>('useCinemaPreferences', { useAccountSession: () => account })
  const preferences = module.useCinemaPreferences()
  preferences.startSynchronization()
  return {
    preferences,
    compile,
    account,
    posts,
    messages,
    storageWrites,
    states,
    another: () => module.useCinemaPreferences(),
    get gets() {
      return gets
    },
    get sessionGets() {
      return sessionGets
    },
    get parisRequests() {
      return theaterQueries.filter((query) => query?.city === 'Paris').length
    },
    get catalogRequests() {
      return theaterQueries.filter((query) => !query).length
    },
    get subscriptions() {
      return app._accountRevalidation?.details.size
    },
    setRead: (fn: typeof read) => {
      read = fn
    },
    setTheaters: (fn: typeof readTheaters) => {
      readTheaters = fn
    },
    setWrite: (fn: typeof write) => {
      write = fn
    },
    setResponse: (next: AccountTheaterPreferences) => {
      response = next
    },
    admit: (next = admitted) => {
      admitted = next
      account.accept(next)
    },
    stop: () => scopes.forEach((scope) => scope.stop()),
  }
}

async function focusPlugin(f: Awaited<ReturnType<typeof fixture>>) {
  const listeners = new Map<string, () => void>()
  const target = {
    addEventListener: (name: string, callback: () => void) =>
      listeners.set(name, callback),
  }
  await f.compile('../plugins/account-session.client', {
    useAccountSession: () => f.account,
    useCinemaPreferences: () => f.preferences,
    defineNuxtPlugin: (
      plugin: (app: {
        hook: (name: string, callback: () => void) => void
      }) => void,
    ) => plugin({ hook: (_name: string, callback: () => void) => callback() }),
    window: target,
    document: { ...target, visibilityState: 'visible' },
  })
  return () => {
    listeners.get('focus')!()
    // Browser return can emit all three events; the session read stays single-flight.
    listeners.get('visibilitychange')!()
    listeners.get('online')!()
    return f.account.revalidate()
  }
}

async function selectionWatcher(
  path: string,
  preferences: ReturnType<typeof usePageCinemaSelection>,
) {
  const source = await readFile(
    new URL(`../app/pages/${path}.vue`, import.meta.url),
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
  const watchers = parsed.statements.filter(
    (node) =>
      ts.isExpressionStatement(node) &&
      ts.isCallExpression(node.expression) &&
      node.expression.expression.getText(parsed) === 'watch' &&
      node.expression.arguments[0]
        ?.getText(parsed)
        .includes('selectionScopeKey'),
  )
  assert.equal(
    watchers.length,
    path === 'film/[slug]' ? 2 : 1,
    `${path}: find every actual selection watcher`,
  )
  let invalidations = 0
  let loads = 0
  const callbackCounts: number[] = []
  const flushModes: string[] = []
  const reload = () => {
    loads++
  }
  const content = ref<unknown>({
    loaded: true,
    currently_screened: true,
    theaters: ['ugc-2'],
  })
  const draft = ref([...preferences.favoriteTheaterIds.value])
  const status = ref('Sélection enregistrée.')
  const bindings = {
    preferences,
    ...preferences,
    preferencesError: preferences.error,
    watch: (
      sources: Parameters<typeof watch>[0],
      callback: () => void,
      options: Parameters<typeof watch>[2],
    ) => {
      const index = callbackCounts.length
      callbackCounts.push(0)
      flushModes.push(options?.flush ?? 'pre')
      return watch(
        sources,
        () => {
          invalidations++
          callbackCounts[index] = callbackCounts[index]! + 1
          callback()
        },
        options,
      )
    },
    isMounted: true,
    isInitializing: false,
    isReady: true,
    isResolvingInitialSearch: ref(false),
    requestId: 0,
    lastLoadKey: 'loaded',
    lastTimelineKey: 'loaded',
    lastSearchKey: 'loaded',
    lastScheduleKey: 'loaded',
    catalog: content,
    timeline: content,
    results: content,
    schedule: content,
    appliedFilters: ref({ allTheaters: false }),
    pending: ref(false),
    notFound: ref(false),
    errorMessage: ref(''),
    route: { query: {} },
    OWNED_QUERY_KEYS: ['q'],
    draftTheaterIds: draft,
    draftFavoriteTheaterIds: draft,
    statusMessage: status,
    theaterValidationMessage: ref(''),
    loadMovies: reload,
    loadTimeline: reload,
    applyRoute: reload,
  }
  const code = ts.transpileModule(
    watchers.map((watcher) => watcher.getFullText(parsed)).join('\n'),
    { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
  ).outputText
  const scope = effectScope()
  scope.run(() =>
    new Function(...Object.keys(bindings), code)(...Object.values(bindings)),
  )
  if (path === 'film/[slug]')
    assert.deepEqual(
      flushModes,
      ['sync', 'pre'],
      'film privacy is synchronous; admission is batched',
    )
  return {
    content,
    draft,
    status,
    get invalidations() {
      return invalidations
    },
    get loads() {
      return loads
    },
    get callbackCounts() {
      return [...callbackCounts]
    },
    stop: () => scope.stop(),
  }
}

test('repeated unchanged focus preserves selection identity while still reading session and account theaters', async (t) => {
  for (const snapshot of [value(), value('2', []), value('0', [])]) {
    const f = await fixture()
    t.after(f.stop)
    f.setResponse(snapshot)
    f.admit()
    await f.preferences.initialize()
    await settle()
    const focus = await focusPlugin(f)
    const ids = f.preferences.favoriteTheaterIds.value
    const theaters = f.preferences.favoriteTheaters.value
    const scope = f.preferences.selectionScopeKey.value
    const gets = f.gets
    for (let attempt = 1; attempt <= 3; attempt++) {
      // Each HTTP response has fresh object and array identities.
      f.setResponse({ ...snapshot, theater_ids: [...snapshot.theater_ids] })
      await focus()
      assert.equal(f.sessionGets, attempt)
      assert.equal(f.gets, gets + attempt)
      assert.equal(f.preferences.favoriteTheaterIds.value, ids)
      assert.equal(f.preferences.favoriteTheaters.value, theaters)
      assert.equal(f.preferences.selectionScopeKey.value, scope)
      assert.equal(f.preferences.isInitialized.value, true)
      assert.equal(f.preferences.isLoading.value, false)
      assert.equal(f.preferences.writesBlocked.value, false)
    }
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.messages, [])
    assert.deepEqual(f.storageWrites, [])
  }
})

test('equal snapshots recover transient read and uncertain save errors without selection churn', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  f.admit()
  await f.preferences.initialize()
  const ids = f.preferences.favoriteTheaterIds.value
  f.setRead(async () => {
    throw new errors.AccountApiError(503)
  })
  await f.account.revalidate()
  assert.equal(f.preferences.writesBlocked.value, true)
  assert.ok(f.preferences.syncError.value)
  assert.equal(f.preferences.favoriteTheaterIds.value, ids)
  const pending = deferred<AccountTheaterPreferences>()
  f.setRead(() => pending.promise)
  const retry = f.preferences.retrySynchronization()
  await settle()
  assert.equal(f.preferences.writesBlocked.value, true)
  assert.equal(f.preferences.isInitialized.value, true)
  assert.equal(f.preferences.favoriteTheaterIds.value, ids)
  pending.resolve(value())
  await retry
  assert.equal(f.preferences.writesBlocked.value, false)
  assert.equal(f.preferences.syncError.value, null)
  assert.equal(f.preferences.favoriteTheaterIds.value, ids)

  f.setRead(async () => value())
  f.setWrite(async () => {
    throw new errors.AccountApiError(503)
  })
  assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-3']), false)
  assert.ok(f.preferences.syncError.value)
  assert.equal(f.preferences.favoriteTheaterIds.value, ids)
  await f.preferences.retrySynchronization()
  assert.equal(f.preferences.syncError.value, null)
  assert.equal(f.preferences.writesBlocked.value, false)
  assert.equal(f.preferences.favoriteTheaterIds.value, ids)
  assert.equal(f.posts.length, 1)
  assert.deepEqual(f.storageWrites, [])
})

test('same-revision changed IDs and newer revisions still apply; older revisions cannot roll back', async (t) => {
  const f = await fixture()
  t.after(f.stop)
  f.admit()
  await f.preferences.initialize()
  for (const next of [
    value('1', ['ugc-3']),
    value('1', ['ugc-1', 'ugc-3']),
    value('2', ['ugc-1', 'ugc-3']),
    value('3', []),
  ]) {
    const previous = f.preferences.favoriteTheaterIds.value
    f.setResponse(next)
    await f.account.revalidate()
    assert.notEqual(f.preferences.favoriteTheaterIds.value, previous)
    assert.deepEqual(
      [...f.preferences.favoriteTheaterIds.value],
      next.theater_ids,
    )
  }
  const ids = f.preferences.favoriteTheaterIds.value
  f.setResponse(value('2', ['ugc-2']))
  await f.account.revalidate()
  assert.equal(f.preferences.favoriteTheaterIds.value, ids)
  assert.deepEqual([...ids], [])
})

test('actual page selection watchers keep content on unchanged focus and invalidate real updates and identity transitions', async (t) => {
  for (const path of [
    'films/index',
    'planning',
    'recherche',
    'film/[slug]',
    'cinemas',
  ]) {
    const f = await fixture({ stored: '["ugc-1"]' })
    t.after(f.stop)
    f.admit()
    await f.preferences.initialize()
    const module = await f.compile<{
      usePageCinemaSelection: typeof usePageCinemaSelection
    }>('usePageCinemaSelection', {
      useCinemaPreferences: () => f.preferences,
      useRoute: () => ({ query: {} }),
      require: () => sharedSelection,
    })
    const watcher = await selectionWatcher(
      path,
      module.usePageCinemaSelection(),
    )
    t.after(watcher.stop)
    const initial = watcher.invalidations
    const initialCallbacks = watcher.callbackCounts
    const loads = watcher.loads
    const content = watcher.content.value
    const draft = watcher.draft.value
    const status = watcher.status.value
    const focus = await focusPlugin(f)
    for (let attempt = 0; attempt < 2; attempt++) {
      f.setResponse(value())
      await focus()
      assert.equal(watcher.invalidations, initial, path)
      assert.deepEqual(watcher.callbackCounts, initialCallbacks, path)
      assert.equal(watcher.loads, loads, path)
      assert.equal(watcher.content.value, content, path)
      assert.equal(watcher.draft.value, draft, path)
      assert.equal(watcher.status.value, status, path)
    }
    f.setResponse(value('2', ['ugc-3']))
    await focus()
    assert.ok(watcher.invalidations > initial, path)
    if (path === 'cinemas')
      assert.deepEqual([...watcher.draft.value], ['ugc-3'])
    else assert.ok(watcher.loads > loads, path)
    if (path === 'film/[slug]') {
      assert.ok(
        watcher.callbackCounts.every((count) => count > 0),
        'both film watchers react to changed theaters',
      )
      assert.equal(
        watcher.loads,
        watcher.callbackCounts[1],
        'only admission watcher reloads',
      )
    }
    let previous = watcher.invalidations
    // Equal IDs and revision must not conceal a different owner.
    f.setResponse(value('2', ['ugc-3'], 'bob'))
    const beforeOwnerChange = watcher.callbackCounts
    const loadsBeforeOwnerChange = watcher.loads
    if (path === 'film/[slug]') watcher.content.value = content
    f.admit(session('bob'))
    if (path === 'film/[slug]') {
      assert.deepEqual(
        watcher.content.value,
        { loaded: true, currently_screened: true, theaters: [] },
        'old account theaters clear before any tick',
      )
      assert.ok(
        watcher.callbackCounts[0]! > beforeOwnerChange[0]!,
        'privacy watcher fires synchronously',
      )
      assert.equal(
        watcher.callbackCounts[1],
        beforeOwnerChange[1],
        'admission watcher waits for batch',
      )
      assert.equal(
        watcher.loads,
        loadsBeforeOwnerChange,
        'no speculative synchronous reload',
      )
    }
    await settle()
    assert.ok(watcher.invalidations > previous, path)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    if (path === 'film/[slug]') {
      assert.ok(
        watcher.callbackCounts[1]! > beforeOwnerChange[1]!,
        'admission watcher resumes after account change',
      )
      assert.equal(watcher.loads, watcher.callbackCounts[1])
    }
    previous = watcher.invalidations
    const beforeLogout = watcher.callbackCounts
    const loadsBeforeLogout = watcher.loads
    if (path === 'film/[slug]') watcher.content.value = content
    f.admit(anonymous)
    if (path === 'film/[slug]') {
      assert.deepEqual(
        watcher.content.value,
        { loaded: true, currently_screened: true, theaters: [] },
        'account theaters clear synchronously on logout',
      )
      assert.ok(watcher.callbackCounts[0]! > beforeLogout[0]!)
      assert.equal(watcher.callbackCounts[1], beforeLogout[1])
      assert.equal(watcher.loads, loadsBeforeLogout)
    }
    await settle()
    assert.ok(watcher.invalidations > previous, path)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
    if (path === 'film/[slug]') {
      assert.ok(
        watcher.callbackCounts[1]! > beforeLogout[1]!,
        'admission watcher resumes with device selection',
      )
      assert.equal(watcher.loads, watcher.callbackCounts[1])
    }
    assert.deepEqual(f.storageWrites, [])
  }
})

test('account wins; client-only snapshot never enters payload or device storage; one app subscription', async () => {
  const f = await fixture({ stored: '["ugc-1"]' })
  try {
    await f.preferences.initialize()
    assert.equal(f.preferences.isInitialized.value, false)
    f.admit()
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
    assert.equal(f.another(), f.preferences)
    await f.another().initialize()
    assert.equal(f.subscriptions, 1)
    assert.equal(f.gets, 1)
    assert.equal(f.parisRequests, 0)
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.storageWrites, [])
    assert.equal(
      [...f.states.keys()].some((key) => key.includes('favorite')),
      false,
    )
    f.admit(anonymous)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
  } finally {
    f.stop()
  }
})

test('fresh device waits for late or already admitted account preferences without requesting Paris', async () => {
  for (const lateAdmission of [false, true]) {
    const f = await fixture()
    try {
      const pending = deferred<AccountTheaterPreferences>()
      f.setRead(() => pending.promise)
      if (!lateAdmission) f.admit()
      const initialized = f.preferences.initialize()
      await settle()
      assert.equal(f.preferences.catalogReady.value, true)
      assert.equal(f.preferences.isInitialized.value, false)
      assert.equal(f.preferences.isLoading.value, true)
      assert.equal(f.preferences.writesBlocked.value, true)
      assert.equal(f.parisRequests, 0)
      if (lateAdmission) f.admit()
      await settle()
      await f.another().initialize()
      assert.equal(f.gets, 1)
      assert.equal(f.parisRequests, 0)
      assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-1']), false)
      pending.resolve(value())
      await initialized
      await settle()
      assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
      assert.equal(f.preferences.isInitialized.value, true)
      assert.equal(f.preferences.writesBlocked.value, false)
      assert.equal(f.parisRequests, 0)
      assert.equal(f.catalogRequests, 1)
      assert.equal(f.posts.length, 0)
      assert.deepEqual(f.storageWrites, [])
    } finally {
      f.stop()
    }
  }
})

test('needed anonymous or unset defaults load once and keep selection blocked until resolved', async () => {
  for (const admission of [anonymous, session()]) {
    const f = await fixture()
    try {
      const paris = deferred<typeof catalog>()
      f.setTheaters((query) =>
        query ? paris.promise : Promise.resolve(catalog),
      )
      f.setResponse(value('0', []))
      await f.preferences.initialize()
      assert.equal(f.parisRequests, 0)
      f.admit(admission)
      await settle()
      const initialized = f.preferences.initialize()
      const another = f.another().initialize()
      assert.equal(f.parisRequests, 1)
      assert.equal(f.preferences.isInitialized.value, false)
      assert.equal(f.preferences.isLoading.value, true)
      assert.equal(f.preferences.writesBlocked.value, true)
      assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
      assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-3']), false)
      paris.resolve([catalog[1]!])
      await Promise.all([initialized, another])
      await settle()
      assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
      assert.equal(f.preferences.isInitialized.value, true)
      assert.equal(f.preferences.writesBlocked.value, false)
      await f.preferences.initialize()
      await f.account.revalidate()
      await settle()
      assert.equal(f.parisRequests, 1)
      assert.equal(f.catalogRequests, 1)
      assert.equal(f.posts.length, 0)
      assert.deepEqual(f.storageWrites, [])
    } finally {
      f.stop()
    }
  }
})

test('logout lazily prepares separate device defaults and does not import them on next login', async () => {
  const f = await fixture()
  try {
    f.admit()
    await f.preferences.initialize()
    assert.equal(f.parisRequests, 0)
    const scope = f.preferences.selectionScopeKey.value
    f.admit(anonymous)
    assert.ok(f.preferences.selectionScopeKey.value > scope)
    assert.equal(f.preferences.isInitialized.value, false)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
    await settle()
    assert.equal(f.parisRequests, 1)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
    f.setResponse(value('0', [], 'bob'))
    f.admit(session('bob'))
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
    assert.equal(f.preferences.writesBlocked.value, false)
    assert.equal(f.posts.length, 0)
    f.admit(anonymous)
    await settle()
    assert.equal(f.parisRequests, 1)
    assert.deepEqual(f.storageWrites, [])
  } finally {
    f.stop()
  }
})

test('late public fallback cannot replace saved account selection after an owner switch', async () => {
  const f = await fixture()
  try {
    const paris = deferred<typeof catalog>()
    f.setTheaters((query) => (query ? paris.promise : Promise.resolve(catalog)))
    f.setResponse(value('0', []))
    f.admit()
    const initialized = f.preferences.initialize()
    await settle()
    assert.equal(f.parisRequests, 1)
    const scope = f.preferences.selectionScopeKey.value
    f.setResponse(value('3', ['ugc-3'], 'bob'))
    f.admit(session('bob'))
    await settle()
    assert.ok(f.preferences.selectionScopeKey.value > scope)
    assert.equal(f.preferences.writesBlocked.value, false)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    paris.resolve([catalog[0]!])
    await initialized
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.storageWrites, [])
    f.admit(anonymous)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
    assert.equal(f.parisRequests, 1)
  } finally {
    f.stop()
  }
})

test('failed or empty Paris result uses first national theater once without importing defaults', async () => {
  for (const failParis of [false, true]) {
    const f = await fixture()
    try {
      f.setTheaters(async (query) => {
        if (!query) return catalog
        if (failParis) throw new Error('unavailable')
        return []
      })
      f.setResponse(value('0', []))
      f.admit()
      await f.preferences.initialize()
      await settle()
      assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
      assert.equal(f.preferences.isInitialized.value, true)
      assert.equal(f.preferences.error.value, null)
      assert.equal(f.preferences.writesBlocked.value, false)
      await f.preferences.initialize()
      assert.equal(f.parisRequests, 1)
      assert.equal(f.posts.length, 0)
      assert.deepEqual(f.storageWrites, [])
    } finally {
      f.stop()
    }
  }
})

test('preference read failure stays blocked without requesting or importing Paris defaults', async () => {
  const f = await fixture()
  try {
    f.setRead(async () => {
      throw new errors.AccountApiError(503)
    })
    f.admit()
    await f.preferences.initialize()
    await settle()
    assert.equal(f.preferences.isInitialized.value, false)
    assert.equal(f.preferences.isLoading.value, false)
    assert.ok(f.preferences.error.value)
    assert.equal(f.preferences.writesBlocked.value, true)
    assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-1']), false)
    assert.equal(f.parisRequests, 0)
    assert.equal(f.posts.length, 0)
    f.setRead(async () => value())
    await f.preferences.retrySynchronization()
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
    assert.equal(f.parisRequests, 0)
    assert.deepEqual(f.storageWrites, [])
  } finally {
    f.stop()
  }
})

test('unset imports valid stored IDs once, not provisional defaults', async () => {
  for (const stored of [
    '["ugc-3","missing","invalid id",3,null,"ugc-3"]',
    undefined,
    '[]',
    '["missing"]',
    'not json',
  ]) {
    const f = await fixture({ stored })
    try {
      f.setResponse(value('0', []))
      f.admit()
      await f.preferences.initialize()
      await settle()
      const imported = stored?.includes('ugc-3') ?? false
      assert.equal(f.posts.length, imported ? 1 : 0)
      assert.equal(f.parisRequests, imported ? 0 : 1)
      if (imported) {
        assert.equal(f.posts[0]?.theater_ids, 'ugc-3')
        assert.equal(f.posts[0]?.expected_revision, '0')
      }
      assert.deepEqual(
        [...f.preferences.favoriteTheaterIds.value],
        [imported ? 'ugc-3' : 'ugc-1'],
      )
      assert.deepEqual(f.storageWrites, [])
      if (!imported) {
        assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-2']), true)
        assert.equal(f.posts[0]?.expected_revision, '0')
      }
    } finally {
      f.stop()
    }
  }
})

test('initialized empty and unavailable IDs stay authoritative; edits retain absent IDs', async () => {
  const f = await fixture()
  try {
    f.setResponse(value('2', ['temporarily-absent']))
    f.admit()
    await f.preferences.initialize()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
    assert.equal(f.preferences.isInitialized.value, true)
    assert.equal(f.posts.length, 0)
    await f.preferences.setFavoriteTheaterIds(['ugc-3'])
    assert.equal(f.posts[0]?.theater_ids, 'temporarily-absent,ugc-3')
    f.setResponse(value('4', []))
    await f.account.revalidate()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
    assert.equal(f.posts.length, 1)
    assert.equal(f.parisRequests, 0)
  } finally {
    f.stop()
  }
})

test('save waits for acknowledgement, rejects duplicates/empty draft and keeps anonymous storage distinct', async () => {
  const f = await fixture({ stored: '["ugc-1"]' })
  try {
    f.admit()
    await f.preferences.initialize()
    const pending = deferred<AccountTheaterPreferences>()
    f.setWrite(() => pending.promise)
    const save = f.preferences.setFavoriteTheaterIds(['ugc-3'])
    assert.equal(f.preferences.isSaving.value, true)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
    assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-1']), false)
    pending.resolve(value('2', ['ugc-3']))
    assert.equal(await save, true)
    assert.equal(await f.preferences.setFavoriteTheaterIds([]), false)
    assert.deepEqual(f.messages, ['theaters-changed'])
    assert.deepEqual(f.storageWrites, [])
    f.admit(anonymous)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
  } finally {
    f.stop()
  }
})

test('conflict adopts winner without replay and transient preference outage does not log out', async () => {
  const f = await fixture()
  try {
    f.admit()
    await f.preferences.initialize()
    f.setWrite(async () => {
      throw new errors.AccountApiError(409, 'theater_selection_changed')
    })
    f.setResponse(value('2', ['ugc-1']))
    assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-3']), false)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
    assert.match(f.preferences.syncError.value!, /autre appareil/)
    assert.equal(f.posts.length, 1)
    assert.equal(f.preferences.writesBlocked.value, false)
    f.setRead(async () => {
      throw new errors.AccountApiError(503)
    })
    await f.account.revalidate()
    assert.equal(f.account.session.value?.state, 'complete')
    assert.equal(f.preferences.writesBlocked.value, true)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-1'])
    f.setRead(async () => value('3', ['ugc-2']))
    await f.preferences.retrySynchronization()
    assert.equal(f.preferences.writesBlocked.value, false)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
    assert.equal(f.posts.length, 1)
    assert.deepEqual(f.messages, [])
  } finally {
    f.stop()
  }
})

test('ambiguous first import reads back once, never enters a bootstrap write loop', async () => {
  const f = await fixture({ stored: '["ugc-1"]' })
  try {
    f.setResponse(value('0', []))
    f.setWrite(async () => {
      throw new errors.AccountApiError()
    })
    f.admit()
    await f.preferences.initialize()
    await settle()
    assert.equal(f.posts.length, 1)
    await f.account.revalidate()
    await settle()
    await f.preferences.initialize()
    await settle()
    assert.equal(f.posts.length, 1)
  } finally {
    f.stop()
  }
})

test('lost save response reads committed state but never claims acknowledgement', async () => {
  const f = await fixture()
  try {
    f.admit()
    await f.preferences.initialize()
    f.setWrite(async () => {
      f.setResponse(value('2', ['ugc-3']))
      throw new errors.AccountApiError()
    })
    assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-3']), false)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    assert.equal(f.preferences.writesBlocked.value, false)
    assert.equal(f.posts.length, 1)
  } finally {
    f.stop()
  }
})

test('owner change and privacy clear discard delayed reads and writes', async () => {
  const f = await fixture()
  try {
    const read = deferred<AccountTheaterPreferences>()
    f.setRead(() => read.promise)
    f.admit()
    const initialized = f.preferences.initialize()
    await settle()
    f.admit(session('bob'))
    f.setRead(async () => value('1', ['ugc-3'], 'bob'))
    await settle()
    read.resolve(value())
    await initialized
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    const write = deferred<AccountTheaterPreferences>()
    f.setWrite(() => write.promise)
    const saved = f.preferences.setFavoriteTheaterIds(['ugc-1'])
    f.account.clear()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
    assert.equal(f.preferences.writesBlocked.value, true)
    write.resolve(value('2', ['ugc-1'], 'bob'))
    assert.equal(await saved, false)
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
  } finally {
    f.stop()
  }
})

test('revalidation waits for dispatched save and applies fresh GET without deadlock or echo', async () => {
  const f = await fixture()
  try {
    f.admit()
    await f.preferences.initialize()
    const write = deferred<AccountTheaterPreferences>()
    f.setWrite(() => write.promise)
    const saved = f.preferences.setFavoriteTheaterIds(['ugc-3'])
    const refreshed = f.account.revalidate()
    await settle()
    assert.equal(f.gets, 1)
    f.setResponse(value('2', ['ugc-3']))
    write.resolve(value('2', ['ugc-3']))
    await saved
    await refreshed
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    assert.equal(f.preferences.writesBlocked.value, false)
    assert.equal(f.posts.length, 1)
    assert.deepEqual(f.messages, ['theaters-changed'])
  } finally {
    f.stop()
  }
})

test('pending, disabled and anonymous sessions use device mode; storage failures retain memory', async () => {
  for (const admission of [
    anonymous,
    { ...anonymous, enabled: false },
    { ...session(), state: 'pending_username' as const },
  ]) {
    const f = await fixture({ storageFails: true })
    try {
      f.admit(admission)
      await f.preferences.initialize()
      assert.equal(f.parisRequests, 1)
      assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-3']), true)
      assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
      assert.equal(f.gets, 0)
      assert.equal(f.posts.length, 0)
    } finally {
      f.stop()
    }
  }
})

test('SSR initialization never fetches private preferences or reads browser storage', async () => {
  const f = await fixture({ client: false, storageFails: true })
  try {
    f.admit()
    await f.preferences.initialize()
    assert.equal(f.gets, 0)
    assert.equal(f.subscriptions, 0)
    assert.equal(f.preferences.isInitialized.value, false)
    assert.equal(await f.preferences.setFavoriteTheaterIds(['ugc-1']), false)
  } finally {
    f.stop()
  }
})

test('first-import conflict loads winner, never overwrites it or persists account data locally', async () => {
  const f = await fixture({ stored: '["ugc-1"]' })
  try {
    f.setResponse(value('0', []))
    f.setWrite(async () => {
      f.setResponse(value('1', ['ugc-3']))
      throw new errors.AccountApiError(409, 'theater_selection_changed')
    })
    f.admit()
    await f.preferences.initialize()
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    assert.equal(f.posts.length, 1)
    assert.match(f.preferences.syncError.value!, /autre appareil/)
    assert.deepEqual(f.storageWrites, [])
  } finally {
    f.stop()
  }
})

test('a wrong-owner GET invalidates admission without displaying its preferences', async () => {
  const f = await fixture()
  try {
    f.setResponse(value('1', ['ugc-3'], 'bob'))
    f.admit()
    await f.preferences.initialize()
    await settle()
    assert.equal(f.account.status.value, 'error')
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], [])
    assert.equal(f.gets, 1)
    assert.equal(f.posts.length, 0)
    f.setResponse(value('1', ['ugc-2']))
    await f.preferences.retrySynchronization()
    await settle()
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
  } finally {
    f.stop()
  }
})

test('account plugin revalidates theater notifications and revisit events without broadcasting reads', async () => {
  const listeners = new Map<string, (event: { persisted?: boolean }) => void>()
  const hooks = new Map<string, () => void>()
  let refreshes = 0
  let revalidations = 0
  let clears = 0
  let starts = 0
  const channels: MockChannel[] = []
  class MockChannel {
    onmessage?: (event: { data: string }) => void
    constructor() {
      channels.push(this)
    }
  }
  const source = await readFile(
    new URL('../app/plugins/account-session.client.ts', import.meta.url),
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
      exports: {},
      defineNuxtPlugin: (
        fn: (app: {
          hook: (name: string, callback: () => void) => void
        }) => void,
      ) => fn({ hook: (name, callback) => hooks.set(name, callback) }),
      useCinemaPreferences: () => ({
        startSynchronization: () => {
          starts++
        },
      }),
      useAccountSession: () => ({
        status: ref('ready'),
        refresh: () => {
          refreshes++
        },
        revalidate: () => {
          revalidations++
        },
        clear: () => {
          clears++
        },
      }),
      BroadcastChannel: MockChannel,
      window: {
        BroadcastChannel: MockChannel,
        addEventListener: (
          name: string,
          callback: (event: { persisted?: boolean }) => void,
        ) => listeners.set(name, callback),
      },
      document: {
        visibilityState: 'visible',
        addEventListener: (
          name: string,
          callback: (event: { persisted?: boolean }) => void,
        ) => listeners.set(name, callback),
      },
    },
  )
  hooks.get('app:mounted')!()
  channels[0]!.onmessage!({ data: 'theaters-changed' })
  for (const name of ['focus', 'online', 'visibilitychange'])
    listeners.get(name)!({})
  assert.equal(revalidations, 4)
  assert.equal(refreshes, 0)
  channels[0]!.onmessage!({ data: 'changed' })
  assert.equal(refreshes, 1)
  listeners.get('pagehide')!({})
  listeners.get('focus')!({})
  assert.equal(clears, 1)
  assert.equal(revalidations, 4)
  listeners.get('pageshow')!({ persisted: true })
  assert.equal(refreshes, 2)
  assert.equal(starts, 1)
})

test('same-owner session admission replaces an obsolete pending read without leaving synchronization blocked', async () => {
  const f = await fixture()
  try {
    const old = deferred<AccountTheaterPreferences>()
    f.setRead(() => old.promise)
    f.admit()
    const initialized = f.preferences.initialize()
    await settle()
    f.setRead(async () => value('2', ['ugc-3']))
    f.admit(session())
    await settle()
    old.resolve(value('1', ['ugc-1']))
    await initialized
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-3'])
    assert.equal(f.preferences.writesBlocked.value, false)
  } finally {
    f.stop()
  }
})

test('shared links override even unresolved account selection without writing account or device preferences', async () => {
  const f = await fixture()
  try {
    await f.preferences.initialize()
    const module = await f.compile<{
      usePageCinemaSelection: typeof usePageCinemaSelection
    }>('usePageCinemaSelection', {
      useCinemaPreferences: () => f.preferences,
      useRoute: () => ({ query: { shared_theaters: 'ugc-3' } }),
      require: () => sharedSelection,
    })
    const page = module.usePageCinemaSelection()
    assert.equal(page.isInitialized.value, true)
    assert.equal(page.isLoading.value, false)
    assert.deepEqual([...page.activeTheaterIds.value], ['ugc-3'])
    f.admit()
    await settle()
    assert.deepEqual([...page.activeTheaterIds.value], ['ugc-3'])
    assert.deepEqual([...f.preferences.favoriteTheaterIds.value], ['ugc-2'])
    const watchers = await Promise.all(
      ['planning', 'recherche', 'film/[slug]'].map((path) =>
        selectionWatcher(path, page),
      ),
    )
    try {
      const counts = watchers.map((watcher) => watcher.invalidations)
      const callbacks = watchers.map((watcher) => watcher.callbackCounts)
      const loads = watchers.map((watcher) => watcher.loads)
      const ids = page.activeTheaterIds.value
      const focus = await focusPlugin(f)
      for (const next of [value(), value('2', ['ugc-1']), value('3', [])]) {
        f.setResponse(next)
        await focus()
        assert.equal(page.activeTheaterIds.value, ids)
        assert.deepEqual(
          watchers.map((watcher) => watcher.invalidations),
          counts,
        )
        assert.deepEqual(
          watchers.map((watcher) => watcher.callbackCounts),
          callbacks,
        )
        assert.deepEqual(
          watchers.map((watcher) => watcher.loads),
          loads,
        )
      }
    } finally {
      for (const watcher of watchers) watcher.stop()
    }
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.storageWrites, [])
  } finally {
    f.stop()
  }
})
