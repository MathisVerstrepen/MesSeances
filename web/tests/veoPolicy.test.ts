import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { veoShowingId } from '../app/utils/veo.ts'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
  toTheaterShowtimeResults,
  toSlotShowtimeResults,
  validShowtimeSelectionKeys,
  filterSelectedShowtimeResults,
} from '../app/utils/showtimeResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'
import type {
  Provider,
  TheaterShowtimesResponse,
  SlotResult,
} from '../app/types/api.ts'

const source = await readFile(
  new URL('../tools/verify-crawlability.mjs', import.meta.url),
  'utf8',
)
const start = source.indexOf('function reservationUrl(')
const crawler = runInNewContext(
  `${source.slice(start, source.indexOf('async function get(', start))}; ({ reservationUrl })`,
  { URL, createHash },
)
const checkout = 'https://achat.veocinemas.fr/muret-veo/r/19'
const root = 'https://castelginest.miracinemas.fr'
const session = 'emsx136100000001'
const ticket = `${root}/#showsession?id=${session}`
const sha = (theater: string, id: string) =>
  createHash('sha256').update(`${theater}\0${id}`).digest('hex')
const hash = sha('P1234', 'Source_001')
const showing = `veo-showing-P1234-${hash}`
const castelia = `veo-showing-EMS1361-${sha('EMS1361', session)}`
const compact = Buffer.from(hash, 'hex').toString('base64url')

function parity(
  url: string,
  expected: 'booking' | 'website' | null,
  id?: string | null,
  theater?: string | null,
) {
  const result = safeBookingUrl(url, 'veo', id, theater)
  assert.deepEqual(
    result,
    expected ? { provider: 'veo', url, kind: expected } : null,
    url,
  )
  assert.equal(
    crawler.reservationUrl({
      booking_url: url,
      provider: 'veo',
      id,
      theater_id: theater,
    }),
    expected ? url : null,
    url,
  )
}

test('Véo exact raw checkout and Castelia grammar have crawlability parity and no normalization', () => {
  const prefix = 'https://achat.veocinemas.fr/muret/r/'
  for (const url of [
    checkout,
    'https://achat.veocinemas.fr/0/r/1',
    prefix + '1'.repeat(4096 - prefix.length),
  ])
    parity(url, 'booking')
  for (const url of [root, `${root}/`])
    parity(url, 'website', castelia, 'veo-EMS1361')
  for (const url of [ticket, ticket.replace('/#', '#')])
    parity(url, 'booking', castelia, 'veo-EMS1361')
  const invalid = [
    checkout.replace('https:', 'http:'),
    checkout.replace('achat.', 'evil.achat.'),
    checkout.replace('.fr/', '.fr.evil.test/'),
    checkout.replace('https://', 'https://user@'),
    checkout.replace('.fr/', '.fr:443/'),
    checkout.replace('.fr/', '.fr:444/'),
    checkout.replace('muret-veo', 'Muret-veo'),
    checkout.replace('muret-veo', 'muret--veo'),
    checkout.replace('muret-veo', '-muret'),
    checkout.replace('muret-veo', 'muret-'),
    checkout.replace('muret-veo', 'muret_veo'),
    checkout.replace('/19', '/0'),
    checkout.replace('/19', '/01'),
    checkout.replace('/19', '/-1'),
    checkout.replace('/19', '/1.5'),
    checkout.replace('/r/', '/reserver/reserver/r/'),
    checkout.replace('/r/', '/x/../r/'),
    checkout.replace('/r/', '/%72/'),
    checkout.replace('/19', '/%31'),
    `${checkout}/`,
    `${checkout}?x=1`,
    `${checkout}#x`,
    `${checkout}\n`,
    ` ${checkout}`,
    `${checkout} `,
    `${checkout}\t`,
    `${checkout}\0`,
    checkout.replace('muret', 'murét'),
    prefix + '1'.repeat(4097 - prefix.length),
    root.replace('castelginest.', 'www.castelginest.'),
    `${root}:443/`,
    `${root}/other`,
    `${root}?x=1`,
    `${root}/#showsession?id=EMSX136100000001`,
    ticket.replace('1361', '1362'),
    `${ticket}0`,
    ticket.slice(0, -1),
    `${ticket}&x=1`,
    `${ticket}#other`,
    `${ticket}\n`,
    ticket.replace('emsx', 'ems%78'),
    ticket.replace('/#', '/%23'),
    ticket.replace('https:', 'http:'),
    ticket.replace('.fr/', '.fr.evil.test/'),
  ]
  for (const url of invalid) parity(url, null)
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
  ] satisfies Provider[]) {
    for (const url of [checkout, ticket, root])
      assert.equal(safeBookingUrl(url, provider), null)
  }
  for (const url of [checkout, ticket, root])
    assert.equal(safeBookingUrl(url)?.provider, 'veo')
})

