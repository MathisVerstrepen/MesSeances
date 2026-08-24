import assert from 'node:assert/strict'
import test from 'node:test'
import type { SlotResult, TheaterShowtimesResponse } from '../app/types/api.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
import { groupShowtimeResults, sortShowtimeResults, toSlotShowtimeResults, toTheaterShowtimeResults } from '../app/utils/showtimeResults.ts'

const movie = { slug: 'film-1', title: 'Film 1', runtime_minutes: 101, updated_at: '2026-08-24T00:00:00Z' }

test('adapts slot results without mutation and preserves effective time, raw end, theater, and top-level media', () => {
  const source: SlotResult[] = [{
    showtime: { provider: 'ugc', id: 'slot-1', movie, start_time: '2026-08-24T18:00:00+02:00', end_time: '2026-08-24T20:01:00+02:00', language: 'VOSTFR', format: 'IMAX', room: '4', booking_url: 'https://www.ugc.fr/reservation' },
    theater: { provider: 'ugc', id: 'ugc-1', name: 'UGC Lille', city: 'Lille' },
    poster_url: 'https://image.tmdb.org/t/p/w500/poster.jpg',
    backdrop_url: 'https://image.tmdb.org/t/p/w780/backdrop.jpg',
    effective_start_time: '2026-08-24T18:15:00+02:00',
    effective_end_time: '2026-08-24T20:16:00+02:00',
    buffer_ads_minutes: 15,
    slack_before_minutes: 0,
    slack_after_minutes: 0
  }]
  const before = structuredClone(source)

  const [result] = toSlotShowtimeResults(source)

  assert.deepEqual(source, before)
  assert.deepEqual(result, {
    key: 'ugc:slot-1', showtimeId: 'slot-1', provider: 'ugc', movieKey: 'ugc:film-1', movieSlug: 'film-1', movieTitle: 'Film 1', movieRuntimeMinutes: 101,
    theaterName: 'UGC Lille', advertisedStartTime: '2026-08-24T18:00:00+02:00', effectiveStartTime: '2026-08-24T18:15:00+02:00', endTime: '2026-08-24T20:01:00+02:00',
    language: 'VOSTFR', format: 'IMAX', room: '4', bookingUrl: 'https://www.ugc.fr/reservation', posterUrl: 'https://image.tmdb.org/t/p/w500/poster.jpg', backdropUrl: 'https://image.tmdb.org/t/p/w780/backdrop.jpg'
  })
})

test('adapts theater showtimes without mutation and injects theater while mapping advertised start as effective', () => {
  const source = {
    generated_at: '2026-08-24T00:00:00Z', timezone: 'Europe/Paris', date: '2026-08-24',
    theater: { provider: 'kinepolis', id: 'k-1', slug: 'kinepolis-lille', name: 'Kinepolis Lille', address: 'Rue du film', city: 'Lille', city_slug: 'lille', postal_code: '59000', available_dates: ['2026-08-24'], accepted_passes: [] },
    showtimes: [{ provider: 'kinepolis', id: 'show-1', movie, start_time: '2026-08-24T19:00:00+02:00', end_time: '2026-08-24T21:01:00+02:00', language: 'VF', format: '2D', room: 'Salle 2', booking_url: null, start_offset_minutes: 0, duration_minutes: 121, poster_url: 'poster', backdrop_url: 'backdrop' }]
  } satisfies TheaterShowtimesResponse
  const before = structuredClone(source)

  const [result] = toTheaterShowtimeResults(source)

  assert.deepEqual(source, before)
  assert.equal(result?.theaterName, 'Kinepolis Lille')
  assert.equal(result?.effectiveStartTime, result?.advertisedStartTime)
  assert.equal(result?.endTime, source.showtimes[0].end_time)
  assert.equal(result?.posterUrl, 'poster')
  assert.equal(result?.backdropUrl, 'backdrop')
})

function view(overrides: Partial<ShowtimeResultViewModel>): ShowtimeResultViewModel {
  return {
    key: 'ugc:id', showtimeId: 'id', provider: 'ugc', movieKey: 'ugc:film-1', movieSlug: 'film-1', movieTitle: 'Film 1', movieRuntimeMinutes: 101, theaterName: 'UGC',
    advertisedStartTime: '2026-08-24T18:00:00+02:00', effectiveStartTime: '2026-08-24T18:00:00+02:00', endTime: '2026-08-24T20:00:00+02:00',
    language: 'VF', format: '2D', room: '', bookingUrl: null, posterUrl: null, backdropUrl: null, ...overrides
  }
}

test('sorts non-mutatively by advertised start then showtime ID', () => {
  const source = [
    view({ key: 'late', showtimeId: 'z', advertisedStartTime: '2026-08-24T19:00:00+02:00' }),
    view({ key: 'tie-b', showtimeId: 'b' }),
    view({ key: 'tie-a', showtimeId: 'a' })
  ]
  const sourceOrder = source.map((result) => result.key)
  const sorted = sortShowtimeResults(source)

  assert.deepEqual(sorted.map((result) => result.key), ['tie-a', 'tie-b', 'late'])
  assert.deepEqual(source.map((result) => result.key), sourceOrder)
  assert.notEqual(sorted, source)
})

test('groups by provider and movie key while preserving first-seen group and item order', () => {
  const sorted = [
    view({ key: 'ugc-a', showtimeId: 'a' }),
    view({ key: 'kinepolis-a', showtimeId: 'b', provider: 'kinepolis', movieKey: 'kinepolis:film-1' }),
    view({ key: 'ugc-c', showtimeId: 'c' })
  ]

  const groups = groupShowtimeResults(sorted)

  assert.deepEqual(groups.map((group) => group.key), ['ugc:film-1', 'kinepolis:film-1'])
  assert.deepEqual(groups.map((group) => group.results.map((result) => result.key)), [['ugc-a', 'ugc-c'], ['kinepolis-a']])
})
