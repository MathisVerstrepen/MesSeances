import assert from 'node:assert/strict'
import test from 'node:test'
import type { SlotResult, TheaterShowtimesResponse } from '../app/types/api.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
import { areShowtimeResultsCompatible, filterCompatibleShowtimeResults, filterSelectedShowtimeResults, groupShowtimeResults, parseShowtimeSelection, serializeShowtimeSelection, showtimeSelectionQueryValues, sortShowtimeResults, toSlotShowtimeResults, toTheaterShowtimeResults, validShowtimeSelectionKeys } from '../app/utils/showtimeResults.ts'

const movie = { slug: 'film-1', title: 'Film 1', original_language: null, runtime_minutes: 101, updated_at: '2026-08-24T00:00:00Z' }

test('adapts slot results without mutation and preserves effective time, raw end, theater, and top-level media', () => {
  const source: SlotResult[] = [{
    showtime: { provider: 'ugc', id: 'slot-1', movie, start_time: '2026-08-24T18:00:00+02:00', end_time: '2026-08-24T20:01:00+02:00', estimated_end_time: null, estimated_end_ads_minutes: null, language: 'VOSTFR', format: 'IMAX', room: '4', booking_url: 'https://www.ugc.fr/reservation' },
    theater: { provider: 'ugc', id: 'ugc-1', name: 'UGC Lille', city: 'Lille' },
    poster_url: 'https://image.tmdb.org/t/p/w500/poster.jpg',
    backdrop_url: 'https://image.tmdb.org/t/p/w780/backdrop.jpg',
    effective_start_time: '2026-08-24T18:15:00+02:00',
    effective_end_time: '2026-08-24T20:01:00+02:00',
    buffer_ads_minutes: 15,
    slack_before_minutes: 0,
    slack_after_minutes: 0
  }]
  const before = structuredClone(source)

  const [result] = toSlotShowtimeResults(source)

  assert.deepEqual(source, before)
  assert.deepEqual(result, {
    key: 'ugc:slot-1', showtimeId: 'slot-1', provider: 'ugc', movieKey: 'ugc:film-1', movieSlug: 'film-1', movieTitle: 'Film 1', movieOriginalLanguage: null, movieRuntimeMinutes: 101,
    theaterName: 'UGC Lille', theaterId: 'ugc-1', advertisedStartTime: '2026-08-24T18:00:00+02:00', effectiveStartTime: '2026-08-24T18:15:00+02:00', end: canonical('2026-08-24T20:01:00+02:00'),
    language: 'VOSTFR', format: 'IMAX', room: '4', bookingUrl: 'https://www.ugc.fr/reservation', posterUrl: 'https://image.tmdb.org/t/p/w500/poster.jpg', backdropUrl: 'https://image.tmdb.org/t/p/w780/backdrop.jpg'
  })
})

test('adapts theater showtimes without mutation and injects theater while mapping advertised start as effective', () => {
  const source = {
    generated_at: '2026-08-24T00:00:00Z', timezone: 'Europe/Paris', date: '2026-08-24',
    theater: { provider: 'kinepolis', id: 'k-1', slug: 'kinepolis-lille', name: 'Kinepolis Lille', address: 'Rue du film', city: 'Lille', city_slug: 'lille', postal_code: '59000', available_dates: ['2026-08-24'], accepted_passes: [] },
    showtimes: [{ provider: 'kinepolis', id: 'show-1', movie, start_time: '2026-08-24T19:00:00+02:00', end_time: '2026-08-24T21:01:00+02:00', estimated_end_time: null, estimated_end_ads_minutes: null, language: 'VF', format: '2D', room: 'Salle 2', booking_url: null, start_offset_minutes: 0, duration_minutes: 121, poster_url: 'poster', backdrop_url: 'backdrop' }]
  } satisfies TheaterShowtimesResponse
  const before = structuredClone(source)

  const [result] = toTheaterShowtimeResults(source)

  assert.deepEqual(source, before)
  assert.equal(result?.theaterName, 'Kinepolis Lille')
  assert.equal(result?.theaterId, 'k-1')
  assert.equal(result?.effectiveStartTime, result?.advertisedStartTime)
  assert.deepEqual(result?.end, canonical(source.showtimes[0].end_time))
  assert.equal(result?.posterUrl, 'poster')
  assert.equal(result?.backdropUrl, 'backdrop')
})

function canonical(time: string) {
  return { time, estimated: false, adsMinutes: null }
}

