import { isAccountPage } from '~~/shared/accountPrivacy'

export default defineNuxtRouteMiddleware((to, from) => {
  const privateDocument = useState('account-private-document', () =>
    isAccountPage(to.path),
  )
  if (
    import.meta.client &&
    (privateDocument.value !== isAccountPage(to.path) ||
      (!useNuxtApp().isHydrating &&
        isAccountPage(to.path) &&
        to.fullPath !== from.fullPath))
  ) {
    const target = new URL(to.fullPath, window.location.origin)
    if (
      target.pathname === window.location.pathname &&
      target.search === window.location.search
    ) {
      // location.href treats fragment-only changes as same-document navigation.
      // Reload explicitly so the first-script token bootstrap and form state reset.
      window.history.replaceState(window.history.state, '', to.fullPath)
      window.location.reload()
      return
    }
    // A router transition cannot unload a tracker already running in this document.
    return navigateTo(to.fullPath, { external: true })
  }
})
