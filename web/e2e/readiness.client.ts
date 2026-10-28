import { defineNuxtPlugin } from '#app'
import { watch } from 'vue'
import { useAccountSession } from '../app/composables/useAccountSession'

// Registered only by the browser fixture, never by the application config.
export default defineNuxtPlugin((nuxtApp) => {
  const account = useAccountSession()
  // A received response does not yet prove that all detail commits reached Vue.
  watch(
    account.revalidating,
    (pending) => {
      document.documentElement.dataset.playwrightAccountRevalidating =
        String(pending)
    },
    { immediate: true, flush: 'post' },
  )
  nuxtApp.hook('app:mounted', () => {
    document.documentElement.dataset.playwrightReady = 'true'
  })
})
