import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { getFrenchShortLinkPreparationError } from '../app/composables/useMesSeancesApi.ts'

const rateLimitMessage = 'Vous avez créé trop de liens trop rapidement. Patientez quelques minutes puis réessayez.'
const genericMessage = 'Le lien n’a pas pu être préparé. Réessayez.'

test('maps short-link rate-limit failures to a specific French message', () => {
  assert.equal(getFrenchShortLinkPreparationError({ data: { error: { code: 'rate_limited' } } }), rateLimitMessage)
  assert.equal(getFrenchShortLinkPreparationError({ status: 429 }), rateLimitMessage)
  assert.equal(getFrenchShortLinkPreparationError({ statusCode: 429 }), rateLimitMessage)
})

test('keeps the generic short-link message for other failures', () => {
  assert.equal(getFrenchShortLinkPreparationError({ data: { error: { code: 'internal_error' } }, status: 500 }), genericMessage)
  assert.equal(getFrenchShortLinkPreparationError(new Error('Invalid shortlink response')), genericMessage)
})

test('maps ShareButton creation failures after its request-sequence guard', async () => {
  const component = await readFile(new URL('../app/components/ShareButton.vue', import.meta.url), 'utf8')

  assert.match(component, /catch \(error\) \{\s+if \(currentRequest !== requestSequence\) return\s+preparationState\.value = 'error'\s+preparationError\.value = getFrenchShortLinkPreparationError\(error\)/)
})
