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
import type { Provider } from '../app/types/api.ts'

const source = await readFile(
  new URL('../tools/verify-crawlability.mjs', import.meta.url),
  'utf8',
)
const start = source.indexOf('function reservationUrl(')
const crawler = runInNewContext(
  `${source.slice(start, source.indexOf('async function get(', start))}; ({ reservationUrl })`,
  { URL, createHash },
)
const session = 'emsx096300171445'
const showing = `ticketingcine-showing-${session}`
const theater = 'ticketingcine-EMS0963'
const global = `https://www.ticketingcine.com/EMS0963/FROCSMD#showsession?id=${session}`
const local = `https://www.ticketingcine.com?EMS0963#showsession?id=${session}`

function parity(
  url: string,
  expected: boolean,
  id?: string | null,
  theaterId?: string | null,
) {
  assert.deepEqual(
    safeBookingUrl(url, 'ticketingcine', id, theaterId),
    expected ? { provider: 'ticketingcine', url, kind: 'booking' } : null,
    url,
  )
  assert.equal(
    crawler.reservationUrl({
      booking_url: url,
      provider: 'ticketingcine',
      id,
      theater_id: theaterId,
    }),
    expected ? url : null,
    url,
  )
}

test('TicketingCiné accepts exact session routes bound to any configured site and session', () => {
  for (const base of [global, local]) {
    for (const suffix of ['', '&ps=ticketingcine']) {
      const url = base + suffix
      parity(url, true, showing, theater)
      parity(url, true)
      assert.equal(safeBookingUrl(url)?.provider, 'ticketingcine')
      const other = url.replaceAll('0963', '1262')
      parity(
        other,
        true,
        showing.replace('0963', '1262'),
        'ticketingcine-EMS1262',
      )
    }
  }
})

test('TicketingCiné rejects normalization, foreign sites and mismatched identities', () => {
  for (const url of [global, local]) {
    for (const bad of [
      url.replace('https:', 'http:'),
      url.replace('www.ticketingcine.com', 'ticketingcine.com'),
      url.replace('.com', '.com.evil.test'),
      url.replace('.com', '.com:443'),
      url.replace('https://', 'https://user@'),
      url.replace('www.', 'WWW.'),
      url.replace('EMS0963', 'EMS1262'),
      url.replace('emsx0963', 'emsx1262'),
      url.replace('emsx', 'EMSX'),
      url.replace('emsx', 'ems%78'),
      url.replace('.com', '.com/'),
      url.replace('#showsession', '?x=1#showsession'),
      url.replace('#showsession', '#showSession'),
      url.slice(0, -1),
      `${url}0`,
      `${url}&ps=erakys`,
      `${url}&ps=ticketingcine&ps=ticketingcine`,
      `${url}#`,
      `${url}\n`,
      ` ${url}`,
    ])
      parity(bad, false)
    for (const [id, theaterId] of [
      [null, theater],
      ['', theater],
      [showing.replace('00171445', '00171446'), theater],
      [showing, null],
      [showing, ''],
      [showing, 'ticketingcine-EMS1262'],
      [showing, 'majestic-EMS0963'],
      [showing.toUpperCase(), theater],
      [`majestic-showing-${session}`, theater],
    ])
      parity(url, false, id, theaterId)
  }
  for (const bad of [
    global.replace('FROCSMD', 'FRocsmd'),
    global.replace('FROCSMD', 'FROCSMD0'),
    local.replace('?EMS0963', '?site=EMS0963'),
    local.replace('?EMS0963', '?EMS0963='),
    'https://www.amphi-vienne.fr/',
  ])
    parity(bad, false)
  for (const provider of [
    'ugc',
    'megarama',
    'majestic',
    'veo',
    'cinewest',
  ] satisfies Provider[])
    for (const url of [global, local])
      assert.equal(safeBookingUrl(url, provider), null)
})

test('Majestic sites keep resolving to Majestic before generic TicketingCiné', () => {
  const majestic =
    'https://www.ticketingcine.com?EMS0700#showsession?id=emsx070000000001'
  assert.equal(safeBookingUrl(majestic)?.provider, 'majestic')
  assert.deepEqual(safeBookingUrl(majestic, 'ticketingcine'), {
    provider: 'ticketingcine',
    url: majestic,
    kind: 'booking',
  })
})

test('TicketingCiné t selections round-trip only complete sessions', () => {
  const key = `ticketingcine:${showing}`
  assert.equal(serializeShowtimeSelection([key]), `t${session}`)
  assert.deepEqual(parseShowtimeSelection(`t${session}`), [key])
  for (const invalid of [
    session.toUpperCase(),
    session.slice(1),
    session.slice(0, -1),
    `${session}0`,
    `${session}\n`,
    session.replace('0', '%30'),
  ]) {
    assert.equal(
      serializeShowtimeSelection([
        `ticketingcine:ticketingcine-showing-${invalid}`,
      ]),
      undefined,
    )
    assert.deepEqual(parseShowtimeSelection(`t${invalid}`), [])
  }
  const all = [
    key,
    'majestic:majestic-showing-emsx070000000001',
    'ugc:ugc-showing-12',
  ]
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection(all)),
    all.toSorted(),
  )
})