test('Véo source aliases preserve both affected cinema URLs with strict tool parity', () => {
  for (const [theater, slug, sessionId] of [
    ['P0448', 'chateaurenard', '22309'],
    ['W3187', 'labarthe-sur-leze', '5093'],
  ]) {
    const id = veoShowingId(theater!, 'opaque-session')
    for (const segment of ['', 'reserver/']) {
      const url = `https://achat.veocinemas.fr/${slug}/${segment}r/${sessionId}`
      parity(url, 'booking', id, `veo-${theater}`)
      parity(url, null, id, 'veo-W0067')
      parity(url, null, castelia, 'veo-EMS1361')
      for (const bad of [
        `${url}?`,
        `${url}?x=1`,
        `${url}#`,
        `${url}/`,
        `${url}/extra`,
        `${url}\n`,
        `${url}\r`,
        ` ${url}`,
        `${url}\0`,
        url.replace('https:', 'http:'),
        url.replace('.fr/', '.fr:443/'),
        url.replace('https://', 'https://user@'),
        url.replace('achat.', 'evil.achat.'),
        url.replace(`/${sessionId}`, '/0'),
        url.replace(`/${sessionId}`, '/01'),
        url.replace('/r/', '/R/'),
        url.replace('/r/', '/%72/'),
        url.replace(`/${slug}/`, `/${slug}/../${slug}/`),
        url.replace(`/${slug}/`, `/${slug}/reserver/reserver/`),
        url.replace(`/${slug}/`, `/${slug}/Reserver/`),
        url.replace(`/${slug}/`, `/${slug}/reserver//`),
        url.replace(`/${slug}/`, `/${slug}/%72eserver/`),
        url.replace(`/${slug}/`, `/${slug}/reserver/F1/D1792065000/VF/`),
        url + '1'.repeat(4097),
      ])
        parity(bad, null, id, `veo-${theater}`)
    }
  }
})

test('Véo qualified identity and exact Castelia SHA-256 binding fail closed when supplied', () => {
  assert.equal(veoShowingId('EMS1361', session), castelia)
  assert.equal(veoShowingId('P1234', 'Source_001'), showing)
  assert.notEqual(veoShowingId('P1234', 'source_001'), showing)
  parity(checkout, 'booking', showing, 'veo-P1234')
  parity(ticket, 'booking', castelia, 'veo-EMS1361')
  for (const id of [
    null,
    '',
    `${castelia}\n`,
    castelia.replace('EMS1361', 'EMS1362'),
    castelia.replace('veo-', 'cinewest-'),
    veoShowingId('EMS1361', 'emsx136100000002'),
  ])
    parity(ticket, null, id, 'veo-EMS1361')
  for (const id of [
    null,
    '',
    showing.toUpperCase(),
    `${showing} `,
    showing.replace('P1234', 'p1234'),
    showing.replace(hash, hash.slice(1)),
  ])
    parity(checkout, null, id, 'veo-P1234')
  for (const theater of [
    null,
    '',
    'P1234',
    'veo-p1234',
    'veo-P1234\n',
    'veo-P5678',
    'noecinemas-P1234',
  ])
    parity(checkout, null, showing, theater)
  parity(checkout, null, castelia, 'veo-EMS1361')
  parity(ticket, null, showing, 'veo-P1234')
  parity(root, null, showing, 'veo-P1234')
})

