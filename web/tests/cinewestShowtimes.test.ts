import assert from 'node:assert/strict'
import test from 'node:test'
import type { MovieShowtimesResponse, Showtime, SlotResult, TheaterShowtimesResponse } from '../app/types/api.ts'
import { buildFilmJsonLd } from '../app/utils/filmJsonLd.ts'
import { resolveShowtimeEnd } from '../app/utils/showtimeEnd.ts'
import { availableFormatOptions, availableLanguageOptions } from '../app/utils/showtimeFilters.ts'
import { areShowtimeResultsCompatible, filterCompatibleShowtimeResults, filterSelectedShowtimeResults, toSlotShowtimeResults, toTheaterShowtimeResults } from '../app/utils/showtimeResults.ts'

const start = '2027-06-27T18:00:00+02:00'
const publishedEnd = '2027-06-27T21:07:42.123+02:00'
const runtimeEnd = '2027-06-27T20:13:00+02:00'
const movie = { slug: 'cinewest-film-cineoffice-42', title: 'Événement Cinewest', runtime_minutes: 118, updated_at: '2026-09-14T00:00:00Z' }

function showing(platform: 'cineoffice' | 'ticketingcine' | 'webediamovies', overrides: Partial<Showtime> = {}): Showtime {
  return {
    provider: 'cinewest', id: `cinewest-showing-${platform}-${'a'.repeat(64)}`, movie,
    start_time: start, end_time: start, estimated_end_time: null, estimated_end_ads_minutes: null,
    language: 'VF', format: '2D', room: 'Salle 1', booking_url: null, ...overrides
  }
}

function theaterResponse(showtimes: Showtime[]): TheaterShowtimesResponse {
  return {
    generated_at: '2026-09-14T12:00:00Z', timezone: 'Europe/Paris', date: '2027-06-27',
    theater: {
      provider: 'cinewest', id: 'cinewest-webediamovies-W8400', slug: 'cinewest-webediamovies-W8400',
      name: 'Capitole Studios', city: 'Le Pontet', city_slug: 'le-pontet', postal_code: '84130',
      address: '1 rue du cinéma', available_dates: ['2027-06-27'], accepted_passes: []
    },
    showtimes: showtimes.map((showtime) => ({
      ...showtime, start_offset_minutes: 600,
      duration_minutes: (Date.parse(showtime.estimated_end_time ?? showtime.end_time) - Date.parse(showtime.start_time)) / 60_000,
      poster_url: null, backdrop_url: null
    }))
  }
}

test('Cine Office published end survives display normalization regardless of source or enriched runtime', () => {
  for (const runtime of [0, 93, 118]) {
    const showtime = showing('cineoffice', { movie: { ...movie, runtime_minutes: runtime }, end_time: publishedEnd, language: 'VOSTFR', format: 'SCREENX' })
    const response = theaterResponse([showtime])
    const slot: SlotResult = {
      showtime, theater: response.theater, poster_url: null, backdrop_url: null,
      effective_start_time: '2027-06-27T18:15:00+02:00', effective_end_time: publishedEnd,
      buffer_ads_minutes: 15, slack_before_minutes: 0, slack_after_minutes: 0
    }
    const before = structuredClone({ response, slot })
    for (const result of [toTheaterShowtimeResults(response)[0]!, toSlotShowtimeResults([slot])[0]!]) {
      assert.equal(result.provider, 'cinewest')
      assert.equal(result.key, `cinewest:${showtime.id}`)
      assert.equal(result.movieRuntimeMinutes, runtime)
      assert.equal(result.advertisedStartTime, start)
      assert.equal(result.language, 'VOSTFR')
      assert.equal(result.format, 'SCREENX')
      assert.equal(result.room, 'Salle 1')
      assert.deepEqual(result.end, { time: publishedEnd, estimated: false, adsMinutes: null })
    }
    assert.equal(toSlotShowtimeResults([slot])[0]!.effectiveStartTime, slot.effective_start_time)
    assert.deepEqual({ response, slot }, before)
    // Even an inconsistent response estimate cannot override the published instant.
    assert.deepEqual(resolveShowtimeEnd({ ...showtime, estimated_end_time: runtimeEnd, estimated_end_ads_minutes: 15 }), { time: publishedEnd, estimated: false, adsMinutes: null })
  }
})

