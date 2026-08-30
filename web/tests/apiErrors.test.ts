import assert from 'node:assert/strict'
import test from 'node:test'
import { isNotFoundError } from '../app/composables/useMesSeancesApi.ts'

test('classifies API not-found failures by status or error code', () => {
  assert.equal(isNotFoundError({ status: 404 }), true)
  assert.equal(isNotFoundError({ statusCode: 404 }), true)
  assert.equal(isNotFoundError({ data: { error: { code: 'not_found' } } }), true)
  assert.equal(isNotFoundError({ status: 500, data: { error: { code: 'internal_error' } } }), false)
  assert.equal(isNotFoundError(new Error('not found')), false)
  assert.equal(isNotFoundError(null), false)
})
