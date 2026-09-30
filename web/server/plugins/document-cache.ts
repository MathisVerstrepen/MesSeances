import { isAccountPrivatePath } from '../../shared/accountPrivacy'

export default defineNitroPlugin((nitroApp) => {
  nitroApp.hooks.hook('render:response', (response, { event }) => {
    const path = getRequestURL(event).pathname
    let normalized = path.toLowerCase()
    try {
      normalized = decodeURIComponent(path).toLowerCase()
    } catch {
      // Keep malformed paths out of the admin prefix too.
    }
    normalized = normalized.replace(/\\/g, '/').replace(/\/{2,}/g, '/')
    if (isAccountPrivatePath(path) || /^\/admin(?:\/|$)/.test(normalized))
      return

    const renderedHeaders = Object.entries(response.headers || {})
    const existingHeaders = Object.entries(getResponseHeaders(event))
    const headers = [...renderedHeaders, ...existingHeaders]
    const contentType =
      renderedHeaders.find(([name]) => name.toLowerCase() === 'content-type') ||
      existingHeaders.find(([name]) => name.toLowerCase() === 'content-type')
    if (
      !contentType ||
      !/^text\/html(?:;|$)/i.test(String(contentType[1])) ||
      headers.some(
        ([name, value]) =>
          name.toLowerCase() === 'set-cookie' ||
          (name.toLowerCase() === 'cache-control' &&
            /(?:^|,)\s*(?:private|no-store)(?:\s*(?:=|,|$))/i.test(
              String(value),
            )),
      )
    )
      return

    // Remove aliases so a mixed-case response cannot override our final policy.
    for (const [name] of renderedHeaders)
      if (name.toLowerCase() === 'cache-control') delete response.headers[name]
    response.headers['Cache-Control'] = 'no-cache'
  })
})
