import {
  accountFragmentBootstrap,
  accountPrivacyHeaders,
  isAccountPage,
  isAccountPrivatePath,
} from '../../shared/accountPrivacy'

export default defineNitroPlugin((nitroApp) => {
  const protectedResponses = new WeakSet()
  nitroApp.hooks.hook('error', (_error, { event }) => {
    if (!event || !isAccountPrivatePath(getRequestURL(event).pathname)) return
    const response = event.node.res
    if (response.headersSent || protectedResponses.has(response)) return
    protectedResponses.add(response)
    // Nitro's HTML and JSON error handlers send directly, skipping both final
    // response hooks, and force no-cache on 404. Keep the existing no-store
    // policy for this response only, including their later header writes.
    const setHeader = response.setHeader
    response.setHeader = (name, value) =>
      setHeader.call(
        response,
        name,
        name.toLowerCase() === 'cache-control'
          ? accountPrivacyHeaders['Cache-Control']
          : value,
      )
    setResponseHeaders(event, accountPrivacyHeaders)
  })
  nitroApp.hooks.hook('render:html', (html, { event }) => {
    if (isAccountPage(getRequestURL(event).pathname)) {
      html.head.unshift(`<script>${accountFragmentBootstrap}</script>`)
    }
  })
})
