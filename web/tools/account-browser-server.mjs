// Test-only Nuxt launcher. Never reads deployment environment or starts providers.
import { createServer } from 'node:http'
import { fileURLToPath } from 'node:url'
import { buildNuxt, loadNuxt } from '@nuxt/kit'
import { toNodeListener } from 'h3'

const origin = 'http://127.0.0.1:13009'
const api = 'http://127.0.0.1:18089'
const script = process.argv.includes('--no-analytics')
  ? ''
  : 'https://analytics.example.test/script.js'
// Runtime env overrides take precedence over Nuxt defaults. Never inherit a
// deployment API, internal bearer or tracker configuration into this fixture.
Object.assign(process.env, {
  NUXT_API_BASE: api,
  NUXT_INTERNAL_API_SHARED_SECRET: '',
  NUXT_PUBLIC_API_BASE: '',
  NUXT_PUBLIC_SITE_URL: origin,
  NUXT_PUBLIC_UMAMI_SCRIPT_URL: script,
  NUXT_PUBLIC_UMAMI_WEBSITE_ID: 'browser-synthetic-only',
})
const nuxt = await loadNuxt({
  cwd: fileURLToPath(new URL('..', import.meta.url)),
  dev: true,
  dotenv: false,
  overrides: {
    devServer: { host: '127.0.0.1', port: 13009, url: origin },
    nitro: {
      devProxy: { '/api': { target: `${api}/api`, changeOrigin: false } },
    },
    runtimeConfig: {
      apiBase: api,
      internalApiSharedSecret: '',
      public: {
        apiBase: '',
        siteUrl: origin,
        umamiScriptUrl: script,
        umamiWebsiteId: 'browser-synthetic-only',
      },
    },
  },
})
await buildNuxt(nuxt)
const server = createServer(
  nuxt.server.handler || toNodeListener(nuxt.server.app),
)
server.listen(13009, '127.0.0.1', () =>
  console.log('ACCOUNT_BROWSER_WEB_READY'),
)
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.once(signal, async () => {
    server.closeAllConnections()
    server.close()
    await nuxt.close()
    process.exit(0)
  })
}
