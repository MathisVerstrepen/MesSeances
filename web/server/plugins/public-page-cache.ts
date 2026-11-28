import { PAGE_CACHE_MAX_CHARS } from '../../shared/publicPageCache'
import { createBoundedMemoryDriver } from '../utils/boundedMemoryDriver'

const HTML_CONTENT_TYPE = /^text\/html(?:;|$)/i
const PRIVATE_CACHE_CONTROL = /(?:^|,)\s*(?:private|no-store)(?:\s*(?:=|,|$))/i

export default defineNitroPlugin((nitroApp) => {
  // Production keeps Nitro caches in the unbounded root memory store; the
  // development server mounts its own cache directory, which stays as is.
  const storage = useStorage()
  if (storage.getMount('cache:').base === '')
    storage.mount('cache', createBoundedMemoryDriver(PAGE_CACHE_MAX_CHARS))

  nitroApp.hooks.hook('request', (event) => {
    if (!getRouteRules(event).cache) return
    // A matching validator would make Nitro answer 304 with max-age, letting
    // browsers keep the page past a deploy. Browsers always get fresh HTML.
    delete event.node.req.headers['if-none-match']
    delete event.node.req.headers['if-modified-since']
  })

  nitroApp.hooks.hook('beforeResponse', (event) => {
    if (!getRouteRules(event).cache) return
    const contentType = String(getResponseHeader(event, 'content-type') ?? '')
    const cacheControl = String(getResponseHeader(event, 'cache-control') ?? '')
    if (!HTML_CONTENT_TYPE.test(contentType)) return
    if (PRIVATE_CACHE_CONTROL.test(cacheControl)) return
    // The route cache stamps its own max-age over the render's policy: put
    // back no-cache for pages, and no-store for temporary failures.
    setResponseHeader(
      event,
      'Cache-Control',
      getResponseStatus(event) >= 500 ? 'no-store' : 'no-cache',
    )
  })
})
