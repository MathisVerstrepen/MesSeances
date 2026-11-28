import { LLMS_CONTENT_TYPE, renderLlmsTxt } from '../utils/llms'

export default defineEventHandler((event) => {
  const config = useRuntimeConfig(event)
  setResponseHeader(event, 'Content-Type', LLMS_CONTENT_TYPE)
  return renderLlmsTxt(config.public.siteUrl)
})
