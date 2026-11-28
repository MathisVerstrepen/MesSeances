import assert from 'node:assert/strict'
import { access, readFile } from 'node:fs/promises'
import test from 'node:test'
import type { H3Event } from 'h3'
import type { CatalogMovie, SitemapDataResponse } from '../app/types/api.ts'
import * as internalApi from '../server/utils/internalApi.ts'
import * as llms from '../server/utils/llms.ts'
import * as robots from '../server/utils/robots.ts'
import * as sitemap from '../server/utils/sitemap.ts'
import { executeModule } from './helpers/renderVue.ts'

const site = 'https://site.test'

function movie(
  id: number,
  overrides: Partial<CatalogMovie> = {},
): CatalogMovie {
  return {
    slug: `film-${id}`,
    title: `Film ${id}`,
    original_language: null,
    runtime_minutes: 100,
    updated_at: '2026-10-01T09:00:00Z',
    poster_url: null,
    tmdb_id: null,
    imdb_id: null,
    metacritic_id: null,
    overview: null,
    release_date: '2026-09-20',
    french_release_date: null,
    genres: ['Drame'],
    showtime_count: 0,
    ...overrides,
  }
}

function snapshot(): SitemapDataResponse {
  const movies = [
    movie(1, { title: 'Peu programmé', showtime_count: 2 }),
    movie(2, {
      title: 'Le *grand* [film]',
      showtime_count: 40,
      genres: ['Action', 'Aventure'],
    }),
    movie(3, { title: 'Sans séance', showtime_count: 0 }),
  ]
  const cities = {
    generated_at: '2026-10-16T09:00:00Z',
    items: [
      {
        name: 'Lyon',
        slug: 'lyon',
        theaters: [
          {
            provider: 'pathe',
            id: 'pathe-1',
            slug: 'pathe-bellecour',
            name: 'Pathé',
          },
        ],
      },
      {
        name: 'Agen',
        slug: 'agen',
        theaters: [
          {
            provider: 'ticketingcine',
            id: 'ticketingcine-EMS0204',
            slug: 'ticketingcine-EMS0204',
            name: "Les montreurs d'images",
          },
          {
            provider: 'cgr',
            id: 'cgr-P0867',
            slug: 'cgr-P0867',
            name: 'CGR Agen',
          },
        ],
      },
    ],
  }
  const paths = [
    ...movies.map((item) => `/film/${item.slug}`),
    ...cities.items.flatMap((city) =>
      city.theaters.map((t) => `/cinema/${t.slug}`),
    ),
    ...cities.items.map((city) => `/ville/${city.slug}/cinemas`),
  ]
  return {
    as_of: '2026-10-16T10:00:00Z',
    revision: 'fixture:1',
    movies,
    movie_total: movies.length,
    cities,
    upcoming_available: true,
    lastmod_by_path: Object.fromEntries(paths.map((path) => [path, null])),
  }
}

function linkTargets(markdown: string): string[] {
  return [...markdown.matchAll(/\]\(([^)\s]+)\)/g)].map((match) => match[1]!)
}

