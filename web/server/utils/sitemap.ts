import type {
  CatalogMovie,
  CitiesResponse,
  SitemapDataResponse,
} from '../../app/types/api.ts'
import { isIndexableMovie } from '../../app/utils/movieIndexability.ts'
import { absoluteSiteUrl } from '../../app/utils/siteUrl.ts'

export interface SitemapEntry {
  path: string
  lastmod?: string
}

// Candidate at the API boundary, not a validated sitemap response.
export interface SitemapDataPayload extends Partial<SitemapDataResponse> {}

interface SitemapCacheKeyInput {
  path: string
}

export interface ApiSitemapCachePolicy {
  readonly maxAge: number
  readonly swr: true
  readonly getKey: (event: SitemapCacheKeyInput) => string
}

interface ValidatedCityInventory {
  citySlugs: string[]
  theaterSlugs: string[]
}

export const SITEMAP_CACHE_SECONDS = 300
export const API_SITEMAP_CACHE_POLICY = Object.freeze({
  maxAge: SITEMAP_CACHE_SECONDS,
  swr: true as const,
})
function staticApiSitemapCachePolicy(
  cacheKey: string,
): Readonly<ApiSitemapCachePolicy> {
  return Object.freeze({
    ...API_SITEMAP_CACHE_POLICY,
    getKey: (_event: SitemapCacheKeyInput) => cacheKey,
  })
}
export const API_SITEMAP_CACHE_POLICIES = Object.freeze({
  films: staticApiSitemapCachePolicy('/sitemaps/films.xml'),
  cinemas: staticApiSitemapCachePolicy('/sitemaps/cinemas.xml'),
  cities: staticApiSitemapCachePolicy('/sitemaps/cities.xml'),
})
function nonblank(value: string | null | undefined): boolean {
  return value !== null && value !== undefined && value.trim().length > 0
}

// Compare full fractional precision, not Date.parse's truncated milliseconds.
function timestampInstant(value: string): bigint | null {
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(
      value,
    )
  if (!match) return null
  const [, year, month, day, hour, minute, second, fraction = '', zone] = match
  const calendar = new Date(`${year}-${month}-${day}T00:00:00Z`)
  if (
    !Number.isFinite(calendar.getTime()) ||
    calendar.toISOString().slice(0, 10) !== `${year}-${month}-${day}` ||
    Number(hour) > 23 ||
    Number(minute) > 59 ||
    Number(second) > 59
  )
    return null
  if (
    zone !== 'Z' &&
    (Number(zone!.slice(1, 3)) > 23 || Number(zone!.slice(4)) > 59)
  )
    return null
  const milliseconds = Date.parse(
    `${year}-${month}-${day}T${hour}:${minute}:${second}${zone}`,
  )
  if (!Number.isFinite(milliseconds)) return null
  return (
    BigInt(milliseconds) * BigInt(1_000_000) + BigInt(fraction.padEnd(9, '0'))
  )
}

export function validTimestamp(value: string): boolean {
  return timestampInstant(value) !== null
}

export function validateMovieInventory(
  movies: CatalogMovie[],
  expectedTotal: number,
): void {
  if (
    !Array.isArray(movies) ||
    !Number.isSafeInteger(expectedTotal) ||
    expectedTotal < 0 ||
    movies.length !== expectedTotal
  )
    throw new Error('Incomplete movie catalog snapshot')
  const slugs = new Set<string>()
  for (const movie of movies) {
    const slug = movie.slug
    if (
      !/^film-[1-9]\d*$/.test(slug) ||
      slugs.has(slug) ||
      !validTimestamp(movie.updated_at)
    ) {
      throw new Error('Invalid movie catalog item')
    }
    slugs.add(slug)
  }
}

export function buildFilmSitemapEntries(
  data: SitemapDataResponse,
): SitemapEntry[] {
  validateSitemapData(data)

  const filmEntries = data.movies
    .flatMap((movie) => {
      const currentlyScreened =
        Number.isFinite(movie.showtime_count) && (movie.showtime_count ?? 0) > 0
      if (!isIndexableMovie(movie, currentlyScreened)) return []
      return [detailEntry(data, `/film/${encodeURIComponent(movie.slug)}`)]
    })
    .sort((left, right) => left.path.localeCompare(right.path))

  return [
    { path: '/' },
    { path: '/films' },
    ...filmEntries,
    ...(data.upcoming_available ? [{ path: '/films/prochainement' }] : []),
  ]
}

