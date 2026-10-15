import type { WatchedPreferences, WatchedSnapshot } from '~/types/watched'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

declare module '#app' {
  interface NuxtApp {
    _watched?: ReturnType<typeof createWatched>
  }
}

export function useWatched() {
  const app = useNuxtApp()
  return (app._watched ??= createWatched())
}

function createWatched() {
  const app = useNuxtApp()
  const api = useAccountApi()
  const account = useAccountSession()
  const watchlist = useWatchlist()
  const lane = useCollectionWrite()
  const snapshot = ref<WatchedSnapshot | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const uncertain = ref(false)
  const error = ref('')
  const scopeKey = ref(0)
  let generation = 0
  let readGeneration = 0
  let controller: AbortController | undefined
  let pending: Promise<boolean> | undefined
  let started = false
  let admissionRevision = account.revision.value
  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.enabled &&
    account.session.value.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const ready = computed(
    () =>
      !!owner.value && !!snapshot.value && !loading.value && !uncertain.value,
  )
  const writesBlocked = computed<boolean>(
    (): boolean =>
      !import.meta.client ||
      account.writesBlocked.value ||
      !ready.value ||
      lane.busy.value ||
      (!!snapshot.value?.preferences.auto_remove_watchlist &&
        !watchlist.ready.value),
  )
  const items = computed(() => snapshot.value?.items ?? [])
  const slugs = computed(() => new Set(items.value.map((item) => item.slug)))
  const preferences = computed(() => snapshot.value?.preferences)

  function capture() {
    const username = owner.value
    const current = generation
    const revision = account.revision.value
    const inScope = () =>
      !!username && username === owner.value && current === generation
    return {
      username,
      inScope,
      valid: () => inScope() && revision === account.revision.value,
    }
  }
  function check(value: WatchedSnapshot, username: string) {
    if (value.username !== username)
      throw new AccountApiError(401, 'authentication_required')
  }
  function apply(value: WatchedSnapshot) {
    if (
      snapshot.value &&
      BigInt(value.revision) < BigInt(snapshot.value.revision)
    )
      return
    snapshot.value = value
    uncertain.value = false
    error.value = ''
  }
  function fail(cause: unknown) {
    if (cause instanceof AccountApiError && [401, 403].includes(cause.status)) {
      account.clear()
      account.errorMessage.value = accountErrorMessage(cause)
      account.status.value = 'error'
      return
    }
    uncertain.value = true
    error.value = accountErrorMessage(cause)
  }
  function reset() {
    lane.reset()
    generation++
    readGeneration++
    controller?.abort()
    snapshot.value = null
    loading.value = false
    saving.value = false
    uncertain.value = false
    error.value = ''
    scopeKey.value++
    admissionRevision = account.revision.value
  }
  async function readSnapshot() {
    const token = capture()
    if (!token.username) return
    const current = ++readGeneration
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    try {
      const value = await api.watched(controller.signal)
      if (!token.valid() || current !== readGeneration) return
      check(value, token.username)
      apply(value)
    } catch (cause) {
      if (token.valid() && current === readGeneration) fail(cause)
    } finally {
      if (token.inScope() && current === readGeneration) loading.value = false
    }
  }
  async function reconcile() {
    const token = capture()
    if (!token.username) return
    const current = ++readGeneration
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    uncertain.value = true
    try {
      const [watched, saved] = await Promise.all([
        api.watched(controller.signal),
        api.watchlist(controller.signal),
      ])
      if (!token.valid() || current !== readGeneration) return
      check(watched, token.username)
      if (saved.username !== token.username)
        throw new AccountApiError(401, 'authentication_required')
      // All owner/generation checks precede the synchronous pair commit.
      watchlist.applySnapshot(saved)
      apply(watched)
    } catch (cause) {
      if (token.valid() && current === readGeneration) fail(cause)
    } finally {
      if (token.inScope() && current === readGeneration) loading.value = false
    }
  }
  async function mutate(
    change: { slug: string; watched: boolean } | WatchedPreferences,
  ): Promise<boolean> {
    if (writesBlocked.value || !snapshot.value) return false
    const removing =
      'slug' in change &&
      change.watched &&
      preferences.value?.auto_remove_watchlist
    if (removing && !watchlist.ready.value) return false
    const token = capture()
    const expected = {
      expected_username: token.username,
      expected_revision: snapshot.value.revision,
    }
    const release = lane.acquire()
    saving.value = true
    readGeneration++
    controller?.abort()
    controller = new AbortController()
    pending = (async () => {
      try {
        const result =
          'slug' in change
            ? await api.saveWatched(
                {
                  ...expected,
                  movie_slug: change.slug,
                  watched: change.watched ? 'true' : 'false',
                  expected_watchlist_revision:
                    watchlist.snapshot.value?.revision ?? '0',
                },
                controller?.signal,
              )
            : await api.saveWatchedPreferences(
                {
                  ...expected,
                  ...change,
                  auto_remove_watchlist: change.auto_remove_watchlist
                    ? 'true'
                    : 'false',
                },
                controller?.signal,
              )
        if (!token.valid()) return false
        const value = 'watched' in result ? result.watched : result
        const saved = 'watched' in result ? result.watchlist : null
        check(value, token.username)
        if (
          snapshot.value &&
          BigInt(value.revision) < BigInt(snapshot.value.revision)
        )
          throw new AccountApiError(409, 'watched_changed')
        if (
          saved &&
          (saved.username !== token.username ||
            watchlist.owner.value !== token.username)
        )
          throw new AccountApiError(401, 'authentication_required')
        if (
          saved &&
          watchlist.snapshot.value &&
          BigInt(saved.revision) < BigInt(watchlist.snapshot.value.revision)
        )
          throw new AccountApiError(409, 'watchlist_changed')
        if (saved) watchlist.applySnapshot(saved)
        apply(value)
        app._accountChannel?.postMessage('watched-changed')
        if (saved) app._accountChannel?.postMessage('watchlist-changed')
        return true
      } catch (cause) {
        if (!token.valid()) return false
        if (
          cause instanceof AccountApiError &&
          [401, 403].includes(cause.status)
        )
          fail(cause)
        else {
          await reconcile()
          if (token.valid()) error.value = accountErrorMessage(cause)
        }
        return false
      } finally {
        release()
        if (token.inScope()) saving.value = false
      }
    })()
    const operation = pending
    try {
      return await operation
    } finally {
      if (pending === operation) pending = undefined
    }
  }
  async function revalidate() {
    await pending
    await lane.wait()
    const token = capture()
    if (!token.username) return () => {}
    const current = ++readGeneration
    controller?.abort()
    controller = new AbortController()
    try {
      const value = await api.watched(controller.signal)
      if (token.valid()) check(value, token.username)
      return () => {
        if (token.valid() && current === readGeneration) {
          apply(value)
          loading.value = false
        }
      }
    } catch (cause) {
      if (
        token.valid() &&
        cause instanceof AccountApiError &&
        [401, 403].includes(cause.status)
      )
        throw cause
      return () => {
        if (token.valid() && current === readGeneration) {
          fail(cause)
          loading.value = false
        }
      }
    }
  }
  function startSynchronization() {
    if (!import.meta.client || started) return
    started = true
    effectScope(true).run(() => {
      account.onRevalidate(revalidate)
      watch([owner, () => account.status.value], reset, { flush: 'sync' })
      watch(account.revision, () => {
        if (admissionRevision === account.revision.value) return
        admissionRevision = account.revision.value
        if (account.revalidating.value) return
        reset()
        if (owner.value) void readSnapshot()
      })
      watch(
        [owner, account.writesBlocked],
        () => {
          if (
            owner.value &&
            !account.writesBlocked.value &&
            !snapshot.value &&
            !loading.value &&
            !uncertain.value
          )
            void readSnapshot()
        },
        { immediate: true },
      )
    })
  }
  return {
    owner,
    ready,
    writesBlocked,
    items,
    slugs,
    preferences,
    snapshot: readonly(snapshot),
    loading: readonly(loading),
    saving: readonly(saving),
    error: readonly(error),
    scopeKey: readonly(scopeKey),
    startSynchronization,
    reconcile,
    refreshCatalog: readSnapshot,
    retry: () => account.revalidate(),
    save: (slug: string, watched: boolean) => mutate({ slug, watched }),
    savePreferences: (value: WatchedPreferences) => mutate(value),
  }
}
