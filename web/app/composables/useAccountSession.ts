import type { AccountSession } from '~/types/account'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

type DetailRevalidation = () => Promise<() => void>
declare module '#app' {
  interface NuxtApp {
    _accountRevalidation?: {
      pending?: Promise<void>
      details: Set<DetailRevalidation>
    }
  }
}

export function useAccountSession() {
  const api = useAccountApi()
  const session = useState<AccountSession | null>('account-session', () => null)
  const status = useState<'idle' | 'loading' | 'ready' | 'error'>(
    'account-session-status',
    () => 'idle',
  )
  const errorMessage = useState('account-session-error', () => '')
  const revision = useState('account-session-request', () => 0)
  const revalidating = useState('account-revalidating', () => false)
  // Promises/callbacks belong to this Nuxt app, never serialized or shared by SSR requests.
  const app = useNuxtApp()
  const runtime = (app._accountRevalidation ??= { details: new Set() })
  const writesBlocked = computed(
    () => revalidating.value || status.value !== 'ready',
  )

  function invalidate() {
    revision.value++
    revalidating.value = false
    runtime.pending = undefined
  }

  function clear() {
    invalidate()
    session.value = null
    status.value = 'idle'
    errorMessage.value = ''
  }

  function accept(value: AccountSession) {
    invalidate()
    session.value = value
    status.value = 'ready'
    errorMessage.value = ''
  }

  function refresh(): Promise<void> {
    invalidate()
    const current = revision.value
    // Initial/recovery and explicit invalidation remain destructive.
    session.value = null
    status.value = 'loading'
    errorMessage.value = ''
    const pending = (async () => {
      try {
        const value = await api.session()
        if (current === revision.value) accept(value)
      } catch (error) {
        if (current !== revision.value) return
        session.value = null
        errorMessage.value = accountErrorMessage(error)
        status.value = 'error'
      } finally {
        if (current === revision.value) runtime.pending = undefined
      }
    })()
    runtime.pending = pending
    return pending
  }

  function revalidate(): Promise<void> {
    if (runtime.pending) return runtime.pending
    const previous = session.value
    if (status.value !== 'ready' || !previous?.enabled) return refresh()
    const current = ++revision.value
    revalidating.value = true
    const pending = (async () => {
      try {
        const value = await api.session()
        if (current !== revision.value) return
        // These fields identify the visible account only, NOT session/grant continuity.
        if (
          !value.enabled ||
          value.state !== previous.state ||
          !!value.account !== !!previous.account ||
          value.account?.email !== previous.account?.email ||
          value.account?.username !== previous.account?.username
        ) {
          clear()
          errorMessage.value = accountErrorMessage(new AccountApiError(401))
          status.value = 'error'
          return
        }
        const commits = await Promise.all(
          value.state === 'complete'
            ? [...runtime.details].map((refreshDetails) => refreshDetails())
            : [],
        )
        if (current !== revision.value) return
        for (const commit of commits) commit()
        session.value = value
      } catch (error) {
        if (current !== revision.value) return
        clear()
        errorMessage.value = accountErrorMessage(error)
        status.value = 'error'
      } finally {
        if (current === revision.value) {
          revalidating.value = false
          runtime.pending = undefined
        }
      }
    })()
    runtime.pending = pending
    return pending
  }

  function onRevalidate(refreshDetails: DetailRevalidation) {
    runtime.details.add(refreshDetails)
    return () => runtime.details.delete(refreshDetails)
  }

  function notify() {
    if (import.meta.client && 'BroadcastChannel' in window) {
      const channel = new BroadcastChannel('messeances-account')
      channel.postMessage('changed')
      channel.close()
    }
  }

  async function logout() {
    if (writesBlocked.value)
      throw new AccountApiError(403, 'recent_auth_required')
    clear()
    const current = revision.value
    try {
      await api.logout()
    } catch (error) {
      if (current !== revision.value) throw error
      clear()
      notify()
      await refresh()
      if (session.value?.state === 'anonymous') return
      throw error
    }
    notify()
    if (current !== revision.value) return
    clear()
    await refresh()
  }

  return {
    session,
    status,
    errorMessage,
    revision,
    revalidating,
    writesBlocked,
    revalidate,
    onRevalidate,
    refresh,
    clear,
    accept,
    notify,
    logout,
  }
}
