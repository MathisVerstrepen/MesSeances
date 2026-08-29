import assert from 'node:assert/strict'
import test from 'node:test'
import type { SlotResult, TheaterShowtimesResponse } from '../app/types/api.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
import { areShowtimeResultsCompatible, filterCompatibleShowtimeResults, groupShowtimeResults, parseShowtimeSelection, serializeShowtimeSelection, sortShowtimeResults, toSlotShowtimeResults, toTheaterShowtimeResults, validShowtimeSelectionKeys } from '../app/utils/showtimeResults.ts'

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

test('round-trips compact selection tokens for every provider in canonical key order', () => {
  const cgr = 'cgr:cgr-showing-P0798-eb8c701bf9eb902f738cb7a32ed14cb55b9e2b42e0fc346ac79d9cf11d171bbc'
  const kinepolis = 'kinepolis:kinepolis-showing-Vista_Session-42'
  const pathe = 'pathe:pathe-showing-V3001S170227'
  const ugc = 'ugc:ugc-showing-330660140434'
  const keys = [pathe, ugc, cgr, kinepolis, cgr]
  const compact = 'cP0798-64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w,kVista_Session-42,pV3001S170227,u330660140434'

  assert.equal(serializeShowtimeSelection(keys), compact)
  assert.deepEqual(parseShowtimeSelection(compact), [cgr, kinepolis, pathe, ugc])
  assert.deepEqual(parseShowtimeSelection(`${compact},${compact}`), [cgr, kinepolis, pathe, ugc])
  assert.deepEqual(parseShowtimeSelection(undefined), [])
  assert.equal(serializeShowtimeSelection([]), undefined)
})

test('ignores malformed provider tokens and old verbose selection values', () => {
  const valid = 'ugc:ugc-showing-12'
  const malformed = [
    'ugc:ugc-showing-12',
    'x12',
    'u0',
    'u18446744073709551616',
    'u12x',
    'k',
    'kbad.value',
    `k${'a'.repeat(129)}`,
    'pV0S1',
    'pV1S0',
    'pV1s2',
    'cP0798-short',
    'cp0798-64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w',
    'cP0798-64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7x',
    'cP0798-64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w='
  ]

  assert.deepEqual(parseShowtimeSelection(valid), [])
  assert.deepEqual(parseShowtimeSelection([...malformed, 'u12', 'u12'].join(',')), [valid])
  assert.equal(serializeShowtimeSelection([...malformed, valid]), 'u12')
  assert.equal(serializeShowtimeSelection([
    'ugc:ugc-showing-0',
    'kinepolis:kinepolis-showing-bad.value',
    'pathe:pathe-showing-V0S1',
    `cgr:cgr-showing-P0798-${'A'.repeat(64)}`
  ]), undefined)
})

test('materially reduces realistic selected-screening value length', () => {
  const keys = [
    'cgr:cgr-showing-P0798-eb8c701bf9eb902f738cb7a32ed14cb55b9e2b42e0fc346ac79d9cf11d171bbc',
    'cgr:cgr-showing-P1016-252b96e16cf563c832f2ffc5c35d55f318d15e4398708bc748fbb88482c0f052',
    'cgr:cgr-showing-P1016-57435c8260ab73a85f6cd30038f21572df9b71a18c68467bef03566bdc5d36f2',
    'pathe:pathe-showing-V3001S170227'
  ]
  const verbose = keys.join(',')
  const compact = serializeShowtimeSelection(keys)

  assert.ok(compact)
  assert.equal(verbose.length, 293)
  assert.equal(compact.length, 166)
  assert.equal(encodeURIComponent(verbose).length, 307)
  assert.equal(encodeURIComponent(compact).length, 172)
  assert.ok(compact.length < verbose.length * 0.6)
})

test('keeps only available selection keys in deterministic order', () => {
  const results = [view({ key: 'ugc:b' }), view({ key: 'ugc:a' })]
  assert.deepEqual(validShowtimeSelectionKeys(results, ['stale:key', 'ugc:b', 'ugc:a', 'ugc:b']), ['ugc:a', 'ugc:b'])
})

test('treats touching effective-start-to-end intervals as compatible', () => {
  const selected = view({ key: 'selected', effectiveStartTime: '2026-08-24T18:15:00+02:00', endTime: '2026-08-24T20:00:00+02:00' })
  const before = view({ key: 'before', effectiveStartTime: '2026-08-24T16:00:00+02:00', endTime: '2026-08-24T18:15:00+02:00' })
  const after = view({ key: 'after', effectiveStartTime: '2026-08-24T20:00:00+02:00', endTime: '2026-08-24T22:00:00+02:00' })
  const overlapping = view({ key: 'overlap', effectiveStartTime: '2026-08-24T19:59:00+02:00', endTime: '2026-08-24T21:00:00+02:00' })

  assert.equal(areShowtimeResultsCompatible(selected, before), true)
  assert.equal(areShowtimeResultsCompatible(selected, after), true)
  assert.equal(areShowtimeResultsCompatible(selected, overlapping), false)
})

test('shows selections and only candidates compatible with every selection', () => {
  const source = [
    view({ key: 'early', effectiveStartTime: '2026-08-24T16:00:00+02:00', endTime: '2026-08-24T18:00:00+02:00' }),
    view({ key: 'middle', effectiveStartTime: '2026-08-24T18:00:00+02:00', endTime: '2026-08-24T20:00:00+02:00' }),
    view({ key: 'late', effectiveStartTime: '2026-08-24T20:00:00+02:00', endTime: '2026-08-24T22:00:00+02:00' }),
    view({ key: 'overlap-early', effectiveStartTime: '2026-08-24T17:00:00+02:00', endTime: '2026-08-24T18:30:00+02:00' }),
    view({ key: 'overlap-late', effectiveStartTime: '2026-08-24T19:30:00+02:00', endTime: '2026-08-24T21:00:00+02:00' })
  ]
  const before = source.map((result) => result.key)

  const filtered = filterCompatibleShowtimeResults(source, ['early', 'late'])

  assert.deepEqual(filtered.map((result) => result.key), ['early', 'middle', 'late'])
  assert.deepEqual(source.map((result) => result.key), before)
})

test('ignores stale selections and fails open for invalid intervals', () => {
  const valid = view({ key: 'valid' })
  const invalid = view({ key: 'invalid', effectiveStartTime: 'not-a-date' })

  assert.deepEqual(filterCompatibleShowtimeResults([valid, invalid], ['stale:key']).map((result) => result.key), ['valid', 'invalid'])
  assert.deepEqual(filterCompatibleShowtimeResults([valid, invalid], ['invalid']).map((result) => result.key), ['valid', 'invalid'])
})
