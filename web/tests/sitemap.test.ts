import assert from 'node:assert/strict'
import test from 'node:test'
import type { CatalogMovie, CitiesResponse, MoviesResponse } from '../app/types/api.ts'
import {
  API_SITEMAP_CACHE_POLICY,
  buildCinemaSitemapEntries,
  buildCitySitemapEntries,
  buildFilmSitemapEntries,
  latestTimestamp,
  renderSitemap,
  renderSitemapIndex,
  validateCatalogPage
} from '../server/utils/sitemap.ts'

const GENERATED_AT = '2026-08-30T10:00:00Z'
const REVISION = 'revision-1'

test('uses one immutable five-minute SWR policy for API-backed child sitemaps', () => {
  assert.deepEqual(API_SITEMAP_CACHE_POLICY, { maxAge: 300, swr: true })
  assert(Object.isFrozen(API_SITEMAP_CACHE_POLICY))
})

function movie(slug: string, overrides: Partial<CatalogMovie> = {}): CatalogMovie {
  return {
    slug,
    title: slug,
    runtime_minutes: 100,
    updated_at: '2026-08-30T09:00:00Z',
    poster_url: 'https://example.test/poster.jpg',
    tmdb_id: null,
    imdb_id: null,
    overview: 'Résumé durable',
    release_date: '2026-08-20',
    genres: ['Drame'],
    ...overrides
  }
}

function catalog(items: CatalogMovie[], pageSize: number, total = items.length): MoviesResponse {
  return {
    items,
    available_genres: ['Drame'],
    page: 1,
    page_size: pageSize,
    total,
    generated_at: GENERATED_AT,
    catalog_revision: REVISION
  }
}

const cities: CitiesResponse = {
  generated_at: '2026-08-30T08:00:00Z',
  items: [
    {
      name: 'Paris & proche',
      slug: 'paris',
      theaters: [
        { provider: 'ugc', id: 'ugc-2', slug: 'ugc-zeta', name: 'UGC Zeta' },
        { provider: 'pathe', id: 'pathe-1', slug: 'pathe-alpha', name: 'Pathé Alpha' }
      ]
    },
    {
      name: 'Lyon',
      slug: 'lyon',
      theaters: [{ provider: 'cgr', id: 'cgr-1', slug: 'cgr-lyon', name: 'CGR Lyon' }]
    }
  ]
}

test('groups canonical film URLs and applies source-backed lastmod formulas', () => {
  const current = movie('film-20', { showtime_count: 3 })
  const enrichedCurrent = movie('film-10', { showtime_count: 1, updated_at: '2026-08-30T11:00:00Z' })
  const richEnded = movie('film-30', { showtime_count: 0, updated_at: '2026-08-29T07:00:00Z', poster_url: null, imdb_id: 'tt0030' })
  const thinEnded = movie('film-40', { showtime_count: undefined, overview: ' ' })
  const homepageMovie = movie('film-10', { updated_at: '2026-08-30T12:00:00Z', showtime_count: 1 })
  const filmsMovie = movie('film-20', { updated_at: '2026-08-30T09:30:00Z', showtime_count: 3 })
  const all = [current, enrichedCurrent, richEnded, thinEnded]
  const before = structuredClone(all)

  const entries = buildFilmSitemapEntries(
    all,
    { total: all.length, generated_at: GENERATED_AT, catalog_revision: REVISION },
    catalog([homepageMovie], 6),
    catalog([filmsMovie], 24)
  )

  assert.deepEqual(entries, [
    { path: '/', lastmod: '2026-08-30T12:00:00Z' },
    { path: '/films', lastmod: '2026-08-30T11:00:00Z' },
    { path: '/film/film-10', lastmod: '2026-08-30T11:00:00Z' },
    { path: '/film/film-20', lastmod: GENERATED_AT },
    { path: '/film/film-30', lastmod: '2026-08-29T07:00:00Z' }
  ])
  assert.deepEqual(all, before)
  assert(!entries.some((entry) => entry.path.includes('film-40') || ['/planning', '/recherche'].includes(entry.path)))
})

