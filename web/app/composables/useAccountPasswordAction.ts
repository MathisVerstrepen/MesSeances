import type { AccountAction } from '~/types/account'

export function useAccountPasswordAction() {
  const api = useAccountApi()
  const account = useAccountSession()
  let revision = 0
  const invalidate = () => {
    revision++
  }
  useAccountLifetime(invalidate)
  watch(account.revision, invalidate, { flush: 'sync' })
  onMounted(() => window.addEventListener('pagehide', invalidate))
  onBeforeUnmount(() => {
    invalidate()
    if (import.meta.client) window.removeEventListener('pagehide', invalidate)
  })

  return async (
    password: string,
    action: AccountAction,
    target: string | undefined,
    write: (grant: string) => Promise<void>,
  ): Promise<boolean> => {
    if (account.writesBlocked.value) return false
    const current = revision
    // Never put the single-use grant in Nuxt state, the URL, or storage.
    const proof = await api.reauthPassword(password, action, target)
    try {
      if (current !== revision || account.writesBlocked.value) return false
      await write(proof.grant)
      return current === revision
    } finally {
      proof.grant = ''
    }
  }
}
