import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import type { SlotResult, TheaterShowtimesResponse } from '../app/types/api.ts'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { safePosterUrl } from '../app/utils/safeImageUrl.ts'
import { availableLanguageOptions } from '../app/utils/showtimeFilters.ts'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
  toSlotShowtimeResults,
  toTheaterShowtimeResults,
} from '../app/utils/showtimeResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'
import {
  THEATER_PROVIDER_LABELS,
  buildTheaterFeatureCollection,
} from '../app/utils/theaterMap.ts'
import { theaterDisplayName } from '../app/utils/theaterDisplayName.ts'
import { cinemaDescription } from '../app/utils/entityDescriptions.ts'

const hash = 'eb8c701bf9eb902f738cb7a32ed14cb55b9e2b42e0fc346ac79d9cf11d171bbc'
const compactHash = '64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w'
const showingId = `grandecran-showing-G028P-${hash}`
const booking = 'https://achat.grandecran.fr/grand-ecran-vichy/r/12345'
const corpus: { valid: string[]; invalid: string[] } = JSON.parse(
  await readFile(
    new URL('./fixtures/grandecran-booking-urls.json', import.meta.url),
    'utf8',
  ),
)

test('Grand Ecran booking accepts only canonical exact-host routes and 4096-byte bounds', () => {
  for (const value of corpus.valid) {
    assert.deepEqual(safeBookingUrl(value, 'grandecran'), {
      provider: 'grandecran',
      url: value,
      kind: 'booking',
    })
    assert.equal(safeBookingUrl(value)?.url, value)
  }
  for (const value of corpus.invalid)
    assert.equal(safeBookingUrl(value), null, JSON.stringify(value))
  for (const provider of [
    'ugc',
    'kinepolis',
    'pathe',
    'cgr',
    'megarama',
    'cineville',
    'mk2',
  ] as const)
    assert.equal(safeBookingUrl(booking, provider), null)
  const prefix = 'https://achat.grandecran.fr/a/r/'
  const max = prefix + '1'.repeat(4096 - prefix.length)
  assert.equal(safeBookingUrl(max)?.url, max)
  assert.equal(safeBookingUrl(`${max}1`), null)
})

test('Grand Ecran crawlability booking policy matches frontend malicious corpus', async () => {
  const source = await readFile(
    new URL('../tools/verify-crawlability.mjs', import.meta.url),
    'utf8',
  )
  const start = source.indexOf('function reservationUrl(')
  const end = source.indexOf('async function get(', start)
  const policy = runInNewContext(
    `${source.slice(start, end)}; ({ reservationUrl })`,
    { URL },
  )
  for (const value of [...corpus.valid, ...corpus.invalid]) {
    assert.equal(
      policy.reservationUrl({
        booking_url: value,
        provider: 'grandecran',
        id: showingId,
      }),
      safeBookingUrl(value, 'grandecran')?.url ?? null,
      JSON.stringify(value),
    )
  }
})

test('Grand Ecran g tokens preserve both theater shapes and canonical SHA-256 encoding', () => {
  const keys = ['P9488', 'G028P', '00000'].map(
    (theater) => `grandecran:grandecran-showing-${theater}-${hash}`,
  )
  for (const [index, theater] of ['P9488', 'G028P', '00000'].entries()) {
    assert.equal(
      serializeShowtimeSelection([keys[index]!]),
      `g${theater}-${compactHash}`,
    )
    assert.deepEqual(parseShowtimeSelection(`g${theater}-${compactHash}`), [
      keys[index],
    ])
  }
  const all = [
    ...keys,
    `cgr:cgr-showing-P9488-${hash}`,
    'mk2:mk2-showing-0004-140350',
    'ugc:ugc-showing-12',
  ]
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection([...all, ...keys])),
    all.toSorted(),
  )
  for (const theater of [
    'g028p',
    'P948',
    'P94888',
    'G02_P',
    'G02-P',
    'G02éP',
  ]) {
    assert.equal(
      serializeShowtimeSelection([
        `grandecran:grandecran-showing-${theater}-${hash}`,
      ]),
      undefined,
    )
    assert.deepEqual(parseShowtimeSelection(`g${theater}-${compactHash}`), [])
  }
  for (const token of [
    `gG028P-${compactHash}=`,
    `gG028P-${compactHash.slice(0, -1)}x`,
    `gG028P-${compactHash}\n`,
    `gG028P-${compactHash} `,
    `gG028P-${compactHash.slice(1)}`,
  ])
    assert.deepEqual(parseShowtimeSelection(token), [], token)
  for (const value of [
    hash.toUpperCase(),
    `${hash}\n`,
    `${hash} `,
    hash.slice(1),
    `${hash}0`,
  ])
    assert.equal(
      serializeShowtimeSelection([
        `grandecran:grandecran-showing-G028P-${value}`,
      ]),
      undefined,
    )
})