test('both result adapters propagate original language without rewriting the concrete session language', () => {
  for (const original_language of ['fr', 'en', null]) {
    const start = '2027-06-27T18:00:00+02:00'
    const showtime = { provider: 'ugc' as const, id: 'ugc-showing-1', movie: { ...movie, original_language }, start_time: start, end_time: '2027-06-27T20:00:00+02:00', estimated_end_time: null, estimated_end_ads_minutes: null, language: 'VF' as const, format: '2D' as const, room: '', booking_url: null }
    const slot: SlotResult = { showtime, theater: { provider: 'ugc', id: 'ugc-25', name: 'UGC', city: 'Lille' }, poster_url: null, backdrop_url: null, effective_start_time: start, effective_end_time: showtime.end_time, buffer_ads_minutes: 0, slack_before_minutes: 0, slack_after_minutes: 0 }
    const theater: TheaterShowtimesResponse = { generated_at: start, timezone: 'Europe/Paris', date: '2027-06-27', theater: { ...slot.theater, slug: 'ugc-25', address: '', city_slug: 'lille', postal_code: '59000', available_dates: ['2027-06-27'], accepted_passes: [] }, showtimes: [{ ...showtime, start_offset_minutes: 0, duration_minutes: 120, poster_url: null, backdrop_url: null }] }
    for (const result of [toSlotShowtimeResults([slot])[0]!, toTheaterShowtimeResults(theater)[0]!]) {
      assert.equal(result.movieOriginalLanguage, original_language)
      assert.equal(result.language, 'VF')
    }
  }
})

test('normalizers preserve explicit estimated provenance including custom zero ads and never infer from runtime', () => {
  for (const ads of [0, 15, 30, 120]) {
    const start = '2026-09-14T18:00:00+02:00'
    const time = new Date(Date.parse(start) + (93 + ads) * 60_000).toISOString()
    const showtime = { provider: 'cineville' as const, id: 'cineville-showing-4670-1', movie: { ...movie, runtime_minutes: 93 }, start_time: start, end_time: start, estimated_end_time: time, estimated_end_ads_minutes: ads, language: 'VF' as const, format: '2D' as const, room: '', booking_url: null }
    const slot: SlotResult = { showtime, theater: { provider: 'cineville', id: 'cineville-4670', name: 'Cinéville', city: 'Laval' }, poster_url: null, backdrop_url: null, effective_start_time: new Date(Date.parse(start) + ads * 60_000).toISOString(), effective_end_time: time, buffer_ads_minutes: ads, slack_before_minutes: 0, slack_after_minutes: 0 }
    const theater: TheaterShowtimesResponse = { generated_at: start, timezone: 'Europe/Paris', date: '2026-09-14', theater: { ...slot.theater, slug: 'cineville-4670', address: '', city_slug: 'laval', postal_code: '53000', available_dates: ['2026-09-14'], accepted_passes: [] }, showtimes: [{ ...showtime, start_offset_minutes: 600, duration_minutes: 93 + ads, poster_url: null, backdrop_url: null }] }
    const before = structuredClone({ slot, theater })
    for (const result of [toSlotShowtimeResults([slot])[0]!, toTheaterShowtimeResults(theater)[0]!]) {
      assert.deepEqual(result.end, { time, estimated: true, adsMinutes: ads })
      assert.equal(result.movieRuntimeMinutes, 93)
    }
    assert.equal(toSlotShowtimeResults([slot])[0]!.effectiveStartTime, slot.effective_start_time)
    assert.deepEqual({ slot, theater }, before)
    slot.showtime = { ...showtime, estimated_end_ads_minutes: null }
    assert.equal(toSlotShowtimeResults([slot])[0]!.end, null)
    slot.showtime = { ...showtime, end_time: '2026-09-14T19:40:00+02:00' }
    assert.deepEqual(toSlotShowtimeResults([slot])[0]!.end, canonical(slot.showtime.end_time))
  }
})

