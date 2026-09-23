import type { AccountDetails } from '~/types/account'
import { accountErrorMessage } from '~/utils/accountState'

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

  async function refresh() {
    const current = ++revision
    details.value = null
    errorMessage.value = ''
    loading.value = false
    if (account.session.value?.state !== 'complete') return
    loading.value = true
    try {
      const value = await api.details()
      if (current === revision) details.value = value
    } catch (error) {
      if (current === revision) errorMessage.value = accountErrorMessage(error)
    } finally {
      if (current === revision) loading.value = false
    }
  }

  watch(
    account.session,
    () => {
      if (mounted) void refresh()
    },
    { flush: 'sync' },
  )
  onMounted(() => {
    mounted = true
    void refresh()
  })
  onBeforeUnmount(() => {
    mounted = false
    revision++
    details.value = null
  })
  return { details, loading, errorMessage, refresh }
}