test('Grand Ecran selected-showing URLs reload with cinema scope, future dates and filters', () => {
  const theaterIds = ['grandecran-G028P', 'grandecran-P9488']
  const selectedShowtimeKeys = theaterIds.map(
    (theater) =>
      `grandecran:${theater.replace('grandecran-', 'grandecran-showing-')}-${hash}`,
  )
  const target = buildCompleteSearchShareTarget({
    theaterIds,
    date: '2027-07-01',
    startAfter: '00:00',
    finishBefore: '23:30',
    language: 'VOSTFR',
    format: 'DOLBY',
    includeAds: false,
    bufferAds: 15,
    grouping: 'chronological',
    layout: 'boxes',
    selectedShowtimeKeys,
    selectedOnly: true,
  })
  const shared = withSharedTheaterSelection(target, theaterIds)!
  assert.equal(isValidShortLinkTarget(shared), true)
  const query = new URL(shared, 'https://messeances.fr').searchParams
  assert.equal(query.get('shared_theaters'), theaterIds.join(','))
  assert.equal(
    query.get('selected'),
    `gG028P-${compactHash},gP9488-${compactHash}`,
  )
  assert.equal(query.get('selected_only'), '1')
  assert.deepEqual(
    parseShowtimeSelection(query.get('selected')!),
    selectedShowtimeKeys,
  )
})

test('Grand Ecran result adapters keep local events and unknown metadata without inferring source ends', () => {
  const start = '2027-07-01T00:15:00+02:00'
  const time = '2027-07-01T02:03:00+02:00'
  for (const runtime of [0, 93, 118]) {
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-09-14T12:00:00Z',
      timezone: 'Europe/Paris',
      date: '2027-07-01',
      theater: {
        provider: 'grandecran',
        id: 'grandecran-G028P',
        slug: 'grandecran-G028P',
        name: 'Grand Ecran Vichy',
        city: 'Vichy',
        city_slug: 'vichy',
        postal_code: '03200',
        address: '1 rue du cinéma',
        available_dates: ['2027-07-01'],
        accepted_passes: [],
        latitude: 46.1278,
        longitude: 3.4255,
      },
      showtimes: [
        {
          provider: 'grandecran',
          id: showingId,
          movie: {
            slug: 'grandecran-film-cEvent_2027-1',
            title: 'Événement local',
            runtime_minutes: runtime,
            updated_at: start,
          },
          start_time: start,
          end_time: start,
          estimated_end_time: null,
          estimated_end_ads_minutes: null,
          language: 'VOSTFR',
          format: 'DOLBY',
          room: '',
          booking_url: booking,
          start_offset_minutes: 15,
          duration_minutes: 0,
          poster_url: null,
          backdrop_url: null,
        },
      ],
    }
    const slot: SlotResult = {
      showtime: response.showtimes[0]!,
      theater: response.theater,
      poster_url: null,
      backdrop_url: null,
      effective_start_time: start,
      effective_end_time: start,
      buffer_ads_minutes: 0,
      slack_before_minutes: 0,
      slack_after_minutes: 0,
    }
    const before = structuredClone({ response, slot })
    for (const result of [
      toTheaterShowtimeResults(response)[0]!,
      toSlotShowtimeResults([slot])[0]!,
    ]) {
      assert.equal(result.key, `grandecran:${showingId}`)
      assert.equal(result.movieSlug, 'grandecran-film-cEvent_2027-1')
      assert.equal(result.movieRuntimeMinutes, runtime)
      assert.equal(result.room, '')
      assert.equal(result.posterUrl, null)
      assert.equal(result.backdropUrl, null)
      assert.equal(result.end, null)
      assert.equal(result.bookingUrl, booking)
      assert.equal(result.language, 'VOSTFR')
      assert.equal(result.format, 'DOLBY')
    }
    assert.deepEqual({ response, slot }, before)
    if (runtime > 0) {
      for (const ads of [0, 15, 30]) {
        response.showtimes[0]!.estimated_end_time = time
        response.showtimes[0]!.estimated_end_ads_minutes = ads
        assert.deepEqual(toTheaterShowtimeResults(response)[0]!.end, {
          time,
          estimated: true,
          adsMinutes: ads,
        })
        assert.deepEqual(toSlotShowtimeResults([slot])[0]!.end, {
          time,
          estimated: true,
          adsMinutes: ads,
        })
        assert.equal(response.showtimes[0]!.end_time, start)
      }
    }
    assert.equal(THEATER_PROVIDER_LABELS.grandecran, 'Grand Ecran')
    assert.deepEqual(
      buildTheaterFeatureCollection(
        [response.theater],
        new Set([response.theater.id]),
      ).features[0]?.properties,
      { id: 'grandecran-G028P', provider: 'grandecran', favorite: true },
    )
  }
  assert.deepEqual(
    availableLanguageOptions(['VOSTFR', 'VF']).map((option) => option.value),
    ['ALL', 'VOSTFR', 'VF'],
  )
  assert.equal(safePosterUrl(null), null)
  assert.equal(
    safePosterUrl('https://fr.web.img6.acsta.net/pictures/event.jpg'),
    'https://fr.web.img6.acsta.net/pictures/event.jpg',
  )
})

test('Grand Ecran brand-only names keep city context without duplicating location text', () => {
  for (const name of [
    'Grand Ecran',
    'Grand Écran',
    'grand écran',
    'GRAND ECRAN',
  ])
    assert.equal(theaterDisplayName({ name, city: 'Vichy' }), `${name} Vichy`)
  assert.equal(
    theaterDisplayName({ name: 'Grand Écran Vichy', city: 'Vichy' }),
    'Grand Écran Vichy',
  )
  assert.match(
    cinemaDescription({
      provider: 'grandecran',
      name: 'Grand Ecran Vichy',
      city: 'Vichy',
      availableDateCount: 2,
    }),
    /est un cinéma Grand Ecran/,
  )
})
