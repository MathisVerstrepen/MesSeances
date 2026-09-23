import { accountDestination } from '~/utils/accountState'

export default defineNuxtRouteMiddleware(async (to) => {
  const { session, refresh } = useAccountSession()
  await refresh()
  // An unavailable service is not an anonymous session.
  if (!session.value?.enabled) return
  if (to.path === '/connexion' && to.query.error === 'google_failed') return
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
  if (to.path === '/compte' && session.value.state !== 'complete') {
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
