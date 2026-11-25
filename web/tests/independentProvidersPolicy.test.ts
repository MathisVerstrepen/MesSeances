import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
} from '../app/utils/showtimeResults.ts'

const source = await readFile(
  new URL('../tools/verify-crawlability.mjs', import.meta.url),
  'utf8',
)
const start = source.indexOf('function reservationUrl(')
const crawler = runInNewContext(
  `${source.slice(start, source.indexOf('async function get(', start))}; ({ reservationUrl })`,
  { URL, createHash },
)

const cases = {
  scare: {
    showing: 'scare-showing-1854-12560',
    theater: 'scare-1854',
    url: 'https://latoiledemer.fr/film.html#/media/1582?showId=12560',
    otherTheater: 'scare-1855',
  },
  webedia: {
    showing: 'webedia-showing-P5385-0123456789abcdef0123456789abcdef',
    theater: 'webedia-P5385',
    url: 'https://tickets.allocine.fr/cinepolesud/reserver/r/376105',
    otherTheater: 'webedia-W0001',
  },
  cotecine: {
    showing: 'cotecine-showing-1824-628177-1796758200',
    theater: 'cotecine-1824',
    url: 'https://www.cinema-confluences.com/varennes/reserver/F628177/D1796758200/VF/188300/',
    otherTheater: 'cotecine-4464',
  },
} as const

function parity(
  provider: keyof typeof cases,
  url: string,
  expected: boolean,
  id?: string | null,
  theaterId?: string | null,
) {
  assert.deepEqual(
    safeBookingUrl(url, provider, id, theaterId),
    expected ? { provider, url, kind: 'booking' } : null,
    url,
  )
  assert.equal(
    crawler.reservationUrl({
      booking_url: url,
      provider,
      id,
      theater_id: theaterId,
    }),
    expected ? url : null,
    url,
  )
}

test('SCARE, Webedia and Cotecine accept canonical public HTTPS links bound to the cinema', () => {
  for (const provider of ['scare', 'webedia', 'cotecine'] as const) {
    const c = cases[provider]
    parity(provider, c.url, true, c.showing, c.theater)
    parity(provider, c.url, true)
    for (const bad of [
      c.url.replace('https:', 'http:'),
      c.url.replace('https://', 'https://user@'),
      c.url.replace(/^https:\/\/[^/]+/, 'https://127.0.0.1'),
      c.url.replace(/^https:\/\/[^/]+/, (h) => `${h}:8443`),
      c.url.replace(/^https:\/\/[a-z]/, (h) => h.toUpperCase()),
      `${c.url} `,
      `${c.url}\n`,
      c.url.replace('/', '\\'),
      'javascript:alert(1)',
    ])
      parity(provider, bad, false, c.showing, c.theater)
    parity(provider, c.url, false, c.showing, c.otherTheater)
    parity(provider, c.url, false, null, c.theater)
    parity(provider, c.url, false, `${c.showing}x!`, c.theater)
  }
})

test('Independent links are never inferred from their host', () => {
  for (const c of Object.values(cases)) {
    assert.equal(safeBookingUrl(c.url), null)
    assert.equal(safeBookingUrl(c.url, 'ugc'), null)
  }
  assert.equal(
    safeBookingUrl(cases.scare.url, 'webedia', cases.scare.showing),
    null,
  )
})

test('SCARE s, Webedia b and Cotecine a selections round-trip only complete showings', () => {
  for (const [provider, token] of [
    ['scare', 's1854-12560'],
    ['webedia', 'bP5385-0123456789abcdef0123456789abcdef'],
    ['cotecine', 'a1824-628177-1796758200'],
  ] as const) {
    const key = `${provider}:${cases[provider].showing}`
    assert.equal(serializeShowtimeSelection([key]), token)
    assert.deepEqual(parseShowtimeSelection(token), [key])
    assert.deepEqual(parseShowtimeSelection(`${token}!`), [])
    assert.equal(serializeShowtimeSelection([`${key}!`]), undefined)
  }
  assert.deepEqual(parseShowtimeSelection('s0854-1'), [])
  assert.deepEqual(parseShowtimeSelection('bp5385-00'), [])
  // Repertory films are scoped to their cinema by slug.
  const repertory =
    'cotecine:cotecine-showing-4013-4013-lady-bird-0000-1795698000'
  assert.equal(
    serializeShowtimeSelection([repertory]),
    'a4013-4013-lady-bird-0000-1795698000',
  )
  assert.deepEqual(
    parseShowtimeSelection('a4013-4013-lady-bird-0000-1795698000'),
    [repertory],
  )
  for (const invalid of [
    'a0-628177-1',
    'a1824-628177',
    'a1824-Lady-1',
    'a1824-628177-01',
  ])
    assert.deepEqual(parseShowtimeSelection(invalid), [])
})
