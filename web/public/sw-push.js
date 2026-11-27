/* Push handlers imported by the generated Workbox service worker.
 * Payloads are rendered by the API; links are restricted to same-origin paths. */
const fallbackUrl = '/compte/notifications'

// Resolve before checking: browsers treat `/\\host` like `//host`.
function safePath(value) {
  const path = String(value ?? '')
  if (!path.startsWith('/')) return fallbackUrl
  try {
    const url = new URL(path, self.location.origin)
    if (url.origin === self.location.origin) return url.pathname + url.search
  } catch {
    return fallbackUrl
  }
  return fallbackUrl
}

// Parse the untrusted push data once, at the event boundary.
function parsePayload(message) {
  try {
    const data = message ? message.json() : null
    return {
      title: String(data?.title ?? '').slice(0, 120),
      body: String(data?.body ?? '').slice(0, 500),
      url: safePath(data?.url),
      tag: String(data?.tag ?? '').slice(0, 64),
    }
  } catch {
    return { title: '', body: '', url: fallbackUrl, tag: '' }
  }
}

self.addEventListener('push', (event) => {
  const payload = parsePayload(event.data)
  event.waitUntil(
    self.registration.showNotification(payload.title || 'MesSeances', {
      body: payload.body,
      tag: payload.tag || undefined,
      renotify: !!payload.tag,
      lang: 'fr',
      icon: '/pwa-192x192.png',
      badge: '/pwa-64x64.png',
      data: { url: payload.url },
    }),
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const target = new URL(safePath(event.notification.data?.url), self.location.origin).href
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({
        type: 'window',
        includeUncontrolled: true,
      })
      for (const client of windows) {
        if (new URL(client.url).origin !== self.location.origin) continue
        const focused = await client.focus()
        if (focused && 'navigate' in focused) await focused.navigate(target)
        return
      }
      await self.clients.openWindow(target)
    })(),
  )
})
