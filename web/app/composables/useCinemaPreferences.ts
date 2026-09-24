import type { Theater } from '~/types/api'
import type { AccountTheaterPreferences } from '~/types/account'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

const STORAGE_KEY = 'messeances.favoriteTheaterIds.v1'

declare module '#app' {
  interface NuxtApp {
    _cinemaPreferences?: ReturnType<typeof createCinemaPreferences>
  }
}

// Only the public catalog uses Nuxt payload state. Private snapshots, promises,
// storage fallback and subscriptions belong to this app, never to an SSR module.
export function useCinemaPreferences() {
  const app = useNuxtApp()
  return (app._cinemaPreferences ??= createCinemaPreferences())
}

function createCinemaPreferences() {
  const app = useNuxtApp()
  const api = useMesSeancesApi()
  const accountApi = useAccountApi()
  const account = useAccountSession()
  const theaters = useState<Theater[]>('cinema-preferences:theaters', () => [])
  const deviceIds = ref<string[]>([])
  const snapshot = ref<AccountTheaterPreferences | null>(null)
  const catalogReady = ref(false)
  const catalogError = ref<string | null>(null)
  const syncError = ref<string | null>(null)
  const synchronizing = ref(false)
  const isSaving = ref(false)
  const selectionScopeKey = ref(0)
  const needsReconciliation = ref(false)
  let initializationPromise: Promise<void> | undefined
  let savingPromise: Promise<boolean> | undefined
  let memoryFavoriteIds: string[] | null = null
  let importIds: string[] = []
  let importAttempted = false
  let generation = 0
  let admissionRevision = account.revision.value
  let controller: AbortController | undefined
  let started = false

  const owner = computed(() => {
    const value = account.session.value
    return account.status.value === 'ready' &&
      value?.enabled &&
      value.state === 'complete'
      ? (value.account?.username ?? '')
      : ''
  })
  const deviceMode = computed(
    () =>
      account.status.value === 'ready' &&
      !!account.session.value &&
      !owner.value,
  )
  const isInitialized = computed(
    () => catalogReady.value && (deviceMode.value || !!snapshot.value),
  )
  const favoriteTheaterIds = computed(() => {
    if (!isInitialized.value) return []
    return orderCurrentIds(
      owner.value && snapshot.value?.revision !== '0'
        ? (snapshot.value?.theater_ids ?? [])
        : deviceIds.value,
    )
  })
  const favoriteTheaters = computed(() => {
    const selected = new Set(favoriteTheaterIds.value)
    return theaters.value.filter((theater) => selected.has(theater.id))
  })
  const error = computed(
    () =>
      catalogError.value ||
      (!isInitialized.value
        ? syncError.value ||
          (account.status.value === 'error' ? account.errorMessage.value : null)
        : null),
  )
  const writesBlocked = computed(
    () =>
      !isInitialized.value ||
      account.writesBlocked.value ||
      synchronizing.value ||
      isSaving.value ||
      needsReconciliation.value,
  )
  const isLoading = computed(() => !error.value && !isInitialized.value)

  function orderCurrentIds(ids: readonly string[]): string[] {
    const selected = new Set(ids)
    return theaters.value
      .filter((theater) => selected.has(theater.id))
      .map((theater) => theater.id)
  }

  function storedFavoriteIds(): string[] {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw === null) return memoryFavoriteIds ?? []
      const value: unknown = JSON.parse(raw)
      if (!Array.isArray(value)) return memoryFavoriteIds ?? []
      return [
        ...new Set(
          value.filter(
            (id): id is string =>
              id === String(id) && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(id),
          ),
        ),
      ].slice(0, 4096)
    } catch {
      return memoryFavoriteIds ?? []
    }
  }

  function persist(ids: string[]) {
    memoryFavoriteIds = [...ids]
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(ids))
    } catch {
      // Device selection remains usable in this app when browser storage fails.
    }
  }

  function capture() {
    const expectedOwner = owner.value
    const sessionRevision = account.revision.value
    const current = generation
    return {
      expectedOwner,
      inScope: () =>
        !!expectedOwner &&
        expectedOwner === owner.value &&
        current === generation,
      valid: () =>
        !!expectedOwner &&
        expectedOwner === owner.value &&
        current === generation &&
        sessionRevision === account.revision.value,
    }
  }

  function checkOwner(value: AccountTheaterPreferences, expected: string) {
    if (value.username !== expected)
      throw new AccountApiError(401, 'authentication_required')
  }

  function apply(value: AccountTheaterPreferences) {
    const previous = snapshot.value
    if (previous && BigInt(value.revision) < BigInt(previous.revision)) return
    snapshot.value = value
    needsReconciliation.value = false
    syncError.value = null
  }

  function fail(cause: unknown) {
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    ) {
      account.clear()
      account.errorMessage.value = accountErrorMessage(cause)
      account.status.value = 'error'
      return
    }
    needsReconciliation.value = true
    syncError.value = accountErrorMessage(cause)
  }

  async function readSnapshot() {
    const token = capture()
    controller?.abort()
    controller = new AbortController()
    synchronizing.value = true
    try {
      const value = await accountApi.theaterPreferences(controller.signal)
      if (!token.valid()) return
      checkOwner(value, token.expectedOwner)
      apply(value)
    } catch (cause) {
      if (token.valid()) fail(cause)
    } finally {
      if (token.valid()) synchronizing.value = false
    }
  }

  async function save(ids: string[]): Promise<boolean> {
    if (writesBlocked.value || !snapshot.value) return false
    const token = capture()
    const expectedRevision = snapshot.value.revision
    isSaving.value = true
    controller?.abort()
    controller = new AbortController()
    savingPromise = (async () => {
      try {
        const value = await accountApi.saveTheaterPreferences(
          {
            expected_username: token.expectedOwner,
            expected_revision: expectedRevision,
            theater_ids: [...new Set(ids)].sort().join(','),
          },
          controller?.signal,
        )
        app._accountChannel?.postMessage('theaters-changed')
        if (!token.valid()) return false
        checkOwner(value, token.expectedOwner)
        apply(value)
        return true
      } catch (cause) {
        if (!token.valid()) return false
        if (
          cause instanceof AccountApiError &&
          (cause.status === 401 || cause.status === 403)
        ) {
          fail(cause)
          return false
        }
        needsReconciliation.value = true
        // Read back even an ambiguous commit, but never replay the write.
        await readSnapshot()
        if (token.valid()) syncError.value = accountErrorMessage(cause)
        return false
      } finally {
        // A revalidation may have changed session revision while waiting for us.
        if (token.inScope()) isSaving.value = false
      }
    })()
    const pending = savingPromise
    try {
      return await pending
    } finally {
      if (savingPromise === pending) savingPromise = undefined
    }
  }

  async function bootstrap() {
    if (
      !owner.value ||
      account.writesBlocked.value ||
      !catalogReady.value ||
      synchronizing.value ||
      isSaving.value ||
      needsReconciliation.value
    )
      return
    if (!snapshot.value) await readSnapshot()
    if (
      snapshot.value?.revision === '0' &&
      !importAttempted &&
      !writesBlocked.value
    ) {
      importAttempted = true
      if (importIds.length) await save(importIds)
    }
  }

  async function revalidate() {
    // Revalidation owns its read/commit phase; never import from this callback.
    await savingPromise
    const token = capture()
    try {
      const value = await accountApi.theaterPreferences()
      if (token.valid()) checkOwner(value, token.expectedOwner)
      return () => {
        if (token.valid()) {
          apply(value)
          synchronizing.value = false
        }
      }
    } catch (cause) {
      if (
        token.valid() &&
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      )
        throw cause
      return () => {
        if (token.valid()) {
          fail(cause)
          synchronizing.value = false
        }
      }
    }
  }

  function startSynchronization() {
    if (!import.meta.client || started) return
    started = true
    // App lifetime, not the lifetime of the first page calling the facade.
    effectScope(true).run(() => {
      account.onRevalidate(revalidate)
      watch(
        [owner, () => account.status.value],
        () => {
          admissionRevision = account.revision.value
          generation++
          controller?.abort()
          snapshot.value = null
          synchronizing.value = false
          isSaving.value = false
          needsReconciliation.value = false
          syncError.value = null
          importAttempted = false
          selectionScopeKey.value++
        },
        { flush: 'sync' },
      )
      // accept() can rotate a session without changing its visible username.
      // Observe after the session's synchronous transition, not between the
      // revision increment and revalidating flag in revalidate().
      watch(account.revision, () => {
        if (admissionRevision === account.revision.value) return
        admissionRevision = account.revision.value
        if (!owner.value || account.revalidating.value) return
        generation++
        controller?.abort()
        snapshot.value = null
        synchronizing.value = false
        isSaving.value = false
        needsReconciliation.value = false
        importAttempted = false
        selectionScopeKey.value++
        void bootstrap()
      })
      watch(
        [owner, catalogReady, account.writesBlocked],
        () => {
          void bootstrap()
        },
        { immediate: true },
      )
    })
  }

  async function initialize(
    initialTheaters?: readonly Theater[],
  ): Promise<void> {
    if (!import.meta.client) return
    startSynchronization()
    if (catalogReady.value) {
      await bootstrap()
      return
    }
    if (initializationPromise) return initializationPromise
    initializationPromise = (async () => {
      catalogError.value = null
      try {
        theaters.value = initialTheaters
          ? [...initialTheaters]
          : await api.theaters()
        importIds = orderCurrentIds(storedFavoriteIds())
        deviceIds.value = [...importIds]
        if (!deviceIds.value.length) {
          let defaultIds: string[] = []
          try {
            defaultIds = (await api.theaters({ city: 'Paris' })).map(
              (theater) => theater.id,
            )
          } catch {
            /* National fallback remains available. */
          }
          deviceIds.value = orderCurrentIds(defaultIds)
          if (!deviceIds.value.length && theaters.value[0])
            deviceIds.value = [theaters.value[0].id]
        }
        // Defaults are provisional, not a stored user choice to import next visit.
        catalogReady.value = true
        await bootstrap()
      } catch (cause) {
        catalogError.value = getFrenchApiError(cause)
      } finally {
        initializationPromise = undefined
      }
    })()
    return initializationPromise
  }

  async function setFavoriteTheaterIds(ids: string[]): Promise<boolean> {
    if (!import.meta.client || writesBlocked.value) return false
    const nextIds = orderCurrentIds(ids)
    if (theaters.value.length > 0 && nextIds.length === 0) return false
    if (deviceMode.value) {
      deviceIds.value = nextIds
      importIds = [...nextIds]
      persist(nextIds)
      return true
    }
    const catalogIds = new Set(theaters.value.map((theater) => theater.id))
    const unavailableIds =
      snapshot.value?.theater_ids.filter((id) => !catalogIds.has(id)) ?? []
    return save([...nextIds, ...unavailableIds])
  }

  function toggleFavoriteTheater(id: string): Promise<boolean> {
    const selected = new Set(favoriteTheaterIds.value)
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
    return setFavoriteTheaterIds([...selected])
  }

  async function retrySynchronization() {
    if (isSaving.value || account.revalidating.value) return
    if (!catalogReady.value) await initialize()
    await account.revalidate()
  }

  return {
    theaters: readonly(theaters),
    favoriteTheaterIds,
    favoriteTheaters,
    catalogReady: readonly(catalogReady),
    isInitialized,
    isLoading,
    error,
    syncError: readonly(syncError),
    isSaving: readonly(isSaving),
    writesBlocked,
    selectionScopeKey: readonly(selectionScopeKey),
    initialize,
    setFavoriteTheaterIds,
    toggleFavoriteTheater,
    retrySynchronization,
    startSynchronization,
  }
}
