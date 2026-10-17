import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  cineodeSlugs,
  cineodeShowingParts,
  isValidCineodeMovieId,
} from '../app/utils/cineode.ts'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
  toTheaterShowtimeResults,
  validShowtimeSelectionKeys,
} from '../app/utils/showtimeResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'
import type { Provider, TheaterShowtimesResponse } from '../app/types/api.ts'

const showing = 'cineode-showing-991-atelier-local-1792188900'
const theater = 'cineode-991'
const website = 'https://www.cineode.fr/le-don-camillo/seances/'

test('Cinéode complete manifest matches authoritative Go and SQL, and all 37 websites bind to their identities', async () => {
  const go = await readFile(
    new URL('../../api/internal/schedule/cineode.go', import.meta.url),
    'utf8',
  )
  const sql = await readFile(
    new URL(
      '../../api/internal/database/migrations/067_cineode_provider.sql',
      import.meta.url,
    ),
    'utf8',
  )
  assert.equal(Object.keys(cineodeSlugs).length, 37)
  assert.deepEqual(
    cineodeSlugs,
    Object.fromEntries(
      [...go.matchAll(/case "([0-9]+)":\s*return "([a-z0-9-]+)"/g)].map(
        (match) => [match[1], match[2]],
      ),
    ),
  )
  assert.deepEqual(
    cineodeSlugs,
    Object.fromEntries(
      [
        ...sql
          .slice(0, sql.indexOf('CREATE FUNCTION cineode_identity_valid'))
          .matchAll(/WHEN '([0-9]+)' THEN '([a-z0-9-]+)'/g),
      ].map((match) => [match[1], match[2]]),
    ),
  )
  for (const [id, slug] of Object.entries(cineodeSlugs)) {
    for (const film of [
      '631242',
      'atelier-local',
      'james-et-la-peche-geante-1997',
    ]) {
      const raw = `https://www.cineode.fr/${slug}/seances/`
      const showtime = `cineode-showing-${id}-${film}-1792188900`
      const expected = { provider: 'cineode', url: raw, kind: 'website' }
      assert.deepEqual(
        safeBookingUrl(raw, 'cineode', showtime, `cineode-${id}`),
        expected,
      )
      assert.deepEqual(safeBookingUrl(raw), expected)
      assert.equal(
        safeBookingUrl(
          raw,
          'cineode',
          showtime,
          `cineode-${id === '991' ? '4444' : '991'}`,
        ),
        null,
      )
    }
  }
})

test('Cinéode rejects raw URL variants, fabricated checkout, cross-provider and invalid supplied contexts', () => {
  for (const raw of [
    website.replace('https:', 'http:'),
    website.replace('www.', ''),
    website.replace('www.', 'WWW.'),
    website.replace('.fr', '.fr.evil.test'),
    website.replace('.fr', '.fr:443'),
    website.replace('.fr', '.fr:444'),
    website.replace('https://', 'https://user@'),
    website.replace('https://', 'https://user:pass@'),
    website.replace('/le-don-camillo', '/la-grange'),
    website.replace('/le-don-camillo', '/x/../le-don-camillo'),
    website.replace('/le-don-camillo', '//le-don-camillo'),
    website.replace('seances/', 'seances'),
    website.replace('seances', '%73eances'),
    website.replace('/seances/', '/film/631242/'),
    website.replace('/seances/', '/r/1792188900'),
    website.replace('/seances/', '\\seances/'),
    `${website}?x=1`,
    `${website}#`,
    `${website}#showsession?id=1792188900`,
    ` ${website}`,
    `${website} `,
    `${website}\n`,
    `${website}\r`,
    `${website}\t`,
    `${website}\0`,
  ])
    assert.equal(safeBookingUrl(raw, 'cineode', showing, theater), null, raw)
  for (const [id, venue] of [
    [null, theater],
    ['', theater],
    [showing + '\n', theater],
    [showing.toUpperCase(), theater],
    [showing.replace('991-', '4444-'), theater],
    [showing, null],
    [showing, ''],
    [showing, theater + '\n'],
    [showing, 'veo-991'],
    [showing, 'cineode-0991'],
    [showing, 'cineode-999999'],
  ])
    assert.equal(safeBookingUrl(website, 'cineode', id, venue), null)
  assert.equal(safeBookingUrl(website, null), null)
  for (const provider of [
    'ugc',
    'kinepolis',
    'pathe',
    'cgr',
    'megarama',
    'cineville',
    'mk2',
    'cinewest',
    'grandecran',
    'noecinemas',
    'veo',
    'majestic',
  ] satisfies Provider[]) {
    assert.equal(safeBookingUrl(website, provider), null)
    assert.equal(safeBookingUrl('https://www.ugc.fr/', 'cineode'), null)
  }
})

