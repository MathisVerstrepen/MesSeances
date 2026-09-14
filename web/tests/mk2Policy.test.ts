import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import type { TheaterShowtimesResponse } from '../app/types/api.ts'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { posterImageSources, safePosterUrl } from '../app/utils/safeImageUrl.ts'
import { availableLanguageOptions, queryLanguageValues } from '../app/utils/showtimeFilters.ts'
import { parseShowtimeSelection, serializeShowtimeSelection, toTheaterShowtimeResults } from '../app/utils/showtimeResults.ts'

const booking = 'https://www.mk2.com/panier/seance/tickets?cinemaId=0004&sessionId=140350'
const showingId = 'mk2-showing-0004-140350'
const poster = 'https://srv-web-vista.mk2.com/CDN/media/entity/get/FilmPosterGraphic/HO00006568'

const invalidBookings = [
  ` ${booking}`, `${booking}\n`, `${booking}\t`, `${booking}#`, `${booking}#tickets`, `${booking}&extra=1`, `${booking}&sessionId=140350`,
  booking.replace('https:', 'http:'), booking.replace('www.mk2.com', 'www.mk2.com.evil.test'),
  booking.replace('www.mk2.com', 'WWW.MK2.COM'), booking.replace('www.mk2.com', 'user@www.mk2.com'),
  booking.replace('www.mk2.com', 'www.mk2.com:443'), booking.replace('www.mk2.com', 'www.mk2.com:8443'),
  booking.replace('/panier/', '/other/../panier/'), booking.replace('/panier/', '/%70anier/'),
  booking.replace('cinemaId=0004&sessionId=140350', 'sessionId=140350&cinemaId=0004'),
  booking.replace('0004', '0000'), booking.replace('0004', '%30%30%30%34'), booking.replace('0004', '+4'),
  booking.replace('140350', '0'), booking.replace('140350', '0140350'), booking.replace('140350', '1e3'),
  booking.replace('140350', '1'.repeat(112)), booking.replace('tickets?', 'tickets/?'), 'https://www.mk2.com/'
]
const invalidPosters = [
  ` ${poster}`, `${poster}\n`, `${poster}?`, `${poster}?v=1`, `${poster}#`, `${poster}#image`,
  poster.replace('https:', 'http:'), poster.replace('.mk2.com', '.mk2.com.evil.test'),
  poster.replace('srv-web-vista.', 'www.'), poster.replace('srv-web-vista.', 'user@srv-web-vista.'),
  poster.replace('.com/', '.com:443/'), poster.replace('.com/', '.com:8443/'),
  poster.replace('/CDN/', '/foo/../CDN/'), poster.replace('/CDN/', '/%43DN/'),
  poster.replace('FilmPosterGraphic', 'FilmBackdrop'), poster.replace('HO00006568', 'ho00006568'),
  poster.replace('HO00006568', 'HO'), poster.replace('HO00006568', `HO${'1'.repeat(118)}`),
  poster.replace('HO00006568', '%48O00006568'), `${poster}/`, `${poster}${'1'.repeat(2048)}`
]

test('MK2 booking permits only canonical bytes, bounded identities and exact showing binding', () => {
  assert.deepEqual(safeBookingUrl(booking, 'mk2', showingId), { provider: 'mk2', url: booking, kind: 'booking' })
  assert.equal(safeBookingUrl(booking)?.url, booking)
  for (const id of ['mk2-showing-4-140350', 'mk2-showing-0005-140350', 'mk2-showing-0004-140351', 'ugc-showing-0004-140350', '', null]) {
    assert.equal(safeBookingUrl(booking, 'mk2', id), null)
  }
  for (const provider of ['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville'] as const) assert.equal(safeBookingUrl(booking, provider), null)
  for (const value of invalidBookings) assert.equal(safeBookingUrl(value), null, value)
  const max = booking.replace('140350', '1'.repeat(111))
  assert.equal(safeBookingUrl(max)?.url, max)
})

test('MK2 source posters use only verified FilmPosterGraphic grammar without TMDB transforms', () => {
  assert.equal(safePosterUrl(poster), poster)
  assert.deepEqual(posterImageSources(poster), { src: poster, srcset: null })
  for (const value of invalidPosters) assert.equal(safePosterUrl(value), null, value)
  const max = poster.replace('HO00006568', `HO${'1'.repeat(117)}`)
  assert.equal(safePosterUrl(max), max)
})