export function validateCityInventory(
  inventory: CitiesResponse,
): ValidatedCityInventory {
  if (
    !validTimestamp(inventory.generated_at) ||
    !Array.isArray(inventory.items)
  )
    throw new Error('Invalid city inventory snapshot')
  const citySlugs: string[] = []
  const theaterSlugs: string[] = []
  const theaterIds = new Set<string>()

  for (const city of inventory.items) {
    if (
      !nonblank(city.name) ||
      !nonblank(city.slug) ||
      !Array.isArray(city.theaters) ||
      city.theaters.length === 0
    ) {
      throw new Error('Invalid city inventory item')
    }
    citySlugs.push(city.slug.trim())
    for (const theater of city.theaters) {
      if (
        !nonblank(theater.provider) ||
        !nonblank(theater.id) ||
        !nonblank(theater.slug) ||
        !nonblank(theater.name) ||
        theaterIds.has(theater.id)
      ) {
        throw new Error('Invalid city theater inventory')
      }
      theaterIds.add(theater.id)
      theaterSlugs.push(theater.slug.trim())
    }
  }

  if (
    new Set(citySlugs).size !== citySlugs.length ||
    new Set(theaterSlugs).size !== theaterSlugs.length
  ) {
    throw new Error('Duplicate city or theater identity')
  }
  return { citySlugs: citySlugs.sort(), theaterSlugs: theaterSlugs.sort() }
}

export function buildCinemaSitemapEntries(
  data: SitemapDataResponse,
): SitemapEntry[] {
  const { theaterSlugs } = validateSitemapData(data)
  if (!data.cities) throw new Error('Local sitemap unavailable')
  return [
    { path: '/cinemas' },
    ...theaterSlugs.map((slug) =>
      detailEntry(data, `/cinema/${encodeURIComponent(slug)}`),
    ),
  ]
}

export function buildCitySitemapEntries(
  data: SitemapDataResponse,
): SitemapEntry[] {
  const { citySlugs } = validateSitemapData(data)
  if (!data.cities) throw new Error('Local sitemap unavailable')
  return citySlugs.map((slug) =>
    detailEntry(data, `/ville/${encodeURIComponent(slug)}/cinemas`),
  )
}

function detailEntry(data: SitemapDataResponse, path: string): SitemapEntry {
  const lastmod = data.lastmod_by_path[path]
  return lastmod === null ? { path } : { path, lastmod }
}

export function parseSitemapData(
  payload: SitemapDataPayload,
): SitemapDataResponse {
  if (
    Object.prototype.toString.call(payload.as_of) !== '[object String]' ||
    Object.prototype.toString.call(payload.revision) !== '[object String]' ||
    !Array.isArray(payload.movies) ||
    !Number.isSafeInteger(payload.movie_total) ||
    payload.cities === undefined ||
    (payload.upcoming_available !== true &&
      payload.upcoming_available !== false) ||
    Object.prototype.toString.call(payload.lastmod_by_path) !==
      '[object Object]'
  )
    throw new Error('Invalid sitemap data')

  for (const movie of payload.movies) {
    if (
      Object.prototype.toString.call(movie) !== '[object Object]' ||
      Object.prototype.toString.call(movie.slug) !== '[object String]' ||
      Object.prototype.toString.call(movie.updated_at) !== '[object String]' ||
      [
        movie.overview,
        movie.release_date,
        movie.poster_url,
        movie.imdb_id,
      ].some(
        (value) =>
          value !== null &&
          value !== undefined &&
          Object.prototype.toString.call(value) !== '[object String]',
      ) ||
      (movie.tmdb_id !== null &&
        movie.tmdb_id !== undefined &&
        !Number.isSafeInteger(movie.tmdb_id)) ||
      (movie.showtime_count !== undefined &&
        (!Number.isSafeInteger(movie.showtime_count) ||
          movie.showtime_count < 0)) ||
      !Array.isArray(movie.genres) ||
      movie.genres.some(
        (genre) => Object.prototype.toString.call(genre) !== '[object String]',
      )
    )
      throw new Error('Invalid movie catalog item')
  }
  if (payload.cities !== null) {
    const inventory = payload.cities
    if (
      Object.prototype.toString.call(inventory) !== '[object Object]' ||
      Object.prototype.toString.call(inventory.generated_at) !==
        '[object String]' ||
      !Array.isArray(inventory.items)
    )
      throw new Error('Invalid city inventory snapshot')
    for (const city of inventory.items) {
      if (
        Object.prototype.toString.call(city) !== '[object Object]' ||
        [city.name, city.slug].some(
          (value) =>
            Object.prototype.toString.call(value) !== '[object String]',
        ) ||
        !Array.isArray(city.theaters)
      )
        throw new Error('Invalid city inventory item')
      for (const theater of city.theaters) {
        if (
          Object.prototype.toString.call(theater) !== '[object Object]' ||
          [theater.provider, theater.id, theater.slug, theater.name].some(
            (value) =>
              Object.prototype.toString.call(value) !== '[object String]',
          )
        )
          throw new Error('Invalid city theater inventory')
      }
    }
  }
  if (
    Object.values(payload.lastmod_by_path!).some(
      (value) =>
        value !== null &&
        Object.prototype.toString.call(value) !== '[object String]',
    )
  )
    throw new Error('Invalid sitemap detail timestamp')

  // All fields consumed by sitemap builders passed boundary validation above.
  return {
    as_of: payload.as_of!,
    revision: payload.revision!,
    movies: payload.movies,
    movie_total: payload.movie_total!,
    cities: payload.cities,
    upcoming_available: payload.upcoming_available,
    lastmod_by_path: payload.lastmod_by_path!,
  }
}

