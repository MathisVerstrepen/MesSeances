export default defineNitroPlugin((nitroApp) => {
  function setNoindexHeader(event: Parameters<typeof getResponseStatus>[0]) {
    const existingValue = getResponseHeader(event, 'x-robots-tag')
    const existingRobots = existingValue ? String(existingValue) : ''
    setResponseHeader(event, 'X-Robots-Tag', existingRobots.includes('noindex') ? existingRobots : existingRobots ? `${existingRobots}, noindex,follow` : 'noindex,follow')
  }

  nitroApp.hooks.hook('error', (_error, { event }) => {
    if (event) setNoindexHeader(event)
  })

  nitroApp.hooks.hook('render:response', (response, { event }) => {
    const statusCode = response.statusCode ?? getResponseStatus(event)
    if (statusCode < 400) return

    const headers = response.headers ?? {}
    const existingKey = Object.keys(headers).find((key) => key.toLowerCase() === 'x-robots-tag')
    const existingValue = existingKey ? headers[existingKey] : undefined
    if (existingKey) delete headers[existingKey]
    response.headers = {
      ...headers,
      'X-Robots-Tag': existingValue?.includes('noindex') ? existingValue : existingValue ? `${existingValue}, noindex,follow` : 'noindex,follow'
    }
  })

  nitroApp.hooks.hook('beforeResponse', (event) => {
    if (getResponseStatus(event) < 400) return
    setNoindexHeader(event)
  })
})
