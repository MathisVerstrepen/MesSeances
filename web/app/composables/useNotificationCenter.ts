import type {
  AccountNotification,
  NotificationStatus,
} from '~/types/notifications'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

declare module '#app' {
  interface NuxtApp {
    _notificationCenter?: ReturnType<typeof createNotificationCenter>
  }
}

const PAGE_SIZE = 20

// One private inbox shared by the header bell and the notifications page.
// Nothing here is persisted, rendered on the server or kept after logout.
export function useNotificationCenter() {
  const app = useNuxtApp()
  return (app._notificationCenter ??= createNotificationCenter())
}

function createNotificationCenter() {
  const app = useNuxtApp()
  const account = useAccountSession()
  const api = useAccountApi()
  const status = ref<NotificationStatus | null>(null)
  const items = ref<AccountNotification[]>([])
  const nextCursor = ref<string | null>(null)
  const loaded = ref(false)
  const loading = ref(false)
  const morePending = ref(false)
  const error = ref('')
  const actionError = ref('')
  const pendingRead = ref(false)
  let started = false
  let statusRequest = 0
  let listRequest = 0
  let statusController: AbortController | undefined
  let listController: AbortController | undefined

  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.enabled &&
    account.session.value.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const unreadCount = computed(() =>
    owner.value && status.value?.username === owner.value
      ? status.value.unread_count
      : 0,
  )

  function reset() {
    statusRequest++
    listRequest++
    statusController?.abort()
    listController?.abort()
    status.value = null
    items.value = []
    nextCursor.value = null
    loaded.value = false
    loading.value = false
    morePending.value = false
    error.value = ''
    actionError.value = ''
  }

  function acceptStatus(value: NotificationStatus) {
    if (value.username !== owner.value) return
    const previous = status.value
    status.value = value
    // A newer delivery invalidates the cached first page of the bell panel.
    if (loaded.value && previous && previous.latest_id !== value.latest_id)
      loaded.value = false
  }

  async function prepareStatus() {
    const expectedOwner = owner.value
    const revision = account.revision.value
    const current = ++statusRequest
    statusController?.abort()
    statusController = new AbortController()
    const valid = () =>
      started &&
      !!expectedOwner &&
      expectedOwner === owner.value &&
      revision === account.revision.value &&
      current === statusRequest
    try {
      const value = await api.notificationStatus(statusController.signal)
      if (valid() && value.username !== expectedOwner)
        throw new AccountApiError(401, 'authentication_required')
      return () => {
        if (valid()) acceptStatus(value)
      }
    } catch (cause) {
      if (
        valid() &&
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      )
        throw cause
      return () => {}
    }
  }

  async function refresh() {
    if (
      !import.meta.client ||
      !started ||
      !owner.value ||
      account.writesBlocked.value
    )
      return
    const expectedOwner = owner.value
    const revision = account.revision.value
    try {
      const commit = await prepareStatus()
      commit()
    } catch {
      if (expectedOwner === owner.value && revision === account.revision.value)
        account.clear()
    }
  }

  async function fetchPage(before?: string) {
    const expectedOwner = owner.value
    // Private inbox rows are never fetched while rendering on the server.
    if (!import.meta.client || !expectedOwner) return
    const current = ++listRequest
    listController?.abort()
    listController = new AbortController()
    const valid = () => expectedOwner === owner.value && current === listRequest
    if (before) morePending.value = true
    else loading.value = true
    error.value = ''
    try {
      const value = await api.notifications(
        { limit: PAGE_SIZE, before },
        listController.signal,
      )
      if (!valid()) return
      if (value.username !== expectedOwner) {
        account.clear()
        return
      }
      status.value = {
        username: value.username,
        unread_count: value.unread_count,
        latest_id: value.latest_id,
      }
      items.value = before ? [...items.value, ...value.items] : value.items
      nextCursor.value = value.next_cursor
      loaded.value = true
    } catch (cause) {
      if (!valid()) return
      if (
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      ) {
        account.clear()
        return
      }
      error.value = accountErrorMessage(cause)
    } finally {
      if (current === listRequest) {
        loading.value = false
        morePending.value = false
      }
    }
  }

  function load(force = false) {
    if (!force && (loaded.value || loading.value)) return Promise.resolve()
    return fetchPage()
  }

  function loadMore() {
    if (!nextCursor.value || morePending.value) return Promise.resolve()
    return fetchPage(nextCursor.value)
  }

  async function write(
    ids: string[],
    request: (username: string) => Promise<NotificationStatus>,
  ) {
    const expectedOwner = owner.value
    if (!expectedOwner || account.writesBlocked.value || pendingRead.value)
      return false
    const previous = items.value
    const marked = new Set(ids)
    items.value = items.value.map((item) =>
      marked.has(item.id) ? { ...item, read: true } : item,
    )
    pendingRead.value = true
    actionError.value = ''
    try {
      const value = await request(expectedOwner)
      if (expectedOwner !== owner.value) return false
      acceptStatus(value)
      app._accountChannel?.postMessage('notifications-changed')
      return true
    } catch (cause) {
      if (expectedOwner !== owner.value) return false
      items.value = previous
      actionError.value = accountErrorMessage(cause)
      return false
    } finally {
      pendingRead.value = false
    }
  }

  function markRead(item: AccountNotification) {
    if (item.read) return Promise.resolve(true)
    return write([item.id], (username) =>
      api.markNotificationsRead({
        expected_username: username,
        ids: [item.id],
      }),
    )
  }

  function markAllRead() {
    const unread = items.value.filter((item) => !item.read)
    // Bound by what this view displayed: later deliveries stay unread.
    const through = items.value[0]?.id ?? ''
    if (!through) return Promise.resolve(false)
    return write(
      unread.map((item) => item.id),
      (username) =>
        api.markAllNotificationsRead({
          expected_username: username,
          through_id: through,
        }),
    )
  }

  function startSynchronization() {
    if (!import.meta.client || started) return
    started = true
    effectScope(true).run(() => {
      account.onRevalidate(prepareStatus)
      watch(owner, reset, { flush: 'sync' })
      watch(
        owner,
        () => {
          if (owner.value) void refresh()
        },
        { immediate: true },
      )
      watch(account.writesBlocked, (blocked) => {
        if (!blocked && !status.value) void refresh()
      })
    })
  }

  return {
    owner,
    status: readonly(status),
    unreadCount,
    items: computed(() => items.value),
    nextCursor: readonly(nextCursor),
    loaded: readonly(loaded),
    loading: readonly(loading),
    morePending: readonly(morePending),
    error: readonly(error),
    actionError: readonly(actionError),
    pendingRead: readonly(pendingRead),
    refresh,
    load,
    loadMore,
    markRead,
    markAllRead,
    startSynchronization,
  }
}
