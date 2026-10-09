import assert from 'node:assert/strict'
import test from 'node:test'
import type { CatalogMovie, SitemapDataResponse } from '../app/types/api.ts'
import {
  buildFilmSitemapEntries,
  buildCinemaSitemapEntries,
  buildCitySitemapEntries,
  renderSitemap,
  renderSitemapIndex,
  validateSitemapData,
  validTimestamp,
  parseSitemapData,
} from '../server/utils/sitemap.ts'

const observed = '2026-10-09T10:00:00.123456Z'
function movie(
  id: number,
  overrides: Partial<CatalogMovie> = {},
): CatalogMovie {
  return {
    slug: `film-${id}`,
    title: `Film ${id}`,
    original_language: null,
    runtime_minutes: 100,
    updated_at: '2026-08-30T09:00:00Z',
    poster_url: null,
    tmdb_id: null,
    imdb_id: 'tt123456',
    metacritic_id: null,
    overview: 'Résumé durable',
    release_date: '2026-08-20',
    french_release_date: null,
    genres: ['Drame'],
    showtime_count: 0,
    ...overrides,
  }
}
function snapshot(): SitemapDataResponse {
  return {
    as_of: observed,
    revision: 'schedule:9007199254740993;enrichment:2;location:1',
    movies: [
      movie(20, { showtime_count: 3 }),
      movie(10, { showtime_count: 1, overview: null }),
      movie(30),
      movie(40, { overview: ' ' }),
      movie(50, { french_release_date: '2027-01-01' }),
    ],
    movie_total: 5,
    cities: {
      generated_at: '2026-08-30T08:00:00Z',
      items: [
        {
          name: 'Paris & proche',
          slug: 'paris & proche',
          theaters: [
            {
              provider: 'ugc',
              id: 'ugc-2',
              slug: 'ugc-zeta',
              name: 'UGC Zeta',
            },
            {
              provider: 'pathe',
              id: 'pathe-1',
              slug: 'pathé alpha',
              name: 'Pathé Alpha',
            },
          ],
        },
        {
          name: 'Lyon',
          slug: 'lyon',
          theaters: [
            {
              provider: 'cgr',
              id: 'cgr-1',
              slug: 'cgr-lyon',
              name: 'CGR Lyon',
            },
          ],
        },
      ],
    },
    upcoming_available: true,
    lastmod_by_path: {
      '/film/film-10': null,
      '/film/film-20': observed,
      '/film/film-30': '2026-09-01T08:00:00Z',
      '/film/film-40': null,
      '/film/film-50': null,
      '/cinema/ugc-zeta': null,
      '/cinema/path%C3%A9%20alpha': observed,
      '/cinema/cgr-lyon': null,
      '/ville/paris%20%26%20proche/cinemas': observed,
      '/ville/lyon/cinemas': null,
    },
  }
}

test('preserves eligibility for screened, rich ended/upcoming and thin films with only observed detail dates', () => {
  const data = parseSitemapData(snapshot())
  const before = structuredClone(data)
  assert.deepEqual(buildFilmSitemapEntries(data), [
    { path: '/' },
    { path: '/films' },
    { path: '/film/film-10' },
    { path: '/film/film-20', lastmod: observed },
    { path: '/film/film-30', lastmod: '2026-09-01T08:00:00Z' },
    { path: '/film/film-50' },
    { path: '/films/prochainement' },
  ])
  assert.deepEqual(data, before)
  data.upcoming_available = false
  assert(
    !buildFilmSitemapEntries(data).some(
      (entry) => entry.path === '/films/prochainement',
    ),
  )
})

test('local inventories preserve encoded sorted URLs, null omission and independent observed dates', () => {
  assert.deepEqual(buildCinemaSitemapEntries(snapshot()), [
    { path: '/cinemas' },
    { path: '/cinema/cgr-lyon' },
    { path: '/cinema/path%C3%A9%20alpha', lastmod: observed },
    { path: '/cinema/ugc-zeta' },
  ])
  assert.deepEqual(buildCitySitemapEntries(snapshot()), [
    { path: '/ville/lyon/cinemas' },
    { path: '/ville/paris%20%26%20proche/cinemas', lastmod: observed },
  ])
})

test('catalogue-only supports films but rejects unavailable local sitemap, with no fallback dates', () => {
  const data = snapshot()
  data.cities = null
  for (const key of Object.keys(data.lastmod_by_path))
    if (!key.startsWith('/film/')) delete data.lastmod_by_path[key]
  assert.doesNotThrow(() => buildFilmSitemapEntries(data))
  assert.throws(
    () => buildCinemaSitemapEntries(data),
    /Local sitemap unavailable/,
  )
  assert.throws(
    () => buildCitySitemapEntries(data),
    /Local sitemap unavailable/,
  )
})