test('estimated compatibility uses returned ends and effective starts, with touching boundaries and invalid intervals', () => {
  const selected = view({ provider: 'cineville', key: 'cineville:cineville-showing-4670-1', movieRuntimeMinutes: 93, effectiveStartTime: '2026-08-24T18:30:00+02:00', end: { time: '2026-08-24T20:03:00+02:00', estimated: true, adsMinutes: 30 } })
  const touching = view({ effectiveStartTime: '2026-08-24T20:03:00+02:00', end: canonical('2026-08-24T22:00:00+02:00') })
  assert.equal(areShowtimeResultsCompatible(selected, touching), true)
  assert.equal(areShowtimeResultsCompatible(selected, { ...touching, effectiveStartTime: '2026-08-24T20:02:00+02:00' }), false)
  // Ends between advertised and effective start are compatible, preserving attendance semantics.
  assert.equal(areShowtimeResultsCompatible(selected, view({ effectiveStartTime: '2026-08-24T16:00:00+02:00', end: canonical('2026-08-24T18:30:00+02:00') })), true)
  for (const end of [null, canonical('invalid'), canonical(selected.effectiveStartTime), canonical(selected.advertisedStartTime)]) {
    assert.equal(areShowtimeResultsCompatible({ ...selected, end }, touching), false)
  }
  assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection([selected.key])), [selected.key])
})

function view(overrides: Partial<ShowtimeResultViewModel>): ShowtimeResultViewModel {
  return {
    key: 'ugc:id', showtimeId: 'id', provider: 'ugc', movieKey: 'ugc:film-1', movieSlug: 'film-1', movieTitle: 'Film 1', movieOriginalLanguage: null, movieRuntimeMinutes: 101, theaterName: 'UGC', theaterId: 'ugc-25',
    advertisedStartTime: '2026-08-24T18:00:00+02:00', effectiveStartTime: '2026-08-24T18:00:00+02:00', end: canonical('2026-08-24T20:00:00+02:00'),
    language: 'VF', format: '2D', room: '', bookingUrl: null, posterUrl: null, backdropUrl: null, ...overrides
  }
}

test('Cinéville selection tokens preserve cinema-scoped IDs and reject noncanonical numbers', () => {
  const ids = ['707-149056', '709-149056', '7-7149056', '9223372036854775807-9223372036854775807']
  const keys = ids.map((id) => `cineville:cineville-showing-${id}`)
  assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection(keys)), keys.toSorted())
  assert.equal(new Set(parseShowtimeSelection(serializeShowtimeSelection(keys))).size, ids.length)
  for (const id of ['0-1', '1-0', '01-1', '1-01', '-1-2', '1--2', '+1-2', '1-2-3', '1', '1-2\n', '1.5-2', '1e3-2', '1-2/3', '9223372036854775808-1', '1-9223372036854775808', `${'1'.repeat(20)}-1`]) {
    assert.deepEqual(parseShowtimeSelection(`v${id}`), [], id)
    assert.equal(serializeShowtimeSelection([`cineville:cineville-showing-${id}`]), undefined, id)
  }
})

test('runtime alone never turns unknown ends into compatible sessions', () => {
  const known = view({ key: 'ugc:ugc-showing-1', advertisedStartTime: '2026-08-24T22:00:00+02:00', effectiveStartTime: '2026-08-24T22:00:00+02:00', end: canonical('2026-08-24T23:00:00+02:00') })
  for (const runtime of [0, 93, 118]) {
    const unknown = view({ provider: 'cineville', key: 'cineville:cineville-showing-707-149056', movieRuntimeMinutes: runtime, end: null })
    assert.equal(areShowtimeResultsCompatible(unknown, known), false)
    assert.equal(areShowtimeResultsCompatible(known, unknown), false)
    assert.deepEqual(filterCompatibleShowtimeResults([unknown, known], []), [unknown, known])
    assert.deepEqual(filterCompatibleShowtimeResults([unknown, known], [unknown.key]), [unknown])
    assert.deepEqual(filterSelectedShowtimeResults([unknown, known], [unknown.key]), [unknown])
    assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection([unknown.key])), [unknown.key])
    assert.equal(areShowtimeResultsCompatible({ ...unknown, end: { time: '2026-08-24T19:48:00+02:00', estimated: true, adsMinutes: 15 } }, known), true)
  }
})

