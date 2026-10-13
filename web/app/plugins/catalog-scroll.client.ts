import { installCatalogScrollRestoration } from '~/utils/catalogScrollRestoration'

export default defineNuxtPlugin((nuxtApp) => {
  const router = useRouter()
  // Nuxt installs its final scroll behavior in app:created. Extend it after
  // hydration so initial SSR navigation and framework setup stay untouched.
  nuxtApp.hook('app:mounted', () => {
    installCatalogScrollRestoration(router, (callback) =>
      nuxtApp.runWithContext(callback),
    )
  })
})
