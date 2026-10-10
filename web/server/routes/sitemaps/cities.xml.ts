import { internalApiHeaders } from '../../utils/internalApi'
import {
  API_SITEMAP_CACHE_POLICIES,
  buildCitySitemapEntries,
  parseSitemapData,
  renderSitemap,
  type SitemapDataPayload,
} from '../../utils/sitemap'

export default defineCachedEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')

  try {
    const response = await $fetch<unknown>(`${apiBase}/api/v1/sitemap-data`, {
      headers: internalApiHeaders(event, config.internalApiSharedSecret),
      retry: false,
    })
    // SAFETY: Object coercion creates a field-readable candidate; parser checks every consumed field.
    const payload = Object(response) as SitemapDataPayload
    const entries = buildCitySitemapEntries(parseSitemapData(payload))
    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    return renderSitemap(config.public.siteUrl, entries)
  } catch {
    throw createError({
      statusCode: 503,
      statusMessage: 'Sitemap unavailable',
      message: 'Sitemap unavailable',
    })
  }
}, API_SITEMAP_CACHE_POLICIES.cities)
