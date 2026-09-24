import { isAccountPage } from '~~/shared/accountPrivacy'

export default defineNuxtRouteMiddleware((to) => {
  if (!import.meta.client) return
  const app = useNuxtApp()
  app._pausePublicAnalytics?.()
  const runtime = useAccountNavigation()
  const router = useRouter()
  const privatePage = isAccountPage(to.path)
  // Tighten before any private admission request. Relax only after a public
  // navigation commits, never while a private document may remain on screen.
  if (privatePage || /^\/admin(?:\/|$)/i.test(to.path)) {
    document
      .querySelector('meta[name="referrer"]')
      ?.setAttribute('content', 'no-referrer')
  }
  const target = router.resolve({
    path: to.path,
    query: to.query,
    hash: '',
  }).fullPath
  const bootstrap = window.__takeAccountToken?.(to.path) ?? ''
  delete window.__takeAccountToken
  const id = runtime.begin(
    to.path,
    target,
    privatePage ? to.hash : '',
    bootstrap,
  )
  runtime.ids.set(to, id)
  if (privatePage && to.hash) {
    // Vue Router inherits the original `force` on guard redirects. Start an
    // explicit forced replacement so same-path fragments still run admission.
    return router
      .replace({
        path: to.path,
        query: to.query,
        hash: '',
        force: true,
        replace: true,
      })
      .then(() => false)
  }
})
