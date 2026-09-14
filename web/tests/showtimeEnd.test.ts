import assert from 'node:assert/strict'
import test from 'node:test'
import { hasCanonicalShowtimeEnd, resolveShowtimeEnd } from '../app/utils/showtimeEnd.ts'

const start = '2026-09-14T18:00:00+02:00'
const estimate = '2026-09-14T17:48:00Z'
const unknown = { start_time: start, end_time: start, estimated_end_time: null, estimated_end_ads_minutes: null }

test('canonical end predicate validates finite positive intervals independently of provider', () => {
  assert.equal(hasCanonicalShowtimeEnd(start, estimate), true)
  for (const end of [start, '2026-09-14T16:00:00Z', '2026-09-14T15:00:00Z', 'invalid']) {
    assert.equal(hasCanonicalShowtimeEnd(start, end), false)
  }
  assert.equal(hasCanonicalShowtimeEnd('invalid', estimate), false)
})

test('explicit estimates resolve default, custom and zero ads without frontend calculation', () => {
  for (const [ads, time] of [[15, estimate], [0, '2026-09-14T17:33:00Z'], [30, '2026-09-14T18:03:00Z'], [120, '2026-09-14T19:33:00Z']] as const) {
    const input = { ...unknown, estimated_end_time: time, estimated_end_ads_minutes: ads }
    const before = structuredClone(input)
    assert.deepEqual(resolveShowtimeEnd(input), { time, estimated: true, adsMinutes: ads })
    assert.deepEqual(input, before)
  }
  assert.equal(resolveShowtimeEnd(unknown), null)
})

test('canonical end wins even over inconsistent estimate metadata', () => {
  assert.deepEqual(resolveShowtimeEnd({ ...unknown, end_time: estimate, estimated_end_time: 'invalid', estimated_end_ads_minutes: -1 }), { time: estimate, estimated: false, adsMinutes: null })
})

test('malformed estimates and canonical intervals fail closed', () => {
  const input = { ...unknown, estimated_end_time: estimate, estimated_end_ads_minutes: 15 }
  for (const ads of [null, -1, 121, 1.5, Infinity, NaN]) {
    assert.equal(resolveShowtimeEnd({ ...input, estimated_end_ads_minutes: ads }), null)
  }
  for (const time of [null, 'invalid', start, '2026-09-14T15:00:00Z']) {
    assert.equal(resolveShowtimeEnd({ ...input, estimated_end_time: time }), null)
  }
  for (const end of ['invalid', '2026-09-14T15:00:00Z']) {
    assert.equal(resolveShowtimeEnd({ ...input, end_time: end }), null)
  }
  assert.equal(resolveShowtimeEnd({ ...input, start_time: 'invalid' }), null)
})
