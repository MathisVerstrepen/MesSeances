import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
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

const source = await readFile(
  new URL('../tools/verify-crawlability.mjs', import.meta.url),
  'utf8',
)
const start = source.indexOf('function reservationUrl(')
const crawler = runInNewContext(
  `${source.slice(start, source.indexOf('async function get(', start))}; ({ reservationUrl })`,
  { URL, createHash },
)
const sites = ['0700', '0654', '1333', '1212', '1299', '1211', '0957']
const hosts = [
  'majestic-compiegne.fr',
  'majestic-douai.com',
  'dole.majestic-cinemas.com',
  'vesoul.majestic-cinemas.com',
  'cinelaon.fr',
  'remiremont.majestic-cinemas.com',
  'cinevendome.fr',
]
const session = 'emsx070000000001'
const showing = `majestic-showing-${session}`
const theater = 'majestic-EMS0700'
const global = `https://www.ticketingcine.com/EMS0700/FRZH1EX#showsession?id=${session}`
const local = `https://www.ticketingcine.com?EMS0700#showsession?id=${session}`

function parity(
  url: string,
  expected: 'booking' | 'website' | null,
  id?: string | null,
  theaterId?: string | null,
) {
  assert.deepEqual(
    safeBookingUrl(url, 'majestic', id, theaterId),
    expected ? { provider: 'majestic', url, kind: expected } : null,
    url,
  )
  assert.equal(
    crawler.reservationUrl({
      booking_url: url,
      provider: 'majestic',
      id,
      theater_id: theaterId,
    }),
    expected ? url : null,
    url,
  )
}

test('Majestic seven-site global/local raw booking grammar and official roots match crawlability policy', () => {
  for (const [index, site] of sites.entries()) {
    const id = showing.replace('0700', site)
    const theaterId = theater.replace('0700', site)
    for (const base of [global, local]) {
      for (const suffix of ['', '&ps=ticketingcine']) {
        const url = base.replaceAll('0700', site) + suffix
        parity(url, 'booking', id, theaterId)
        parity(url, 'booking')
        assert.equal(safeBookingUrl(url)?.provider, 'majestic')
      }
    }
    parity(`https://${hosts[index]}/`, 'website', id, theaterId)
    parity(`https://${hosts[(index + 1) % hosts.length]}/`, null, id, theaterId)
  }
})

test('Majestic rejects normalization, extra keys, cross-cinema sessions and unrelated ticketingcine identities', () => {
  // Same frozen raw-byte cases apply to Go/SQL acquisition and persistence policy.
  for (const url of [global, local]) {
    for (const bad of [
      url.replace('https:', 'http:'),
      url.replace('www.ticketingcine.com', 'ticketingcine.com'),
      url.replace('.com', '.com.evil.test'),
      url.replace('.com', '.com:443'),
      url.replace('.com', '.com:444'),
      url.replace('https://', 'https://user@'),
      url.replace('www.', 'WWW.'),
      url.replace('EMS0700', 'EMS1361'),
      url.replace('EMS0700', 'EMS0654'),
      url.replace('emsx0700', 'emsx0654'),
      url.replace('emsx', 'EMSX'),
      url.replace('emsx', 'ems%78'),
      url.replace('EMS0700', 'EMS%300700'),
      url.replace('.com', '.com/x/..'),
      url.replace('.com', '.com/'),
      url.replace('.com', '.com\\'),
      url.replace('#showsession', '?x=1#showsession'),
      url.replace('?id=', '?id=x&id='),
      url.replace('?id=', '?session='),
      url.replace('#showsession', '#showSession'),
      url.slice(0, -1),
      `${url}0`,
      `${url}&x=1`,
      `${url}&ps=ticketingcine&ps=ticketingcine`,
      `${url}&ps=other`,
      `${url}#`,
      `${url}/`,
      `${url}\n`,
      `${url}\r`,
      `${url}\t`,
      `${url}\0`,
      ` ${url}`,
      `${url} `,
    ])
      parity(bad, null)
    for (const [id, theaterId] of [
      [null, theater],
      ['', theater],
      [showing + '\n', theater],
      [showing.replace('00000001', '00000002'), theater],
      [showing, null],
      [showing, ''],
      [showing, theater + '\n'],
      [showing, 'majestic-EMS0654'],
      [showing, 'veo-EMS0700'],
      [showing.toUpperCase(), theater],
    ])
      parity(url, null, id, theaterId)
  }
  for (const bad of [
    global.replace('FRZH1EX', 'FRzh1ex'),
    global.replace('FRZH1EX', 'FRZH1EX0'),
    global.replace('/EMS0700/FRZH1EX', ''),
    local.replace('?EMS0700', '?site=EMS0700'),
    local.replace('?EMS0700', '?EMS0700&EMS0700'),
    local.replace('?EMS0700', '?EMS0700='),
    'https://douai.majestic-cinemas.com/',
    'https://www.majestic-cinemas.com/',
    'https://cinelaon.fr/films',
    'https://cinelaon.fr',
    'https://cinelaon.fr:443/',
    'https://cinelaon.fr/?x=1',
    'https://cinelaon.fr/#',
    'https://www.cinelaon.fr/',
    'https://cinelaon.fr/\n',
  ])
    parity(bad, null)
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
  ] satisfies Provider[])
    for (const url of [global, local, 'https://cinelaon.fr/'])
      assert.equal(safeBookingUrl(url, provider), null)
})

