import type { CatalogMovie, CitiesResponse, MoviesResponse } from '../../app/types/api.ts'
import { isIndexableMovie } from '../../app/utils/movieIndexability.ts'
import { absoluteSiteUrl } from '../../app/utils/siteUrl.ts'

export interface SitemapEntry {
  path: string
  lastmod: string
}

interface SitemapCacheKeyInput {
  path: string
}

export interface ApiSitemapCachePolicy {
  readonly maxAge: number
  readonly swr: true
  readonly getKey: (event: SitemapCacheKeyInput) => string
}

interface CatalogPageExpectation {
  page: number
  pageSize: number
  generatedAt?: string
  catalogRevision?: string
  total?: number
}

interface ValidatedCityInventory {
  citySlugs: string[]
  theaterSlugs: string[]
}

export const SITEMAP_CACHE_SECONDS = 300
export const API_SITEMAP_CACHE_POLICY = Object.freeze({ maxAge: SITEMAP_CACHE_SECONDS, swr: true as const })
function staticApiSitemapCachePolicy(cacheKey: string): Readonly<ApiSitemapCachePolicy> {
  return Object.freeze({ ...API_SITEMAP_CACHE_POLICY, getKey: (_event: SitemapCacheKeyInput) => cacheKey })
}
export const API_SITEMAP_CACHE_POLICIES = Object.freeze({
  films: staticApiSitemapCachePolicy('/sitemaps/films.xml'),
  cinemas: staticApiSitemapCachePolicy('/sitemaps/cinemas.xml'),
  cities: staticApiSitemapCachePolicy('/sitemaps/cities.xml')
})
export const SITEMAP_CATALOG_PAGE_SIZE = 100

function nonblank(value: string | null | undefined): boolean {
  return value !== null && value !== undefined && value.trim().length > 0
}

export function validTimestamp(value: string | null | undefined): boolean {
  if (value === null || value === undefined || value.trim().length === 0) return false
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
    && Number.isFinite(Date.parse(value))
}

export function latestTimestamp(...values: string[]): string {
  if (values.length === 0 || values.some((value) => !validTimestamp(value))) throw new Error('Invalid sitemap timestamp')
  return values.reduce((latest, value) => Date.parse(value) > Date.parse(latest) ? value : latest)
}

export function validateCatalogPage(page: MoviesResponse, expected: CatalogPageExpectation): void {
  const expectedItemCount = Math.max(0, Math.min(page.page_size, page.total - (page.page - 1) * page.page_size))
  if (
    page.page !== expected.page
    || page.page_size !== expected.pageSize
    || !Number.isSafeInteger(page.total)
    || page.total < 0
    || !Array.isArray(page.items)
    || page.items.length !== expectedItemCount
    || !validTimestamp(page.generated_at)
    || !nonblank(page.catalog_revision)
    || (expected.generatedAt !== undefined && page.generated_at !== expected.generatedAt)
    || (expected.catalogRevision !== undefined && page.catalog_revision !== expected.catalogRevision)
    || (expected.total !== undefined && page.total !== expected.total)
  ) {
    throw new Error('Inconsistent movie catalog snapshot')
  }
}

export function validateMovieInventory(movies: CatalogMovie[], expectedTotal: number): void {
  if (movies.length !== expectedTotal) throw new Error('Incomplete movie catalog snapshot')
  const slugs = new Set<string>()
  for (const movie of movies) {
    const slug = movie.slug.trim()
    if (!/^film-[1-9]\d*$/.test(slug) || slugs.has(slug) || !validTimestamp(movie.updated_at)) {
      throw new Error('Invalid movie catalog item')
    }
    slugs.add(slug)
  }
}

function visibleCatalogLastmod(page: MoviesResponse): string {
  return latestTimestamp(page.generated_at, ...page.items.map((movie) => movie.updated_at))
}

function currentCatalogLastmod(movies: CatalogMovie[], generatedAt: string): string {
  const currentMovieTimestamps = movies
    .filter((movie) => Number.isFinite(movie.showtime_count) && (movie.showtime_count ?? 0) > 0)
    .map((movie) => movie.updated_at)
  return latestTimestamp(generatedAt, ...currentMovieTimestamps)
}

