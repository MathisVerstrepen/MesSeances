// Owned, offline Nuxt fixture. No deployment dotenv, provider, database or real auth.
import { createServer } from 'node:http'
import { fileURLToPath } from 'node:url'
import { buildNuxt, loadNuxt } from '@nuxt/kit'
import { toNodeListener } from 'h3'
import { date, generatedAt, movie, showtimes, theater } from '../e2e/data.mjs'

const port = Number(process.env.PLAYWRIGHT_PORT || 13400)
if (!Number.isInteger(port) || port < 1024 || port > 65535)
  throw new Error('Invalid PLAYWRIGHT_PORT')
const origin = `http://127.0.0.1:${port}`
const unexpected = []
function json(response, body, status = 200) {
  response.writeHead(status, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
  })
  response.end(JSON.stringify(body))
}
const mock = createServer((request, response) => {
  const url = new URL(request.url, origin)
  const path = url.pathname
  if (request.method === 'GET') {
    if (path === '/api/v1/auth/session')
      return json(response, {
        enabled: false,
        state: 'anonymous',
        account: null,
      })
    if (path === '/api/v1/admin/session')
      return json(response, { authenticated: false })
    if (path === '/api/v1/theaters') return json(response, [theater])
    if (path === '/api/v1/cities')
      return json(response, {
        generated_at: generatedAt,
        items: [{ name: 'Lille', slug: 'lille', theaters: [theater] }],
      })
    if (path === `/api/v1/theaters/${theater.slug}/showtimes`)
      return json(response, showtimes(url.searchParams.get('date') || date))
    if (path === '/api/v1/theaters/missing/showtimes')
      return json(
        response,
        { error: { code: 'not_found', message: 'Cinéma introuvable' } },
        404,
      )
    if (path === '/api/v1/movies')
      return json(response, {
        items: [movie],
        available_genres: [],
        page: 1,
        page_size: 100,
        total: 1,
        generated_at: generatedAt,
        catalog_revision: 'fixture-1',
      })
  }
  if (request.method === 'POST' && path === '/api/v1/admin/login') {
    request.resume()
    return json(
      response,
      { error: { code: 'unauthorized', message: 'Mot de passe incorrect.' } },
      401,
    )
  }
  unexpected.push(`${request.method} ${path}`)
  return json(
    response,
    {
      error: { code: 'unmocked_endpoint', message: 'Missing browser fixture' },
    },
    501,
  )
})
await new Promise((resolve, reject) => {
  mock.once('error', reject)
  mock.listen(0, '127.0.0.1', resolve)
})
const api = `http://127.0.0.1:${mock.address().port}`
Object.assign(process.env, {
  NUXT_API_BASE: api,
  NUXT_INTERNAL_API_SHARED_SECRET: '',
  NUXT_PUBLIC_API_BASE: '',
  NUXT_PUBLIC_SITE_URL: origin,
  NUXT_PUBLIC_UMAMI_SCRIPT_URL: '',
  NUXT_PUBLIC_UMAMI_WEBSITE_ID: '',
})
let nuxt
let web
let closing = false
async function close() {
  if (closing) return
  closing = true
  web?.closeAllConnections()
  web?.close()
  mock.closeAllConnections()
  mock.close()
  await nuxt?.close()
}
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.once(signal, async () => {
    await close()
    process.exit(0)
  })
}
try {
  nuxt = await loadNuxt({
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    dev: true,
    dotenv: false,
    overrides: {
      buildDir: fileURLToPath(
        new URL(`../node_modules/.cache/playwright-${port}`, import.meta.url),
      ),
      devServer: { host: '127.0.0.1', port, url: origin },
      typescript: { typeCheck: false },
      plugins: [
        fileURLToPath(new URL('../e2e/readiness.client.ts', import.meta.url)),
      ],
      hooks: {
        'vite:extendConfig'(config) {
          // Nuxt supplies aliases; don't discover the developer's .nuxt tsconfig.
          config.oxc = { ...config.oxc, tsconfig: false }
        },
      },
      nitro: {
        devProxy: { '/api': { target: `${api}/api`, changeOrigin: false } },
      },
      runtimeConfig: {
        apiBase: api,
        internalApiSharedSecret: '',
        public: {
          apiBase: '',
          siteUrl: origin,
          umamiScriptUrl: '',
          umamiWebsiteId: '',
        },
      },
    },
  })
  await buildNuxt(nuxt)
  const handler = nuxt.server.handler || toNodeListener(nuxt.server.app)
  web = createServer((request, response) => {
    if (request.url === '/__playwright/health')
      return json(response, { ready: true })
    if (request.url === '/__playwright/state')
      return json(response, { unexpected })
    return handler(request, response)
  })
  await new Promise((resolve, reject) => {
    web.once('error', reject)
    web.listen(port, '127.0.0.1', resolve)
  })
  console.log(`PLAYWRIGHT_READY ${origin}`)
} catch (error) {
  await close()
  throw error
}