test('advances films lastmod when an off-page current movie changes global catalog metadata', () => {
  const currentMovies = Array.from({ length: 25 }, (_, index) => movie(`film-${index + 1}`, {
    showtime_count: 1,
    updated_at: index === 24 ? '2026-08-30T13:00:00Z' : '2026-08-30T09:00:00Z',
    genres: index === 24 ? ['Genre hors première page'] : ['Drame']
  }))
  const homepageCatalog = catalog(currentMovies.slice(0, 6), 6, currentMovies.length)
  const filmsCatalog = {
    ...catalog(currentMovies.slice(0, 24), 24, currentMovies.length),
    available_genres: ['Drame', 'Genre hors première page']
  }

  const entries = buildFilmSitemapEntries(
    currentMovies,
    { total: currentMovies.length, generated_at: GENERATED_AT, catalog_revision: REVISION },
    homepageCatalog,
    filmsCatalog
  )

  assert.equal(filmsCatalog.items.some((item) => item.slug === 'film-25'), false)
  assert.equal(filmsCatalog.available_genres.includes('Genre hors première page'), true)
  assert.equal(entries.find((entry) => entry.path === '/films')?.lastmod, '2026-08-30T13:00:00Z')
})

test('groups cinema and city URLs with inventory generation timestamp', () => {
  assert.deepEqual(buildCinemaSitemapEntries(cities), [
    { path: '/cinemas', lastmod: cities.generated_at },
    { path: '/cinema/cgr-lyon', lastmod: cities.generated_at },
    { path: '/cinema/pathe-alpha', lastmod: cities.generated_at },
    { path: '/cinema/ugc-zeta', lastmod: cities.generated_at }
  ])
  assert.deepEqual(buildCitySitemapEntries(cities), [
    { path: '/ville/lyon/cinemas', lastmod: cities.generated_at },
    { path: '/ville/paris/cinemas', lastmod: cities.generated_at }
  ])
})

test('renders deterministic escaped sitemap index and URL set with trailing newlines', () => {
  const index = renderSitemapIndex('https://messeances.fr', ['/sitemaps/films&archive.xml'])
  assert(index.includes('<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'))
  assert(index.includes('<loc>https://messeances.fr/sitemaps/films&amp;archive.xml</loc>'))
  assert(!index.includes('<lastmod>'))
  assert(index.endsWith('\n'))

  const urlset = renderSitemap('https://messeances.fr', [{ path: '/film/film-42', lastmod: GENERATED_AT }])
  assert(urlset.includes('<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'))
  assert(urlset.includes('<loc>https://messeances.fr/film/film-42</loc>'))
  assert(urlset.includes(`<lastmod>${GENERATED_AT}</lastmod>`))
  assert(urlset.endsWith('\n'))
})

test('rejects malformed timestamps, inconsistent snapshots, and duplicate URLs', () => {
  assert.throws(() => latestTimestamp(GENERATED_AT, 'not-a-timestamp'), /Invalid sitemap timestamp/)
  assert.throws(() => validateCatalogPage({ ...catalog([], 6), generated_at: 'invalid' }, { page: 1, pageSize: 6 }), /Inconsistent movie catalog snapshot/)
  assert.throws(() => buildFilmSitemapEntries(
    [movie('film-1', { updated_at: 'invalid', showtime_count: 1 })],
    { total: 1, generated_at: GENERATED_AT, catalog_revision: REVISION },
    catalog([], 6, 0),
    catalog([], 24, 0)
  ), /Invalid movie catalog item/)
  assert.throws(() => renderSitemap('https://messeances.fr', [
    { path: '/films', lastmod: GENERATED_AT },
    { path: '/films', lastmod: GENERATED_AT }
  ]), /Invalid sitemap entries/)
})

test('validates later paginated catalog item counts against remaining total', () => {
  const page = {
    ...catalog([movie('film-101')], 100, 101),
    page: 2
  }
  assert.doesNotThrow(() => validateCatalogPage(page, { page: 2, pageSize: 100, total: 101 }))
})
