import type { ActivityNotification } from '~/types/cinemaFollows'
import { AccountApiError } from '~/utils/accountState'

declare module '#app' {
  interface NuxtApp {
    _activityNotification?: ReturnType<typeof createActivityNotification>
  }
}

// One private, client-admitted status shared by the header and account menus.
export function useActivityNotification() {
  const app = useNuxtApp()
  return (app._activityNotification ??= createActivityNotification())
}

function createActivityNotification() {
  const account = useAccountSession()
  const follows = useCinemaFollows()
  const api = useAccountApi()
  const snapshot = ref<ActivityNotification | null>(null)
  let started = false
  let request = 0
  let controller: AbortController | undefined
  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.enabled &&
    account.session.value.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const hasNew = computed(
    () =>
      !!owner.value &&
      snapshot.value?.username === owner.value &&
      snapshot.value.follows_revision === follows.revision.value &&
      snapshot.value.has_new_activity,
  )
  function reset() {
    request++
    controller?.abort()
    snapshot.value = null
  }
  async function prepareRead() {
    const expectedOwner = owner.value
    const revision = account.revision.value
    const current = ++request
    controller?.abort()
    controller = new AbortController()
    const valid = () =>
      started &&
      !!expectedOwner &&
      expectedOwner === owner.value &&
      revision === account.revision.value &&
      current === request
    try {
      const value = await api.activityNotification(controller.signal)
      if (valid() && value.username !== expectedOwner)
        throw new AccountApiError(401, 'authentication_required')
      return () => {
        if (valid()) snapshot.value = value
      }
    } catch (cause) {
      if (
        valid() &&
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      )
        throw cause
      return () => {
        if (valid()) snapshot.value = null
      }
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
      const commit = await prepareRead()
      commit()
    } catch {
      if (expectedOwner === owner.value && revision === account.revision.value)
        account.clear()
    }
  }
  function startSynchronization() {
    if (!import.meta.client || started) return
    started = true
    effectScope(true).run(() => {
      account.onRevalidate(prepareRead)
      watch(owner, reset, { flush: 'sync' })
      watch(
        [owner, follows.revision],
        () => {
          if (owner.value && follows.revision.value !== null) void refresh()
        },
        { immediate: true },
      )
      watch(account.writesBlocked, (blocked) => {
        if (!blocked && !snapshot.value && follows.revision.value !== null)
          void refresh()
      })
    })
  }
  return { hasNew, refresh, startSynchronization }
}
