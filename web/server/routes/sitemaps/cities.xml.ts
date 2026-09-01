import type { CitiesResponse } from '../../../app/types/api'
import { internalApiHeaders } from '../../utils/internalApi'
import { API_SITEMAP_CACHE_POLICIES, buildCitySitemapEntries, renderSitemap } from '../../utils/sitemap'

export default defineCachedEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')

  try {
    const inventory = await $fetch<CitiesResponse>(`${apiBase}/api/v1/cities`, {
      headers: internalApiHeaders(event, config.internalApiSharedSecret),
      retry: false
    })
    const entries = buildCitySitemapEntries(inventory)
    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    return renderSitemap(config.public.siteUrl, entries)
  } catch {
    throw createError({ statusCode: 503, statusMessage: 'Sitemap unavailable', message: 'Sitemap unavailable' })
  }
}, API_SITEMAP_CACHE_POLICIES.cities)
