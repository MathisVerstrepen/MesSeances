import {
  createPublicAnalytics,
  publicAnalyticsReferrer,
  type PublicPageview,
  type TrackerPayload,
} from '~/utils/publicAnalytics'
import { isNavigationFailure, NavigationFailureType } from 'vue-router'

declare module '#app' {
  interface NuxtApp {
    _pausePublicAnalytics?: () => void
  }
}

declare global {
  interface Window {
    __publicPageview?: (
      type: string,
      payload: TrackerPayload | null,
    ) => PublicPageview | null
    umami?: { track: (payload: PublicPageview) => Promise<void> }
  }
}

export default defineNuxtPlugin((app) => {
  const config = useRuntimeConfig().public
  const scriptUrl = config.umamiScriptUrl.trim()
  const website = config.umamiWebsiteId.trim()
  if (!scriptUrl || !website) return
  const router = useRouter()
  const owner = createPublicAnalytics(
    {
      website,
      hostname: location.hostname,
      language: navigator.language,
      screen: `${screen.width}x${screen.height}`,
    },
    publicAnalyticsReferrer(document.referrer, location.origin),
  )
  window.__publicPageview = owner.beforeSend
  let mounted = false
  let loading = false
  let generation = 0
  app._pausePublicAnalytics = () => {
    generation++
    owner.pause()
  }

  function send() {
    if (window.umami) void owner.send((payload) => window.umami!.track(payload))
  }

  async function settle() {
    const current = ++generation
    await nextTick()
    if (!mounted || current !== generation) return
    const route = router.currentRoute.value
    if (!owner.settle(route.path, route.matched.length > 0)) return
    // The root owns this head entry. Update synchronously before any tracker fetch,
    // including same-origin collectors, independently of Unhead's batched rendering.
    document
      .querySelector('meta[name="referrer"]')
      ?.setAttribute('content', 'strict-origin')
    if (window.umami) return send()
    if (loading) return
    loading = true
    const script = document.createElement('script')
    script.src = scriptUrl
    script.defer = true
    script.referrerPolicy = 'no-referrer'
    script.dataset.websiteId = website
    script.dataset.autoTrack = 'false'
    script.dataset.beforeSend = '__publicPageview'
    script.dataset.excludeSearch = 'true'
    script.dataset.excludeHash = 'true'
    script.onload = () => {
      script.onload = script.onerror = null
      send()
    }
    script.onerror = () => {
      script.onload = script.onerror = null
    }
    document.head.append(script)
  }

  router.beforeEach(() => {
    generation++
    owner.pause()
  })
  router.afterEach((_to, _from, failure) => {
    if (!isNavigationFailure(failure, NavigationFailureType.cancelled))
      void settle()
  })
  router.onError(() => void settle())
  app.hook('app:mounted', () => {
    mounted = true
    void settle()
  })
})
