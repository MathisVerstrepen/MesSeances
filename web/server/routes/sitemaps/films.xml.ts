import type { MoviesResponse } from '../../../app/types/api'
import { buildFilmSitemapEntries, renderSitemap, SITEMAP_CACHE_SECONDS, SITEMAP_CATALOG_PAGE_SIZE, validateCatalogPage } from '../../utils/sitemap'

export default defineCachedEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')

  try {
    const fetchAllPage = (page: number) => $fetch<MoviesResponse>(`${apiBase}/api/v1/movies`, {
      query: { include_ended: true, sort: 'title_asc', page, page_size: SITEMAP_CATALOG_PAGE_SIZE }
    })
    const fetchCurrentPage = (pageSize: number) => $fetch<MoviesResponse>(`${apiBase}/api/v1/movies`, {
      query: { currently_screened: true, sort: 'showtimes_desc', page: 1, page_size: pageSize }
    })

    const [firstPage, homepageCatalog, filmsCatalog] = await Promise.all([
      fetchAllPage(1),
      fetchCurrentPage(6),
      fetchCurrentPage(24)
    ])
    validateCatalogPage(firstPage, { page: 1, pageSize: SITEMAP_CATALOG_PAGE_SIZE })

    const movies = [...firstPage.items]
    const pageCount = Math.max(1, Math.ceil(firstPage.total / SITEMAP_CATALOG_PAGE_SIZE))
    for (let page = 2; page <= pageCount; page++) {
      const response = await fetchAllPage(page)
      validateCatalogPage(response, {
        page,
        pageSize: SITEMAP_CATALOG_PAGE_SIZE,
        generatedAt: firstPage.generated_at,
        catalogRevision: firstPage.catalog_revision,
        total: firstPage.total
      })
      movies.push(...response.items)
    }

    const entries = buildFilmSitemapEntries(movies, firstPage, homepageCatalog, filmsCatalog)
    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    return renderSitemap(config.public.siteUrl, entries)
  } catch {
    throw createError({ statusCode: 503, statusMessage: 'Sitemap unavailable', message: 'Sitemap unavailable' })
  }
}, { maxAge: SITEMAP_CACHE_SECONDS, swr: false })
