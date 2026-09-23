import assert from 'node:assert/strict'
import test from 'node:test'
import {
  AccountApiError,
  accountDestination,
  accountErrorMessage,
  accountWriteUncertain,
  normalizeAccountEmail,
  normalizeAccountUsername,
  passwordCriteria,
  validAccountUsername,
} from '../app/utils/accountState.ts'

test('account navigation preserves restricted onboarding states', () => {
  assert.equal(
    accountDestination({ enabled: true, state: 'anonymous', account: null }),
    '/connexion',
  )
  assert.equal(
    accountDestination({
      enabled: true,
      state: 'pending_email',
      account: null,
    }),
    '/verification',
  )
  assert.equal(
    accountDestination({
      enabled: true,
      state: 'pending_username',
      account: null,
    }),
    '/finaliser',
  )
  assert.equal(
    accountDestination({ enabled: true, state: 'complete', account: null }),
    '/compte',
  )
  assert.equal(
    accountDestination({
      enabled: false,
      state: 'pending_username',
      account: null,
    }),
    '/connexion',
  )
})

test('email targets normalize ASCII exactly without provider-specific rewriting', () => {
  assert.equal(
    normalizeAccountEmail(' Alice.Test+Cinema@EXAMPLE.COM '),
    'alice.test+cinema@example.com',
  )
})

test('settings errors distinguish email conflicts and uncertain writes', () => {
  assert.match(
    accountErrorMessage(new AccountApiError(409, 'email_unavailable')),
    /adresse actuelle reste inchangée/,
  )
  assert.doesNotMatch(
    accountErrorMessage(new AccountApiError(409, 'email_unavailable')),
    /utilisateur/,
  )
  assert.match(
    accountErrorMessage(new AccountApiError(403, 'recent_auth_required')),
    /Recommencez la vérification depuis votre compte/,
  )
  assert.equal(accountWriteUncertain(new AccountApiError(0)), true)
  assert.equal(accountWriteUncertain(new AccountApiError(503)), true)
  assert.equal(accountWriteUncertain(new AccountApiError(400)), false)
  assert.equal(accountWriteUncertain(new AccountApiError(401)), false)
})

test('username client syntax normalizes ASCII without silently trimming', () => {
  assert.equal(normalizeAccountUsername('Alice_42'), 'alice_42')
  for (const name of ['abc', 'Alice_42', 'a'.repeat(30)])
    assert.equal(validAccountUsername(name), true)
  for (const name of [
    'ab',
    'a'.repeat(31),
    '_alice',
    '4alice',
    'Éloise',
    ' alice ',
    'a-b',
  ])
    assert.equal(validAccountUsername(name), false)
})

test('password criteria count code points, preserve spaces and bound bytes', () => {
  for (const character of ['a', 'é', '🎬']) {
    assert.equal(passwordCriteria(character.repeat(9)).minimum, false)
    assert.deepEqual(passwordCriteria(character.repeat(10)), {
      minimum: true,
      maximum: true,
    })
  }
  assert.deepEqual(passwordCriteria(' '.repeat(10)), {
    minimum: true,
    maximum: true,
  })
  assert.deepEqual(passwordCriteria('🎬'.repeat(128)), {
    minimum: true,
    maximum: true,
  })
  assert.equal(passwordCriteria('a'.repeat(129)).maximum, false)
  assert.equal(passwordCriteria('🎬'.repeat(129)).maximum, false)
})

test('missing registration proof directs original browser or fresh registration, never password login', () => {
  const message = accountErrorMessage(
    new AccountApiError(403, 'verification_browser_required'),
  )
  assert.match(message, /navigateur où vous avez commencé votre inscription/)
  assert.match(message, /recommencez votre inscription/)
  assert.doesNotMatch(message, /mot de passe|reconnectez/i)
})

test('errors never display server bodies, raw errors, or secrets', () => {
  assert.doesNotMatch(
    accountErrorMessage(new Error('secret-password')),
    /secret-password/,
  )
  assert.match(accountErrorMessage(new AccountApiError(503)), /indisponible/)
  assert.match(accountErrorMessage(new AccountApiError(429)), /Patientez/)
  assert.match(accountErrorMessage(new AccountApiError(409)), /autre/)
  assert.match(accountErrorMessage(new AccountApiError(400)), /nouveau lien/)
})