test('Majestic j selections round-trip only complete allowed sessions and preserve existing tokens and share URLs', () => {
  const keys = sites.map((site) => `majestic:${showing.replace('0700', site)}`)
  for (const [index, key] of keys.entries()) {
    const token = `j${session.replace('0700', sites[index]!)}`
    assert.equal(serializeShowtimeSelection([key]), token)
    assert.deepEqual(parseShowtimeSelection(token), [key])
  }
  for (const invalid of [
    session.replace('0700', '1361'),
    session.replace('0700', '0000'),
    session.toUpperCase(),
    session.slice(1),
    session.slice(0, -1),
    `${session}0`,
    `${session}\n`,
    `${session} `,
    session.replace('0', '%30'),
  ]) {
    assert.equal(
      serializeShowtimeSelection([`majestic:majestic-showing-${invalid}`]),
      undefined,
    )
    assert.deepEqual(parseShowtimeSelection(`j${invalid}`), [])
  }
  const all = [...keys, 'ugc:ugc-showing-12', 'mk2:mk2-showing-0004-140350']
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection([...all, ...keys])),
    all.toSorted(),
  )
  const target = buildCompleteSearchShareTarget({
    theaterIds: [theater],
    date: '2027-08-27',
    startAfter: '00:00',
    finishBefore: '23:30',
    language: 'VOSTFR',
    format: 'ICE',
    includeAds: false,
    bufferAds: 15,
    grouping: 'chronological',
    layout: 'boxes',
    selectedShowtimeKeys: keys,
    selectedOnly: true,
  })!
  assert.equal(isValidShortLinkTarget(target), true)
  const query = new URL(target, 'https://messeances.fr').searchParams
  assert.deepEqual(
    parseShowtimeSelection(query.get('selected')),
    keys.toSorted(),
  )
  assert.equal(query.get('selected_only'), '1')
})

test('Majestic result mapping preserves local/global film slugs, real source fields and absent posters', () => {
  for (const film of ['ZH1EX', 'EMS0700-emsx0700HC123']) {
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-10-16T12:00:00Z',
      timezone: 'Europe/Paris',
      date: '2027-08-27',
      theater: {
        provider: 'majestic',
        id: theater,
        slug: theater,
        name: 'Ciné Laon',
        city: 'Laon',
        city_slug: 'laon',
        postal_code: '02000',
        address: '',
        available_dates: ['2027-08-27'],
        accepted_passes: [],
        image: null,
      },
      showtimes: [
        {
          provider: 'majestic',
          id: showing,
          movie: {
            slug: `majestic-film-${film}`,
            title: 'Événement source',
            original_language: null,
            runtime_minutes: 93,
            updated_at: '2026-10-16T12:00:00Z',
          },
          start_time: '2027-08-27T20:00:00+02:00',
          end_time: '2027-08-27T21:43:00+02:00',
          estimated_end_time: null,
          estimated_end_ads_minutes: null,
          language: 'VOSTFR',
          format: 'ICE',
          room: 'ICE',
          booking_url: local,
          start_offset_minutes: 1200,
          duration_minutes: 103,
          poster_url: null,
          backdrop_url: null,
        },
      ],
    }
    const before = structuredClone(response)
    const result = toTheaterShowtimeResults(response)[0]!
    assert.equal(result.movieSlug, `majestic-film-${film}`)
    assert.equal(result.posterUrl, null)
    assert.equal(result.theaterName, 'Ciné Laon')
    assert.equal(result.movieRuntimeMinutes, 93)
    assert.equal(result.room, 'ICE')
    assert.equal(result.language, 'VOSTFR')
    assert.equal(result.bookingUrl, local)
    assert.deepEqual(result.end, {
      time: response.showtimes[0]!.end_time,
      estimated: false,
      adsMinutes: null,
    })
    const selected = parseShowtimeSelection(
      serializeShowtimeSelection([result.key]),
    )
    assert.deepEqual(validShowtimeSelectionKeys([result], selected), [
      result.key,
    ])
    assert.deepEqual(response, before)
  }
})
