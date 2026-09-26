import tailwindcss from '@tailwindcss/vite'
import {
  accountPageRoots,
  accountPrivacyHeaders,
} from './shared/accountPrivacy'

declare const process: { env: Record<string, string | undefined> }

export default defineNuxtConfig({
  compatibilityDate: '2024-11-01',
  devtools: { enabled: false },
  experimental: {
    defaults: {
      nuxtLink: {
        prefetchOn: {
          visibility: false,
          interaction: true,
        },
      },
    },
  },
  modules: ['@vite-pwa/nuxt'],
  routeRules: Object.fromEntries(
    [...accountPageRoots, '/api/v1/auth', '/api/v1/account'].flatMap((path) =>
      [path, `${path}/**`].map((pattern) => [
        pattern,
        {
          cache: false,
          prerender: false,
          headers: accountPrivacyHeaders,
        },
      ]),
    ),
  ),
  nitro: {
    devProxy: {
      '/api': { target: 'http://localhost:8080/api', changeOrigin: false },
    },
  },
  build: {
    transpile: ['@vuepic/vue-datepicker'],
  },
  css: ['~/assets/css/main.css'],
  vite: {
    plugins: [tailwindcss()],
  },
  app: {
    head: {
      htmlAttrs: { lang: 'fr' },
      title: 'MesSeances - Vos séances, au bon moment',
      meta: [
        {
          name: 'description',
          content:
            'Explorez les séances de cinéma de Paris sur une frise horaire et trouvez celles qui tiennent dans votre créneau.',
        },
        { name: 'robots', content: 'noindex,follow' },
        { name: 'theme-color', content: '#FCFAF8' },
      ],
      link: [
        { rel: 'icon', href: '/favicon.ico', sizes: '48x48' },
        { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' },
        {
          rel: 'apple-touch-icon',
          href: '/apple-touch-icon-180x180.png',
          sizes: '180x180',
        },
      ],
    },
  },
  pwa: {
    strategies: 'generateSW',
    registerType: 'prompt',
    registerWebManifestInRouteRules: true,
    includeAssets: [
      'favicon.svg',
      'favicon.ico',
      'apple-touch-icon-180x180.png',
    ],
    includeManifestIcons: true,
    client: {
      installPrompt: false,
    },
    manifest: {
      name: 'MesSeances - Vos séances, au bon moment',
      short_name: 'MesSeances',
      description:
        'Explorez les séances de cinéma de Paris sur une frise horaire et trouvez celles qui tiennent dans votre créneau.',
      lang: 'fr',
      start_url: '/',
      scope: '/',
      display: 'standalone',
      theme_color: '#FCFAF8',
      background_color: '#FCFAF8',
      icons: [
        { src: '/pwa-64x64.png', sizes: '64x64', type: 'image/png' },
        { src: '/pwa-192x192.png', sizes: '192x192', type: 'image/png' },
        {
          src: '/pwa-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any',
        },
        {
          src: '/maskable-icon-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'maskable',
        },
      ],
    },
    workbox: {
      navigateFallback: null,
      globIgnores: ['**/*.html', '**/*_payload*', '**/api/**'],
      navigateFallbackDenylist: [
        /^\/(connexion|inscription|verification|finaliser|mot-de-passe-oublie|reinitialiser-mot-de-passe|compte)(\/|$)/i,
        /^\/api\/v1\/(auth|account)(\/|$)/i,
      ],
      runtimeCaching: [
        {
          urlPattern:
            /\/(connexion|inscription|verification|finaliser|mot-de-passe-oublie|reinitialiser-mot-de-passe|compte)(\/|$)|\/api\/v1\/(auth|account)(\/|$)/i,
          handler: 'NetworkOnly',
        },
      ],
    },
  },
  runtimeConfig: {
    apiBase: 'http://localhost:8080',
    internalApiSharedSecret: '',
    public: {
      appVersion: process.env.NUXT_PUBLIC_APP_VERSION || 'dev',
      apiBase: '',
      siteUrl: 'http://localhost:3000',
      umamiScriptUrl: '',
      umamiWebsiteId: '',
    },
  },
  typescript: {
    strict: true,
    typeCheck: true,
  },
})
