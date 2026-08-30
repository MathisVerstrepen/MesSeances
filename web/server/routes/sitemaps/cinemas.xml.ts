import type { CitiesResponse } from '../../../app/types/api'
import { buildCinemaSitemapEntries, renderSitemap, SITEMAP_CACHE_SECONDS } from '../../utils/sitemap'

export default defineCachedEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')

  try {
    const inventory = await $fetch<CitiesResponse>(`${apiBase}/api/v1/cities`)
    const entries = buildCinemaSitemapEntries(inventory)
    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    return renderSitemap(config.public.siteUrl, entries)
  } catch {
    throw createError({ statusCode: 503, statusMessage: 'Sitemap unavailable', message: 'Sitemap unavailable' })
  }
}, { maxAge: SITEMAP_CACHE_SECONDS, swr: false })
