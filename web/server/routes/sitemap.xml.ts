import type { CatalogMovie } from '../../app/types/api'
import { absoluteSiteUrl } from '../../app/utils/siteUrl'

interface CatalogPage {
  items: CatalogMovie[]
  page: number
  page_size: number
  total: number
  generated_at: string
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

    const firstPage = await fetchPage(1)
    if (!Number.isSafeInteger(firstPage.total) || firstPage.total < 0 || !validTimestamp(firstPage.generated_at)) {
      throw new Error('Invalid movie catalog metadata')
    }
    assertPage(firstPage, 1, firstPage.total, firstPage.generated_at)

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

    const dataPaths = ['/', '/films', ...slugs.map((slug) => `/film/${encodeURIComponent(slug)}`)]
    const staticPaths = ['/planning', '/recherche']
    const entries = [
      ...dataPaths.slice(0, 2).map((path) => ({ path, lastmod: firstPage.generated_at })),
      ...staticPaths.map((path) => ({ path, lastmod: null })),
      ...dataPaths.slice(2).map((path) => ({ path, lastmod: firstPage.generated_at }))
    ]
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
