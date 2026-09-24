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
  let gets = 0
  let response = value()
  let admitted = options.admission ?? session()
  let read = async () => response
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
      theaters: async (query?: { city: string }) =>
        query ? [catalog[0]] : catalog,
    }),
    useAccountApi: () => ({
      session: async () => admitted,
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
    get subscriptions() {
      return app._accountRevalidation?.details.size
    },
    setRead: (fn: typeof read) => {
      read = fn
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

test('unset imports valid stored IDs once, not provisional defaults', async () => {
  for (const stored of [
    '["ugc-3","missing"]',
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
  const f = await fixture({ stored: '["ugc-1"]' })
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
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.storageWrites, [])
  } finally {
    f.stop()
  }
})
