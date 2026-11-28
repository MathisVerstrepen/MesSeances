import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type {
  CatalogMovie,
  MovieShowtimesResponse,
  Showtime,
  TheaterShowtimesResponse,
  TimelineShowtime,
} from '../app/types/api.ts'
import * as date from '../app/utils/date.ts'
import * as siteUrl from '../app/utils/siteUrl.ts'
import * as internalApi from '../server/utils/internalApi.ts'
import * as markdownPages from '../server/utils/markdownPages.ts'
import { executeModule } from './helpers/renderVue.ts'

const site = 'https://site.test'
const {
  MarkdownQueryError,
  parseMarkdownPageRequest,
  renderCinemaMarkdown,
  renderFilmMarkdown,
} = markdownPages

const movie: CatalogMovie = {
  slug: 'film-42',
  title: 'Le *grand* film',
  original_title: 'The Big Film',
  original_language: 'en',
  runtime_minutes: 118,
  updated_at: '2026-08-01T00:00:00Z',
  poster_url: 'https://image.tmdb.org/t/p/w500/poster.jpg',
  tmdb_id: 42,
  imdb_id: null,
  metacritic_id: null,
  overview: 'Une histoire\nde cinéma.',
  release_date: '2026-07-01',
  french_release_date: '2026-08-20',
  genres: ['Drame', 'Comédie'],
}

function showtime(overrides: Partial<Showtime> = {}): Showtime {
  return {
    provider: 'cgr',
    id: 'cgr-showing-1',
    movie,
    start_time: '2026-08-29T16:00:00Z',
    end_time: '2026-08-29T17:58:00Z',
    estimated_end_time: null,
    estimated_end_ads_minutes: null,
    language: 'VOSTFR',
    format: '2D',
    room: 'Salle 1',
    booking_url: 'https://achat.cgrcinemas.fr/cgr-agen/r/123456',
    ...overrides,
  }
}

function schedule(): MovieShowtimesResponse {
  return {
    discovery: { window: null, cities: [] },
    release_status: 'showing',
    movie,
    backdrop_url: null,
    date: '2026-08-29',
    currently_screened: true,
    available_dates: ['2026-08-29', '2026-08-30'],
    catalog_revision: 'r1',
    available_languages: ['VOSTFR'],
    available_formats: ['2D'],
    pagination: { page: 1, page_size: 10, total: 12, has_more: true },
    theaters: [
      {
        provider: 'cgr',
        id: 'cgr-P0867',
        slug: 'cgr-P0867',
        name: 'CGR Agen',
        city: 'Agen',
        city_slug: 'agen',
        showtimes: [
          showtime({
            id: 'late',
            start_time: '2026-08-29T19:00:00Z',
            end_time: '2026-08-29T19:00:00Z',
            estimated_end_time: '2026-08-29T21:13:00Z',
            estimated_end_ads_minutes: 15,
            format: 'IMAX',
            room: '',
          }),
          showtime(),
        ],
      },
      {
        provider: 'megarama',
        id: 'megarama-1',
        slug: 'megarama-1',
        name: 'Megarama',
        city: 'Bordeaux',
        city_slug: 'bordeaux',
        showtimes: [],
      },
    ],
  }
}

function timelineShowtime(overrides: Partial<Showtime>): TimelineShowtime {
  return {
    ...showtime(overrides),
    start_offset_minutes: 0,
    duration_minutes: 118,
    poster_url: null,
    backdrop_url: null,
  }
}

function theaterResponse(): TheaterShowtimesResponse {
  const other = {
    ...movie,
    slug: 'film-7',
    title: 'Autre',
    runtime_minutes: 0,
    original_language: 'fr',
  }
  return {
    generated_at: '2026-08-29T00:00:00Z',
    timezone: 'Europe/Paris',
    technologies: ['IMAX', 'LASER_ULTRA'],
    available_formats: ['2D'],
    date: '2026-08-29',
    theater: {
      provider: 'cgr',
      id: 'cgr-P0867',
      slug: 'cgr-P0867',
      name: 'CGR Agen',
      address: 'Place du Pin',
      city: 'Agen',
      city_slug: 'agen',
      postal_code: '47000',
      available_dates: ['2026-08-29', '2026-08-31'],
      accepted_passes: ['CGR Pass'],
      image: null,
    },
    showtimes: [
      timelineShowtime({
        id: 'b',
        start_time: '2026-08-29T18:00:00Z',
        end_time: '2026-08-29T19:58:00Z',
      }),
      timelineShowtime({
        id: 'a',
        movie: other,
        language: 'VF',
        start_time: '2026-08-29T08:00:00Z',
        end_time: '2026-08-29T08:00:00Z',
      }),
      timelineShowtime({
        id: 'c',
        start_time: '2026-08-29T12:00:00Z',
        end_time: '2026-08-29T13:58:00Z',
        booking_url: null,
      }),
    ],
    discovery: { window: null, movies: [], other_theaters: [] },
  }
}

