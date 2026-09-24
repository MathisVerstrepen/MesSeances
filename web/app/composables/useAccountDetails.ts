import type { AccountDetails } from '~/types/account'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

// Settings are client-fetched after the request-scoped SSR session check. Keep
// details local, never in a shared cache or persisted form draft.
export function useAccountDetails() {
  const api = useAccountApi()
  const account = useAccountSession()
  const details = ref<AccountDetails | null>(null)
  const loading = ref(false)
  const errorMessage = ref('')
  let revision = 0
  let mounted = false
  let unregister: (() => void) | undefined
  const lifetime = useAccountLifetime(() => {
    unregister?.()
    unregister = undefined
    revision++
    details.value = null
    loading.value = false
  })

  const identity = () => {
    const value = account.session.value
    return value?.state === 'complete' && value.account
      ? `${value.account.username}:${value.account.email}`
      : ''
  }

  async function revalidate() {
    const current = ++revision
    const sessionRevision = account.revision.value
    const expected = account.session.value?.account
    const value = await api.details()
    if (
      value.email !== expected?.email ||
      value.username !== expected?.username
    )
      throw new AccountApiError(403, 'authentication_required')
    return () => {
      if (
        mounted &&
        current === revision &&
        sessionRevision === account.revision.value
      ) {
        details.value = value
        loading.value = false
        errorMessage.value = ''
      }
    }
  }

  async function refresh() {
    if (mounted && !unregister) return
    const active = lifetime.capture()
    const current = ++revision
    details.value = null
    errorMessage.value = ''
    loading.value = false
    if (account.session.value?.state !== 'complete') return
    loading.value = true
    try {
      const value = await api.details()
      if (active() && current === revision) details.value = value
    } catch (error) {
      if (current === revision) errorMessage.value = accountErrorMessage(error)
    } finally {
      if (current === revision) loading.value = false
    }
  }

  watch(
    identity,
    () => {
      if (mounted) void refresh()
    },
    { flush: 'sync' },
  )
  onMounted(() => {
    mounted = true
    unregister = account.onRevalidate(revalidate)
    void refresh()
  })
  // A reused confirmation page needs its detail subscription back after admission.
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
    revision++
    details.value = null
  })
  return { details, loading, errorMessage, refresh }
}
