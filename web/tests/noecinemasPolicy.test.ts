import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import type { Provider, SlotResult, TheaterShowtimesResponse } from '../app/types/api.ts'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { safePosterUrl } from '../app/utils/safeImageUrl.ts'
import { availableLanguageOptions, languageLabel } from '../app/utils/showtimeFilters.ts'
import { filterSelectedShowtimeResults, parseShowtimeSelection, serializeShowtimeSelection, toSlotShowtimeResults, toTheaterShowtimeResults, validShowtimeSelectionKeys } from '../app/utils/showtimeResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'
import { THEATER_PROVIDER_COLORS, THEATER_PROVIDER_LABELS, buildTheaterFeatureCollection } from '../app/utils/theaterMap.ts'
import { theaterDisplayName } from '../app/utils/theaterDisplayName.ts'
import { cinemaDescription } from '../app/utils/entityDescriptions.ts'

const hash = 'eb8c701bf9eb902f738cb7a32ed14cb55b9e2b42e0fc346ac79d9cf11d171bbc'
const compactHash = '64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w'
const showingId = `noecinemas-showing-P8088-${hash}`
const booking = 'https://achat.cinema-laigle.com/reserver/r/19'
const theaters = ['B0158', 'B0181', 'P0089', 'P0101', 'P0276', 'P0290', 'P0297', 'P0542', 'P0613', 'P0713', 'P0714', 'P0733', 'P0975', 'P0997', 'P2132', 'P2425', 'P2478', 'P7898', 'P8088', 'P9554', 'W2750', 'W5200', 'W7619', 'W8390']
const providers: Provider[] = ['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran']
const corpus: { valid: string[], invalid: string[] } = JSON.parse(await readFile(new URL('./fixtures/noecinemas-booking-urls.json', import.meta.url), 'utf8'))
const prefix = 'https://achat.cinema-laigle.com/reserver/r/'
const maximum = prefix + '1'.repeat(4096 - prefix.length)
const crawlerSource = await readFile(new URL('../tools/verify-crawlability.mjs', import.meta.url), 'utf8')
const crawlerStart = crawlerSource.indexOf('function reservationUrl(')
const crawlerEnd = crawlerSource.indexOf('async function get(', crawlerStart)
const crawler = runInNewContext(`${crawlerSource.slice(crawlerStart, crawlerEnd)}; ({ reservationUrl })`, { URL })

test('Noé default desktop booking corpus freezes exactly 24 theater paths on 14 hosts', () => {
  assert.equal(new Set(corpus.valid.map((value) => new URL(value).hostname)).size, 14)
  assert.ok(corpus.valid.some((value) => value.length === 4096))
  assert.ok(corpus.invalid.some((value) => value.length === 4097))
  for (const value of [...corpus.valid, maximum]) {
    assert.deepEqual(safeBookingUrl(value, 'noecinemas'), { provider: 'noecinemas', url: value, kind: 'booking' })
    assert.equal(safeBookingUrl(value)?.url, value)
    assert.equal(crawler.reservationUrl({ booking_url: value, provider: 'noecinemas' }), value)
  }
  for (const value of [...corpus.invalid, `${maximum}1`]) {
    assert.equal(safeBookingUrl(value, 'noecinemas'), null, JSON.stringify(value))
    assert.equal(crawler.reservationUrl({ booking_url: value, provider: 'noecinemas' }), null, JSON.stringify(value))
  }
  for (const provider of providers) {
    assert.equal(safeBookingUrl(booking, provider), null)
    assert.equal(crawler.reservationUrl({ booking_url: booking, provider }), null)
  }
})

