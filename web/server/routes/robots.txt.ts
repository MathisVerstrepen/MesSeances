import { absoluteSiteUrl } from '../../app/utils/siteUrl'

export default defineEventHandler((event) => {
  const config = useRuntimeConfig(event)
  setResponseHeader(event, 'Content-Type', 'text/plain; charset=utf-8')
  return `User-agent: *\nAllow: /\nSitemap: ${absoluteSiteUrl(config.public.siteUrl, '/sitemap.xml')}\n`
})
