import type { CatalogMovie } from '../../app/types/api'
import { absoluteSiteUrl } from '../../app/utils/siteUrl'

interface CatalogPage {
  items: CatalogMovie[]
  page: number
  page_size: number
  total: number
  generated_at: string
  catalog_revision: string
}

interface CityInventory {
  generated_at: string
  items: Array<{
    name: string
    slug: string
    theaters: Array<{ provider: string; id: string; slug: string; name: string }>
  }>
}

const PAGE_SIZE = 100

function xmlEscape(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

function validTimestamp(value: string): boolean {
  return value.length > 0 && Number.isFinite(Date.parse(value))
}

function assertPage(page: CatalogPage, expectedPage: number, total: number, generatedAt: string, catalogRevision: string): void {
  if (
    page.page !== expectedPage
    || page.page_size !== PAGE_SIZE
    || page.total !== total
    || page.generated_at !== generatedAt
    || page.catalog_revision !== catalogRevision
    || !Array.isArray(page.items)
  ) {
    throw new Error('Inconsistent movie catalog snapshot')
  }
}

function nonempty(value: string): boolean {
  return value.trim().length > 0
}

function validateCities(inventory: CityInventory, generatedAt: string) {
  if (inventory.generated_at !== generatedAt || !Array.isArray(inventory.items)) throw new Error('Inconsistent city inventory snapshot')
  const citySlugs: string[] = []
  const theaterSlugs: string[] = []
  const theaterIds = new Set<string>()
  for (const city of inventory.items) {
    if (!nonempty(city.name) || !nonempty(city.slug) || !Array.isArray(city.theaters) || city.theaters.length === 0) throw new Error('Invalid city inventory item')
    citySlugs.push(city.slug)
    for (const theater of city.theaters) {
      if (!nonempty(theater.provider) || !nonempty(theater.id) || !nonempty(theater.slug) || !nonempty(theater.name) || theaterIds.has(theater.id)) throw new Error('Invalid city theater inventory')
      theaterIds.add(theater.id)
      theaterSlugs.push(theater.slug)
    }
  }
  if (new Set(citySlugs).size !== citySlugs.length || new Set(theaterSlugs).size !== theaterSlugs.length) throw new Error('Duplicate city or theater identity')
  return { citySlugs: citySlugs.sort(), theaterSlugs: theaterSlugs.sort() }
}

export default defineEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')

  try {
    const fetchPage = (page: number) => $fetch<CatalogPage>(`${apiBase}/api/v1/movies`, {
      query: {
        include_ended: true,
        sort: 'title_asc',
        page,
        page_size: PAGE_SIZE
      }
    })

    const [firstPage, cityInventory] = await Promise.all([
      fetchPage(1),
      $fetch<CityInventory>(`${apiBase}/api/v1/cities`)
    ])
    if (
      !Number.isSafeInteger(firstPage.total)
      || firstPage.total < 0
      || !validTimestamp(firstPage.generated_at)
      || !nonempty(firstPage.catalog_revision)
    ) {
      throw new Error('Invalid movie catalog metadata')
    }
    assertPage(firstPage, 1, firstPage.total, firstPage.generated_at, firstPage.catalog_revision)
    const { citySlugs, theaterSlugs } = validateCities(cityInventory, firstPage.generated_at)

    const movies = [...firstPage.items]
    const pageCount = Math.max(1, Math.ceil(firstPage.total / PAGE_SIZE))
    for (let page = 2; page <= pageCount; page++) {
      const response = await fetchPage(page)
      assertPage(response, page, firstPage.total, firstPage.generated_at, firstPage.catalog_revision)
      movies.push(...response.items)
    }

    const filmEntries = movies.map((movie) => ({ slug: movie.slug.trim() }))
    if (
      filmEntries.some(({ slug }) => !/^film-[1-9]\d*$/.test(slug))
      || new Set(filmEntries.map(({ slug }) => slug)).size !== filmEntries.length
      || filmEntries.length !== firstPage.total
    ) {
      throw new Error('Invalid movie catalog items')
    }
    filmEntries.sort((left, right) => left.slug.localeCompare(right.slug))

    const entries = [
      ...['/', '/films', '/cinemas', '/planning', '/recherche'].map((path) => ({ path })),
      ...citySlugs.map((slug) => ({ path: `/ville/${encodeURIComponent(slug)}/cinemas` })),
      ...theaterSlugs.map((slug) => ({ path: `/cinema/${encodeURIComponent(slug)}` })),
      ...filmEntries.map(({ slug }) => ({ path: `/film/${encodeURIComponent(slug)}` }))
    ]
    if (new Set(entries.map((entry) => entry.path)).size !== entries.length) throw new Error('Duplicate sitemap URL')
    const body = entries.map(({ path }) => {
      const location = xmlEscape(absoluteSiteUrl(config.public.siteUrl, path))
      return `  <url>\n    <loc>${location}</loc>\n  </url>`
    }).join('\n')

    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    setResponseHeader(event, 'Cache-Control', 'no-store')
    return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</urlset>\n`
  } catch {
    throw createError({ statusCode: 503, statusMessage: 'Sitemap unavailable', message: 'Sitemap unavailable' })
  }
})