const invalidCases = {
  'non-string observation': (data) => {
    Object.defineProperty(data, 'as_of', { value: 123 })
  },
  'non-string revision': (data) => {
    Object.defineProperty(data, 'revision', { value: 123 })
  },
  'non-array movies': (data) => {
    Object.defineProperty(data, 'movies', { value: {} })
  },
  'non-number movie total': (data) => {
    Object.defineProperty(data, 'movie_total', { value: '5' })
  },
  'missing cities': (data) => {
    Object.defineProperty(data, 'cities', { value: undefined })
  },
  'non-object cities': (data) => {
    Object.defineProperty(data, 'cities', { value: [] })
  },
  'non-array city inventory': (data) => {
    Object.defineProperty(data.cities, 'items', { value: null })
  },
  'non-string movie metadata': (data) => {
    Object.defineProperty(data.movies[0], 'overview', { value: 123 })
  },
  'non-string city name': (data) => {
    Object.defineProperty(data.cities!.items[0], 'name', { value: 123 })
  },
  'non-string theater slug': (data) => {
    Object.defineProperty(data.cities!.items[0]!.theaters[0], 'slug', {
      value: 123,
    })
  },
  'non-object timestamp map': (data) => {
    Object.defineProperty(data, 'lastmod_by_path', { value: [] })
  },
  'null timestamp map': (data) => {
    Object.defineProperty(data, 'lastmod_by_path', { value: null })
  },
  'non-string detail timestamp': (data) => {
    Object.defineProperty(data.lastmod_by_path, '/film/film-20', { value: 123 })
  },
  'duplicate film': (data) => {
    data.movies[1] = data.movies[0]!
  },
  'merged alias': (data) => {
    data.movies[1]!.slug = 'old-alias'
  },
  'duplicate city': (data) => {
    data.cities!.items[1]!.slug = data.cities!.items[0]!.slug
  },
  'duplicate theater ID': (data) => {
    data.cities!.items[1]!.theaters[0]!.id = 'ugc-2'
  },
  'duplicate theater slug': (data) => {
    data.cities!.items[1]!.theaters[0]!.slug = 'ugc-zeta'
  },
  'incomplete movie total': (data) => {
    data.movie_total++
  },
  'missing map key including thin film': (data) => {
    delete data.lastmod_by_path['/film/film-40']
  },
  'extra map key': (data) => {
    data.lastmod_by_path['/film/film-999'] = null
  },
  'hub map key': (data) => {
    data.lastmod_by_path['/'] = observed
  },
  'undefined map value': (data) => {
    Object.defineProperty(data.lastmod_by_path, '/film/film-20', {
      value: undefined,
    })
  },
  'malformed timestamp': (data) => {
    data.lastmod_by_path['/film/film-20'] = 'invalid'
  },
  'impossible date': (data) => {
    data.lastmod_by_path['/film/film-20'] = '2026-02-30T10:00:00Z'
  },
  'future timestamp': (data) => {
    data.lastmod_by_path['/film/film-20'] = '2026-10-10T00:00:00Z'
  },
  'future microsecond': (data) => {
    data.lastmod_by_path['/film/film-20'] = '2026-10-09T10:00:00.123457Z'
  },
  'invalid as_of': (data) => {
    data.as_of = 'invalid'
  },
  'missing revision': (data) => {
    data.revision = ''
  },
  'invalid availability': (data) => {
    Object.defineProperty(data, 'upcoming_available', { value: undefined })
  },
} satisfies Record<string, (data: SitemapDataResponse) => void>
for (const [name, mutate] of Object.entries(invalidCases)) {
  test(`complete snapshot rejects ${name} in every family`, () => {
    const data = snapshot()
    mutate(data)
    for (const build of [
      buildFilmSitemapEntries,
      buildCinemaSitemapEntries,
      buildCitySitemapEntries,
    ])
      assert.throws(() => build(parseSitemapData(data)))
  })
}

test('timestamps compare observation instants including offset equivalence and full microseconds', () => {
  const data = snapshot()
  data.lastmod_by_path['/film/film-20'] = '2026-10-09T12:00:00.123456+02:00'
  assert.doesNotThrow(() => validateSitemapData(data))
  for (const value of [
    '',
    '2026-02-29T12:00:00Z',
    '2026-01-01T24:00:00Z',
    '2026-01-01T12:00:00+25:00',
  ])
    assert.equal(validTimestamp(value), false)
  assert.equal(validTimestamp('2024-02-29T12:00:00.000001Z'), true)
})

test('XML escapes URLs, omits unknown/hub dates, preserves trailing newlines and rejects invalid provided dates/duplicates', () => {
  const index = renderSitemapIndex('https://messeances.fr', [
    '/sitemaps/films&archive.xml',
  ])
  assert(
    index.includes(
      '<loc>https://messeances.fr/sitemaps/films&amp;archive.xml</loc>',
    ),
  )
  assert(!index.includes('<lastmod>'))
  assert(index.endsWith('\n'))
  const entries = buildFilmSitemapEntries(snapshot())
  const xml = renderSitemap('https://messeances.fr', entries)
  assert.equal((xml.match(/<lastmod>/g) ?? []).length, 2)
  assert(xml.includes(`<lastmod>${observed}</lastmod>`))
  assert(xml.includes('<url>\n    <loc>https://messeances.fr/</loc>\n  </url>'))
  assert(xml.endsWith('\n'))
  assert.throws(() =>
    renderSitemap('https://messeances.fr', [...entries, entries[0]!]),
  )
  assert.throws(() =>
    renderSitemap('https://messeances.fr', [
      { path: '/films', lastmod: 'invalid' },
    ]),
  )
})
