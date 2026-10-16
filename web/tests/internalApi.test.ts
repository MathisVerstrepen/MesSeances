import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { H3Event } from 'h3'
import * as internalApi from '../server/utils/internalApi.ts'
import * as accountPrivacy from '../shared/accountPrivacy.ts'
import { executeModule } from './helpers/renderVue.ts'
import {
  buildInternalApiHeaders,
  generateRequestId,
  INTERNAL_API_TOKEN_HEADER,
  REQUEST_ID_HEADER,
} from '../server/utils/internalApi.ts'

const SECRET = 'a'.repeat(64)
const REQUEST_ID = '1'.repeat(32)

const configSource = await readFile(
  new URL('../nuxt.config.ts', import.meta.url),
  'utf8',
)
const composableSource = await readFile(
  new URL('../app/composables/useMesSeancesApi.ts', import.meta.url),
  'utf8',
)
const middlewareSource = await readFile(
  new URL('../server/middleware/request-id.ts', import.meta.url),
  'utf8',
)

test('builds server API headers with one bounded request ID and optional private token', () => {
  assert.deepEqual(buildInternalApiHeaders(SECRET, REQUEST_ID), {
    [REQUEST_ID_HEADER]: REQUEST_ID,
    [INTERNAL_API_TOKEN_HEADER]: SECRET,
  })
  assert.deepEqual(buildInternalApiHeaders('', REQUEST_ID), {
    [REQUEST_ID_HEADER]: REQUEST_ID,
  })
  assert.throws(
    () => buildInternalApiHeaders(SECRET, 'visitor-value'),
    /Invalid internal request ID/,
  )
})

test('generates fresh cryptographic request IDs in the API contract format', () => {
  const first = generateRequestId()
  const second = generateRequestId()
  assert.match(first, /^[0-9a-f]{32}$/)
  assert.match(second, /^[0-9a-f]{32}$/)
  assert.notEqual(first, second)
})

test('actual middleware replaces visitor IDs and SSR API propagates identity without leaking it to client calls', async () => {
  const config = executeModule<{
    default: {
      runtimeConfig: {
        apiBase: string
        internalApiSharedSecret: string
        public: { apiBase: string }
      }
    }
  }>(
    configSource,
    { defineNuxtConfig: <Config>(value: Config) => value },
    {
      '@tailwindcss/vite': () => ({}),
      './shared/accountPrivacy': accountPrivacy,
    },
  ).default.runtimeConfig
  assert.equal(config.internalApiSharedSecret, '')
  assert.doesNotMatch(
    JSON.stringify(config.public),
    /internalApiSharedSecret|INTERNAL_API_SHARED_SECRET/,
  )
  const event = {
    context: {},
    node: { req: { headers: { 'x-request-id': 'visitor-value' } } },
  }
  const responseHeaders = new Headers()
  const middleware = executeModule<{
    default: (request: typeof event) => void
  }>(
    middlewareSource,
    {
      defineEventHandler: (handler: (request: typeof event) => void) => handler,
      setResponseHeader: (_event: typeof event, name: string, value: string) =>
        responseHeaders.set(name, value),
    },
    { '../utils/internalApi': internalApi },
  ).default
  middleware(event)
  const id = responseHeaders.get(REQUEST_ID_HEADER)!
  assert.match(id, /^[0-9a-f]{32}$/)
  assert.notEqual(id, 'visitor-value')
  // SAFETY: requestIdForEvent only reads this fixture's event.context, matching H3Event's request-scoped context.
  assert.equal(internalApi.requestIdForEvent(event as H3Event), id)

  for (const server of [true, false])
    for (const secret of [SECRET, '']) {
      const calls: Array<{ url: string; options: { headers?: Headers } }> = []
      const fetch = async (
        url: string,
        options: { headers?: Headers } = {},
      ) => {
        calls.push({ url, options })
        return []
      }
      fetch.create =
        (hooks: {
          onRequest: (value: {
            options: { headers?: Headers }
          }) => Promise<void>
        }) =>
        async (url: string, options: { headers?: Headers } = {}) => {
          await hooks.onRequest({ options })
          return fetch(url, options)
        }
      const api = executeModule<{
        useMesSeancesApi: typeof import('../app/composables/useMesSeancesApi.ts').useMesSeancesApi
      }>(
        composableSource
          .replaceAll('import.meta.server', String(server))
          .replaceAll('import.meta.client', String(!server)),
        {
          useRuntimeConfig: () => ({
            ...config,
            internalApiSharedSecret: secret,
            public: { ...config.public, apiBase: 'https://public.test' },
          }),
          useRequestEvent: () => event,
          $fetch: fetch,
        },
        {
          '~~/server/utils/internalApi': internalApi,
          '../utils/adminCinemaImages.ts': {},
        },
      ).useMesSeancesApi()
      assert.equal(api.hasInternalApiIdentity, server && secret !== '')
      await api.theaters()
      assert.equal(
        calls[0]!.url,
        `${server ? config.apiBase : 'https://public.test'}/api/v1/theaters`,
      )
      const headers = new Headers(calls[0]!.options.headers)
      assert.equal(headers.get(REQUEST_ID_HEADER), server ? id : null)
      assert.equal(
        headers.get(INTERNAL_API_TOKEN_HEADER),
        server && secret ? secret : null,
      )
      assert.ok(!calls[0]!.url.includes(SECRET))
      if (!server || !secret)
        assert.throws(
          () => api.movieShowtimesBundle('film', '2026-10-16'),
          /Internal API identity unavailable/,
        )
    }
})