test('Guéret relay single ASCII decoding has strict frontend/tool parity and canonical scope', () => {
  const raw =
    'https://relay.mvtx.us/ticketing/dbz?code_theater=P1394&gid_entity=fixture_123.opaque&tags=Localization.Language.French,Format.Projection.Digital&perfd=20261016&perft=15:00'
  const id = veoShowingId('G06DB', 'synthetic')
  for (const value of [
    raw,
    raw.replaceAll(',', '%2C').replaceAll(':00', '%3a00'),
    raw.replace('20261016', '20000229'),
    raw.replace('fixture_123.opaque', '%41'.repeat(1024)),
  ]) {
    parity(value, 'booking', id, 'veo-G06DB')
    parity(value, null)
    for (const theater of ['P1394', 'P1028', 'W0067', 'EMS1361'])
      parity(value, null, veoShowingId(theater, 'synthetic'), `veo-${theater}`)
  }
  const bad = [
    raw + '&tags=x',
    raw + '&x=1',
    raw + '&',
    raw + '#',
    raw + '\n',
    raw + ' ',
    raw.replace('code_theater', 'code%5ftheater'),
    raw.replace('P1394', 'G06DB'),
    ...['20260229', '19000229', '00001016'].map((day) =>
      raw.replace('20261016', day),
    ),
    raw.replace('15:00', '24:00'),
    raw.replace('15:00', '15:60'),
    raw.replace('https:', 'http:'),
    raw.replace('relay.mvtx.us', 'relay.mvtx.us.evil.test'),
    raw.replace('relay.mvtx.us', 'relay.mvtx.us:443'),
    raw.replace('https://', 'https://user@'),
    raw.replace('/ticketing/dbz', '/ticketing/%64bz'),
    raw.replace('/ticketing/dbz', '/x/../ticketing/dbz'),
    'https://achat.cinema-senechal.com/r/1',
  ]
  for (const value of [
    '',
    '%',
    '%GG',
    '%252C',
    '%26x%3D1',
    '%3B',
    '%23',
    '%3F',
    '%2F',
    '%5C',
    '%00',
    '%0A',
    '+',
    '%20',
    'é',
    '%C3%A9',
    'a=b',
    'a'.repeat(1025),
  ])
    bad.push(raw.replace('fixture_123.opaque', value))
  for (const value of bad) parity(value, null, id, 'veo-G06DB')
})

test('Véo e tokens are full-string canonical cinema-qualified SHA-256 encodings, preserving share reload and other providers', () => {
  const keys = ['P1234', 'EMS1361', '00000'].map(
    (theater) => `veo:veo-showing-${theater}-${hash}`,
  )
  for (const [index, theater] of ['P1234', 'EMS1361', '00000'].entries()) {
    assert.equal(
      serializeShowtimeSelection([keys[index]!]),
      `e${theater}-${compact}`,
    )
    assert.deepEqual(parseShowtimeSelection(`e${theater}-${compact}`), [
      keys[index],
    ])
  }
  const all = [
    ...keys,
    `noecinemas:noecinemas-showing-P1234-${hash}`,
    `grandecran:grandecran-showing-P1234-${hash}`,
    `cgr:cgr-showing-P1234-${hash}`,
    'ugc:ugc-showing-12',
    'mk2:mk2-showing-0004-140350',
  ]
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection([...all, ...keys])),
    all.toSorted(),
  )
  for (const theater of [
    'p1234',
    'P123',
    'P12345',
    'EMS1362',
    'ems1361',
    'P12_4',
    'P12é4',
  ]) {
    assert.equal(
      serializeShowtimeSelection([`veo:veo-showing-${theater}-${hash}`]),
      undefined,
    )
    assert.deepEqual(parseShowtimeSelection(`e${theater}-${compact}`), [])
  }
  const alphabet =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
  const noncanonical =
    compact.slice(0, -1) + alphabet[alphabet.indexOf(compact.at(-1)!) + 1]
  for (const token of [
    `eP1234-${compact}=`,
    `eP1234-${noncanonical}`,
    `eP1234-${compact}\n`,
    `eP1234-${compact} `,
    `eP1234-${compact.slice(1)}`,
  ])
    assert.deepEqual(parseShowtimeSelection(token), [], token)
  for (const value of [
    hash.toUpperCase(),
    `${hash}\n`,
    hash.slice(1),
    `${hash}0`,
  ])
    assert.equal(
      serializeShowtimeSelection([`veo:veo-showing-P1234-${value}`]),
      undefined,
    )
  const theaterIds = ['veo-P1234', 'veo-EMS1361']
  const target = buildCompleteSearchShareTarget({
    theaterIds,
    date: '2027-07-01',
    startAfter: '00:00',
    finishBefore: '23:30',
    language: 'VOSTFR',
    format: '2D',
    includeAds: false,
    bufferAds: 15,
    grouping: 'chronological',
    layout: 'boxes',
    selectedShowtimeKeys: keys.slice(0, 2),
    selectedOnly: true,
  })
  const shared = withSharedTheaterSelection(target, theaterIds)!
  assert.equal(isValidShortLinkTarget(shared), true)
  const query = new URL(shared, 'https://messeances.fr').searchParams
  assert.equal(query.get('shared_theaters'), theaterIds.join(','))
  assert.equal(query.get('selected_only'), '1')
  assert.deepEqual(
    parseShowtimeSelection(query.get('selected')!),
    keys.slice(0, 2).toSorted(),
  )
})

