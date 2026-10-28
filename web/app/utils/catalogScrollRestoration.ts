import type { Router } from 'vue-router'

const readinessElements = new Map([
  ['/films', 'films-catalog'],
  ['/films/prochainement', 'upcoming-catalog'],
  ['/planning', 'planning-catalog'],
  ['/recherche', 'search-catalog'],
  ['/statistiques', 'statistics-catalog'],
  ['/compte/activite', 'account-activity'],
])

// Catalog loading is deliberately outside Nuxt's page Suspense. Wait for its
// rendered success/error/empty state, not just page:loading:end. A stalled
// request gets one bounded, naturally clamped restore, never a later retry.
export function waitForCatalogRender(
  signal: AbortSignal,
  elementId: string,
  timeoutMs = 10000,
) {
  return new Promise<boolean>((resolve) => {
    if (signal.aborted) return resolve(false)
    const ready = () =>
      document.getElementById(elementId)?.getAttribute('aria-busy') === 'false'
    if (ready()) return resolve(true)
    const observer = new MutationObserver(() => {
      if (ready()) finish(true)
    })
    const timer = setTimeout(() => finish(true), timeoutMs)
    const cancel = () => finish(false)
    function finish(restore: boolean) {
      observer.disconnect()
      clearTimeout(timer)
      signal.removeEventListener('abort', cancel)
      resolve(restore)
    }
    signal.addEventListener('abort', cancel, { once: true })
    observer.observe(document.documentElement, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ['aria-busy'],
    })
  })
}

export function installCatalogScrollRestoration(
  router: Router,
  runWithContext: <T>(callback: () => T) => T | Promise<Awaited<T>>,
) {
  const original = router.options.scrollBehavior
  if (!original) return
  let pending: AbortController | undefined
  router.beforeEach(() => {
    pending?.abort()
  })
  router.options.scrollBehavior = async (to, from, savedPosition) => {
    pending?.abort()
    const controller = new AbortController()
    pending = controller
    const path = to.path.replace(/\/$/, '')
    // Delegate hash, meta and transition decisions to Nuxt.
    let position = await runWithContext(() => original(to, from, savedPosition))
    if (controller.signal.aborted) return false
    const activityTabHistory =
      /^\/cinema\/[^/]+$/.test(path) &&
      path === from.path.replace(/\/$/, '') &&
      to.query.view === 'activity' &&
      from.query.view !== 'activity' &&
      !to.hash &&
      !from.hash &&
      to.meta.scrollToTop !== false
    // Nuxt skips all same-path scrolling. History between cinema tabs needs
    // restoration too, once the lazy activity feed has rendered.
    if (savedPosition && activityTabHistory && position === false)
      position = savedPosition
    const elementId =
      readinessElements.get(path) ??
      (/^\/film\/[^/]+$/.test(path)
        ? 'film-catalog'
        : /^\/ville\/[^/]+\/cinemas$/.test(path)
          ? 'city-catalog'
          : /^\/cinema\/[^/]+$/.test(path) && to.query.view === 'activity'
            ? 'cinema-activity'
            : undefined)
    if (
      savedPosition &&
      position === savedPosition &&
      elementId &&
      (path !== from.path.replace(/\/$/, '') || activityTabHistory)
    ) {
      if (!(await waitForCatalogRender(controller.signal, elementId)))
        return false
    }
    return controller.signal.aborted ? false : position
  }
}
