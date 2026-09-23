import type { GoogleStart } from '~/types/account'
import { googleAuthorizationUrl } from '~/utils/accountGoogle'

export function useAccountGoogle() {
  const api = useAccountApi()
  const account = useAccountSession()
  let revision = 0
  const invalidate = () => {
    revision++
  }
  watch(account.session, invalidate, { flush: 'sync' })
  onMounted(() => window.addEventListener('pagehide', invalidate))
  onBeforeUnmount(() => {
    invalidate()
    if (import.meta.client) window.removeEventListener('pagehide', invalidate)
  })
  return async (options: GoogleStart = { mode: 'login' }) => {
    const current = revision
    const result = await api.googleStart(options)
    if (current !== revision) throw new Error('Account state changed')
    window.location.assign(googleAuthorizationUrl(result.authorization_url))
  }
}
