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
  function clear() {
    generation++
    controller?.abort()
    response.value = null
    loading.value = false
    morePending.value = false
    error.value = ''
    moreError.value = ''
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
    clear()
    const token = capture()
    if (!token.expectedOwner) return () => {}
    loading.value = true
    controller = new AbortController()
    try {
      const value = await api.accountActivity(undefined, controller.signal)
      if (token.valid()) check(value, token.expectedOwner)
      return () => {
        if (token.valid()) apply(value)
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
    groups: computed(() => groupActivityItems(response.value?.items ?? [])),
    refresh,
    loadMore,
  }
}