test('Véo results preserve opaque source slugs, VOSTFR, absent rooms and unknown versus explicitly estimated ends', () => {
  for (const theater of ['P1234', 'EMS1361']) {
    const start = '2027-07-01T00:15:00+02:00'
    const end = '2027-07-01T02:03:00+02:00'
    const id = theater === 'EMS1361' ? castelia : showing
    const movieSlug = `veo-film-${theater === 'EMS1361' ? 'ticketingcine-' : ''}Source_001`
    const response: TheaterShowtimesResponse = {
      generated_at: '2026-10-15T12:00:00Z',
      timezone: 'Europe/Paris',
      date: '2027-06-30',
      theater: {
        provider: 'veo',
        id: `veo-${theater}`,
        slug: `veo-${theater}`,
        name: 'Véo Cinémas',
        city: 'Muret',
        city_slug: 'muret',
        postal_code: '31600',
        address: '',
        available_dates: ['2027-06-30'],
        image: null,
        accepted_passes: [],
      },
      showtimes: [
        {
          provider: 'veo',
          id,
          movie: {
            slug: movieSlug,
            title: 'Événement Véo',
            original_language: null,
            runtime_minutes: 0,
            updated_at: start,
          },
          start_time: start,
          end_time: start,
          estimated_end_time: null,
          estimated_end_ads_minutes: null,
          language: 'VOSTFR',
          format: '2D',
          room: '',
          booking_url: theater === 'EMS1361' ? ticket : checkout,
          start_offset_minutes: 1275,
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
      assert.equal(result.key, `veo:${id}`)
      assert.equal(result.movieSlug, movieSlug)
      assert.equal(result.end, null)
      assert.equal(result.room, '')
      assert.equal(result.language, 'VOSTFR')
      const selected = parseShowtimeSelection(
        serializeShowtimeSelection([result.key]),
      )
      assert.deepEqual(validShowtimeSelectionKeys([result], selected), [
        result.key,
      ])
      assert.deepEqual(filterSelectedShowtimeResults([result], selected), [
        result,
      ])
    }
    assert.deepEqual({ response, slot }, before)
    response.showtimes[0]!.movie.runtime_minutes = 93
    assert.equal(toTheaterShowtimeResults(response)[0]!.end, null)
    response.showtimes[0]!.estimated_end_time = end
    response.showtimes[0]!.estimated_end_ads_minutes = 15
    response.showtimes[0]!.room = 'Salle 2'
    assert.deepEqual(toTheaterShowtimeResults(response)[0]!.end, {
      time: end,
      estimated: true,
      adsMinutes: 15,
    })
    assert.equal(toTheaterShowtimeResults(response)[0]!.room, 'Salle 2')
    assert.equal(response.showtimes[0]!.end_time, start)
    response.showtimes[0]!.end_time = end
    assert.equal(toTheaterShowtimeResults(response)[0]!.end?.estimated, false)
  }
})