test('Noé booking binds each path to optional theater and showing contexts, never reconstructs session from hash', () => {
  for (const [index, theater] of theaters.entries()) {
    const value = corpus.valid[index]!
    const id = `noecinemas-showing-${theater}-${hash}`
    assert.equal(safeBookingUrl(value, 'noecinemas', id, `noecinemas-${theater}`)?.url, value)
    for (const other of [...theaters.filter((item) => item !== theater), 'W8391']) {
      assert.equal(safeBookingUrl(value, 'noecinemas', undefined, `noecinemas-${other}`), null)
      assert.equal(safeBookingUrl(value, 'noecinemas', `noecinemas-showing-${other}-${hash}`), null)
    }
    assert.equal(crawler.reservationUrl({ booking_url: value, provider: 'noecinemas', id, theater_id: `noecinemas-${theater}` }), value)
  }
  for (const id of [null, '', showingId + '\n', showingId + ' ', showingId.replace(hash, hash.toUpperCase()), showingId.replace('P8088', 'p8088'), showingId.replace('noecinemas', 'grandecran'), 'noecinemas-showing-P8088-19']) {
    assert.equal(safeBookingUrl(booking, 'noecinemas', id), null)
    assert.equal(crawler.reservationUrl({ booking_url: booking, provider: 'noecinemas', id }), null)
  }
  for (const theater of [null, '', 'P8088', 'noecinemas-p8088', 'noecinemas-P8088\n', 'noecinemas-W8391', 'grandecran-P8088']) {
    assert.equal(safeBookingUrl(booking, 'noecinemas', showingId, theater), null)
    assert.equal(crawler.reservationUrl({ booking_url: booking, provider: 'noecinemas', id: showingId, theater_id: theater }), null)
  }
})

test('Noé n tokens are canonical, full-string, collision-free cinema-qualified SHA-256 encodings', () => {
  const keys = ['P8088', 'B0158', '00000'].map((theater) => `noecinemas:noecinemas-showing-${theater}-${hash}`)
  for (const [index, theater] of ['P8088', 'B0158', '00000'].entries()) {
    assert.equal(serializeShowtimeSelection([keys[index]!]), `n${theater}-${compactHash}`)
    assert.deepEqual(parseShowtimeSelection(`n${theater}-${compactHash}`), [keys[index]])
  }
  const all = [...keys, `grandecran:grandecran-showing-P8088-${hash}`, `cgr:cgr-showing-P8088-${hash}`, 'mk2:mk2-showing-0004-140350', 'ugc:ugc-showing-12']
  assert.deepEqual(parseShowtimeSelection(serializeShowtimeSelection([...all, ...keys])), all.toSorted())
  for (const theater of ['p8088', 'P808', 'P80888', 'P80_8', 'P80-8', 'P80é8']) {
    assert.equal(serializeShowtimeSelection([`noecinemas:noecinemas-showing-${theater}-${hash}`]), undefined)
    assert.deepEqual(parseShowtimeSelection(`n${theater}-${compactHash}`), [])
  }
  for (const token of [`nP8088-${compactHash}=`, `nP8088-${compactHash.slice(0, -1)}x`, `nP8088-${compactHash}\n`, `nP8088-${compactHash} `, `nP8088-${compactHash.slice(1)}`]) assert.deepEqual(parseShowtimeSelection(token), [], token)
  for (const value of [hash.toUpperCase(), `${hash}\n`, `${hash} `, hash.slice(1), `${hash}0`]) assert.equal(serializeShowtimeSelection([`noecinemas:noecinemas-showing-P8088-${value}`]), undefined)
})

test('Noé selected URL reload retains cinema scope, future dates and selected-only filters', () => {
  const theaterIds = ['noecinemas-B0158', 'noecinemas-P8088']
  const selectedShowtimeKeys = theaterIds.map((theater) => `noecinemas:${theater.replace('noecinemas-', 'noecinemas-showing-')}-${hash}`)
  const target = buildCompleteSearchShareTarget({
    theaterIds, date: '2027-07-01', startAfter: '00:00', finishBefore: '23:30', language: 'VF', format: 'DOLBY', includeAds: false, bufferAds: 15,
    grouping: 'chronological', layout: 'boxes', selectedShowtimeKeys, selectedOnly: true
  })
  const shared = withSharedTheaterSelection(target, theaterIds)!
  assert.equal(isValidShortLinkTarget(shared), true)
  const query = new URL(shared, 'https://messeances.fr').searchParams
  assert.equal(query.get('shared_theaters'), theaterIds.join(','))
  assert.equal(query.get('selected'), `nB0158-${compactHash},nP8088-${compactHash}`)
  assert.equal(query.get('selected_only'), '1')
  assert.deepEqual(parseShowtimeSelection(query.get('selected')!), selectedShowtimeKeys)
})

