import type { AccountActivityResponse } from '~/types/cinemaFollows'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'
import { appendActivityItems, groupActivityItems } from '~/utils/cinemaActivity'
import { activityPageDepth } from '~/utils/activityPageDepth'
import { latestActivityEventID } from '~/utils/activityVisit'

export function useAccountActivity() {
  const account = useAccountSession()
  const follows = useCinemaFollows()
  const api = useAccountApi()
  const app = useNuxtApp()
  const notification = useActivityNotification()
  const response = ref<AccountActivityResponse | null>(null)
  const loading = ref(true)
  const morePending = ref(false)
  const error = ref('')
  const moreError = ref('')
  const revalidationError = ref('')
  const newActivityAvailable = ref(false)
  let depth = activityPageDepth(import.meta.client ? window.history : undefined)
  let pagesLoaded = 1
  let restorePages = depth.target
  const departingHeight = ref(0)
  const readingPending = ref('')
  const readingError = ref('')
  let mounted = false
  let generation = 0
  let controller: AbortController | undefined
  let readingController: AbortController | undefined
  let unregister: (() => void) | undefined
  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.enabled &&
    account.session.value.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  function cancelPending() {
    generation++
    controller?.abort()
    readingController?.abort()
    readingPending.value = ''
    morePending.value = false
  }
  function clear() {
    cancelPending()
    response.value = null
    loading.value = false
    error.value = ''
    moreError.value = ''
    revalidationError.value = ''
    newActivityAvailable.value = false
    readingError.value = ''
  }
  const lifetime = useAccountLifetime(() => {
    // Keep outgoing geometry while clearing private rows, so the browser cannot
    // clamp scroll before Vue Router captures its history position.
    if (import.meta.client && response.value)
      departingHeight.value =
        document.getElementById('account-activity')?.getBoundingClientRect()
          .height ?? 0
    unregister?.()
    unregister = undefined
    clear()
  })
  function capture() {
    const active = lifetime.capture()
    const expectedOwner = owner.value
    const current = generation
    return {
      expectedOwner,
      valid: () =>
        mounted &&
        !!unregister &&
        !!expectedOwner &&
        expectedOwner === owner.value &&
        current === generation &&
        active(),
    }
  }
  function check(value: AccountActivityResponse, expected: string) {
    if (value.username !== expected)
      throw new AccountApiError(401, 'authentication_required')
  }
  function fail(cause: unknown) {
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    ) {
      account.clear()
      account.errorMessage.value = accountErrorMessage(cause)
      account.status.value = 'error'
    } else error.value = accountErrorMessage(cause)
  }
  function apply(value: AccountActivityResponse, loadedPages = 1) {
    // A known newer membership cannot authorize an older walk.
    if (
      follows.revision.value !== null &&
      BigInt(follows.revision.value) > BigInt(value.follows_revision)
    ) {
      clear()
      error.value = accountErrorMessage(
        new AccountApiError(409, 'theater_follows_changed'),
      )
      return
    }
    response.value = { ...value, items: appendActivityItems([], value.items) }
    departingHeight.value = 0
    pagesLoaded = loadedPages
    depth.record(pagesLoaded)
    error.value = ''
    moreError.value = ''
    revalidationError.value = ''
    newActivityAvailable.value = false
    loading.value = false
  }
  async function readLoadedStates(ids: string[], signal?: AbortSignal) {
    const states = new Map<string, boolean>()
    let reading: AccountActivityResponse['reading'] | undefined
    for (let start = 0; start < Math.max(ids.length, 1); start += 100) {
      const value = await api.activityReading(
        ids.slice(start, start + 100),
        signal,
      )
      if (value.reading.username !== owner.value)
        throw new AccountApiError(401, 'authentication_required')
      reading = value.reading
      for (const item of value.items) states.set(item.event_id, item.consulted)
    }
    return { reading, states }
  }
  async function refresh() {
    if (!import.meta.client || !mounted || !unregister) return
    clear()
    const token = capture()
    if (!token.expectedOwner || account.writesBlocked.value) return
    loading.value = true
    controller = new AbortController()
    try {
      let value = await api.accountActivity(undefined, controller.signal)
      if (!token.valid()) return
      check(value, token.expectedOwner)
      const target = restorePages
      restorePages = 1
      pagesLoaded = 1
      let continuationError = ''
      const cursors = new Set<string>()
      while (pagesLoaded < target && value.next_cursor) {
        const cursor = value.next_cursor
        if (cursors.has(cursor)) break
        cursors.add(cursor)
        try {
          const next = await api.accountActivity(cursor, controller.signal)
          if (!token.valid()) return
          check(next, token.expectedOwner)
          if (next.follows_revision !== value.follows_revision)
            return await refresh()
          value = {
            ...next,
            items: appendActivityItems(value.items, next.items),
          }
          pagesLoaded++
        } catch (cause) {
          if (!token.valid()) return
          if (
            cause instanceof AccountApiError &&
            cause.code === 'theater_follows_changed'
          )
            return await refresh()
          if (
            cause instanceof AccountApiError &&
            (cause.status === 401 || cause.status === 403)
          )
            throw cause
          continuationError = accountErrorMessage(cause)
          break
        }
      }
      apply(value, pagesLoaded)
      if (response.value) {
        moreError.value = continuationError
      }
    } catch (cause) {
      if (token.valid()) fail(cause)
    } finally {
      if (token.valid()) {
        departingHeight.value = 0
        loading.value = false
      }
    }
  }
  async function revalidate() {
    // Focus checks cancel old work, but keep the current walk and its DOM intact.
    cancelPending()
    const token = capture()
    if (!token.expectedOwner) return () => {}
    loading.value = !response.value
    error.value = ''
    controller = new AbortController()
    try {
      const value = await api.accountActivity(undefined, controller.signal)
      if (!token.valid()) return () => {}
      check(value, token.expectedOwner)
      // Reconcile every loaded row after another tab/device changes reading
      // state, keeping the current walk, continuation cursor and DOM in place.
      const reading = response.value
        ? await readLoadedStates(
            response.value.items.map((item) => item.event_id),
            controller.signal,
          )
        : undefined
      return () => {
        if (!token.valid()) return
        const previous = response.value
        if (!previous || previous.follows_revision !== value.follows_revision) {
          apply(value)
          return
        }
        if (reading?.reading) {
          previous.reading = reading.reading
          previous.items = previous.items.map((item) => ({
            ...item,
            consulted: reading.states.get(item.event_id) ?? item.consulted,
          }))
        }
        // IDs are monotonically allocated decimal strings. Older events filling
        // gaps, changed metadata and a new cursor alone are not new activity.
        const newestKnownId = previous.items.reduce(
          (newest, item) =>
            BigInt(item.event_id) > newest ? BigInt(item.event_id) : newest,
          0n,
        )
        newActivityAvailable.value = value.items.some(
          (item) => BigInt(item.event_id) > newestKnownId,
        )
        // A fresh first page has a different upper bound. Never splice it into
        // the existing walk or replace its continuation cursor implicitly.
        revalidationError.value = ''
        loading.value = false
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
          if (response.value)
            revalidationError.value = accountErrorMessage(cause)
          else fail(cause)
          loading.value = false
        }
      }
    }
  }
  async function loadMore() {
    const previous = response.value
    const cursor = previous?.next_cursor
    if (
      !cursor ||
      !previous ||
      loading.value ||
      morePending.value ||
      account.writesBlocked.value ||
      readingPending.value
    )
      return
    const token = capture()
    if (!token.valid()) return
    const followsRevision = previous.follows_revision
    controller = new AbortController()
    morePending.value = true
    moreError.value = ''
    try {
      const value = await api.accountActivity(cursor, controller.signal)
      if (
        !token.valid() ||
        response.value?.next_cursor !== cursor ||
        response.value.follows_revision !== followsRevision
      )
        return
      check(value, token.expectedOwner)
      if (value.follows_revision !== followsRevision) {
        await refresh()
        return
      }
      response.value = {
        ...value,
        items: appendActivityItems(previous.items, value.items),
      }
      depth.record(++pagesLoaded)
    } catch (cause) {
      if (!token.valid()) return
      if (
        cause instanceof AccountApiError &&
        cause.code === 'theater_follows_changed'
      ) {
        await refresh()
      } else if (
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      )
        fail(cause)
      else moreError.value = accountErrorMessage(cause)
    } finally {
      if (token.valid()) morePending.value = false
    }
  }
  async function changeReading<T>(
    key: string,
    write: (username: string, signal: AbortSignal) => Promise<T>,
    commit: (value: T, current: AccountActivityResponse) => void,
  ) {
    if (
      !import.meta.client ||
      !response.value ||
      loading.value ||
      morePending.value ||
      readingPending.value ||
      account.writesBlocked.value
    )
      return
    const token = capture()
    if (!token.valid()) return
    readingPending.value = key
    readingError.value = ''
    readingController = new AbortController()
    try {
      const value = await write(token.expectedOwner, readingController.signal)
      if (!token.valid() || !response.value) return
      commit(value, response.value)
      void notification.refresh()
      app._accountChannel?.postMessage('activity-visited')
    } catch (cause) {
      if (!token.valid()) return
      readingError.value = `Impossible de mémoriser ce changement. ${accountErrorMessage(cause)}`
      // A lost response may follow a committed write. Reconcile without
      // replaying the mutation or clearing the list/scroll position.
      void account.revalidate()
    } finally {
      if (token.valid()) readingPending.value = ''
    }
  }
  function saveReadingMode(manual: boolean) {
    return changeReading(
      'mode',
      (expected_username, signal) =>
        api.saveActivityReading(
          { expected_username, manual_reading: manual ? 'true' : 'false' },
          signal,
        ),
      (value, current) => {
        if (value.username !== current.username) throw new AccountApiError(401)
        if (
          BigInt(value.last_read_event_id) >
          BigInt(current.reading.last_read_event_id)
        ) {
          for (const item of current.items)
            if (BigInt(item.event_id) <= BigInt(value.last_read_event_id))
              item.consulted = true
        }
        current.reading = value
      },
    )
  }
  function toggleConsulted(eventID: string) {
    const item = response.value?.items.find((item) => item.event_id === eventID)
    if (!item || !response.value?.reading.manual_reading) return
    const consulted = !item.consulted
    return changeReading(
      eventID,
      (expected_username, signal) =>
        api.saveActivityRead(
          {
            expected_username,
            event_id: eventID,
            consulted: consulted ? 'true' : 'false',
          },
          signal,
        ),
      (value, current) => {
        if (value.username !== current.username || value.event_id !== eventID)
          throw new AccountApiError(401)
        const saved = current.items.find((item) => item.event_id === eventID)
        if (saved) saved.consulted = value.consulted
      },
    )
  }
  function markAllConsulted() {
    const latest = latestActivityEventID(response.value?.items ?? [])
    if (latest === '0' || !response.value?.reading.manual_reading) return
    return changeReading(
      'all',
      (expected_username, signal) =>
        api.markAllActivityConsulted(
          { expected_username, last_event_id: latest },
          signal,
        ),
      (value, current) => {
        if (value.username !== current.username || value.scope !== 'followed')
          throw new AccountApiError(401)
        current.reading.last_read_event_id =
          BigInt(value.previous_last_event_id) > BigInt(latest)
            ? value.previous_last_event_id
            : latest
        for (const item of current.items)
          if (BigInt(item.event_id) <= BigInt(latest)) item.consulted = true
      },
    )
  }
  watch(
    owner,
    () => {
      clear()
      if (mounted && owner.value) void refresh()
    },
    { flush: 'sync' },
  )
  watch(account.revision, () => {
    if (!account.revalidating.value) {
      clear()
      if (mounted && owner.value) void refresh()
    }
  })
  watch(
    follows.revision,
    (value) => {
      if (
        value !== null &&
        response.value &&
        BigInt(value) > BigInt(response.value.follows_revision)
      ) {
        clear()
        if (!account.revalidating.value) void refresh()
      }
    },
    { flush: 'sync' },
  )
  watch(account.revalidating, (value) => {
    if (
      !value &&
      mounted &&
      owner.value &&
      !response.value &&
      !error.value &&
      !loading.value
    )
      void refresh()
  })
  onMounted(() => {
    mounted = true
    unregister = account.onRevalidate(revalidate)
    void refresh()
  })
  if (import.meta.client) {
    const runtime = useAccountNavigation()
    const path = useRoute().path
    const router = useRouter()
    const resume = () => {
      if (mounted && router.currentRoute.value.path === path && !unregister) {
        depth = activityPageDepth(window.history)
        restorePages = depth.target
        unregister = account.onRevalidate(revalidate)
        void refresh()
      }
    }
    runtime.arrivals.add(resume)
    onBeforeUnmount(() => runtime.arrivals.delete(resume))
  }
  onBeforeUnmount(() => {
    mounted = false
    unregister?.()
    unregister = undefined
    clear()
  })
  return {
    response,
    loading,
    morePending,
    error,
    moreError,
    revalidationError,
    newActivityAvailable,
    departingHeight,
    readingPending,
    readingError,
    groups: computed(() => groupActivityItems(response.value?.items ?? [])),
    refresh,
    loadMore,
    saveReadingMode,
    toggleConsulted,
    markAllConsulted,
  }
}
