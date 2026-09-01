import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { API_SITEMAP_CACHE_POLICIES, API_SITEMAP_CACHE_POLICY } from '../server/utils/sitemap.ts'

const handlerFixtures = [
  { route: 'films', url: new URL('../server/routes/sitemaps/films.xml.ts', import.meta.url), retryDeclarations: 2 },
  { route: 'cinemas', url: new URL('../server/routes/sitemaps/cinemas.xml.ts', import.meta.url), retryDeclarations: 1 },
  { route: 'cities', url: new URL('../server/routes/sitemaps/cities.xml.ts', import.meta.url), retryDeclarations: 1 }
]
const handlerSources = await Promise.all(handlerFixtures.map(({ url }) => readFile(url, 'utf8')))
const nitroCacheSource = await readFile(new URL('../node_modules/nitropack/dist/runtime/internal/cache.mjs', import.meta.url), 'utf8')

test('all API-backed child sitemaps share immutable 300-second SWR policy', () => {
  assert.deepEqual(API_SITEMAP_CACHE_POLICY, { maxAge: 300, swr: true })
  assert(Object.isFrozen(API_SITEMAP_CACHE_POLICY))
  for (const [{ route }, source] of handlerFixtures.map((fixture, index) => [fixture, handlerSources[index]] as const)) {
    assert.match(source, new RegExp(`defineCachedEventHandler\\([\\s\\S]*\\}, API_SITEMAP_CACHE_POLICIES\\.${route}\\)`, 'u'))
    assert.doesNotMatch(source, /swr:\s*false/u)
  }
})

test('query variants share one bounded route key and child route keys never collide', async () => {
  const routeKeys = []
  for (const route of ['films', 'cinemas', 'cities'] as const) {
    const policy = API_SITEMAP_CACHE_POLICIES[route]
    assert(Object.isFrozen(policy))
    const firstKey = await policy.getKey({ path: `/sitemaps/${route}.xml?variant=one` })
    const secondKey = await policy.getKey({ path: `/sitemaps/${route}.xml?variant=two&arbitrary=value` })
    assert.equal(firstKey, secondKey)
    routeKeys.push(firstKey)
  }
  assert.equal(new Set(routeKeys).size, 3)
})

test('all sitemap API reads explicitly disable automatic GET retries', () => {
  for (const [{ retryDeclarations }, source] of handlerFixtures.map((fixture, index) => [fixture, handlerSources[index]] as const)) {
    assert.equal(source.match(/retry:\s*false/gu)?.length, retryDeclarations)
  }
})

test('all API-backed child sitemaps authenticate and correlate upstream requests', () => {
  for (const source of handlerSources) {
    assert.match(source, /internalApiHeaders\(event, config\.internalApiSharedSecret\)/u)
    assert.match(source, /headers/u)
  }
})

test('all child handlers preserve cold-failure 503 without partial XML', () => {
  for (const source of handlerSources) {
    assert.match(source, /catch\s*\{[\s\S]*createError\(\{ statusCode: 503,[\s\S]*Sitemap unavailable/u)
  }
})

test('installed Nitro SWR implementation coalesces pending work and retains stale success on refresh errors', () => {
  assert.match(nitroCacheSource, /const pending = \{\}/u)
  assert.match(nitroCacheSource, /pending\[key\] = Promise\.resolve\(resolver\(\)\)/u)
  assert.match(nitroCacheSource, /entry\.value = await pending\[key\]/u)
  assert.match(nitroCacheSource, /if \(opts\.swr && validate\(entry\) !== false\)/u)
  assert.match(nitroCacheSource, /_resolvePromise\.catch/u)
  assert.match(nitroCacheSource, /if \(entry\.value === void 0\) \{\s*await _resolvePromise/u)
})
