import { createAccountNavigation } from '~/utils/accountNavigation'
import { isAccountPage } from '~~/shared/accountPrivacy'
import { isNavigationFailure, NavigationFailureType } from 'vue-router'

declare module '#app' {
  interface NuxtApp {
    _accountNavigation?: ReturnType<typeof createAccountNavigation>
  }
}

export function useAccountNavigation() {
  const app = useNuxtApp()
  if (!import.meta.client) throw new Error('Account navigation is client-only')
  if (!app._accountNavigation) {
    const runtime = createAccountNavigation()
    app._accountNavigation = runtime
    const router = useRouter()
    function restoreReferrerPolicy() {
      const path = router.currentRoute.value.path
      document
        .querySelector('meta[name="referrer"]')
        ?.setAttribute(
          'content',
          isAccountPage(path) || /^\/admin(?:\/|$)/i.test(path)
            ? 'no-referrer'
            : 'strict-origin',
        )
    }
    router.afterEach((to, _from, failure) => {
      if (!isNavigationFailure(failure, NavigationFailureType.cancelled))
        restoreReferrerPolicy()
      const id = runtime.ids.get(to)
      if (id !== undefined) runtime.finish(id, to.path, to.fullPath, !failure)
    })
    router.onError(() => {
      runtime.clear()
      restoreReferrerPolicy()
    })
    window.addEventListener('pagehide', runtime.clear)
  }
  return app._accountNavigation
}

export function useAccountToken() {
  const path = useRoute().path
  const token = ref('')
  const ready = ref(false)
  const runtime = import.meta.client ? useAccountNavigation() : undefined
  function clear() {
    token.value = ''
  }
  useAccountFlowDraft(token)

  onMounted(() => {
    runtime!.starts.add(clear)
    runtime!.arrivals.add(arrive)
    arrive()
    window.addEventListener('pagehide', clear)
  })
  function arrive() {
    token.value = runtime!.take(path)
    ready.value = true
  }
  onBeforeUnmount(() => {
    clear()
    runtime?.starts.delete(clear)
    runtime?.arrivals.delete(arrive)
    if (import.meta.client) window.removeEventListener('pagehide', clear)
  })
  return { token, ready, clear }
}
