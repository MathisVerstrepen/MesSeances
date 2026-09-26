import {
  accountDestination,
  accountGoogleCallbackMessage,
} from '~/utils/accountState'

export default defineNuxtRouteMiddleware(async (to) => {
  const { session, status, revalidate } = useAccountSession()
  const app = useNuxtApp()
  // Request-scoped SSR result already admitted the initial page. Every later
  // client admission performs a fresh check, sharing concurrent focus work.
  if (!(import.meta.client && app.isHydrating && status.value === 'ready'))
    await revalidate()
  // An unavailable service is not an anonymous session.
  if (!session.value?.enabled) return
  if (to.path === '/connexion' && accountGoogleCallbackMessage(to.query.error))
    return
  // A pending password session cannot recover registration-browser proof.
  // Let its owner submit a fresh registration instead of looping to verification.
  if (
    to.path === '/inscription' &&
    session.value.state === 'pending_email' &&
    session.value.account?.has_password
  )
    return
  // Preserve email fragments in memory on anonymous confirmation pages. The
  // page offers ordinary login, then asks the user to reopen their email.
  const path = to.path.toLowerCase().replace(/\/+$/, '')
  if (
    ['/compte', '/compte/parametres'].includes(path) &&
    session.value.state !== 'complete'
  ) {
    return navigateTo(accountDestination(session.value))
  }
  if (to.path === '/finaliser' && session.value.state !== 'pending_username') {
    return navigateTo(accountDestination(session.value))
  }
  if (
    ['/connexion', '/inscription'].includes(to.path) &&
    session.value.state !== 'anonymous'
  ) {
    return navigateTo(accountDestination(session.value))
  }
})
