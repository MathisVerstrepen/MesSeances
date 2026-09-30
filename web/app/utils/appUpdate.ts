export interface UpdateState {
  available: boolean
  busy: boolean
  error: string
}

export type BuildJson =
  | null
  | boolean
  | number
  | string
  | BuildJson[]
  | { [key: string]: BuildJson }

interface Worker extends EventTarget {
  readonly state: string
}

interface Registration extends EventTarget {
  readonly waiting: Worker | null
  readonly installing: Worker | null
  readonly active: Worker | null
  update(): Promise<void | Registration>
}

interface UpdateOptions {
  buildId: string
  state: UpdateState
  fetch: (
    url: string,
    options: RequestInit,
  ) => Promise<{
    ok: boolean
    json(): Promise<BuildJson>
  }>
  nonce: () => string
  now: () => number
  online: () => boolean
  visible: () => boolean
  reload: () => void
  activate: () => Promise<void>
  storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
  setTimeout: (
    callback: () => void,
    ms: number,
  ) => ReturnType<typeof setTimeout>
  clearTimeout: (timer: ReturnType<typeof setTimeout>) => void
}

const storageKey = 'messeances:build-refresh'
const retryError = 'Actualisation impossible pour le moment. Réessayez.'
const offlineError = 'Connexion indisponible. Réessayez en ligne.'