test('Markdown page requests accept only documented, well-formed parameters', () => {
  const query = (value: string) => new URLSearchParams(value)
  assert.equal(parseMarkdownPageRequest('/film/film-1', query('')), null)
  assert.equal(parseMarkdownPageRequest('/films/film-1.md', query('')), null)
  assert.equal(parseMarkdownPageRequest('/film/a/b.md', query('')), null)
  assert.deepEqual(
    parseMarkdownPageRequest(
      '/film/film-1.md',
      query('date=2026-08-29&city=agen&page=2'),
    ),
    { kind: 'film', slug: 'film-1', date: '2026-08-29', city: 'agen', page: 2 },
  )
  assert.deepEqual(
    parseMarkdownPageRequest('/cinema/cgr-P0867.md', query('city=agen&page=2')),
    { kind: 'cinema', slug: 'cgr-P0867' },
  )
  for (const bad of [
    'date=2026-02-30',
    'date=29/08/2026',
    'city=a%20b',
    'page=0',
    'page=1.5',
    'page=99999',
  ])
    assert.throws(
      () => parseMarkdownPageRequest('/film/film-1.md', query(bad)),
      MarkdownQueryError,
      bad,
    )
})

test('film Markdown lists facts, dated showtimes per theater and navigation links', () => {
  const body = renderFilmMarkdown(site, schedule(), {
    kind: 'film',
    slug: 'film-42',
  })
  assert.match(body, /^# Le \\\*grand\\\* film\n\n> Une histoire de cinéma\.\n/)
  for (const fact of [
    '- Titre original : The Big Film',
    '- Durée : 1h 58min',
    '- Genres : Drame, Comédie',
    '- Sortie en France : 2026-08-20',
    '- Affiche : https://image.tmdb.org/t/p/w500/poster.jpg',
    '- Slug : `film-42`',
  ])
    assert.ok(body.includes(`${fact}\n`), fact)
  assert.match(
    body,
    /\n## Séances du samedi 29 août \(2026-08-29\) en France\n/,
  )
  assert.match(
    body,
    /\nAutres dates : \[2026-08-30\]\(https:\/\/site\.test\/film\/film-42\.md\?date=2026-08-30\)\n/,
  )
  assert.match(
    body,
    /\n### \[CGR Agen\]\(https:\/\/site\.test\/cinema\/cgr-P0867\) · Agen\n/,
  )
  const lines = body
    .split('\n')
    .filter((line) => line.startsWith('- 1') || line.startsWith('- 2'))
  assert.deepEqual(lines, [
    '- 18h00 → 19h58 · VOSTFR · 2D · Salle 1 · [Réserver](https://achat.cgrcinemas.fr/cgr-agen/r/123456)',
    '- 21h00 → ~23h13 · VOSTFR · IMAX · [Réserver](https://achat.cgrcinemas.fr/cgr-agen/r/123456)',
  ])
  assert.doesNotMatch(
    body,
    /Megarama/,
    'theaters without showtimes are omitted',
  )
  assert.match(
    body,
    /\nPage 1 sur 2 \(12 cinémas\) · \[cinémas suivants\]\(https:\/\/site\.test\/film\/film-42\.md\?date=2026-08-29&page=2\)\n/,
  )
  assert.match(body, /Page complète : \[https:\/\/site\.test\/film\/film-42\]/)
  assert.match(
    body,
    /Données JSON : \[https:\/\/site\.test\/api\/v1\/movies\/film-42\/showtimes\?date=2026-08-29\]/,
  )
  assert.match(body, /\/api\/v1\/openapi\.json/)
  assert.ok(body.endsWith('\n'))
})

test('film Markdown keeps the city filter and explains empty schedules', () => {
  const data = schedule()
  data.pagination = { page: 2, page_size: 10, total: 12, has_more: false }
  const body = renderFilmMarkdown(site, data, {
    kind: 'film',
    slug: 'film-42',
    city: 'agen',
    page: 2,
  })
  assert.match(body, /## Séances du .* à Agen\n/)
  assert.match(body, /date=2026-08-30&city=agen\)/)
  assert.match(
    body,
    /\[cinémas précédents\]\(https:\/\/site\.test\/film\/film-42\.md\?date=2026-08-29&city=agen\)/,
  )
  assert.doesNotMatch(body, /cinémas suivants/)
  assert.match(body, /movies\/film-42\/showtimes\?date=2026-08-29&city=agen/)

  data.theaters = []
  data.available_dates = []
  data.release_status = 'upcoming'
  data.pagination = null
  assert.match(
    renderFilmMarkdown(site, data, { kind: 'film', slug: 'film-42' }),
    /\nPas encore de séance programmée : le film sort prochainement\.\n/,
  )
})

test('cinema Markdown groups showtimes by film in first-screening order', () => {
  const body = renderCinemaMarkdown(site, theaterResponse())
  for (const fact of [
    '- Adresse : Place du Pin, 47000 Agen',
    '- Ville : [Agen](https://site.test/ville/agen/cinemas)',
    '- Exploitant : `cgr`',
    '- Technologies : IMAX, Laser ULTRA by Kinepolis',
    '- Cartes acceptées : CGR Pass',
    '- Slug : `cgr-P0867`, id : `cgr-P0867`',
  ])
    assert.ok(body.includes(`${fact}\n`), fact)
  assert.match(
    body,
    /\nAutres dates : \[2026-08-31\]\(https:\/\/site\.test\/cinema\/cgr-P0867\.md\?date=2026-08-31\)\n/,
  )
  const other = body.indexOf('### [Autre](https://site.test/film/film-7)\n')
  const main = body.indexOf(
    '### [Le \\*grand\\* film](https://site.test/film/film-42) · 1h 58min\n',
  )
  assert.ok(other > 0 && main > other, 'films ordered by first showtime')
  assert.ok(
    body.includes('\n- 10h00 · VOF · 2D · Salle 1 · [Réserver]'),
    'unknown end shows start only; French original VF is VOF',
  )
  assert.ok(
    body.includes('\n- 14h00 → 15h58 · VOSTFR · 2D · Salle 1\n- 20h00 → 21h58'),
  )
  assert.match(body, /theaters\/cgr-P0867\/showtimes\?date=2026-08-29\]/)

  const none = theaterResponse()
  none.date = null
  assert.match(
    renderCinemaMarkdown(site, none),
    /\nAucune séance programmée actuellement\.\n/,
  )
})

