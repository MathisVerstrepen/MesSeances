import { internalApiHeaders } from '../utils/internalApi'
import {
  LLMS_CONTENT_TYPE,
  LLMS_FULL_CACHE_POLICY,
  renderLlmsFullTxt,
} from '../utils/llms'
import { parseSitemapData, type SitemapDataPayload } from '../utils/sitemap'

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
    const body = renderLlmsFullTxt(
      config.public.siteUrl,
      parseSitemapData(payload),
    )
    setResponseHeader(event, 'Content-Type', LLMS_CONTENT_TYPE)
    return body
  } catch {
    throw createError({
      statusCode: 503,
      statusMessage: 'Guide unavailable',
      message: 'Guide unavailable',
    })
  }
}, LLMS_FULL_CACHE_POLICY)