test('ticketingcine computed ends, Capitole response estimates and unknown ends retain distinct provenance', () => {
  const ticketing = showing('ticketingcine', { end_time: runtimeEnd, language: 'VFSTF', format: '4DX' })
  const estimated = showing('webediamovies', { estimated_end_time: runtimeEnd, estimated_end_ads_minutes: 15, format: 'DOLBY' })
  const unknown = showing('webediamovies', { id: `cinewest-showing-webediamovies-${'b'.repeat(64)}`, movie: { ...movie, runtime_minutes: 0 }, language: 'VO', format: 'ICE' })
  const response = theaterResponse([ticketing, estimated, unknown])
  const before = structuredClone(response)
  const results = toTheaterShowtimeResults(response)
  assert.deepEqual(results.map((result) => result.end), [
    { time: runtimeEnd, estimated: false, adsMinutes: null },
    { time: runtimeEnd, estimated: true, adsMinutes: 15 },
    null
  ])
  assert.equal((Date.parse(ticketing.end_time) - Date.parse(start)) / 60_000, movie.runtime_minutes + 15)
  assert.equal(estimated.end_time, start)
  assert.equal(unknown.end_time, start)
  assert.equal(response.showtimes[2]!.duration_minutes, 0)
  for (const runtime of [0, 93, 118]) {
    const enriched: Showtime = { ...unknown, movie: { ...movie, runtime_minutes: runtime } }
    assert.equal(resolveShowtimeEnd(enriched), null)
  }
  assert.deepEqual(response, before)
})

test('Cinewest compatibility uses exact published or returned estimated boundaries, never catalog runtime', () => {
  const [published, estimated, unknown] = toTheaterShowtimeResults(theaterResponse([
    showing('cineoffice', { end_time: publishedEnd }),
    showing('webediamovies', { estimated_end_time: runtimeEnd, estimated_end_ads_minutes: 15 }),
    showing('ticketingcine', { movie: { ...movie, runtime_minutes: 0 } })
  ]))
  assert.ok(published && estimated && unknown)
  const following = { ...published, key: 'following', effectiveStartTime: publishedEnd, end: { time: '2027-06-27T23:00:00+02:00', estimated: false, adsMinutes: null } }
  assert.equal(areShowtimeResultsCompatible(published, following), true)
  assert.equal(areShowtimeResultsCompatible(published, { ...following, effectiveStartTime: '2027-06-27T21:07:42.122+02:00' }), false)
  assert.equal(areShowtimeResultsCompatible(estimated, { ...following, effectiveStartTime: runtimeEnd }), true)
  assert.equal(areShowtimeResultsCompatible(estimated, { ...following, effectiveStartTime: '2027-06-27T20:12:59.999+02:00' }), false)
  assert.equal(areShowtimeResultsCompatible(unknown, following), false)
  assert.deepEqual(filterCompatibleShowtimeResults([published, unknown, following], [published.key]), [published, following])
  assert.deepEqual(filterCompatibleShowtimeResults([unknown, following], [unknown.key]), [unknown])
  assert.deepEqual(filterSelectedShowtimeResults([unknown, following], [unknown.key]), [unknown])
})