test('MK2 crawlability policy agrees with frontend policy on accepted and rejected bytes and binding', async () => {
  const source = await readFile(new URL('../tools/verify-crawlability.mjs', import.meta.url), 'utf8')
  const start = source.indexOf('function hasSafeImagePath(')
  const end = source.indexOf('async function get(', start)
  const policy = runInNewContext(`${source.slice(start, end)}; ({ safePosterUrl, reservationUrl })`, { URL })
  for (const value of [poster, ...invalidPosters]) assert.equal(policy.safePosterUrl(value), safePosterUrl(value), value)
  for (const value of [booking, ...invalidBookings]) {
    for (const id of [showingId, 'mk2-showing-0005-140350', '', null]) {
      assert.equal(policy.reservationUrl({ booking_url: value, provider: 'mk2', id }), safeBookingUrl(value, 'mk2', id)?.url ?? null, `${value}: ${id}`)
    }
  }
  assert.equal(policy.reservationUrl({ booking_url: booking, provider: 'ugc', id: showingId }), null)
})

test('MK2 x tokens preserve leading zeros, cinema scope, bounds and cross-provider identities', () => {
  const keys = ['mk2:mk2-showing-0004-140350', 'mk2:mk2-showing-0005-140350', 'mk2:mk2-showing-4-140350', 'cineville:cineville-showing-4-140350', 'ugc:ugc-showing-140350']
  assert.equal(serializeShowtimeSelection([keys[0]!]), 'x0004-140350')
  assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection(keys)), keys.toSorted())
  for (const id of ['0000-1', '0-1', '0004-0', '0004-01', '0004--1', '0004-1-2', '+4-1', '4.0-1', '4e1-1', '0004-1\n', '0004-1 ', '0004-%31', '0004/1', `0004-${'1'.repeat(112)}`]) {
    assert.deepEqual(parseShowtimeSelection(`x${id}`), [], id)
    assert.equal(serializeShowtimeSelection([`mk2:mk2-showing-${id}`]), undefined, id)
  }
  const max = `mk2:mk2-showing-0004-${'1'.repeat(111)}`
  assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection([max])), [max])
})

test('MK2 silent sessions retain source fields, future local date, empty room and explicit end provenance', () => {
  const start = '2027-06-28T00:15:00+02:00'
  const time = '2027-06-28T02:03:00+02:00'
  for (const runtime of [0, 93, 118]) {
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-09-14T12:00:00Z', timezone: 'Europe/Paris', date: '2027-06-28',
      theater: { provider: 'mk2', id: 'mk2-0004', slug: 'mk2-0004', name: 'MK2 Bibliothèque', city: 'Paris', city_slug: 'paris', postal_code: '75013', address: '128 avenue de France', available_dates: ['2027-06-28'], accepted_passes: [] },
      showtimes: [{ provider: 'mk2', id: showingId, movie: { slug: 'mk2-film-HO00006568', title: 'Film muet', runtime_minutes: runtime, updated_at: start }, start_time: start, end_time: start, estimated_end_time: null, estimated_end_ads_minutes: null, language: '', format: '2D', room: '', booking_url: booking, start_offset_minutes: 15, duration_minutes: 0, poster_url: poster, backdrop_url: null }]
    }
    const before = structuredClone(response)
    const result = toTheaterShowtimeResults(response)[0]!
    assert.deepEqual(response, before)
    assert.equal(result.key, `mk2:${showingId}`)
    assert.equal(result.language, '')
    assert.equal(result.room, '')
    assert.equal(result.movieRuntimeMinutes, runtime)
    assert.equal(result.advertisedStartTime, start)
    assert.equal(result.end, null)
    assert.equal(result.posterUrl, poster)
    assert.equal(result.bookingUrl, booking)
    response.showtimes[0]!.estimated_end_time = time
    response.showtimes[0]!.estimated_end_ads_minutes = 15
    assert.deepEqual(toTheaterShowtimeResults(response)[0]!.end, { time, estimated: true, adsMinutes: 15 })
  }
  assert.deepEqual(availableLanguageOptions(['']), [{ value: 'ALL', label: 'Toutes les langues' }])
  assert.deepEqual(availableLanguageOptions(['', 'VF']).map((option) => option.value), ['ALL', 'VF'])
  assert.deepEqual(queryLanguageValues, ['ALL', 'VOSTFR', 'VF'])
})
