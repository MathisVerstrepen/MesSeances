import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  buildInternalApiHeaders,
  generateRequestId,
  INTERNAL_API_TOKEN_HEADER,
  REQUEST_ID_HEADER
} from '../server/utils/internalApi.ts'

const SECRET = 'a'.repeat(64)
const REQUEST_ID = '1'.repeat(32)

const configSource = await readFile(new URL('../nuxt.config.ts', import.meta.url), 'utf8')
const composableSource = await readFile(new URL('../app/composables/useMesSeancesApi.ts', import.meta.url), 'utf8')
const middlewareSource = await readFile(new URL('../server/middleware/request-id.ts', import.meta.url), 'utf8')

test('builds server API headers with one bounded request ID and optional private token', () => {
  assert.deepEqual(buildInternalApiHeaders(SECRET, REQUEST_ID), {
    [REQUEST_ID_HEADER]: REQUEST_ID,
    [INTERNAL_API_TOKEN_HEADER]: SECRET
  })
  assert.deepEqual(buildInternalApiHeaders('', REQUEST_ID), { [REQUEST_ID_HEADER]: REQUEST_ID })
  assert.throws(() => buildInternalApiHeaders(SECRET, 'visitor-value'), /Invalid internal request ID/)
})

test('generates fresh cryptographic request IDs in the API contract format', () => {
  const first = generateRequestId()
  const second = generateRequestId()
  assert.match(first, /^[0-9a-f]{32}$/)
  assert.match(second, /^[0-9a-f]{32}$/)
  assert.notEqual(first, second)
})

test('keeps service identity private and server-only while blank config preserves public calls', () => {
  const runtimeConfig = configSource.match(/runtimeConfig:\s*\{([\s\S]*?)\n  \},\n  typescript:/u)?.[1] ?? ''
  const publicConfig = runtimeConfig.match(/public:\s*\{([\s\S]*)\n    \}/u)?.[1] ?? ''
  assert.match(runtimeConfig, /internalApiSharedSecret:\s*''/u)
  assert.doesNotMatch(publicConfig, /internalApiSharedSecret|INTERNAL_API_SHARED_SECRET/u)
  assert.match(composableSource, /hasInternalApiIdentity = import\.meta\.server && config\.internalApiSharedSecret !== ''/u)
  assert.match(composableSource, /import\.meta\.server[\s\S]*?import\('~~\/server\/utils\/internalApi'\)/u)
  assert.match(composableSource, /if \(!hasInternalApiIdentity\) throw new Error\('Internal API identity unavailable'\)/u)
  assert.doesNotMatch(composableSource, /X-Messeances-Internal-Token/u)
})

test('Nitro middleware replaces visitor request IDs and propagates its generated value', () => {
  assert.match(middlewareSource, /initializeRequestId\(event\)/u)
  assert.match(middlewareSource, /setResponseHeader\(event, REQUEST_ID_HEADER, requestId\)/u)
  assert.doesNotMatch(middlewareSource, /get(?:Request)?Header|headers\[['"]x-request-id/u)
})