type MiddlewareHandler = (event: MiddlewareEvent) => Promise<string | undefined>
type CachedRender = (
  event: MiddlewareEvent,
  request: markdownPages.MarkdownPageRequest,
) => Promise<string>

interface MiddlewareEvent {
  path: string
  method: string
  context: Record<string, string>
  status?: number
  headers: Headers
}

async function middleware(
  fetchImpl: (
    url: string,
    options: {
      headers: Record<string, string>
      query: Record<string, string | number>
    },
  ) => Promise<MovieShowtimesResponse | TheaterShowtimesResponse>,
) {
  const source = await readFile(
    new URL('../server/middleware/markdown-pages.ts', import.meta.url),
    'utf8',
  )
  return executeModule<{
    default: (event: MiddlewareEvent) => Promise<string | undefined>
  }>(
    source,
    {
      defineEventHandler: (handler: MiddlewareHandler) => handler,
      cachedFunction: (
        fn: CachedRender,
        options: {
          maxAge: number
          swr: boolean
          getKey: (
            event: MiddlewareEvent,
            request: markdownPages.MarkdownPageRequest,
          ) => string
        },
      ) => {
        assert.equal(
          options.getKey(event('/film/film-1.md'), {
            kind: 'film',
            slug: 'film-1',
            date: '2026-08-29',
            city: 'agen',
            page: 2,
          }),
          'film:film-1:2026-08-29:agen:2',
        )
        assert.equal(options.maxAge, 300)
        assert.equal(options.swr, true)
        return fn
      },
      useRuntimeConfig: () => ({
        apiBase: 'http://api.test/',
        internalApiSharedSecret: 'private-token',
        public: { siteUrl: site },
      }),
      $fetch: fetchImpl,
      getRequestURL: (event: MiddlewareEvent) =>
        new URL(event.path, 'http://localhost'),
      setResponseStatus: (event: MiddlewareEvent, status: number) => {
        event.status = status
      },
      setResponseHeader: (
        event: MiddlewareEvent,
        name: string,
        value: string,
      ) => event.headers.set(name, value),
    },
    {
      '../../app/utils/date': date,
      '../../app/utils/siteUrl': siteUrl,
      '../utils/internalApi': internalApi,
      '../utils/markdownPages': markdownPages,
    },
  ).default
}

