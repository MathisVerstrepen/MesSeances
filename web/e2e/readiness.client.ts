import { defineNuxtPlugin } from '#app'

// Registered only by the browser fixture, never by the application config.
export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.hook('app:mounted', () => {
    document.documentElement.dataset.playwrightReady = 'true'
  })
})