test('Cinéville theater results retain source fields, missing metadata and zero duration without fabrication', () => {
  const start = '2027-07-01T00:15:00+02:00'
  for (const runtime of [0, 93, 118]) {
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-09-14T12:00:00Z', timezone: 'Europe/Paris', date: '2027-07-01',
      theater: { provider: 'cineville', id: 'cineville-639', slug: 'cineville-639', name: 'Katorza', city: 'Quimper', city_slug: 'quimper', postal_code: '29000', address: '', available_dates: ['2027-07-01'], accepted_passes: [] },
      showtimes: [{ provider: 'cineville', id: 'cineville-showing-639-149056', movie: { ...movie, slug: 'cineville-film--693091020261', runtime_minutes: runtime }, start_time: start, end_time: start, estimated_end_time: null, estimated_end_ads_minutes: null, language: 'VFSTF', format: 'DOLBY', room: '4', booking_url: 'https://www.cineville.fr/vad/639/149056/1234', start_offset_minutes: 15, duration_minutes: 0, poster_url: null, backdrop_url: null }]
    }
    const before = structuredClone(response)
    const [result] = toTheaterShowtimeResults(response)
    assert.deepEqual(response, before)
    assert.equal(result?.key, 'cineville:cineville-showing-639-149056')
    assert.equal(result?.movieSlug, 'cineville-film--693091020261')
    assert.equal(result?.movieRuntimeMinutes, runtime)
    assert.equal(result?.advertisedStartTime, start)
    assert.equal(result?.end, null)
    assert.equal(result?.language, 'VFSTF')
    assert.equal(result?.format, 'DOLBY')
    assert.equal(result?.room, '4')
    assert.equal(result?.bookingUrl, response.showtimes[0]!.booking_url)
    assert.equal(result?.posterUrl, null)
  }
})

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
  const megarama = 'megarama:megarama-showing-emsx056500123456'
  const cineville = 'cineville:cineville-showing-707-149056'
  const ugc = 'ugc:ugc-showing-330660140434'
  const keys = [pathe, ugc, cgr, kinepolis, cgr, megarama, cineville]
  const compact = 'cP0798-64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w,v707-149056,kVista_Session-42,memsx056500123456,pV3001S170227,u330660140434'

  assert.equal(serializeShowtimeSelection(keys), compact)
  assert.deepEqual(parseShowtimeSelection(compact), [cgr, cineville, kinepolis, megarama, pathe, ugc])
  assert.deepEqual(parseShowtimeSelection(`${compact},${compact}`), [cgr, cineville, kinepolis, megarama, pathe, ugc])
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

test('round-trips exact Megarama case and ASCII identity limits while rejecting unsafe tokens', () => {
  for (const id of ['emsx056500123456', 'Ab_1-2', 'a'.repeat(111)]) {
    const key = `megarama:megarama-showing-${id}`
    assert.equal(serializeShowtimeSelection([key]), `m${id}`)
    assert.deepEqual(parseShowtimeSelection(`m${id}`), [key])
  }
  for (const id of ['', '-bad', '_bad', 'bad.id', 'bad/id', 'bad%2Fid', 'é', 'a'.repeat(112), 'abc\n', 'abc\r', ' abc', 'abc ']) {
    assert.equal(serializeShowtimeSelection([`megarama:megarama-showing-${id}`]), undefined)
    assert.deepEqual(parseShowtimeSelection(`m${id}`), [])
  }
})

test('unknown Megarama ends remain selectable but never prove compatibility', () => {
  const unknown = view({ provider: 'megarama', key: 'megarama:megarama-showing-emsx056500123456', end: null, effectiveStartTime: '2026-08-24T18:15:00+02:00' })
  const known = view({ key: 'known', effectiveStartTime: '2026-08-24T22:00:00+02:00', end: canonical('2026-08-24T23:00:00+02:00') })
  assert.equal(areShowtimeResultsCompatible(unknown, known), false)
  assert.equal(areShowtimeResultsCompatible(known, unknown), false)
  assert.deepEqual(filterCompatibleShowtimeResults([unknown, known], []), [unknown, known])
  assert.deepEqual(filterCompatibleShowtimeResults([unknown, known], [unknown.key]), [unknown])
  assert.deepEqual(filterCompatibleShowtimeResults([unknown, known], [known.key]), [known])
  assert.deepEqual(filterCompatibleShowtimeResults([unknown, known], [unknown.key, known.key]), [unknown, known])
  assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection([unknown.key])), [unknown.key])
  const resolved = { ...unknown, end: canonical('2026-08-24T20:10:00+02:00') }
  assert.equal(areShowtimeResultsCompatible(resolved, known), true)
})

test('keeps only available selection keys in deterministic order', () => {
  const results = [view({ key: 'ugc:b' }), view({ key: 'ugc:a' })]
  assert.deepEqual(validShowtimeSelectionKeys(results, ['stale:key', 'ugc:b', 'ugc:a', 'ugc:b']), ['ugc:a', 'ugc:b'])
})

