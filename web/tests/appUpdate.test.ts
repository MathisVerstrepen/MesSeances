import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import {
  createAppUpdate,
  type BuildJson,
  type UpdateState,
} from '../app/utils/appUpdate.ts'

const flush = async () => {
  for (let i = 0; i < 12; i++) await Promise.resolve()
}
const deferred = <T>() => {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
class Worker extends EventTarget {
  state = 'installed'
  move(state: string) {
    this.state = state
    this.dispatchEvent(new Event('statechange'))
  }
}
class Registration extends EventTarget {
  waiting: Worker | null = null
  installing: Worker | null = null
  active: Worker | null = null
  updates = 0
  updateResult: () => Promise<void> = async () => {}
  update() {
    this.updates++
    return this.updateResult()
  }
}
function fixture(shared = new Map<string, string>()) {
  let now = 100000
  let online = true
  let visible = true
  // oxlint-disable-next-line anti-slop/no-known-value-widening -- Fixtures deliberately cover malformed JSON bodies as well as valid IDs.
  let id: BuildJson = { id: 'a' }
  let fetches = 0
  let reloads = 0
  let activations = 0
  let activationResult: () => Promise<void> = async () => {}
  let fetcher:
    | (() => Promise<{ ok: boolean; json(): Promise<BuildJson> }>)
    | undefined
  const requests: { url: string; options: RequestInit }[] = []
  const timers = new Map<
    ReturnType<typeof setTimeout>,
    { at: number; callback: () => void }
  >()
  let serial = 0
  const storage = {
    getItem: (key: string) => shared.get(key) ?? null,
    setItem: (key: string, value: string) => {
      shared.set(key, value)
    },
    removeItem: (key: string) => {
      shared.delete(key)
    },
  }
  const state = { available: false, busy: false, error: '' }
  const owner = createAppUpdate({
    buildId: 'a',
    state,
    now: () => now,
    online: () => online,
    visible: () => visible,
    nonce: () => `document-${++serial}`,
    fetch: (url, options) => {
      fetches++
      requests.push({ url, options })
      if (fetcher) return fetcher()
      return Promise.resolve({ ok: true, json: async () => id })
    },
    reload: () => {
      reloads++
    },
    activate: async () => {
      activations++
      await activationResult()
    },
    storage,
    setTimeout: (callback, ms) => {
      const handle = setTimeout(() => {}, 0)
      clearTimeout(handle)
      timers.set(handle, { at: now + ms, callback })
      return handle
    },
    clearTimeout: (handle) => {
      timers.delete(handle)
    },
  })
  return {
    owner,
    state,
    requests,
    storage,
    shared,
    timers,
    get fetches() {
      return fetches
    },
    get reloads() {
      return reloads
    },
    get activations() {
      return activations
    },
    id: (value: BuildJson) => {
      id = value
    },
    online: (value: boolean) => {
      online = value
    },
    visible: (value: boolean) => {
      visible = value
    },
    fetcher: (value: typeof fetcher) => {
      fetcher = value
    },
    activate: (value: typeof activationResult) => {
      activationResult = value
    },
    advance: async (ms: number) => {
      now += ms
      for (const [handle, entry] of timers)
        if (entry.at <= now) {
          timers.delete(handle)
          entry.callback()
        }
      await flush()
    },
  }
}

test('matching build and first install show nothing; discovery is uncached and credential-free', async () => {
  const f = fixture()
  await f.owner.check()
  assert.deepEqual(f.state, { available: false, busy: false, error: '' })
  assert.equal(f.reloads, 0)
  assert.equal(f.requests[0]!.options.cache, 'no-store')
  assert.equal(f.requests[0]!.options.credentials, 'omit')
  assert.match(f.requests[0]!.url, /\?fresh=document-/)
  const r = new Registration()
  r.active = new Worker()
  r.active.move('activated')
  f.owner.setRegistration(r)
  assert.equal(f.state.available, false)
})
test('opaque mismatched ID prompts but never reloads without consent', async () => {
  const f = fixture()
  f.id({ id: 'rollback-build' })
  await f.owner.check()
  f.owner.needReload()
  await flush()
  assert.equal(f.state.available, true)
  assert.equal(f.reloads, 0)
  await f.owner.refresh()
  assert.equal(f.reloads, 1)
  await f.owner.refresh()
  f.owner.needReload()
  await flush()
  assert.equal(f.reloads, 1)
})
test('first installed-to-activated transition is not a waiting update', () => {
  for (const lateRegistration of [false, true]) {
    const f = fixture()
    const r = new Registration()
    const w = new Worker()
    w.state = 'installing'
    r.installing = w
    if (!lateRegistration) f.owner.setRegistration(r)
    r.installing = null
    r.waiting = w
    if (lateRegistration) f.owner.setRegistration(r)
    w.move('installed')
    assert.equal(f.state.available, false)
    r.waiting = null
    r.active = w
    w.move('activating')
    w.move('activated')
    assert.equal(f.state.available, false)
    assert.equal(f.reloads, 0)
    f.owner.dispose()
  }
})
test('installed waiting worker with an old active worker surfaces an update', () => {
  const f = fixture()
  const r = new Registration()
  const w = new Worker()
  r.active = new Worker()
  r.active.move('activated')
  w.state = 'installing'
  r.installing = w
  f.owner.setRegistration(r)
  r.installing = null
  r.waiting = w
  w.move('installed')
  assert.equal(f.state.available, true)
  assert.equal(f.reloads, 0)
  f.owner.dispose()
})
test('automatic bursts share discovery and sixty-second cooldown', async () => {
  const f = fixture()
  const gate = deferred<{ ok: boolean; json(): Promise<BuildJson> }>()
  f.fetcher(() => gate.promise)
  const first = f.owner.check()
  const second = f.owner.check()
  f.owner.check(true)
  assert.equal(first, second)
  assert.equal(f.fetches, 1)
  gate.resolve({ ok: true, json: async () => ({ id: 'b' }) })
  await first
  await f.owner.check()
  await f.advance(59999)
  await f.owner.check()
  assert.equal(f.fetches, 1)
  await f.advance(1)
  await f.owner.check()
  assert.equal(f.fetches, 2)
  assert.notEqual(f.requests[0]!.url, f.requests[1]!.url)
})
test('hidden/offline skips checks; online retries failed discovery within cooldown', async () => {
  const f = fixture()
  f.visible(false)
  await f.owner.check()
  f.visible(true)
  f.online(false)
  await f.owner.check()
  assert.equal(f.fetches, 0)
  f.online(true)
  f.fetcher(async () => {
    throw new Error('unavailable')
  })
  await f.owner.check(true)
  assert.equal(f.fetches, 1)
  assert.equal(f.state.available, false)
  f.fetcher(undefined)
  f.id({ id: 'b' })
  await f.owner.check(true)
  assert.equal(f.fetches, 2)
  assert.equal(f.state.available, true)
  assert.equal(f.reloads, 0)
})
for (const body of [null, {}, { id: '' }, { id: '  ' }, { id: 2 }]) {
  test(`invalid discovery ${JSON.stringify(body)} never creates an update`, async () => {
    const f = fixture()
    f.id(body)
    await f.owner.check()
    assert.equal(f.state.available, false)
    assert.equal(f.reloads, 0)
  })
}
test('HTTP failure and JSON failure preserve known notice without background error', async () => {
  const f = fixture()
  f.owner.needRefresh()
  f.fetcher(async () => ({ ok: false, json: async () => ({ id: 'b' }) }))
  await f.owner.check()
  assert.equal(f.state.available, true)
  assert.equal(f.state.error, '')
  f.fetcher(async () => ({
    ok: true,
    json: async () => {
      throw new Error('invalid')
    },
  }))
  await f.owner.refresh()
  assert.equal(f.reloads, 0)
  assert.match(f.state.error, /Réessayez/)
})
test('discovery aborts at five seconds; explicit attempt fails without reload', async () => {
  const f = fixture()
  f.fetcher(
    () =>
      new Promise((_resolve, reject) =>
        f.requests
          .at(-1)!
          .options.signal!.addEventListener('abort', () =>
            reject(new Error('aborted')),
          ),
      ),
  )
  f.owner.needRefresh()
  const attempt = f.owner.refresh()
  await flush()
  await f.advance(5000)
  await attempt
  assert.equal(f.requests[0]!.options.signal!.aborted, true)
  assert.equal(f.state.busy, false)
  assert.equal(f.reloads, 0)
})
test('offline explicit action preserves form/page and retries when online', async () => {
  const f = fixture()
  f.owner.needRefresh()
  f.online(false)
  await f.owner.refresh()
  assert.equal(f.fetches, 0)
  assert.equal(f.reloads, 0)
  assert.equal(f.state.error, 'Connexion indisponible. Réessayez en ligne.')
  f.online(true)
  f.id({ id: 'b' })
  await f.owner.refresh()
  assert.equal(f.reloads, 1)
})
test('preexisting waiting worker requires actual activation, not helper promise', async () => {
  const f = fixture()
  const r = new Registration()
  const w = new Worker()
  r.active = new Worker()
  r.waiting = w
  f.owner.setRegistration(r)
  assert.equal(f.state.available, true)
  const attempt = f.owner.refresh()
  await flush()
  assert.equal(f.activations, 1)
  assert.equal(f.reloads, 0)
  assert.equal(f.state.busy, true)
  w.move('activated')
  f.owner.needReload()
  await attempt
  assert.equal(f.reloads, 1)
  assert.equal(f.state.busy, false)
  assert.equal(f.timers.size, 0)
})
test('duplicate clicks join one fresh attempt; matching stale notice clears without reload', async () => {
  const f = fixture()
  f.owner.needRefresh()
  const a = f.owner.refresh()
  const b = f.owner.refresh()
  assert.equal(a, b)
  assert.equal(f.state.busy, true)
  await a
  assert.equal(f.fetches, 1)
  assert.equal(f.state.available, false)
  assert.equal(f.reloads, 0)
})
test('helper activation rejection releases busy state and revokes late reload signals', async () => {
  const f = fixture()
  const r = new Registration()
  const w = new Worker()
  r.waiting = w
  f.owner.setRegistration(r)
  f.activate(async () => {
    throw new Error('rejected')
  })
  await f.owner.refresh()
  assert.equal(f.state.busy, false)
  assert.match(f.state.error, /Réessayez/)
  w.move('activated')
  f.owner.needReload()
  await flush()
  assert.equal(f.reloads, 0)
})
test('SW update failure or absent registration cannot block server mismatch', async () => {
  for (const registration of [undefined, new Registration()]) {
    const f = fixture()
    f.id({ id: 'b' })
    if (registration)
      registration.updateResult = async () => {
        throw new Error('SW failure')
      }
    f.owner.setRegistration(registration)
    await f.owner.check()
    await f.owner.refresh()
    assert.equal(f.reloads, 1)
  }
})
test('installation in flight is bounded and then activates observed waiting worker', async () => {
  const f = fixture()
  const r = new Registration()
  const w = new Worker()
  r.active = new Worker()
  w.state = 'installing'
  r.installing = w
  f.owner.setRegistration(r)
  f.id({ id: 'b' })
  const attempt = f.owner.refresh()
  await flush()
  assert.equal(f.activations, 0)
  r.installing = null
  r.waiting = w
  w.move('installed')
  await flush()
  assert.equal(f.activations, 1)
  w.move('activated')
  await attempt
  assert.equal(f.reloads, 1)
})
test('redundant worker and ten-second activation timeout retain retryable notice', async () => {
  for (const outcome of ['redundant', 'timeout']) {
    const f = fixture()
    const r = new Registration()
    const w = new Worker()
    r.waiting = w
    f.owner.setRegistration(r)
    const attempt = f.owner.refresh()
    await flush()
    if (outcome === 'redundant') w.move('redundant')
    else await f.advance(10000)
    await attempt
    assert.equal(f.state.busy, false)
    assert.equal(f.state.available, true)
    assert.match(f.state.error, /Réessayez/)
    assert.equal(f.reloads, 0)
    w.move('activated')
    f.owner.needReload()
    await flush()
    assert.equal(f.reloads, 0)
  }
})
test('activation racing before failed discovery or from another tab never navigates', async () => {
  const f = fixture()
  const r = new Registration()
  const w = new Worker()
  r.waiting = w
  f.owner.setRegistration(r)
  const gate = deferred<{ ok: boolean; json(): Promise<BuildJson> }>()
  f.fetcher(() => gate.promise)
  const attempt = f.owner.refresh()
  r.waiting = null
  r.active = w
  w.move('activated')
  f.owner.needReload()
  assert.equal(f.reloads, 0)
  gate.reject(new Error('offline'))
  await attempt
  f.owner.needReload()
  await flush()
  assert.equal(f.reloads, 0)
})
test('activation racing before successful discovery still waits for fresh server evidence', async () => {
  const f = fixture()
  const r = new Registration()
  const w = new Worker()
  r.waiting = w
  f.owner.setRegistration(r)
  const gate = deferred<{ ok: boolean; json(): Promise<BuildJson> }>()
  f.fetcher(() => gate.promise)
  const attempt = f.owner.refresh()
  r.waiting = null
  r.active = w
  w.move('activated')
  f.owner.needReload()
  assert.equal(f.reloads, 0)
  gate.resolve({ ok: true, json: async () => ({ id: 'b' }) })
  await attempt
  assert.equal(f.reloads, 1)
})
test('late registration update after timeout cannot authorize reload', async () => {
  const f = fixture()
  const r = new Registration()
  const gate = deferred<void>()
  r.updateResult = () => gate.promise
  f.owner.setRegistration(r)
  f.id({ id: 'b' })
  const attempt = f.owner.refresh()
  await flush()
  await f.advance(10000)
  await attempt
  gate.resolve()
  await flush()
  assert.equal(f.reloads, 0)
  assert.equal(f.state.busy, false)
})
test('public-ID cross-load cooldown blocks same attempt, expires, and clears satisfied/stale records', async () => {
  const shared = new Map<string, string>()
  const first = fixture(shared)
  first.id({ id: 'b' })
  await first.owner.refresh()
  const second = fixture(shared)
  second.id({ id: 'b' })
  await second.owner.refresh()
  assert.equal(second.reloads, 0)
  assert.deepEqual(Object.keys(JSON.parse([...shared.values()][0]!)).sort(), [
    'source',
    'target',
    'time',
  ])
  await second.advance(30000)
  await second.owner.refresh()
  assert.equal(second.reloads, 1)
  const satisfied = fixture(shared)
  satisfied.owner.needRefresh()
  await satisfied.owner.refresh()
  assert.equal(shared.size, 0)
})
test('denied/malformed storage does not block reload', async () => {
  for (const mode of ['denied', 'malformed']) {
    const f = fixture()
    f.id({ id: 'b' })
    if (mode === 'denied')
      f.storage.getItem = () => {
        throw new Error('denied')
      }
    else f.shared.set('messeances:build-refresh', '{bad')
    await f.owner.refresh()
    assert.equal(f.reloads, 1)
  }
})
test('disposal aborts discovery and prevents late callbacks or timer work', async () => {
  const f = fixture()
  const gate = deferred<{ ok: boolean; json(): Promise<BuildJson> }>()
  f.fetcher(() => gate.promise)
  const check = f.owner.check()
  f.owner.dispose()
  assert.equal(f.requests[0]!.options.signal!.aborted, true)
  assert.equal(f.timers.size, 0)
  gate.resolve({ ok: true, json: async () => ({ id: 'b' }) })
  await check
  f.owner.needReload()
  f.owner.needRefresh()
  await f.owner.refresh()
  assert.equal(f.reloads, 0)
  assert.equal(f.state.available, false)
})
test('plugin wires readiness/lifecycle/chunk events to one owner and disposes listeners', async () => {
  const events: string[] = []
  const calls: boolean[] = []
  let ready!: () => void
  let dispose!: () => void
  const win = new EventTarget()
  const doc = new EventTarget()
  let registrationOptions!: { onNeedReload(): void; onNeedRefresh(): void }
  const owner = {
    check: (online = false) => {
      calls.push(online)
    },
    dispose: () => events.push('dispose'),
    refresh: () => {},
    setRegistration: () => {},
    needReload: () => events.push('reload-signal'),
    needRefresh: () => events.push('waiting'),
  }
  const source = ts.transpileModule(
    (
      await readFile(
        new URL('../app/plugins/app-update.client.ts', import.meta.url),
        'utf8',
      )
    ).replace('if (import.meta.hot) import.meta.hot.dispose(dispose)', ''),
    { compilerOptions: { module: ts.ModuleKind.CommonJS } },
  ).outputText
  runInNewContext(source, {
    exports: {},
    require: (name: string) =>
      name === 'vue'
        ? {
            reactive: (value: UpdateState) => value,
            onScopeDispose: (callback: () => void) => {
              dispose = callback
            },
          }
        : name.startsWith('virtual:')
          ? {
              useRegisterSW: (options: typeof registrationOptions) => {
                registrationOptions = options
                return { updateServiceWorker: async () => {} }
              },
            }
          : { createAppUpdate: () => owner },
    defineNuxtPlugin: (
      plugin: (app: {
        hook: (name: string, callback: () => void) => () => void
      }) => { provide: object },
    ) =>
      plugin({
        hook: (name: string, callback: () => void) => {
          events.push(name)
          doc.addEventListener(name, callback)
          return () => doc.removeEventListener(name, callback)
        },
      }),
    useRuntimeConfig: () => ({ app: { buildId: 'a' } }),
    onNuxtReady: (callback: () => void) => {
      ready = callback
    },
    window: win,
    document: doc,
    navigator: {},
    crypto: {},
    setTimeout,
    clearTimeout,
  })
  ready()
  for (const event of ['focus', 'pageshow', 'online'])
    win.dispatchEvent(new Event(event))
  doc.dispatchEvent(new Event('visibilitychange'))
  doc.dispatchEvent(new Event('app:chunkError'))
  assert.deepEqual(calls, [false, false, false, true, false, false])
  registrationOptions.onNeedReload()
  assert.ok(events.includes('reload-signal'))
  dispose()
  win.dispatchEvent(new Event('focus'))
  doc.dispatchEvent(new Event('visibilitychange'))
  assert.equal(calls.length, 6)
  assert.ok(events.includes('dispose'))
})
