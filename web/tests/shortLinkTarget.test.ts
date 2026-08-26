import assert from 'node:assert/strict'
import test from 'node:test'
import { isValidShortLinkCode, isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'
import { mergeOwnedQuery } from '../app/utils/routeQuery.ts'
import { canonicalizeTheaterSelection, parseSharedTheaterSelection, theaterSelectionsEqual, withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'

test('accepts every supported route and owned query key', () => {
  const targets = [
    '/',
    '/planning?date=2026-08-22&language=VOSTFR&format=IMAX&mode=map&zoom=12',
    '/recherche?theaters=ugc-25%2Cugc-26&date=2026-08-22&start_after=18%3A00&finish_before=23%3A00&language=VF&format=2D&include_ads=true&buffer_ads=20&grouping=movie&layout=grid',
    '/films?q=Am%C3%A9lie+Poulain&sort=title&page=2&genres=Animation%2CDrame&duration=medium&date=2026-08-22&date_to=2026-08-24',
    '/credits',
    '/film/ugc-film-42?date=2026-08-22&language=VF&format=2D&sort=time',
    '/cinema/ugc-lille?date=2026-08-22',
    '/ville/villeneuve-d_ascq/cinemas'
  ]
  for (const target of targets) assert.equal(isValidShortLinkTarget(target), true, target)
})

test('accepts encoded values without changing order or encoding', () => {
  assert.equal(isValidShortLinkTarget('/films?sort=title&q=a%26b%3Dc'), true)
  assert.equal(isValidShortLinkTarget('/films?%71=Am%C3%A9lie+Poulain'), true)
  assert.equal(isValidShortLinkTarget(`/films?q=${'x'.repeat(2039)}`), true)
})

test('accepts cinema films view and its display query', () => {
  assert.equal(isValidShortLinkTarget('/cinema/ugc-lille?view=films'), true)
  assert.equal(
    isValidShortLinkTarget('/cinema/ugc-lille?date=2026-08-22&grouping=movie&layout=boxes&view=films&shared_theaters=ugc-25%2Cugc-26'),
    true
  )
})

test('rejects unsafe, malformed, and unsupported targets', () => {
  const targets = [
    '', 'planning', '//evil.example/x', 'https://evil.example/', '/\\evil', '/films#fragment',
    '/films/', '/film//x', '/film/.', '/film/..', '/film/%2e%2e', '/film/caf%C3%A9', '/film/-slug', '/film/slug!',
    '/admin', '/ville/lille/cinemas/extra', '/?q=x', '/films?unknown=x', '/credits?q=x', '/cinema/ugc-lille?unknown=x',
    '/cinemas', '/cinemas?q=Lille', '/cinemas?%71=Lille+centre', '/cinemas?shared_theaters=ugc-25', '/cinemas?q=Lille&shared_theaters=ugc-25',
    '/films?q=x&q=y', '/films?q=x&%71=y', '/films?', '/films?q=x&', '/films?q=%', '/films?q=%C3',
    '/films?q=%0A', '/films?q=%00', '/films?%0A=x', '/films?q=x\r\nInjected:x',
    '/films?date=today&date=tomorrow', '/films?duration=short&duration=long', '/films?q=x#fragment', `/films?q=${'x'.repeat(2040)}`, `/${'x'.repeat(2048)}`,
    `/films?q=${String.fromCharCode(0xd800)}`
  ]
  for (const target of targets) assert.equal(isValidShortLinkTarget(target), false, target)
})

test('validates shortlink codes exactly', () => {
  for (const code of ['AAAAAAAAAAAAAAAAAAAAAA', 'Abcdefghijklmnopqr_1-2']) assert.equal(isValidShortLinkCode(code), true, code)
  for (const code of ['', 'short', 'AAAAAAAAAAAAAAAAAAAAA!', 'éAAAAAAAAAAAAAAAAAAAAA', 'AAAAAAAAAAAAAAAAAAAAAAA']) assert.equal(isValidShortLinkCode(code), false, code)
})

test('accepts shared theater selection on seven remaining routes', () => {
  for (const path of ['/planning', '/recherche', '/films', '/credits', '/film/film-1', '/cinema/cinema-1', '/ville/lille/cinemas']) {
    const separator = path.includes('?') ? '&' : '?'
    assert.equal(isValidShortLinkTarget(`${path}${separator}shared_theaters=ugc-25%2Ckinepolis_42`), true, path)
  }
  assert.equal(isValidShortLinkTarget(`/films?shared_theaters=${'a'.repeat(128)}`), true)
  assert.equal(isValidShortLinkTarget('/?shared_theaters=ugc-25'), false)
})

test('removes only shared theaters while preserving every other query value', () => {
  const source = {
    shared_theaters: 'ugc-25',
    q: 'Amélie',
    repeated: ['first', null, 'last'],
    flag: null
  }
  const result = mergeOwnedQuery(source, ['shared_theaters'], {})
  assert.deepEqual(result, { q: 'Amélie', repeated: ['first', null, 'last'], flag: null })
  assert.deepEqual(source, { shared_theaters: 'ugc-25', q: 'Amélie', repeated: ['first', null, 'last'], flag: null })
  assert.notEqual(result, source)
})

test('rejects malformed shared theater selections', () => {
  for (const value of ['', 'ugc-25,', ',ugc-25', 'ugc-25,,ugc-26', 'ugc-25,ugc-25', ' ugc-25', 'ugc.25', '-ugc-25', `${'a'.repeat(129)}`]) {
    assert.equal(isValidShortLinkTarget(`/films?shared_theaters=${encodeURIComponent(value)}`), false, value)
  }
  assert.equal(isValidShortLinkTarget('/films?shared_theaters=ugc-25&shared_theaters=ugc-26'), false)
  assert.equal(isValidShortLinkTarget('/films?shared_theaters=%'), false)
})

test('parses, canonicalizes, and compares theater selections', () => {
  assert.deepEqual(parseSharedTheaterSelection('known-2,stale,known-1'), ['known-2', 'stale', 'known-1'])
  assert.equal(parseSharedTheaterSelection('known-1,known-1'), null)
  const catalog = [{ id: 'known-1' }, { id: 'known-2' }, { id: 'known-3' }]
  assert.deepEqual(canonicalizeTheaterSelection(['known-2', 'stale', 'known-1'], catalog), ['known-1', 'known-2'])
  assert.deepEqual(canonicalizeTheaterSelection(['stale'], catalog), [])
  assert.equal(theaterSelectionsEqual(['known-1', 'known-2'], ['known-2', 'known-1']), true)
  assert.equal(theaterSelectionsEqual(['known-1'], ['known-2']), false)
})

test('upserts one canonical shared field without changing other raw fields', () => {
  assert.equal(
    withSharedTheaterSelection('/films?sort=title&q=Am%C3%A9lie+Poulain', ['known-1', 'known-2']),
    '/films?sort=title&q=Am%C3%A9lie+Poulain&shared_theaters=known-1%2Cknown-2'
  )
  assert.equal(
    withSharedTheaterSelection('/films?shared%5Ftheaters=old&q=a%26b%3Dc', ['known-2']),
    '/films?q=a%26b%3Dc&shared_theaters=known-2'
  )
  assert.equal(
    withSharedTheaterSelection('/films?shared_theaters=old&q=x&shared_theaters=older', ['known-1']),
    '/films?q=x&shared_theaters=known-1'
  )
  assert.equal(withSharedTheaterSelection('/films?q=x', []), null)
  assert.equal(withSharedTheaterSelection('/films?%zz=x', ['known-1']), null)
})