test('Cinéode o codec enforces Go derived ID and positive int64 bounds, preserves existing tokens and complete shares', () => {
  const valid = [
    '991-631242-1792188900',
    '991-atelier-local-1792188900',
    '4444-james-et-la-peche-geante-1997-9223372036854775807',
    `991-${'a'.repeat(106)}-1`,
  ]
  for (const id of valid) {
    assert.ok(cineodeShowingParts(id))
    const key = `cineode:cineode-showing-${id}`
    assert.equal(serializeShowtimeSelection([key]), `o${id}`)
    assert.deepEqual(parseShowtimeSelection(`o${id}`), [key])
  }
  const invalid = [
    '991-01-1792188900',
    '0991-631242-1792188900',
    '9999-631242-1792188900',
    '991-A-1792188900',
    '991-a--b-1792188900',
    '991-a_-1792188900',
    '991-a-0',
    '991-a-01',
    '991-a--1',
    '991-a-9223372036854775808',
    `991-${'a'.repeat(107)}-1`,
    '991-a-1\n',
    '991-a-1 ',
    '991-a-%31',
    '991-a-1.0',
  ]
  for (const id of invalid) {
    assert.equal(cineodeShowingParts(id), null, id)
    assert.equal(
      serializeShowtimeSelection([`cineode:cineode-showing-${id}`]),
      undefined,
    )
    assert.deepEqual(parseShowtimeSelection(`o${id}`), [])
  }
  assert.equal(isValidCineodeMovieId('a'.repeat(115)), true)
  assert.equal(isValidCineodeMovieId('a'.repeat(116)), false)
  const keys = valid.map((id) => `cineode:cineode-showing-${id}`)
  const all = [
    ...keys,
    'ugc:ugc-showing-12',
    'mk2:mk2-showing-0004-140350',
    'majestic:majestic-showing-emsx070000000001',
  ]
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection([...all, ...keys])),
    all.toSorted(),
  )
  const target = buildCompleteSearchShareTarget({
    theaterIds: [theater],
    date: '2026-10-17',
    startAfter: '00:00',
    finishBefore: '23:30',
    language: 'VOSTFR',
    format: '2D',
    includeAds: false,
    bufferAds: 15,
    grouping: 'movie',
    layout: 'boxes',
    selectedShowtimeKeys: keys,
    selectedOnly: true,
  })!
  assert.equal(isValidShortLinkTarget(target), true)
  assert.deepEqual(
    parseShowtimeSelection(
      new URL(target, 'https://messeances.fr').searchParams.get('selected'),
    ),
    keys.toSorted(),
  )
})

test('Cinéode HTTP fixture mapping preserves midnight date, unknown metadata and explicit response-only estimates', () => {
  for (const estimated of [false, true]) {
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-10-16T12:00:00Z',
      timezone: 'Europe/Paris',
      date: '2026-10-17',
      theater: {
        provider: 'cineode',
        id: theater,
        slug: theater,
        name: 'Le Don Camillo',
        city: 'Montmirail',
        city_slug: 'montmirail',
        postal_code: '',
        address: '',
        available_dates: ['2026-10-17'],
        accepted_passes: [],
      },
      showtimes: [
        {
          provider: 'cineode',
          id: showing,
          movie: {
            slug: 'durable-public-movie',
            title: "L'été",
            original_language: null,
            runtime_minutes: estimated ? 100 : 0,
            updated_at: '2026-10-16T12:00:00Z',
          },
          start_time: '2026-10-17T00:15:00+02:00',
          end_time: '2026-10-17T00:15:00+02:00',
          estimated_end_time: estimated ? '2026-10-17T02:10:00+02:00' : null,
          estimated_end_ads_minutes: estimated ? 15 : null,
          language: 'VOSTFR',
          format: '2D',
          room: '',
          booking_url: website,
          start_offset_minutes: 15,
          duration_minutes: 0,
          poster_url: null,
          backdrop_url: null,
        },
      ],
    }
    const before = structuredClone(response)
    const result = toTheaterShowtimeResults(response)[0]!
    assert.equal(result.movieSlug, 'durable-public-movie')
    assert.equal(result.movieRuntimeMinutes, estimated ? 100 : 0)
    assert.equal(result.posterUrl, null)
    assert.equal(result.room, '')
    assert.equal(result.language, 'VOSTFR')
    assert.equal(result.bookingUrl, website)
    assert.deepEqual(
      result.end,
      estimated
        ? { time: '2026-10-17T02:10:00+02:00', estimated: true, adsMinutes: 15 }
        : null,
    )
    assert.deepEqual(
      validShowtimeSelectionKeys(
        [result],
        parseShowtimeSelection(serializeShowtimeSelection([result.key])),
      ),
      [result.key],
    )
    assert.deepEqual(response, before)
    response.showtimes = []
    response.theater.available_dates = []
    assert.deepEqual(toTheaterShowtimeResults(response), [])
  }
})
