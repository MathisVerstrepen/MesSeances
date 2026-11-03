import type { MaybeRefOrGetter } from 'vue'
import {
  activityEventID,
  latestActivityEventID,
  visitGuestActivity,
} from '~/utils/activityVisit'

export function useActivityVisit(
  scope: MaybeRefOrGetter<string>,
  items: MaybeRefOrGetter<readonly { event_id: string }[]>,
  ready: MaybeRefOrGetter<boolean>,
) {
  const account = useAccountSession()
  const api = useAccountApi()
  const previousLastEventID = ref<string | null>(null)
  const error = ref(false)
  const active = ref(false)
  const owner = computed(() => {
    const session = account.session.value
    if (account.status.value !== 'ready' || !session) return ''
    if (!session.enabled || session.state === 'anonymous') return 'guest'
    return session.state === 'complete' && session.account?.username
      ? `account:${session.account.username}`
      : ''
  })
  let identity = ''
  let generation = 0
  let revision = account.revision.value
  let pending = false
  let attempted: string | null = null
  let controller: AbortController | undefined

  function cancel() {
    generation++
    controller?.abort()
    pending = false
    attempted = null
  }
  function reset() {
    cancel()
    identity = ''
    previousLastEventID.value = null
    error.value = false
  }
  async function synchronize() {
    // A background session refresh can temporarily remove the known owner.
    // Suspend writes and hide markers, retaining this visit's baseline until
    // the same owner returns. A confirmed different owner resets it below.
    if (!owner.value) {
      cancel()
      error.value = false
      return
    }
    const feed = toValue(scope)
    const currentIdentity = `${owner.value}/${feed}`
    if (identity !== currentIdentity) {
      reset()
      identity = currentIdentity
    }
    if (revision !== account.revision.value) {
      cancel()
      revision = account.revision.value
    }
    if (
      !import.meta.client ||
      !active.value ||
      !toValue(ready) ||
      !feed ||
      !owner.value ||
      account.writesBlocked.value ||
      pending
    )
      return
    const latest = latestActivityEventID(toValue(items))
    if (attempted !== null && BigInt(latest) <= BigInt(attempted)) return
    attempted = latest
    const token = generation
    const expectedOwner = owner.value
    const expectedRevision = account.revision.value
    const valid = () =>
      active.value &&
      token === generation &&
      expectedOwner === owner.value &&
      feed === toValue(scope) &&
      expectedRevision === account.revision.value
    pending = true
    error.value = false
    controller = new AbortController()
    try {
      let previous: string
      if (expectedOwner === 'guest') {
        previous = visitGuestActivity(window.localStorage, feed, latest)
      } else {
        const value = await api.visitActivity(
          {
            expected_username: expectedOwner.slice('account:'.length),
            scope: feed,
            last_event_id: latest,
          },
          controller.signal,
        )
        if (!valid()) return
        if (
          value.username !== expectedOwner.slice('account:'.length) ||
          value.scope !== feed ||
          activityEventID(value.previous_last_event_id) === null
        )
          throw new Error('Invalid activity visit')
        previous = value.previous_last_event_id
      }
      if (valid()) {
        // Keep the previous visit fixed across pagination and refreshes.
        previousLastEventID.value ??= previous
      }
    } catch {
      if (valid()) error.value = true
    } finally {
      if (valid()) {
        pending = false
        // Pagination may have completed while this write was in flight.
        if (!error.value) void synchronize()
      }
    }
  }
  watch(
    [
      () => toValue(scope),
      owner,
      () => toValue(ready),
      () => latestActivityEventID(toValue(items)),
      account.writesBlocked,
      account.revision,
      active,
    ],
    () => {
      void synchronize()
    },
    { flush: 'sync' },
  )
  onMounted(() => {
    active.value = true
  })
  onBeforeUnmount(() => {
    active.value = false
    reset()
  })
  if (import.meta.client) {
    const runtime = useAccountNavigation()
    const path = useRoute().path
    const router = useRouter()
    const depart = () => {
      active.value = false
      reset()
    }
    const arrive = () => {
      if (router.currentRoute.value.path === path) active.value = true
    }
    runtime.starts.add(depart)
    runtime.arrivals.add(arrive)
    onBeforeUnmount(() => {
      runtime.starts.delete(depart)
      runtime.arrivals.delete(arrive)
    })
  }
  function retry() {
    attempted = null
    void synchronize()
  }
  return {
    previousLastEventID: computed(() =>
      owner.value ? previousLastEventID.value : null,
    ),
    error,
    retry,
  }
}
