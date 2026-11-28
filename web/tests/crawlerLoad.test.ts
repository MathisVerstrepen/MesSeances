import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { H3Event } from 'h3'
import {
  PAGE_CACHE_MAX_CHARS,
  PUBLIC_PAGE_CACHE_SECONDS,
  publicPageCacheRoutes,
} from '../shared/publicPageCache.ts'
import { isAccountPrivatePath } from '../shared/accountPrivacy.ts'
import { createBoundedMemoryDriver } from '../server/utils/boundedMemoryDriver.ts'
import {
  DEFAULT_RETRY_AFTER_SECONDS,
  markResponseUnavailable,
  recordUpstreamFailure,
  upstreamFailureForEvent,
  upstreamRetryAfterSeconds,
} from '../server/utils/upstreamAvailability.ts'

function fakeEvent(): H3Event {
  const event = { context: {} }
  // SAFETY: the upstream helpers only read and write event.context, as on an H3Event.
  return event as H3Event
}

test('only saturation and outages count as temporary upstream failures', () => {
  assert.equal(upstreamRetryAfterSeconds(429, '7'), 7)
  assert.equal(
    upstreamRetryAfterSeconds(503, null),
    DEFAULT_RETRY_AFTER_SECONDS,
  )
  assert.equal(
    upstreamRetryAfterSeconds(500, 'Wed, 21 Oct 2026 07:28:00 GMT'),
    30,
  )
  assert.equal(upstreamRetryAfterSeconds(undefined, null), 30)
  assert.equal(upstreamRetryAfterSeconds(429, '0'), 1)
  assert.equal(upstreamRetryAfterSeconds(429, '99999'), 3600)
  for (const status of [400, 401, 404, 410])
    assert.equal(upstreamRetryAfterSeconds(status, '5'), null)
})

test('a render remembers the longest Retry-After of its failed API calls', () => {
  const event = fakeEvent()
  recordUpstreamFailure(event, 404, null)
  assert.equal(upstreamFailureForEvent(event), null)
  recordUpstreamFailure(event, 429, '12')
  recordUpstreamFailure(event, 429, '3')
  assert.equal(upstreamFailureForEvent(event), 12)
})

test('pages rendered after an upstream failure answer 503 without caching', () => {
  const response = {
    statusCode: 200,
    headers: {
      'cache-control': 'no-cache',
      'retry-after': '1',
      'X-Robots-Tag': 'all',
    },
  }
  assert.equal(markResponseUnavailable(response, 200, 12), true)
  assert.deepEqual(response, {
    statusCode: 503,
    statusMessage: 'Service Unavailable',
    headers: {
      'X-Robots-Tag': 'all',
      'Cache-Control': 'no-store',
      'Retry-After': '12',
    },
  })
  // An upstream 502 page also becomes a retryable 503.
  const upstream = { statusCode: 502 }
  assert.equal(markResponseUnavailable(upstream, 502, 5), true)
  assert.equal(upstream.statusCode, 503)
  // Authoritative answers stay: a missing film is still a 404.
  for (const status of [301, 404]) {
    const kept = { statusCode: status, headers: {} }
    assert.equal(markResponseUnavailable(kept, status, 5), false)
    assert.equal(kept.statusCode, status)
  }
})

test('the SSR API client records failed calls on the request event', async () => {
  const source = await readFile(
    new URL('../app/composables/useMesSeancesApi.ts', import.meta.url),
    'utf8',
  )
  assert.match(source, /async onResponseError\(\{ response \}\)/)
  assert.match(
    source,
    /recordUpstreamFailure\(\s*requestEvent,\s*response\.status,\s*response\.headers\.get\('retry-after'\),\s*\)/,
  )
  assert.match(source, /async onRequestError\(\)/)
  const plugin = await readFile(
    new URL('../server/plugins/upstream-unavailable.ts', import.meta.url),
    'utf8',
  )
  assert.match(plugin, /hook\('render:response'/)
})

test('the bounded page cache evicts least recently used entries', () => {
  const driver = createBoundedMemoryDriver(20)
  driver.setItem('a', '12345')
  driver.setItem('b', '12345')
  driver.setItem('c', '12345')
  assert.equal(driver.size(), 18)
  assert.equal(driver.getItem('a'), '12345')
  driver.setItem('d', '12345')
  // "b" was the least recently used once "a" was read.
  assert.deepEqual(driver.getKeys().sort(), ['a', 'c', 'd'])
  assert.equal(driver.getItem('b'), null)
  driver.setItem('a', '1')
  assert.equal(driver.size(), 14)
  driver.setItem('huge', 'x'.repeat(30))
  assert.equal(driver.hasItem('huge'), false)
  driver.clear('c')
  assert.deepEqual(driver.getKeys().sort(), ['a', 'd'])
  driver.removeItem('a')
  driver.dispose()
  assert.equal(driver.size(), 0)
  assert.ok(PAGE_CACHE_MAX_CHARS <= 128 * 1024 * 1024)
})

test('production caches only public pages that render without visitor data', async () => {
  assert.equal(PUBLIC_PAGE_CACHE_SECONDS, 300)
  for (const route of publicPageCacheRoutes) {
    const path = route.replace('/**', '/x')
    assert.equal(isAccountPrivatePath(path), false, route)
    assert.doesNotMatch(path, /^\/(?:admin|api|compte|connexion)/)
  }
  const config = await readFile(
    new URL('../nuxt.config.ts', import.meta.url),
    'utf8',
  )
  assert.match(
    config,
    /\$production: \{\s*routeRules: Object\.fromEntries\(\s*publicPageCacheRoutes\.map/,
  )
  const plugin = await readFile(
    new URL('../server/plugins/public-page-cache.ts', import.meta.url),
    'utf8',
  )
  // Browsers keep revalidating HTML: no 304 with max-age, no-cache sent.
  assert.match(plugin, /delete event\.node\.req\.headers\['if-none-match'\]/)
  assert.match(
    plugin,
    /getResponseStatus\(event\) >= 500 \? 'no-store' : 'no-cache'/,
  )
})
