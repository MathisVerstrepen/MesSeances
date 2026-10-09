// Owned, offline Nuxt fixture. No deployment dotenv, provider, database or real auth.
import { createServer } from 'node:http'
import { fileURLToPath } from 'node:url'
import { buildNuxt, loadNuxt } from '@nuxt/kit'
import { toNodeListener } from 'h3'
import {
  accountActivityItem,
  date,
  generatedAt,
  historyStatistics,
  movie,
  publicActivityPage,
  releaseCatalog,
  screeningCatalog,
  secondTheater,
  showtimes,
  theater,
  syntheticWatchlist,
  statisticsRelease,
} from '../e2e/data.mjs'

const port = Number(process.env.PLAYWRIGHT_PORT || 13400)
if (!Number.isInteger(port) || port < 1024 || port > 65535)
  throw new Error('Invalid PLAYWRIGHT_PORT')
const origin = `http://127.0.0.1:${port}`
const unexpected = []
let scenario = {
  enabled: false,
  state: 'anonymous',
  username: 'fixture_alice',
  feed: 'populated',
}
let follows = { username: scenario.username, revision: '0', theater_ids: [] }
let followPosts = 0
let followGets = 0
let activityGets = 0
let selected = {
  username: scenario.username,
  revision: '1',
  theater_ids: [secondTheater.id],
}
let watchlist = syntheticWatchlist(scenario.username)
const screeningQueries = []
const upcomingQueries = []
function syntheticSession() {
  return {
    enabled: scenario.enabled,
    state: scenario.state,
    account:
      scenario.state === 'anonymous'
        ? null
        : {
            username:
              scenario.state === 'pending_username' ? null : scenario.username,
            email: `${scenario.username}@example.test`,
            has_password: true,
            google_linked: false,
          },
  }
}
function activityPage(cursor) {
  const ids = new Set(follows.theater_ids)
  const items = [
    accountActivityItem(103),
    accountActivityItem(102, secondTheater),
    accountActivityItem(101),
  ].filter((item) => ids.has(item.theater.id))
  const visible = scenario.feed === 'populated' ? items : []
  return {
    username: scenario.username,
    follows_revision: follows.revision,
    followed_theater_count: ids.size,
    generated_at: generatedAt,
    timezone: 'Europe/Paris',
    coverage: {
      initialized_theater_count:
        scenario.feed === 'initializing' ? 0 : ids.size,
      completeness:
        ids.size && scenario.feed !== 'initializing' ? 'partial' : 'unknown',
      bootstrap: 'baseline',
      return_minimum_break_days: 28,
    },
    // Deliberate repeated boundary event proves append deduplication.
    items: cursor ? visible.slice(1) : visible.slice(0, 2),
    limit: 20,
    next_cursor:
      !cursor && visible.length > 2 ? `fixture-${follows.revision}` : null,
  }
}
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
    if (path === '/api/v1/statistics/history')
      return json(response, historyStatistics(url.searchParams))
    if (
      path === `/api/v1/movies/${movie.slug}/showtimes` ||
      path === '/api/v1/movies/film-no-release/showtimes'
    )
      return json(response, {
        generated_at: generatedAt,
        timezone: 'Europe/Paris',
        date: url.searchParams.get('date') || date,
        movie: {
          ...movie,
          french_release_date: path.includes('film-no-release')
            ? null
            : statisticsRelease,
          release_date: '2001-01-01',
        },
        currently_screened: false,
        backdrop_url: null,
        theaters: [],
      })
    if (path === '/api/v1/auth/session')
      return json(response, syntheticSession())
    if (path === '/api/v1/account/theater-follows') {
      followGets++
      return json(response, follows)
    }
    if (path === '/api/v1/account/activity') {
      activityGets++
      if (scenario.feed === 'error')
        return json(
          response,
          { error: { code: 'accounts_unavailable', message: 'Indisponible' } },
          503,
        )
      const cursor = url.searchParams.get('cursor')
      if (cursor && cursor !== `fixture-${follows.revision}`)
        return json(
          response,
          {
            error: {
              code: 'theater_follows_changed',
              message: 'Suivis modifiés',
            },
          },
          409,
        )
      return json(response, activityPage(cursor))
    }
    if (path === '/api/v1/account/theaters') {
      if (scenario.selectionError)
        return json(response, { error: { code: 'accounts_unavailable' } }, 503)
      return json(response, selected)
    }
    if (path === '/api/v1/account/watchlist') return json(response, watchlist)
    if (path === '/api/v1/admin/session')
      return json(response, { authenticated: false })
    if (path === '/api/v1/theaters')
      return json(response, [theater, secondTheater])
    if (path === '/api/v1/cities')
      return json(response, {
        generated_at: generatedAt,
        items: [{ name: 'Lille', slug: 'lille', theaters: [theater] }],
      })
    if (
      path === `/api/v1/theaters/${theater.slug}/activity` ||
      path === `/api/v1/theaters/${secondTheater.slug}/activity`
    ) {
      if (scenario.publicFeed === 'error')
        return json(response, { error: { code: 'history_unavailable' } }, 503)
      return json(
        response,
        publicActivityPage(
          url.searchParams.get('cursor'),
          path.includes(secondTheater.slug) ? secondTheater : theater,
          scenario.publicFeed || 'populated',
        ),
      )
    }
    if (path === `/api/v1/theaters/${theater.slug}/showtimes`)
      return json(response, showtimes(url.searchParams.get('date') || date))
    if (path === `/api/v1/theaters/${secondTheater.slug}/showtimes`)
      return json(response, {
        ...showtimes(url.searchParams.get('date') || date),
        theater: secondTheater,
      })
    if (path === '/api/v1/theaters/missing/showtimes')
      return json(
        response,
        { error: { code: 'not_found', message: 'Cinéma introuvable' } },
        404,
      )
    if (path === '/api/v1/movies/upcoming') {
      upcomingQueries.push(Object.fromEntries(url.searchParams))
      const mode = scenario.upcoming || 'populated'
      if (mode === 'error' || mode === 'generic-error')
        return json(
          response,
          {
            error: {
              code:
                mode === 'error'
                  ? 'upcoming_unavailable'
                  : 'schedule_unavailable',
            },
          },
          503,
        )
      const value = releaseCatalog(url.searchParams, mode)
      if (mode === 'delay') {
        setTimeout(() => {
          if (!response.destroyed) json(response, value)
        }, 1500)
        return
      }
      return json(response, value)
    }
    if (
      path === '/api/v1/movies' &&
      url.searchParams.get('screening_summary') === 'true'
    ) {
      screeningQueries.push(Object.fromEntries(url.searchParams))
      const mode = scenario.screenings
      if (mode === 'error')
        return json(response, { error: { code: 'schedule_unavailable' } }, 503)
      const value = screeningCatalog(url.searchParams)
      if (mode === 'partial') delete value.items[0]?.next_7_days_showtime_count
      if (mode === 'delay') {
        // Bounded transport delay only for a loading-state scenario.
        setTimeout(() => {
          if (!response.destroyed) json(response, value)
        }, 1500)
        return
      }
      return json(response, value)
    }
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
  if (
    request.method === 'POST' &&
    [
      '/api/v1/account/watchlist',
      '/api/v1/account/watchlist/preferences',
      '/api/v1/account/watchlist/search',
    ].includes(path)
  ) {
    let body = ''
    request.on('data', (chunk) => {
      body += chunk
    })
    request.on('end', () => {
      const input = JSON.parse(body)
      if (path.endsWith('/search'))
        return json(response, {
          username: scenario.username,
          catalog: [movie],
          external: [
            {
              tmdb_id: '42',
              title: 'Autre film recherché',
              release_date: date,
            },
          ],
          external_status: 'ready',
          catalog_has_more: false,
        })
      if (path.endsWith('/preferences')) {
        watchlist.view_mode = input.view_mode
        watchlist.filter_tag_id = input.filter_tag_id || null
      } else {
        watchlist.items = watchlist.items.filter(
          (item) => item.slug !== input.movie_slug,
        )
      }
      watchlist.revision = String(BigInt(watchlist.revision) + 1n)
      json(response, watchlist)
    })
    return
  }
  if (request.method === 'POST' && path === '/api/v1/account/theater-follows') {
    let body = ''
    request.on('data', (chunk) => {
      body += chunk
    })
    request.on('end', () => {
      const input = JSON.parse(body)
      followPosts++
      if (input.expected_username !== scenario.username)
        return json(
          response,
          {
            error: {
              code: 'authentication_required',
              message: 'Connexion requise',
            },
          },
          401,
        )
      if (input.expected_revision !== follows.revision)
        return json(
          response,
          {
            error: {
              code: 'theater_follows_changed',
              message: 'Suivis modifiés',
            },
          },
          409,
        )
      const ids = new Set(follows.theater_ids)
      if (input.followed === 'true') ids.add(input.theater_id)
      else ids.delete(input.theater_id)
      follows = {
        username: scenario.username,
        revision: String(BigInt(follows.revision) + 1n),
        theater_ids: [...ids].sort(),
      }
      return json(response, follows)
    })
    return
  }
  if (request.method === 'POST' && path === '/api/v1/auth/logout') {
    scenario.state = 'anonymous'
    request.resume()
    return json(response, {})
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
      return json(response, {
        unexpected,
        follows,
        followPosts,
        followGets,
        activityGets,
        screeningQueries,
        upcomingQueries,
        selected,
      })
    if (request.url === '/__playwright/scenario' && request.method === 'POST') {
      let body = ''
      request.on('data', (chunk) => {
        body += chunk
      })
      request.on('end', () => {
        const input = JSON.parse(body)
        scenario = {
          enabled: false,
          state: 'anonymous',
          username: 'fixture_alice',
          feed: 'populated',
          ...input,
        }
        follows = {
          username: scenario.username,
          revision: input.revision || '0',
          theater_ids: input.theater_ids || [],
        }
        selected = {
          username: scenario.username,
          revision: '1',
          theater_ids: input.selected_ids ?? [secondTheater.id],
        }
        watchlist = syntheticWatchlist(scenario.username, !!input.watchlist)
        screeningQueries.length = 0
        upcomingQueries.length = 0
        followPosts = 0
        followGets = 0
        activityGets = 0
        unexpected.length = 0
        json(response, { ready: true })
      })
      return
    }
    if (request.url === '/__playwright/upcoming' && request.method === 'POST') {
      let body = ''
      request.on('data', (chunk) => {
        body += chunk
      })
      request.on('end', () => {
        const input = JSON.parse(body)
        scenario.upcoming = input.mode || 'populated'
        json(response, { ready: true })
      })
      return
    }
    if (
      request.url === '/__playwright/screenings' &&
      request.method === 'POST'
    ) {
      let body = ''
      request.on('data', (chunk) => {
        body += chunk
      })
      request.on('end', () => {
        const input = JSON.parse(body)
        if (input.mode) scenario.screenings = input.mode
        if ('selectionError' in input)
          scenario.selectionError = input.selectionError
        if (input.selected_ids)
          selected = {
            ...selected,
            revision: String(BigInt(selected.revision) + 1n),
            theater_ids: input.selected_ids,
          }
        json(response, { ready: true })
      })
      return
    }
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