function event(path: string, method = 'GET'): MiddlewareEvent {
  return { path, method, context: {}, headers: new Headers() }
}

test('Markdown middleware serves pages from the public API budget with a canonical link', async () => {
  const calls: Array<{
    url: string
    headers: Record<string, string>
    query: Record<string, string | number>
  }> = []
  const handler = await middleware(async (url, options) => {
    calls.push({ url, headers: options.headers, query: options.query })
    return url.includes('/theaters/') ? theaterResponse() : schedule()
  })

  assert.equal(await handler(event('/film/film-42')), undefined)
  assert.equal(calls.length, 0)

  const film = event('/film/film-42.md?date=2026-08-29&city=agen')
  const body = await handler(film)
  assert.match(body!, /^# Le \\\*grand\\\* film/)
  assert.equal(film.status, undefined)
  assert.equal(film.headers.get('Content-Type'), 'text/markdown; charset=utf-8')
  assert.equal(film.headers.get('Cache-Control'), 'public, max-age=300')
  assert.equal(
    film.headers.get('Link'),
    '<https://site.test/film/film-42>; rel="canonical"',
  )
  assert.equal(calls[0]!.url, 'http://api.test/api/v1/movies/film-42/showtimes')
  assert.deepEqual(calls[0]!.query, { date: '2026-08-29', city: 'agen' })
  assert.deepEqual(
    Object.keys(calls[0]!.headers),
    ['X-Request-ID'],
    'no internal token',
  )

  const cinema = event('/cinema/cgr-P0867.md')
  assert.match((await handler(cinema))!, /^# CGR Agen/)
  assert.equal(
    calls[1]!.url,
    'http://api.test/api/v1/theaters/cgr-P0867/showtimes',
  )
  assert.deepEqual(calls[1]!.query, {})
  assert.equal(
    cinema.headers.get('Link'),
    '<https://site.test/cinema/cgr-P0867>; rel="canonical"',
  )
})

test('undated film Markdown opens on the next screening day', async () => {
  const dates: Array<string | number | undefined> = []
  const handler = await middleware(async (_url, options) => {
    dates.push(options.query.date)
    const data = schedule()
    data.date = String(options.query.date)
    data.available_dates = ['2099-01-02', '2099-01-03']
    return data
  })
  const body = await handler(event('/film/film-42.md'))
  assert.deepEqual(dates, [date.todayInParis(), '2099-01-02'])
  assert.match(body!, /\(2099-01-02\) en France/)
})

test('Markdown middleware maps invalid queries and upstream failures to plain responses', async () => {
  let failure: Error = new Error('unused')
  const handler = await middleware(async () => {
    throw failure
  })
  const invalid = event('/film/film-42.md?date=demain')
  assert.match(
    (await handler(invalid))!,
    /^# Requête invalide\n\nLe paramètre date/,
  )
  assert.equal(invalid.status, 400)

  const post = event('/cinema/cgr-P0867.md', 'POST')
  await handler(post)
  assert.equal(post.status, 405)

  for (const [statusCode, status, title] of [
    [404, 404, '# Film introuvable'],
    [400, 400, '# Requête invalide'],
    [500, 503, '# Horaires momentanément indisponibles'],
  ] as const) {
    failure = Object.assign(new Error('private upstream body'), { statusCode })
    const response = event('/film/film-42.md')
    const body = await handler(response)
    assert.equal(response.status, status)
    assert.ok(body!.startsWith(title))
    assert.doesNotMatch(body!, /private upstream body/)
    assert.equal(response.headers.get('Cache-Control'), 'no-store')
  }

  failure = Object.assign(new Error('limited'), {
    statusCode: 429,
    response: { headers: new Headers({ 'Retry-After': '7' }) },
  })
  const limited = event('/cinema/cgr-P0867.md')
  assert.match((await handler(limited))!, /^# Trop de requêtes/)
  assert.equal(limited.status, 429)
  assert.equal(limited.headers.get('Retry-After'), '7')
})

test('film and cinema pages advertise their Markdown versions', async () => {
  for (const page of ['film/[slug].vue', 'cinema/[slug].vue']) {
    const source = await readFile(
      new URL(`../app/pages/${page}`, import.meta.url),
      'utf8',
    )
    assert.match(
      source,
      /rel: 'alternate',\s*type: 'text\/markdown',\s*href: `\$\{canonicalUrl\.value\}\.md`/,
      page,
    )
  }
})
