import { reactive, onScopeDispose } from 'vue'
import { useRegisterSW } from 'virtual:pwa-register/vue'
import { createAppUpdate, type UpdateState } from '../utils/appUpdate'

export default defineNuxtPlugin((nuxtApp) => {
  const state = reactive<UpdateState>({
    available: false,
    busy: false,
    error: '',
  })
  let activate = async () => {}
  let storage: Storage | undefined
  try {
    storage = window.sessionStorage
  } catch {
    /* denied */
  }
  const owner = createAppUpdate({
    buildId: useRuntimeConfig().app.buildId,
    state,
    fetch: (url, options) => fetch(url, options),
    nonce: () => crypto.randomUUID(),
    now: () => Date.now(),
    online: () => navigator.onLine,
    visible: () => document.visibilityState === 'visible',
    reload: () => window.location.reload(),
    activate: () => activate(),
    storage,
    setTimeout: (callback, ms) => setTimeout(callback, ms),
    clearTimeout: (timer) => clearTimeout(timer),
  })
  const sw = useRegisterSW({
    immediate: true,
    onRegisteredSW: (_url, registration) => owner.setRegistration(registration),
    onNeedRefresh: owner.needRefresh,
    onNeedReload: owner.needReload,
    onRegisterError: () => owner.setRegistration(undefined),
  })
  activate = () => sw.updateServiceWorker()

  const check = () => {
    void owner.check()
  }
  const online = () => {
    void owner.check(true)
  }
  window.addEventListener('focus', check)
  window.addEventListener('pageshow', check)
  window.addEventListener('online', online)
  document.addEventListener('visibilitychange', check)
  const unhook = nuxtApp.hook('app:chunkError', check)
  onNuxtReady(check)
  const dispose = () => {
    window.removeEventListener('focus', check)
    window.removeEventListener('pageshow', check)
    window.removeEventListener('online', online)
    document.removeEventListener('visibilitychange', check)
    unhook()
    owner.dispose()
  }
  onScopeDispose(dispose)
  if (import.meta.hot) import.meta.hot.dispose(dispose)
  return { provide: { appUpdate: { state, refresh: owner.refresh } } }
})