test('Noé results preserve event IDs, VFSTF, source rooms and unknown metadata without fabricating ends', () => {
  const start = '2027-07-01T00:15:00+02:00'
  const time = '2027-07-01T02:03:00+02:00'
  for (const runtime of [0, 93, 118]) {
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-09-14T12:00:00Z', timezone: 'Europe/Paris', date: '2027-06-30',
      theater: { provider: 'noecinemas', id: 'noecinemas-P8088', slug: 'noecinemas-P8088', name: "Cinéma L'Aigle", city: "L'Aigle", city_slug: 'l-aigle', postal_code: '61300', address: '1 rue du cinéma', available_dates: ['2027-06-30'], accepted_passes: [], latitude: 48.76, longitude: 0.63 },
      showtimes: [{ provider: 'noecinemas', id: showingId, movie: { slug: 'noecinemas-film-cEvent_2027-1', title: 'Événement local', runtime_minutes: runtime, updated_at: start }, start_time: start, end_time: start, estimated_end_time: null, estimated_end_ads_minutes: null, language: 'VFSTF', format: 'DOLBY', room: '', booking_url: booking, start_offset_minutes: 1275, duration_minutes: 0, poster_url: null, backdrop_url: null }]
    }
    const slot: SlotResult = { showtime: response.showtimes[0]!, theater: response.theater, poster_url: null, backdrop_url: null, effective_start_time: start, effective_end_time: start, buffer_ads_minutes: 0, slack_before_minutes: 0, slack_after_minutes: 0 }
    const before = structuredClone({ response, slot })
    for (const result of [toTheaterShowtimeResults(response)[0]!, toSlotShowtimeResults([slot])[0]!]) {
      assert.equal(result.key, `noecinemas:${showingId}`)
      assert.equal(result.movieSlug, 'noecinemas-film-cEvent_2027-1')
      assert.equal(result.movieRuntimeMinutes, runtime)
      assert.equal(result.room, '')
      assert.equal(result.posterUrl, null)
      assert.equal(result.backdropUrl, null)
      assert.equal(result.end, null)
      assert.equal(result.bookingUrl, booking)
      assert.equal(result.language, 'VFSTF')
      assert.equal(result.format, 'DOLBY')
      const reloaded = parseShowtimeSelection(serializeShowtimeSelection([result.key]))
      assert.deepEqual(validShowtimeSelectionKeys([result], reloaded), [result.key])
      assert.deepEqual(filterSelectedShowtimeResults([result], reloaded), [result])
    }
    assert.deepEqual({ response, slot }, before)
    response.showtimes[0]!.room = 'Salle 2'
    assert.equal(toTheaterShowtimeResults(response)[0]!.room, 'Salle 2')
    if (runtime > 0) {
      response.showtimes[0]!.estimated_end_time = time
      response.showtimes[0]!.estimated_end_ads_minutes = 15
      assert.deepEqual(toTheaterShowtimeResults(response)[0]!.end, { time, estimated: true, adsMinutes: 15 })
      assert.equal(response.showtimes[0]!.end_time, start)
    }
    assert.equal(THEATER_PROVIDER_LABELS.noecinemas, 'Noé Cinémas')
    assert.equal(THEATER_PROVIDER_COLORS.noecinemas, '#795548')
    assert.deepEqual(buildTheaterFeatureCollection([response.theater], new Set([response.theater.id])).features[0]?.properties, { id: 'noecinemas-P8088', provider: 'noecinemas', favorite: true })
  }
  assert.deepEqual(availableLanguageOptions(['VOSTFR', 'VF', 'VFSTF']).map((option) => option.value), ['ALL', 'VOSTFR', 'VF', 'VFSTF'])
  assert.equal(languageLabel('VFSTF'), 'VFSTF')
  assert.equal(safePosterUrl(null), null)
  assert.equal(safePosterUrl('https://all.web.img.acsta.net/pictures/event.jpg'), 'https://all.web.img.acsta.net/pictures/event.jpg')
  assert.equal(safePosterUrl('https://all.web.img.acsta.net.evil.test/pictures/event.jpg'), null)
})

test('Noé brand-only names keep city context without corrupting other words', () => {
  for (const name of ['Noé Cinémas', 'Noe Cinemas', 'noé cinémas', 'NOE CINEMAS', 'Noé Cinémas'.normalize('NFD')]) assert.equal(theaterDisplayName({ name, city: "L'Aigle" }), `${name} L'Aigle`)
  for (const name of ["Noé Cinémas L'Aigle", 'Noe Cinemascope', 'éNoe Cinemas', 'Noe Cinemas_centre']) assert.equal(theaterDisplayName({ name, city: "L'Aigle" }), name)
  assert.match(cinemaDescription({ provider: 'noecinemas', name: "Cinéma L'Aigle", city: "L'Aigle", availableDateCount: 2 }), /est un cinéma Noé Cinémas/)
})