export function buildFilmSitemapEntries(
  movies: CatalogMovie[],
  snapshot: Pick<MoviesResponse, 'total' | 'generated_at' | 'catalog_revision'>,
  homepageCatalog: MoviesResponse,
  filmsCatalog: MoviesResponse
): SitemapEntry[] {
  validateMovieInventory(movies, snapshot.total)
  validateCatalogPage(homepageCatalog, {
    page: 1,
    pageSize: 6,
    generatedAt: snapshot.generated_at,
    catalogRevision: snapshot.catalog_revision
  })
  validateCatalogPage(filmsCatalog, {
    page: 1,
    pageSize: 24,
    generatedAt: snapshot.generated_at,
    catalogRevision: snapshot.catalog_revision
  })

  const filmEntries = movies.flatMap((movie) => {
    const currentlyScreened = Number.isFinite(movie.showtime_count) && (movie.showtime_count ?? 0) > 0
    if (!isIndexableMovie(movie, currentlyScreened)) return []
    return [{
      path: `/film/${encodeURIComponent(movie.slug.trim())}`,
      lastmod: currentlyScreened ? latestTimestamp(movie.updated_at, snapshot.generated_at) : movie.updated_at
    }]
  }).sort((left, right) => left.path.localeCompare(right.path))

  return [
    { path: '/', lastmod: visibleCatalogLastmod(homepageCatalog) },
    { path: '/films', lastmod: currentCatalogLastmod(movies, snapshot.generated_at) },
    ...filmEntries
  ]
}

export function validateCityInventory(inventory: CitiesResponse): ValidatedCityInventory {
  if (!validTimestamp(inventory.generated_at) || !Array.isArray(inventory.items)) throw new Error('Invalid city inventory snapshot')
  const citySlugs: string[] = []
  const theaterSlugs: string[] = []
  const theaterIds = new Set<string>()

  for (const city of inventory.items) {
    if (!nonblank(city.name) || !nonblank(city.slug) || !Array.isArray(city.theaters) || city.theaters.length === 0) {
      throw new Error('Invalid city inventory item')
    }
    citySlugs.push(city.slug.trim())
    for (const theater of city.theaters) {
      if (!nonblank(theater.provider) || !nonblank(theater.id) || !nonblank(theater.slug) || !nonblank(theater.name) || theaterIds.has(theater.id)) {
        throw new Error('Invalid city theater inventory')
      }
      theaterIds.add(theater.id)
      theaterSlugs.push(theater.slug.trim())
    }
  }

  if (new Set(citySlugs).size !== citySlugs.length || new Set(theaterSlugs).size !== theaterSlugs.length) {
    throw new Error('Duplicate city or theater identity')
  }
  return { citySlugs: citySlugs.sort(), theaterSlugs: theaterSlugs.sort() }
}

export function buildCinemaSitemapEntries(inventory: CitiesResponse): SitemapEntry[] {
  const { theaterSlugs } = validateCityInventory(inventory)
  return [
    { path: '/cinemas', lastmod: inventory.generated_at },
    ...theaterSlugs.map((slug) => ({ path: `/cinema/${encodeURIComponent(slug)}`, lastmod: inventory.generated_at }))
  ]
}

export function buildCitySitemapEntries(inventory: CitiesResponse): SitemapEntry[] {
  const { citySlugs } = validateCityInventory(inventory)
  return citySlugs.map((slug) => ({ path: `/ville/${encodeURIComponent(slug)}/cinemas`, lastmod: inventory.generated_at }))
}

export function xmlEscape(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

export function renderSitemap(siteUrl: string, entries: SitemapEntry[]): string {
  const paths = entries.map((entry) => entry.path)
  if (new Set(paths).size !== paths.length || entries.some((entry) => !entry.path.startsWith('/') || !validTimestamp(entry.lastmod))) {
    throw new Error('Invalid sitemap entries')
  }
  const body = entries.map((entry) => {
    const location = xmlEscape(absoluteSiteUrl(siteUrl, entry.path))
    return `  <url>\n    <loc>${location}</loc>\n    <lastmod>${xmlEscape(entry.lastmod)}</lastmod>\n  </url>`
  }).join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</urlset>\n`
}

export function renderSitemapIndex(siteUrl: string, childPaths: string[]): string {
  if (new Set(childPaths).size !== childPaths.length || childPaths.some((path) => !path.startsWith('/'))) throw new Error('Invalid sitemap index entries')
  const body = childPaths.map((path) => `  <sitemap>\n    <loc>${xmlEscape(absoluteSiteUrl(siteUrl, path))}</loc>\n  </sitemap>`).join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</sitemapindex>\n`
}
