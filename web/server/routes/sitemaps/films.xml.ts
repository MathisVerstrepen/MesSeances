import type {
  ApiErrorResponse,
  MoviesResponse,
  UpcomingMoviesResponse,
} from '../../../app/types/api'
import { internalApiHeaders } from '../../utils/internalApi'
import {
  API_SITEMAP_CACHE_POLICIES,
  buildFilmSitemapEntries,
  renderSitemap,
  SITEMAP_CATALOG_PAGE_SIZE,
  upcomingSitemapEntry,
  validateCatalogPage,
} from '../../utils/sitemap'

export default defineCachedEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')
  const headers = internalApiHeaders(event, config.internalApiSharedSecret)

  try {
    const fetchAllPage = (page: number) =>
      $fetch<MoviesResponse>(`${apiBase}/api/v1/movies`, {
        headers,
        retry: false,
        query: {
          include_ended: true,
          sort: 'title_asc',
          page,
          page_size: SITEMAP_CATALOG_PAGE_SIZE,
        },
      })
    const fetchCurrentPage = (pageSize: number) =>
      $fetch<MoviesResponse>(`${apiBase}/api/v1/movies`, {
        headers,
        retry: false,
        query: {
          currently_screened: true,
          sort: 'showtimes_desc',
          page: 1,
          page_size: pageSize,
        },
      })

    const [firstPage, homepageCatalog, filmsCatalog] = await Promise.all([
      fetchAllPage(1),
      fetchCurrentPage(6),
      fetchCurrentPage(24),
    ])
    validateCatalogPage(firstPage, {
      page: 1,
      pageSize: SITEMAP_CATALOG_PAGE_SIZE,
    })

    const movies = [...firstPage.items]
    const pageCount = Math.max(
      1,
      Math.ceil(firstPage.total / SITEMAP_CATALOG_PAGE_SIZE),
    )
    for (let page = 2; page <= pageCount; page++) {
      const response = await fetchAllPage(page)
      validateCatalogPage(response, {
        page,
        pageSize: SITEMAP_CATALOG_PAGE_SIZE,
        generatedAt: firstPage.generated_at,
        catalogRevision: firstPage.catalog_revision,
        total: firstPage.total,
      })
      movies.push(...response.items)
    }

    const entries = buildFilmSitemapEntries(
      movies,
      firstPage,
      homepageCatalog,
      filmsCatalog,
    )
    const upcoming = await $fetch.raw<
      UpcomingMoviesResponse | ApiErrorResponse
    >(`${apiBase}/api/v1/movies/upcoming`, {
      headers,
      retry: false,
      ignoreResponseError: true,
      query: { page: 1 },
    })
    const publication = upcoming._data
    if (upcoming.status === 200 && publication && !('error' in publication)) {
      entries.push(upcomingSitemapEntry(publication.generated_at))
    } else if (
      !(
        upcoming.status === 503 &&
        publication &&
        'error' in publication &&
        publication.error.code === 'upcoming_unavailable'
      )
    ) {
      throw new Error('Upcoming sitemap unavailable')
    }
    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    return renderSitemap(config.public.siteUrl, entries)
  } catch {
    throw createError({
      statusCode: 503,
      statusMessage: 'Sitemap unavailable',
      message: 'Sitemap unavailable',
    })
  }
}, API_SITEMAP_CACHE_POLICIES.films)
