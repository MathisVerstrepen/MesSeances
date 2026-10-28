import type { Router } from 'vue-router'

const readinessElements = new Map([
  ['/films', 'films-catalog'],
  ['/films/prochainement', 'upcoming-catalog'],
  ['/planning', 'planning-catalog'],
  ['/recherche', 'search-catalog'],
  ['/statistiques', 'statistics-catalog'],
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
    // Delegate all same-path, hash, meta and transition decisions to Nuxt.
    const position = await runWithContext(() =>
      original(to, from, savedPosition),
    )
    if (controller.signal.aborted) return false
    const path = to.path.replace(/\/$/, '')
    const elementId =
      readinessElements.get(path) ??
      (/^\/film\/[^/]+$/.test(path) ? 'film-catalog' : undefined)
    if (
      savedPosition &&
      position === savedPosition &&
      elementId &&
      path !== from.path.replace(/\/$/, '')
    ) {
      if (!(await waitForCatalogRender(controller.signal, elementId)))
        return false
    }
    return controller.signal.aborted ? false : position
  }
}