test('treats touching effective-start-to-end intervals as compatible', () => {
  const selected = view({ key: 'selected', effectiveStartTime: '2026-08-24T18:15:00+02:00', end: canonical('2026-08-24T20:00:00+02:00') })
  const before = view({ key: 'before', effectiveStartTime: '2026-08-24T16:00:00+02:00', end: canonical('2026-08-24T18:15:00+02:00') })
  const after = view({ key: 'after', effectiveStartTime: '2026-08-24T20:00:00+02:00', end: canonical('2026-08-24T22:00:00+02:00') })
  const overlapping = view({ key: 'overlap', effectiveStartTime: '2026-08-24T19:59:00+02:00', end: canonical('2026-08-24T21:00:00+02:00') })

  assert.equal(areShowtimeResultsCompatible(selected, before), true)
  assert.equal(areShowtimeResultsCompatible(selected, after), true)
  assert.equal(areShowtimeResultsCompatible(selected, overlapping), false)
})

test('shows selections and only candidates compatible with every selection', () => {
  const source = [
    view({ key: 'early', effectiveStartTime: '2026-08-24T16:00:00+02:00', end: canonical('2026-08-24T18:00:00+02:00') }),
    view({ key: 'middle', effectiveStartTime: '2026-08-24T18:00:00+02:00', end: canonical('2026-08-24T20:00:00+02:00') }),
    view({ key: 'late', effectiveStartTime: '2026-08-24T20:00:00+02:00', end: canonical('2026-08-24T22:00:00+02:00') }),
    view({ key: 'overlap-early', effectiveStartTime: '2026-08-24T17:00:00+02:00', end: canonical('2026-08-24T18:30:00+02:00') }),
    view({ key: 'overlap-late', effectiveStartTime: '2026-08-24T19:30:00+02:00', end: canonical('2026-08-24T21:00:00+02:00') })
  ]
  const before = source.map((result) => result.key)

  const filtered = filterCompatibleShowtimeResults(source, ['early', 'late'])

  assert.deepEqual(filtered.map((result) => result.key), ['early', 'middle', 'late'])
  assert.deepEqual(source.map((result) => result.key), before)
})

test('ignores stale selections and fails closed for invalid intervals while retaining selected entries', () => {
  const valid = view({ key: 'valid' })
  const invalid = view({ key: 'invalid', effectiveStartTime: 'not-a-date' })

  assert.deepEqual(filterCompatibleShowtimeResults([valid, invalid], ['stale:key']).map((result) => result.key), ['valid', 'invalid'])
  assert.deepEqual(filterCompatibleShowtimeResults([valid, invalid], ['invalid']).map((result) => result.key), ['invalid'])
})

test('selected-only results keep exact sessions, not other screenings of selected movies', () => {
  const selected = view({ key: 'ugc:ugc-showing-12' })
  const sameMovie = view({ key: 'ugc:ugc-showing-13', effectiveStartTime: '2026-08-24T20:00:00+02:00', end: canonical('2026-08-24T22:00:00+02:00') })
  const otherMovie = view({ key: 'kinepolis:kinepolis-showing-42', provider: 'kinepolis', movieKey: 'kinepolis:film-2', effectiveStartTime: '2026-08-24T22:00:00+02:00', end: canonical('2026-08-24T23:00:00+02:00') })
  const source = [selected, sameMovie, otherMovie]
  const before = structuredClone(source)

  assert.deepEqual(filterSelectedShowtimeResults(source, [selected.key, 'stale:key', selected.key]), [selected])
  assert.deepEqual(filterSelectedShowtimeResults(source, [otherMovie.key, selected.key]), [selected, otherMovie])
  assert.deepEqual(filterCompatibleShowtimeResults(source, [selected.key]), source)
  assert.deepEqual(source, before)
})

test('selected-only results restore the normal view with no valid selection', () => {
  const source = [view({ key: 'available' })]
  for (const keys of [[], ['stale:key']]) {
    const filtered = filterSelectedShowtimeResults(source, keys)
    assert.deepEqual(filtered, source)
    assert.notEqual(filtered, source)
  }
  assert.deepEqual(filterSelectedShowtimeResults([], ['stale:key']), [])
})

test('selection query serialization requires a nonempty serializable selection for selected-only mode', () => {
  const keys = ['ugc:ugc-showing-12', 'ugc:ugc-showing-12']
  assert.deepEqual(showtimeSelectionQueryValues(keys, true), { selected: 'u12', selected_only: '1' })
  assert.deepEqual(showtimeSelectionQueryValues(keys, false), { selected: 'u12', selected_only: undefined })
  for (const empty of [[], ['invalid']]) {
    assert.deepEqual(showtimeSelectionQueryValues(empty, true), { selected: undefined, selected_only: undefined })
  }
})
