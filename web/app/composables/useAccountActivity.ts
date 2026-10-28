import type { AccountActivityResponse } from '~/types/cinemaFollows'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'
import { appendActivityItems, groupActivityItems } from '~/utils/cinemaActivity'

export function useAccountActivity() {
  const account = useAccountSession()
  const follows = useCinemaFollows()
  const api = useAccountApi()
  const response = ref<AccountActivityResponse | null>(null)
  const loading = ref(true)
  const morePending = ref(false)
  const error = ref('')
  const moreError = ref('')
  const revalidationError = ref('')
  const newActivityAvailable = ref(false)
  let mounted = false
  let generation = 0
  let controller: AbortController | undefined
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
  }
  const lifetime = useAccountLifetime(() => {
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
  function apply(value: AccountActivityResponse) {
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
    error.value = ''
    moreError.value = ''
    revalidationError.value = ''
    newActivityAvailable.value = false
    loading.value = false
  }
  async function refresh() {
    if (!import.meta.client || !mounted || !unregister) return
    clear()
    const token = capture()
    if (!token.expectedOwner || account.writesBlocked.value) return
    loading.value = true
    controller = new AbortController()
    try {
      const value = await api.accountActivity(undefined, controller.signal)
      if (!token.valid()) return
      check(value, token.expectedOwner)
      apply(value)
    } catch (cause) {
      if (token.valid()) fail(cause)
    } finally {
      if (token.valid()) loading.value = false
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
      if (token.valid()) check(value, token.expectedOwner)
      return () => {
        if (!token.valid()) return
        const previous = response.value
        if (!previous || previous.follows_revision !== value.follows_revision) {
          apply(value)
          return
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
      account.writesBlocked.value
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
    groups: computed(() => groupActivityItems(response.value?.items ?? [])),
    refresh,
    loadMore,
  }
}
