import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createPublicAnalytics,
  publicAnalyticsPage,
  publicAnalyticsReferrer,
  type PublicPageview,
} from '../app/utils/publicAnalytics.ts'

test('public analytics routes fail closed and referrers discard sensitive fields', () => {
  for (const path of [
    '/connexion',
    '/Compte',
    '/%63ompte',
    '/admin',
    '/api',
    '/unknown',
    '/film/a%2fb',
    '/films?email=x',
    '/films#token=x',
  ])
    assert.equal(publicAnalyticsPage(path), null)
  assert.equal(publicAnalyticsPage('/films', false), null)
  assert.equal(publicAnalyticsPage('/film/un-film')?.title, 'Film')
  assert.equal(
    publicAnalyticsReferrer(
      'https://user:pass@example.org/private?token=x#x',
      'https://local.test',
    ),
    'https://example.org',
  )
  assert.equal(
    publicAnalyticsReferrer('https://local.test/compte', 'https://local.test'),
    '',
  )
})

test('manual pageviews are one-use, sanitized, route-bound and not replayed after delay', () => {
  const owner = createPublicAnalytics(
    { website: 'test', hostname: 'local.test', language: 'fr', screen: '1x1' },
    '',
  )
  const sent: (PublicPageview | null)[] = []
  const track = (payload: PublicPageview) => {
    sent.push(owner.beforeSend('event', { ...payload }))
  }
  owner.settle('/films', true)
  owner.send(track)
  owner.send(track)
  owner.pause()
  owner.settle('/films', true) // canceled navigation
  owner.send(track)
  assert.equal(sent.length, 1)
  assert.equal(owner.beforeSend('event', { url: '/films' }), null)
  owner.pause()
  owner.settle('/compte', true)
  owner.send(track)
  owner.settle('/films', true)
  owner.send(track)
  assert.equal(sent.length, 2)
  assert.equal(sent[1]?.referrer, '')
  owner.settle('/recherche', true)
  owner.pause()
  owner.settle('/verification', true)
  owner.send(track)
  assert.equal(sent.length, 2)
  owner.settle('/planning', true)
  owner.send((payload) => {
    assert.equal(
      owner.beforeSend('identify', { ...payload, id: 'secret' }),
      null,
    )
  })
})
