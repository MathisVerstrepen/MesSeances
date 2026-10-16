import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { H3Event } from 'h3'
import * as sitemap from '../server/utils/sitemap.ts'
import * as internalApi from '../server/utils/internalApi.ts'
import { executeModule } from './helpers/renderVue.ts'

test('child sitemap handlers share bounded cache keys and make one authenticated correlated read, failing closed', async () => {
  const keys = new Set<string>()
  for (const family of ['films', 'cinemas', 'cities'] as const) {
    const source = await readFile(
      new URL(`../server/routes/sitemaps/${family}.xml.ts`, import.meta.url),
      'utf8',
    )
    const event = { context: {} }
    // SAFETY: initializeRequestId only writes event.context; this fixture provides the same request-scoped context as H3Event.
    const requestId = internalApi.initializeRequestId(event as H3Event)
    const calls: Array<{
      url: string
      options: { headers: Record<string, string>; retry: boolean }
    }> = []
    const headers = new Headers()
    let response: sitemap.SitemapDataPayload | Error = {
      as_of: '2026-10-16T10:00:00Z',
      revision: 'fixture:1',
      movies: [],
      movie_total: 0,
      cities: { generated_at: '2026-10-16T10:00:00Z', items: [] },
      upcoming_available: true,
      lastmod_by_path: {},
    }
    const handler = executeModule<{
      default: (request: typeof event) => Promise<string>
    }>(
      source,
      {
        defineCachedEventHandler: (
          handler: (request: typeof event) => Promise<string>,
          policy: typeof sitemap.API_SITEMAP_CACHE_POLICIES.films,
        ) => {
          assert.equal(policy, sitemap.API_SITEMAP_CACHE_POLICIES[family])
          assert.equal(policy.maxAge, 300)
          assert.equal(policy.swr, true)
          const first = policy.getKey({ path: `/sitemaps/${family}.xml?one` })
          assert.equal(
            first,
            policy.getKey({ path: `/sitemaps/${family}.xml?two` }),
          )
          keys.add(first)
          return handler
        },
        useRuntimeConfig: () => ({
          apiBase: 'http://api.test/',
          internalApiSharedSecret: 'private-token',
          public: { siteUrl: 'https://site.test' },
        }),
        $fetch: async (
          url: string,
          options: { headers: Record<string, string>; retry: boolean },
        ) => {
          calls.push({ url, options })
          if (response instanceof Error) throw response
          return response
        },
        setResponseHeader: (
          _event: typeof event,
          name: string,
          value: string,
        ) => headers.set(name, value),
        createError: (error: { statusCode: number; statusMessage: string }) =>
          Object.assign(new Error('Sitemap unavailable'), error),
      },
      {
        '../../utils/sitemap': sitemap,
        '../../utils/internalApi': internalApi,
      },
    ).default
    const xml = await handler(event)
    assert.equal(calls.length, 1)
    assert.deepEqual(calls[0], {
      url: 'http://api.test/api/v1/sitemap-data',
      options: {
        retry: false,
        headers: {
          'X-Request-ID': requestId,
          'X-Messeances-Internal-Token': 'private-token',
        },
      },
    })
    assert.equal(headers.get('Content-Type'), 'application/xml; charset=utf-8')
    assert.match(xml, /<urlset\b/)
    if (family === 'films')
      assert.match(xml, /https:\/\/site.test\/films\/prochainement/)
    assert.doesNotMatch(xml, /private-token/)
    for (response of [
      new Error('private upstream diagnostic'),
      { movies: [] },
    ]) {
      calls.length = 0
      await assert.rejects(handler(event), {
        statusCode: 503,
        message: 'Sitemap unavailable',
      })
      assert.equal(calls.length, 1, 'no retry or partial catalogue fallback')
    }
  }
  assert.equal(keys.size, 3)
})
