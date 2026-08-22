import type { CatalogMovie } from '../../app/types/api'
import { absoluteSiteUrl } from '../../app/utils/siteUrl'

interface CatalogPage {
  items: CatalogMovie[]
  page: number
  page_size: number
  total: number
  generated_at: string
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

function assertPage(page: CatalogPage, expectedPage: number, total: number, generatedAt: string): void {
  if (
    page.page !== expectedPage
    || page.page_size !== PAGE_SIZE
    || page.total !== total
    || page.generated_at !== generatedAt
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
        currently_screened: true,
        sort: 'title_asc',
        page,
        page_size: PAGE_SIZE
      }
    })

    const [firstPage, cityInventory] = await Promise.all([
      fetchPage(1),
      $fetch<CityInventory>(`${apiBase}/api/v1/cities`)
    ])
    if (!Number.isSafeInteger(firstPage.total) || firstPage.total < 0 || !validTimestamp(firstPage.generated_at)) {
      throw new Error('Invalid movie catalog metadata')
    }
    assertPage(firstPage, 1, firstPage.total, firstPage.generated_at)
    const { citySlugs, theaterSlugs } = validateCities(cityInventory, firstPage.generated_at)

    const movies = [...firstPage.items]
    const pageCount = Math.max(1, Math.ceil(firstPage.total / PAGE_SIZE))
    for (let page = 2; page <= pageCount; page++) {
      const response = await fetchPage(page)
      assertPage(response, page, firstPage.total, firstPage.generated_at)
      movies.push(...response.items)
    }

    const slugs = movies.map((movie) => movie.slug.trim())
    if (slugs.some((slug) => !slug) || new Set(slugs).size !== slugs.length || slugs.length !== firstPage.total) {
      throw new Error('Invalid movie catalog items')
    }
    slugs.sort()

    const dataPaths = [
      '/',
      '/films',
      ...citySlugs.map((slug) => `/ville/${encodeURIComponent(slug)}/cinemas`),
      ...theaterSlugs.map((slug) => `/cinema/${encodeURIComponent(slug)}`),
      ...slugs.map((slug) => `/film/${encodeURIComponent(slug)}`)
    ]
    const staticPaths = ['/planning', '/recherche']
    const entries = [
      ...dataPaths.slice(0, 2).map((path) => ({ path, lastmod: firstPage.generated_at })),
      ...staticPaths.map((path) => ({ path, lastmod: null })),
      ...dataPaths.slice(2).map((path) => ({ path, lastmod: firstPage.generated_at }))
    ]
    if (new Set(entries.map((entry) => entry.path)).size !== entries.length) throw new Error('Duplicate sitemap URL')
    const body = entries.map(({ path, lastmod }) => {
      const location = xmlEscape(absoluteSiteUrl(config.public.siteUrl, path))
      return `  <url>\n    <loc>${location}</loc>${lastmod ? `\n    <lastmod>${xmlEscape(lastmod)}</lastmod>` : ''}\n  </url>`
    }).join('\n')

    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    setResponseHeader(event, 'Cache-Control', 'no-store')
    return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${body}\n</urlset>\n`
  } catch {
    throw createError({ statusCode: 503, statusMessage: 'Sitemap unavailable', message: 'Sitemap unavailable' })
  }
})