// No Nuxt imports or browser globals: one owner, with native adapters at the edge.
export function createAppUpdate(options: UpdateOptions) {
  const { state } = options
  let registration: Registration | undefined
  let disposed = false
  let reloadIssued = false
  let lastAttempt = -Infinity
  let unavailable = false
  let discovery: Promise<string | undefined> | undefined
  let explicit: Promise<void> | undefined
  let generation = 0
  const controllers = new Set<AbortController>()
  const timers = new Set<ReturnType<typeof setTimeout>>()
  const cleanups = new Set<() => void>()
  const workerWaits = new Set<() => void>()

  function timer(callback: () => void, ms: number) {
    const handle = options.setTimeout(() => {
      timers.delete(handle)
      callback()
    }, ms)
    timers.add(handle)
    return handle
  }
  function clear(handle: ReturnType<typeof setTimeout>) {
    options.clearTimeout(handle)
    timers.delete(handle)
  }
  function listen(target: EventTarget, name: string, callback: () => void) {
    target.addEventListener(name, callback)
    const cleanup = () => {
      target.removeEventListener(name, callback)
      cleanups.delete(cleanup)
    }
    cleanups.add(cleanup)
    return cleanup
  }
  function observeWorker(worker: Worker | null) {
    if (!worker) return
    const cleanup = listen(worker, 'statechange', () => {
      if (disposed) return
      if (
        worker.state === 'installed' &&
        registration?.waiting === worker &&
        registration.active
      )
        state.available = true
      if (worker.state === 'activated' || worker.state === 'redundant')
        cleanup()
    })
  }
  function setRegistration(value: Registration | undefined) {
    if (disposed || !value || registration === value) return
    registration = value
    // First installation briefly occupies waiting before automatic activation.
    if (value.waiting && value.active) state.available = true
    observeWorker(value.installing)
    listen(value, 'updatefound', () => observeWorker(value.installing))
  }
  function needRefresh() {
    if (!disposed) state.available = true
  }
  function needReload() {
    // Another tab's activation is never consent. Explicit attempts observe their
    // own worker; this callback only surfaces/rechecks availability.
    if (disposed) return
    state.available = true
    void check()
  }
  function updateWorker() {
    try {
      return Promise.resolve(registration?.update()).catch(() => {})
    } catch {
      return Promise.resolve()
    }
  }
  function discover() {
    if (discovery) return discovery
    if (disposed || !options.online()) {
      unavailable = true
      return Promise.resolve(undefined)
    }
    const controller = new AbortController()
    controllers.add(controller)
    const timeout = timer(() => controller.abort(), 5000)
    discovery = (async () => {
      try {
        // The nonignored, cross-document nonce also bypasses legacy precaches.
        const response = await options.fetch(
          `/_nuxt/builds/latest.json?fresh=${encodeURIComponent(options.nonce())}`,
          { cache: 'no-store', credentials: 'omit', signal: controller.signal },
        )
        if (!response.ok) throw new Error('Build unavailable')
        const body = await response.json()
        if (
          !body ||
          // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Validate untrusted network JSON without coercion.
          typeof body !== 'object' ||
          !('id' in body) ||
          // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Build IDs must be genuine nonempty strings at this network boundary.
          typeof body.id !== 'string' ||
          !body.id.trim()
        )
          throw new Error('Invalid build')
        if (disposed || controller.signal.aborted) return undefined
        unavailable = false
        if (body.id !== options.buildId) state.available = true
        if (body.id === options.buildId && !state.available) {
          try {
            options.storage?.removeItem(storageKey)
          } catch {
            /* denied */
          }
        }
        return body.id
      } catch {
        unavailable = true
        return undefined
      } finally {
        clear(timeout)
        controllers.delete(controller)
        discovery = undefined
      }
    })()
    return discovery
  }
  function check(reconnected = false) {
    if (disposed || !options.visible() || !options.online()) {
      if (!options.online()) unavailable = true
      return Promise.resolve(undefined)
    }
    if (discovery) return discovery
    if (options.now() - lastAttempt < 60000 && !(reconnected && unavailable))
      return Promise.resolve(undefined)
    lastAttempt = options.now()
    void updateWorker()
    return discover()
  }

  function inCooldown(target: string) {
    try {
      const raw = options.storage?.getItem(storageKey)
      if (!raw) return false
      const record: BuildJson = JSON.parse(raw)
      if (
        record &&
        // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Session storage is untrusted, including values from older clients.
        typeof record === 'object' &&
        'source' in record &&
        'target' in record &&
        'time' in record &&
        // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Reject malformed storage timestamps rather than coercing them.
        typeof record.time === 'number' &&
        record.source === options.buildId &&
        record.target === target &&
        options.now() >= record.time &&
        options.now() - record.time < 30000
      )
        return true
      options.storage?.removeItem(storageKey)
    } catch {
      // Storage denial never blocks a user-requested update.
    }
    return false
  }
  function reload(target: string, token: number) {
    if (disposed || token !== generation || reloadIssued) return
    if (inCooldown(target)) throw new Error('Refresh cooldown')
    try {
      options.storage?.setItem(
        storageKey,
        JSON.stringify({
          source: options.buildId,
          target,
          time: options.now(),
        }),
      )
    } catch {
      // In-memory guard remains effective when storage is denied.
    }
    reloadIssued = true
    options.reload()
  }

  function awaitWorker(worker: Worker, token: number, activate: boolean) {
    return new Promise<void>((resolve, reject) => {
      const cleanup = listen(worker, 'statechange', inspect)
      const cancel = () => {
        cleanup()
        workerWaits.delete(cancel)
        reject(new Error('Expired update'))
      }
      workerWaits.add(cancel)
      const finish = () => {
        cleanup()
        workerWaits.delete(cancel)
      }
      function inspect() {
        if (disposed || token !== generation) {
          finish()
          reject(new Error('Expired update'))
        } else if (worker.state === 'activated') {
          finish()
          resolve()
        } else if (worker.state === 'redundant') {
          finish()
          reject(new Error('Worker unavailable'))
        } else if (
          worker.state === 'installed' &&
          registration?.waiting === worker
        ) {
          if (activate) {
            activate = false
            Promise.resolve()
              .then(options.activate)
              .catch(() => {
                finish()
                reject(new Error('Activation failed'))
              })
          }
        }
      }
      inspect()
    })
  }

  function refresh() {
    if (explicit) return explicit
    if (disposed || reloadIssued) return Promise.resolve()
    state.busy = true
    state.error = ''
    const token = ++generation
    let deadline: ReturnType<typeof setTimeout>
    const expired = new Promise<never>((_resolve, reject) => {
      deadline = timer(() => reject(new Error('Update timeout')), 10000)
    })
    const attempt = (async () => {
      const workerUpdate = options.online() ? updateWorker() : Promise.resolve()
      const target = await discover()
      if (!target) throw new Error('Discovery unavailable')
      await workerUpdate
      if (disposed || token !== generation) return
      const worker = registration?.waiting || registration?.installing
      if (worker) {
        await awaitWorker(worker, token, true)
        if (disposed || token !== generation) return
        reload(target, token)
      } else if (target !== options.buildId) {
        reload(target, token)
      } else {
        state.available = false
        try {
          options.storage?.removeItem(storageKey)
        } catch {
          /* denied */
        }
      }
    })()
    explicit = Promise.race([attempt, expired])
      .catch(() => {
        if (!disposed && token === generation) {
          state.available = true
          state.error = options.online() ? retryError : offlineError
        }
      })
      .finally(() => {
        clear(deadline)
        if (token === generation) generation++
        for (const cancel of workerWaits) cancel()
        // An expired attempt may still have a native update promise outstanding.
        // Its token is revoked before another explicit action can start.
        for (const controller of controllers) controller.abort()
        if (!disposed) state.busy = false
        explicit = undefined
      })
    return explicit
  }

  function dispose() {
    disposed = true
    generation++
    for (const cancel of workerWaits) cancel()
    for (const cleanup of cleanups) cleanup()
    for (const handle of timers) options.clearTimeout(handle)
    timers.clear()
    for (const controller of controllers) controller.abort()
    state.busy = false
  }
  return { check, refresh, setRegistration, needRefresh, needReload, dispose }
}