export function validateSitemapData(
  data: SitemapDataResponse,
): ValidatedCityInventory {
  const asOf = timestampInstant(data.as_of)
  if (
    asOf === null ||
    !nonblank(data.revision) ||
    (data.upcoming_available !== true && data.upcoming_available !== false)
  )
    throw new Error('Invalid sitemap data')
  validateMovieInventory(data.movies, data.movie_total)
  const inventory =
    data.cities === null
      ? { citySlugs: [], theaterSlugs: [] }
      : validateCityInventory(data.cities)
  const paths = new Set([
    ...data.movies.map((movie) => `/film/${encodeURIComponent(movie.slug)}`),
    ...inventory.theaterSlugs.map(
      (slug) => `/cinema/${encodeURIComponent(slug)}`,
    ),
    ...inventory.citySlugs.map(
      (slug) => `/ville/${encodeURIComponent(slug)}/cinemas`,
    ),
  ])
  if (
    Object.keys(data.lastmod_by_path).length !== paths.size ||
    Object.keys(data.lastmod_by_path).some((path) => !paths.has(path))
  )
    throw new Error('Incomplete sitemap timestamp map')
  for (const path of paths) {
    if (!Object.hasOwn(data.lastmod_by_path, path))
      throw new Error('Incomplete sitemap timestamp map')
    const value = data.lastmod_by_path[path]
    if (value === undefined) throw new Error('Incomplete sitemap timestamp map')
    if (value === null) continue
    const instant = timestampInstant(value)
    if (instant === null || instant > asOf)
      throw new Error('Invalid sitemap detail timestamp')
  }
  return inventory
}

export function xmlEscape(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

export function renderSitemap(
  siteUrl: string,
  entries: SitemapEntry[],
): string {
  const paths = entries.map((entry) => entry.path)
  if (
    new Set(paths).size !== paths.length ||
    entries.some(
      (entry) =>
        !entry.path.startsWith('/') ||
        (entry.lastmod !== undefined && !validTimestamp(entry.lastmod)),
    )
  ) {
    throw new Error('Invalid sitemap entries')
  }
  const body = entries
    .map((entry) => {
      const location = xmlEscape(absoluteSiteUrl(siteUrl, entry.path))
      const date =
        entry.lastmod === undefined
          ? ''
          : `\n    <lastmod>${xmlEscape(entry.lastmod)}</lastmod>`
      return `  <url>\n    <loc>${location}</loc>${date}\n  </url>`
    })
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</urlset>\n`
}

export function renderSitemapIndex(
  siteUrl: string,
  childPaths: string[],
): string {
  if (
    new Set(childPaths).size !== childPaths.length ||
    childPaths.some((path) => !path.startsWith('/'))
  )
    throw new Error('Invalid sitemap index entries')
  const body = childPaths
    .map(
      (path) =>
        `  <sitemap>\n    <loc>${xmlEscape(absoluteSiteUrl(siteUrl, path))}</loc>\n  </sitemap>`,
    )
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</sitemapindex>\n`
}
