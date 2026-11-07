import { renderSitemap, SITEMAP_CACHE_SECONDS } from '../../utils/sitemap'

export default defineCachedEventHandler(
  (event) => {
    const config = useRuntimeConfig(event)
    setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
    return renderSitemap(config.public.siteUrl, [
      { path: '/guides/imax', lastmod: '2026-11-07T00:00:00Z' },
    ])
  },
  { maxAge: SITEMAP_CACHE_SECONDS, swr: false },
)