test('llms.txt follows the llmstxt.org layout with absolute same-site links', () => {
  const body = llms.renderLlmsTxt(`${site}/ignored/path`)
  assert.match(body, /^# MesSeances\n\n> \S/)
  assert.ok(body.endsWith('\n') && !body.endsWith('\n\n'))
  const headings = [...body.matchAll(/^(#+) (.+)$/gm)].map(
    (m) => `${m[1]} ${m[2]}`,
  )
  assert.deepEqual(headings, [
    '# MesSeances',
    '## Pages du site',
    '## API JSON publique',
    '## Optional',
  ])
  const targets = linkTargets(body)
  assert.ok(targets.length >= 20)
  for (const target of targets) {
    assert.ok(target.startsWith(`${site}/`), target)
    assert.doesNotMatch(
      target,
      /\/(compte|admin)(\/|$)|\/api\/v1\/(account|auth|internal)/,
    )
  }
  assert.match(body, /Retry-After/)
  assert.match(body, /Europe\/Paris/)
})

test('llms.txt documents only registered public GET API routes', async () => {
  const router = await readFile(
    new URL('../../api/internal/httpapi/api.go', import.meta.url),
    'utf8',
  )
  const registered = new Set(
    [...router.matchAll(/\.Get\("(\/api\/v1\/[^"]+)"/g)].map((m) =>
      m[1]!.replace(/\{[^}]+\}/g, '{}'),
    ),
  )
  const documented = linkTargets(llms.renderLlmsTxt(site))
    .filter((target) => target.startsWith(`${site}/api/`))
    .map((target) =>
      target
        .slice(site.length)
        .split('?')[0]!
        .replace(/\{[^}]+\}/g, '{}'),
    )
  assert.ok(documented.length >= 8)
  for (const path of documented)
    assert.ok(registered.has(path), `${path} is not a registered GET route`)
})

test('llms.txt page links resolve to existing Nuxt pages or server routes', async () => {
  const pages = new URL('../app/pages/', import.meta.url)
  const routes = new URL('../server/routes/', import.meta.url)
  const exists = (url: URL) =>
    access(url).then(
      () => true,
      () => false,
    )
  for (const target of linkTargets(llms.renderLlmsTxt(site))) {
    const path = target.slice(site.length).split('?')[0]!
    if (path.startsWith('/api/')) continue
    const route = path.replace(/^\//, '').replace(/\{slug\}/g, '[slug]')
    const candidates = [
      new URL(`${route === '' ? 'index' : route}.vue`, pages),
      new URL(`${route}/index.vue`, pages),
      new URL(`${route}.ts`, routes),
    ]
    const found = await Promise.all(candidates.map(exists))
    assert.ok(found.includes(true), `${path} has no page or route`)
  }
})

test('llms-full.txt lists every city and theater and the most screened films', () => {
  const body = llms.renderLlmsFullTxt(site, snapshot())
  assert.ok(body.startsWith(llms.renderLlmsTxt(site)))
  assert.match(body, /2 villes, 3 cinémas\./)
  assert.match(body, /\)\n\n## Villes et cinémas\n/)
  assert.match(
    body,
    /^- \[Pathé\]\(https:\/\/site\.test\/cinema\/pathe-bellecour\): slug `pathe-bellecour`, id `pathe-1`, chaîne `pathe`$/m,
  )
  assert.ok(body.indexOf('### [Agen]') < body.indexOf('### [Lyon]'))
  assert.match(
    body,
    /^- \[CGR Agen\]\(https:\/\/site\.test\/cinema\/cgr-P0867\): slug `cgr-P0867`, chaîne `cgr`$/m,
  )
  assert.match(body, /\[Agen\]\(https:\/\/site\.test\/ville\/agen\/cinemas\)/)
  assert.ok(
    body.indexOf('CGR Agen') < body.indexOf("Les montreurs d'images"),
    'theaters sorted by name',
  )
  assert.match(
    body,
    /^- \[Le \\\*grand\\\* \\\[film\\\]\]\(https:\/\/site\.test\/film\/film-2\): slug `film-2` ; sortie 2026 ; Action, Aventure$/m,
  )
  assert.ok(
    body.indexOf('film-2`') < body.indexOf('film-1`'),
    'most screened first',
  )
  assert.doesNotMatch(body, /film-3/)
})

test('llms-full.txt caps the film list and rejects incomplete inventories', () => {
  const data = snapshot()
  data.movies = Array.from({ length: llms.LLMS_FULL_TOP_MOVIES + 5 }, (_, i) =>
    movie(i + 1, { showtime_count: i + 1 }),
  )
  data.movie_total = data.movies.length
  data.lastmod_by_path = Object.fromEntries([
    ...data.movies.map((item) => [`/film/${item.slug}`, null]),
    ...Object.entries(data.lastmod_by_path).filter(
      ([path]) => !path.startsWith('/film/'),
    ),
  ])
  const body = llms.renderLlmsFullTxt(site, data)
  assert.equal(
    body.match(/^- \[Film \d+\]/gm)?.length,
    llms.LLMS_FULL_TOP_MOVIES,
  )
  assert.doesNotMatch(body, /film-5`/)

  assert.throws(() =>
    llms.renderLlmsFullTxt(site, { ...snapshot(), cities: null }),
  )
  assert.throws(() =>
    llms.renderLlmsFullTxt(site, { ...snapshot(), movie_total: 9 }),
  )
})

test('robots.txt welcomes AI crawlers, points to llms.txt and keeps private paths out', () => {
  const body = robots.renderRobotsTxt(site)
  assert.match(body, /^# .*https:\/\/site\.test\/llms\.txt\n/)
  assert.match(body, /\nSitemap: https:\/\/site\.test\/sitemap\.xml\n$/)
  const groups = body
    .split(/\n\n+/)
    .filter((block) => /^User-agent:/m.test(block))
  assert.equal(groups.length, 2)
  const agents = groups.map((group) =>
    [...group.matchAll(/^User-agent: (.+)$/gm)].map((m) => m[1]),
  )
  assert.deepEqual(agents[0], ['*'])
  assert.deepEqual(agents[1], [...robots.AI_CRAWLERS])
  for (const crawler of ['GPTBot', 'ClaudeBot', 'Google-Extended', 'CCBot'])
    assert.ok(agents[1]!.includes(crawler))
  for (const group of groups) {
    assert.match(group, /^Allow: \/$/m)
    assert.doesNotMatch(group, /^Disallow: \/$/m)
    for (const path of robots.PRIVATE_PATH_PREFIXES)
      assert.match(group, new RegExp(`^Disallow: ${path}$`, 'm'))
  }
  assert.match(
    groups[0]!,
    /^Content-Signal: search=yes, ai-input=yes, ai-train=yes$/m,
  )
})

test('llms and robots handlers serve plain text from the configured site URL', async () => {
  for (const [file, imports, expected] of [
    ['llms.txt.ts', { '../utils/llms': llms }, llms.renderLlmsTxt(site)],
    [
      'robots.txt.ts',
      { '../utils/robots': robots },
      robots.renderRobotsTxt(site),
    ],
  ] as const) {
    const source = await readFile(
      new URL(`../server/routes/${file}`, import.meta.url),
      'utf8',
    )
    const event = { context: {} }
    const headers = new Headers()
    const handler = executeModule<{
      default: (request: typeof event) => string
    }>(
      source,
      {
        defineEventHandler: (fn: (request: typeof event) => string) => fn,
        useRuntimeConfig: () => ({ public: { siteUrl: site } }),
        setResponseHeader: (
          _event: typeof event,
          name: string,
          value: string,
        ) => headers.set(name, value),
      },
      imports,
    ).default
    assert.equal(handler(event), expected)
    assert.equal(headers.get('Content-Type'), 'text/plain; charset=utf-8')
  }
})

test('llms-full.txt handler makes one cached authenticated read and fails closed', async () => {
  const source = await readFile(
    new URL('../server/routes/llms-full.txt.ts', import.meta.url),
    'utf8',
  )
  const event = { context: {} }
  // SAFETY: initializeRequestId only writes event.context; this fixture provides the same request-scoped context as H3Event.
  const requestId = internalApi.initializeRequestId(event as H3Event)
  const calls: Array<{
    url: string
    options: { headers: Record<string, string>; retry: boolean }
  }> = []
  const headers = new Headers()
  let response: sitemap.SitemapDataPayload | Error = snapshot()
  const handler = executeModule<{
    default: (request: typeof event) => Promise<string>
  }>(
    source,
    {
      defineCachedEventHandler: (
        fn: (request: typeof event) => Promise<string>,
        policy: typeof llms.LLMS_FULL_CACHE_POLICY,
      ) => {
        assert.equal(policy, llms.LLMS_FULL_CACHE_POLICY)
        assert.equal(policy.maxAge, 300)
        assert.equal(policy.swr, true)
        assert.equal(
          policy.getKey({ path: '/llms-full.txt?x=1' }),
          '/llms-full.txt',
        )
        return fn
      },
      useRuntimeConfig: () => ({
        apiBase: 'http://api.test/',
        internalApiSharedSecret: 'private-token',
        public: { siteUrl: site },
      }),
      $fetch: async (
        url: string,
        options: { headers: Record<string, string>; retry: boolean },
      ) => {
        calls.push({ url, options })
        if (response instanceof Error) throw response
        return response
      },
      setResponseHeader: (_event: typeof event, name: string, value: string) =>
        headers.set(name, value),
      createError: (error: { statusCode: number; message: string }) =>
        Object.assign(new Error(error.message), error),
    },
    {
      '../utils/internalApi': internalApi,
      '../utils/llms': llms,
      '../utils/sitemap': sitemap,
    },
  ).default

  const body = await handler(event)
  assert.deepEqual(calls, [
    {
      url: 'http://api.test/api/v1/sitemap-data',
      options: {
        retry: false,
        headers: {
          'X-Request-ID': requestId,
          'X-Messeances-Internal-Token': 'private-token',
        },
      },
    },
  ])
  assert.equal(headers.get('Content-Type'), 'text/plain; charset=utf-8')
  assert.match(body, /## Villes et cinémas/)
  assert.doesNotMatch(body, /private-token/)

  for (response of [new Error('private upstream diagnostic'), { movies: [] }]) {
    calls.length = 0
    await assert.rejects(handler(event), {
      statusCode: 503,
      message: 'Guide unavailable',
    })
    assert.equal(calls.length, 1)
  }
})

test('every page advertises llms.txt to agents reading the HTML head', async () => {
  const config = await readFile(
    new URL('../nuxt.config.ts', import.meta.url),
    'utf8',
  )
  assert.match(
    config,
    /rel: 'alternate',\s*type: 'text\/plain',\s*href: '\/llms\.txt'/,
  )
})

test('sitemap data parsing rejects films without a string title', () => {
  const data = snapshot()
  assert.doesNotThrow(() => sitemap.parseSitemapData(data))
  // Wire payloads arrive as parsed JSON, so the fixture travels the same path.
  const untitled = JSON.parse(
    JSON.stringify({ ...data, movies: [{ ...data.movies[0], title: null }] }),
  )
  assert.throws(
    () => sitemap.parseSitemapData(untitled),
    /Invalid movie catalog item/,
  )
})
