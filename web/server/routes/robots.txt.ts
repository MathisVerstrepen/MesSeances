import { renderRobotsTxt } from '../utils/robots'

export default defineEventHandler((event) => {
  const config = useRuntimeConfig(event)
  setResponseHeader(event, 'Content-Type', 'text/plain; charset=utf-8')
  return renderRobotsTxt(config.public.siteUrl)
})
