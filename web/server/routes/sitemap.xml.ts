import { renderSitemapIndex, SITEMAP_CACHE_SECONDS } from '../utils/sitemap'

const CHILD_SITEMAPS = ['/sitemaps/films.xml', '/sitemaps/cinemas.xml', '/sitemaps/cities.xml']

export default defineCachedEventHandler((event) => {
  const config = useRuntimeConfig(event)
  setResponseHeader(event, 'Content-Type', 'application/xml; charset=utf-8')
  return renderSitemapIndex(config.public.siteUrl, CHILD_SITEMAPS)
}, { maxAge: SITEMAP_CACHE_SECONDS, swr: false })
