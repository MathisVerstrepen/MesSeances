import type {
  AccountWatchlist,
  WatchlistSearch,
  WatchlistSortOrder,
} from '~/types/watchlist'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

declare module '#app' {
  interface NuxtApp {
    _watchlist?: ReturnType<typeof createWatchlist>
  }
}

// Private state belongs to the client app, never a Nuxt payload or browser storage.
export function useWatchlist() {
  const app = useNuxtApp()
  return (app._watchlist ??= createWatchlist())
}

function createWatchlist() {
  const app = useNuxtApp()
  const api = useAccountApi()
  const account = useAccountSession()
  const snapshot = ref<AccountWatchlist | null>(null)
  const error = ref('')
  const loading = ref(false)
  const saving = ref(false)
  const uncertain = ref(false)
  const scopeKey = ref(0)
  const query = ref('')
  const searchResults = ref<WatchlistSearch | null>(null)
  const searchError = ref('')
  const searching = ref(false)
  let generation = 0
  let searchGeneration = 0
  let controller: AbortController | undefined
  let searchController: AbortController | undefined
  let savingPromise: Promise<string | boolean> | undefined
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
      !!owner.value &&
      !!snapshot.value &&
      !loading.value &&
      !uncertain.value &&
      !account.writesBlocked.value,
  )
  const writesBlocked = computed(() => !ready.value || saving.value)
  const items = computed(() => snapshot.value?.items ?? [])
  const sortOrder = computed(() => snapshot.value?.sort_order)
  const slugs = computed(() => new Set(items.value.map((item) => item.slug)))
  const externalAvailable = computed(
    () => snapshot.value?.external_search_available ?? false,
  )

  function capture() {
    const expectedOwner = owner.value
    const revision = account.revision.value
    const current = generation
    const inScope = () =>
      !!expectedOwner && expectedOwner === owner.value && current === generation
    return {
      expectedOwner,
      inScope,
      valid: () => inScope() && revision === account.revision.value,
    }
  }

  function checkOwner(value: { username: string }, expected: string) {
    if (value.username !== expected)
      throw new AccountApiError(401, 'authentication_required')
  }

  function apply(value: AccountWatchlist) {
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
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    ) {
      account.clear()
      account.errorMessage.value = accountErrorMessage(cause)
      account.status.value = 'error'
      return
    }
    uncertain.value = true
    error.value = accountErrorMessage(cause)
  }

  function dismissSearch() {
    searchGeneration++
    searchController?.abort()
    searchResults.value = null
    searchError.value = ''
    searching.value = false
  }

  function clearSearch() {
    dismissSearch()
    query.value = ''
  }

  function reset() {
    admissionRevision = account.revision.value
    generation++
    controller?.abort()
    snapshot.value = null
    loading.value = false
    saving.value = false
    savingPromise = undefined
    uncertain.value = false
    error.value = ''
    scopeKey.value++
    clearSearch()
  }

  async function readSnapshot() {
    const token = capture()
    if (!token.expectedOwner) return
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    try {
      const value = await api.watchlist(controller.signal)
      if (!token.valid()) return
      checkOwner(value, token.expectedOwner)
      apply(value)
    } catch (cause) {
      if (token.valid()) fail(cause)
    } finally {
      if (token.inScope()) loading.value = false
    }
  }

  async function mutate(
    movie:
      | { slug: string; saved: boolean }
      | { tmdbId: string }
      | { sortOrder: WatchlistSortOrder },
  ): Promise<string | boolean> {
    if (!import.meta.client || writesBlocked.value || !snapshot.value)
      return false
    const token = capture()
    const searchScope = searchGeneration
    const expected = {
      expected_username: token.expectedOwner,
      expected_revision: snapshot.value.revision,
    }
    saving.value = true
    controller?.abort()
    controller = new AbortController()
    savingPromise = (async () => {
      try {
        const value =
          'tmdbId' in movie
            ? await api.importWatchlist(
                { ...expected, tmdb_id: movie.tmdbId },
                controller?.signal,
              )
            : 'sortOrder' in movie
              ? await api.saveWatchlistSort(
                  { ...expected, sort_order: movie.sortOrder },
                  controller?.signal,
                )
              : await api.saveWatchlist(
                  {
                    ...expected,
                    movie_slug: movie.slug,
                    saved: movie.saved ? 'true' : 'false',
                  },
                  controller?.signal,
                )
        if (!token.valid()) return false
        const imported = 'watchlist' in value ? value : null
        const next = 'watchlist' in value ? value.watchlist : value
        checkOwner(next, token.expectedOwner)
        apply(next)
        app._accountChannel?.postMessage('watchlist-changed')
        if (
          imported &&
          'tmdbId' in movie &&
          searchScope === searchGeneration &&
          searchResults.value
        ) {
          const film = next.items.find(
            (item) => item.slug === imported.movie_slug,
          )
          searchResults.value.external = searchResults.value.external.filter(
            (item) => item.tmdb_id !== movie.tmdbId,
          )
          if (
            film &&
            !searchResults.value.catalog.some((item) => item.slug === film.slug)
          )
            searchResults.value.catalog.unshift(film)
        }
        return imported?.movie_slug ?? true
      } catch (cause) {
        if (!token.valid()) return false
        if (
          cause instanceof AccountApiError &&
          (cause.status === 401 || cause.status === 403)
        ) {
          fail(cause)
          return false
        }
        uncertain.value = true
        // A timeout can follow a commit. Read back, never replay a mutation.
        await readSnapshot()
        if (token.valid()) error.value = accountErrorMessage(cause)
        return false
      } finally {
        if (token.inScope()) saving.value = false
      }
    })()
    const pending = savingPromise
    try {
      return await pending
    } finally {
      if (savingPromise === pending) savingPromise = undefined
    }
  }

  async function search() {
    if (!import.meta.client || writesBlocked.value || searching.value) return
    const text = query.value.trim()
    if ([...text].length < 2 || [...text].length > 200) {
      searchError.value =
        'Saisissez entre 2 et 200 caractères pour rechercher un film.'
      return
    }
    const token = capture()
    const current = ++searchGeneration
    searchController?.abort()
    searchController = new AbortController()
    searching.value = true
    searchError.value = ''
    searchResults.value = null
    try {
      const value = await api.searchWatchlist(
        { expected_username: token.expectedOwner, query: text },
        searchController.signal,
      )
      if (!token.valid() || current !== searchGeneration) return
      checkOwner(value, token.expectedOwner)
      searchResults.value = value
    } catch (cause) {
      if (!token.valid() || current !== searchGeneration) return
      if (
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      )
        fail(cause)
      else searchError.value = accountErrorMessage(cause)
    } finally {
      if (token.inScope() && current === searchGeneration)
        searching.value = false
    }
  }

  async function revalidate() {
    const token = capture()
    await savingPromise
    if (!token.valid()) return () => {}
    controller?.abort()
    controller = new AbortController()
    try {
      const value = await api.watchlist(controller.signal)
      if (token.valid()) checkOwner(value, token.expectedOwner)
      return () => {
        if (token.valid()) {
          apply(value)
          loading.value = false
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
        if (account.revalidating.value) {
          searchGeneration++
          searchController?.abort()
          searching.value = false
          return
        }
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

  async function retry() {
    if (saving.value || account.revalidating.value) return
    await account.revalidate()
  }

  return {
    owner,
    ready,
    writesBlocked,
    items,
    sortOrder,
    slugs,
    externalAvailable,
    error: readonly(error),
    loading: readonly(loading),
    saving: readonly(saving),
    scopeKey: readonly(scopeKey),
    query,
    searchResults: readonly(searchResults),
    searchError: readonly(searchError),
    searching: readonly(searching),
    startSynchronization,
    retry,
    search,
    dismissSearch,
    clearSearch,
    save: (slug: string, saved: boolean) => mutate({ slug, saved }),
    importMovie: (tmdbId: string) => mutate({ tmdbId }),
    saveSort: (sortOrder: WatchlistSortOrder) => mutate({ sortOrder }),
  }
}