test('Cinewest language and format options reuse existing filters and do not invent a silent-language label', () => {
  const showtimes = [
    showing('cineoffice', { language: 'VOSTFR', format: 'SCREENX', end_time: publishedEnd }),
    showing('cineoffice', { language: '', format: '2D', end_time: publishedEnd }),
    showing('ticketingcine', { language: 'VFSTF', format: '4DX', end_time: runtimeEnd }),
    showing('webediamovies', { language: 'VO', format: 'ICE' }),
    showing('webediamovies', { language: 'VF', format: 'DOLBY' }),
    showing('webediamovies', { id: `cinewest-showing-webediamovies-${'c'.repeat(64)}`, language: 'VF', format: 'INFINITY_VISION' })
  ]
  const results = toTheaterShowtimeResults(theaterResponse(showtimes))
  assert.equal(results[1]!.language, '')
  assert.deepEqual(availableLanguageOptions(results.map((result) => result.language)).map((option) => option.value), ['ALL', 'VOSTFR', 'VF', 'VO', 'VFSTF'])
  assert.deepEqual(availableFormatOptions(results.map((result) => result.format)).map((option) => option.value), ['ALL', '2D', 'DOLBY', 'SCREENX', '4DX', 'ICE', 'INFINITY_VISION'])
  assert.equal(results[3]!.format, 'ICE')
  assert.equal(results[5]!.format, 'INFINITY_VISION')
  assert.notEqual(results[3]!.key, results[5]!.key)
})

test('Cinewest film JSON-LD preserves published ends and omits estimated or unknown endDate', () => {
  const response = theaterResponse([
    showing('cineoffice', { end_time: publishedEnd, language: '' }),
    showing('ticketingcine', { end_time: runtimeEnd }),
    showing('webediamovies', { estimated_end_time: runtimeEnd, estimated_end_ads_minutes: 15 }),
    showing('webediamovies', { movie: { ...movie, runtime_minutes: 0 } })
  ])
  const schedule: MovieShowtimesResponse = {
    release_status: 'showing', movie: { ...movie, poster_url: null, tmdb_id: null, imdb_id: null, overview: null, release_date: null, french_release_date: null, genres: [] },
    date: response.date, available_dates: response.theater.available_dates, currently_screened: true, backdrop_url: null,
    theaters: [{ ...response.theater, showtimes: response.showtimes }]
  }
  const before = structuredClone(schedule)
  const graph = buildFilmJsonLd(schedule, { siteUrl: 'https://messeances.fr', movieUrl: `https://messeances.fr/film/${movie.slug}` })['@graph']
  const events = graph.filter((node) => node['@type'] === 'ScreeningEvent')
  assert.equal(events.length, 4)
  assert.equal(events[0]!.endDate, publishedEnd)
  assert.equal(events[1]!.endDate, runtimeEnd)
  for (const event of events.slice(2)) {
    assert.equal(event.startDate, start)
    assert.equal(Object.hasOwn(event, 'endDate'), false)
  }
  assert.equal(graph.find((node) => node['@type'] === 'MovieTheater')?.name, 'Capitole Studios')
  assert.deepEqual(schedule, before)
})

test('Infinity Vision and ICE sessions of the same movie retain distinct formats in timeline and slot results', () => {
  const showtimes = [
    showing('webediamovies', { format: 'INFINITY_VISION', end_time: publishedEnd }),
    showing('webediamovies', { id: `cinewest-showing-webediamovies-${'b'.repeat(64)}`, format: 'ICE', end_time: publishedEnd })
  ]
  const response = theaterResponse(showtimes)
  const slots: SlotResult[] = showtimes.map(showtime => ({
    showtime, theater: response.theater, poster_url: null, backdrop_url: null,
    effective_start_time: start, effective_end_time: publishedEnd,
    buffer_ads_minutes: 0, slack_before_minutes: 0, slack_after_minutes: 0
  }))
  const before = structuredClone({ response, slots })
  for (const results of [toTheaterShowtimeResults(response), toSlotShowtimeResults(slots)]) {
    assert.deepEqual(results.map(result => result.format), ['INFINITY_VISION', 'ICE'])
    assert.equal(new Set(results.map(result => result.key)).size, 2)
    assert.equal(new Set(results.map(result => result.movieSlug)).size, 1)
  }
  assert.deepEqual({ response, slots }, before)
})
