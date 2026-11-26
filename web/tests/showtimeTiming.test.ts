import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  nextShowtimeTimingTransition,
  SHOWTIME_WARNING_DURATION_MS,
  showtimeTimingState,
  showtimeTimingSuffix,
} from '../app/utils/showtimeTiming.ts'

const start = '2026-11-26T18:00:00+01:00'
const startMs = Date.parse(start)

test('timing state is upcoming before start, warning while started and past after the grace period', () => {
  assert.equal(showtimeTimingState(start, startMs - 1), 'upcoming')
  assert.equal(showtimeTimingState(start, startMs), 'warning')
  assert.equal(
    showtimeTimingState(start, startMs + SHOWTIME_WARNING_DURATION_MS),
    'warning',
  )
  assert.equal(
    showtimeTimingState(start, startMs + SHOWTIME_WARNING_DURATION_MS + 1),
    'past',
  )
})

test('timing state stays upcoming without a client clock or a valid start', () => {
  assert.equal(showtimeTimingState(start, null), 'upcoming')
  assert.equal(showtimeTimingState('invalid', startMs), 'upcoming')
})

test('timing suffix describes started and past showtimes for accessible names', () => {
  assert.equal(showtimeTimingSuffix('upcoming'), '')
  assert.equal(showtimeTimingSuffix('warning'), ', séance commencée')
  assert.equal(showtimeTimingSuffix('past'), ', séance passée')
})

test('next timing transition is the earliest future start or past boundary', () => {
  const later = '2026-11-26T19:00:00+01:00'
  assert.equal(
    nextShowtimeTimingTransition([later, start, 'invalid'], startMs - 10),
    startMs,
  )
  assert.equal(
    nextShowtimeTimingTransition([start], startMs),
    startMs + SHOWTIME_WARNING_DURATION_MS + 1,
  )
  assert.equal(
    nextShowtimeTimingTransition(
      [start],
      startMs + SHOWTIME_WARNING_DURATION_MS + 1,
    ),
    null,
  )
})

test('every showtime listing renders started and past indicators', async () => {
  const sources = await Promise.all(
    [
      'pages/film/[slug].vue',
      'components/ShowtimeResultBox.vue',
      'components/ShowtimeResultLine.vue',
      'components/TimelineMatrix.vue',
    ].map(async (path) => [
      path,
      await readFile(new URL(`../app/${path}`, import.meta.url), 'utf8'),
    ]),
  )
  for (const [path, source] of sources) {
    assert.match(source, /showtimeTimingState\(/, path)
    assert.match(source, /showtimeTimingSuffix\(/, path)
    assert.match(source, /'past'/, path)
    assert.match(source, /'warning'|ShowtimeTimingNotice/, path)
  }
})
