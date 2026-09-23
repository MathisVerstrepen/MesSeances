import type { AccountSession } from '~/types/account'
import { accountErrorMessage } from '~/utils/accountState'

export function useAccountSession() {
  const api = useAccountApi()
  const session = useState<AccountSession | null>('account-session', () => null)
  const status = useState<'idle' | 'loading' | 'ready' | 'error'>(
    'account-session-status',
    () => 'idle',
  )
  const errorMessage = useState('account-session-error', () => '')
  const revision = useState('account-session-request', () => 0)

  function clear() {
    revision.value++
    session.value = null
    status.value = 'idle'
    errorMessage.value = ''
  }

  function accept(value: AccountSession) {
    revision.value++
    session.value = value
    status.value = 'ready'
    errorMessage.value = ''
  }

  async function refresh() {
    const current = ++revision.value
    // Never retain authenticated success while offline or revalidating.
    session.value = null
    status.value = 'loading'
    errorMessage.value = ''
    try {
      const value = await api.session()
      if (current === revision.value) accept(value)
    } catch (error) {
      if (current !== revision.value) return
      session.value = null
      errorMessage.value = accountErrorMessage(error)
      status.value = 'error'
    }
  }

  function notify() {
    if (import.meta.client && 'BroadcastChannel' in window) {
      const channel = new BroadcastChannel('messeances-account')
      channel.postMessage('changed')
      channel.close()
    }
  }

  async function logout() {
    try {
      await api.logout()
    } catch (error) {
      clear()
      notify()
      await refresh()
      if (session.value?.state === 'anonymous') return
      throw error
    }
    clear()
    notify()
    await refresh()
  }

  return {
    session,
    status,
    errorMessage,
    refresh,
    clear,
    accept,
    notify,
    logout,
  }
}
