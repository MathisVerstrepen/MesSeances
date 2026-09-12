import assert from 'node:assert/strict'
import test from 'node:test'
import { hasKnownShowtimeEnd } from '../app/utils/showtimeEnd.ts'

test('recognizes only Megarama unknown ends and trusts server-side source or TMDB effective ends', () => {
  const start = '2026-09-12T18:00:00Z'
  assert.equal(hasKnownShowtimeEnd('megarama', start, start), false)
  assert.equal(hasKnownShowtimeEnd('megarama', start, '2026-09-12T20:00:00+02:00'), false)
  assert.equal(hasKnownShowtimeEnd('megarama', start, '2026-09-12T20:15:00Z'), true)
  assert.equal(hasKnownShowtimeEnd('megarama', start, '2026-09-12T17:00:00Z'), false)
  assert.equal(hasKnownShowtimeEnd('megarama', start, 'invalid'), false)
  assert.equal(hasKnownShowtimeEnd('megarama', 'invalid', start), false)
  for (const provider of ['ugc', 'kinepolis', 'pathe', 'cgr'] as const) {
    assert.equal(hasKnownShowtimeEnd(provider, start, start), true)
  }
})
