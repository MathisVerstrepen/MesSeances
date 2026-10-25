import assert from 'node:assert/strict'
import test from 'node:test'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { cinemovidaBookingHosts } from '../app/utils/cinemovida.ts'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
  toTheaterShowtimeResults,
} from '../app/utils/showtimeResults.ts'
import { cinemovidaTheaters, cinemovidaShowtimes } from '../e2e/data.mjs'

const hash = 'ab'.repeat(32)
test('Ciné-Movida original booking URLs bind all four hosts to venue and hashed showing', () => {
  const routes = Object.entries(cinemovidaBookingHosts).flatMap(
    ([theater, host]) =>
      (theater === 'P0876' ? ['/reserver/r/', '/r/'] : ['/reserver/r/']).map(
        (route) => ({ theater, host, url: `https://${host}${route}123` }),
      ),
  )
  for (const { theater, host, url } of routes) {
    const showing = `cinemovida-showing-${theater}-${hash}`
    const expected = { provider: 'cinemovida', url, kind: 'booking' }
    assert.deepEqual(
      safeBookingUrl(url, 'cinemovida', showing, `cinemovida-${theater}`),
      expected,
    )
    assert.deepEqual(safeBookingUrl(url), expected)
    assert.equal(
      safeBookingUrl(url, 'veo', showing, `cinemovida-${theater}`),
      null,
    )
    for (const [id, venue] of [
      [null, `cinemovida-${theater}`],
      [showing, null],
      [
        `cinemovida-showing-${theater}-${hash.toUpperCase()}`,
        `cinemovida-${theater}`,
      ],
      [showing + '\n', `cinemovida-${theater}`],
      [`veo-showing-${theater}-${hash}`, `cinemovida-${theater}`],
      [showing, 'cinemovida-P9999'],
      ...Object.keys(cinemovidaBookingHosts)
        .filter((id) => id !== theater)
        .flatMap((other) => [
          [showing, `cinemovida-${other}`],
          [`cinemovida-showing-${other}-${hash}`, `cinemovida-${theater}`],
        ]),
    ])
      assert.equal(safeBookingUrl(url, 'cinemovida', id, venue), null)
    for (const raw of [
      ` ${url}`,
      `${url} `,
      `${url}\n`,
      `${url}\r\n`,
      `${url}?`,
      `${url}#`,
      `${url}/`,
      url.replace('https:', 'http:'),
      url.replace(host, host.toUpperCase()),
      url.replace(host, `${host}:443`),
      url.replace(host, `${host}.`),
      url.replace(host, `${host}.evil.test`),
      url.replace(host, `user@${host}`),
      url.replace('/r/123', '/r/0'),
      url.replace('/r/123', '/r/0123'),
      url.replace('/r/123', '/r/-1'),
      url.replace('/r/123', '/r/1.5'),
      url.replace('/r/123', '/r/abc'),
      url.replace('/r/123', '/r/%31'),
      url.replace('/r/123', '/r/1/../123'),
      url.replace('/r/123', '/r/1%2f123'),
      url.replace('/r/123', '/r/1\\123'),
      url.replace('/r/123', '/F1/D1792188900/VF/1/'),
      'https://relay.mvtx.us/a',
      'https://www.cine-movida.com/',
    ]) {
      assert.equal(
        safeBookingUrl(raw, 'cinemovida', showing, `cinemovida-${theater}`),
        null,
        raw,
      )
      assert.equal(safeBookingUrl(raw), null, raw)
    }
  }
})

test('Ciné-Movida short /r routes remain exclusive to Concorde/P0876', () => {
  for (const [theater, host] of Object.entries(cinemovidaBookingHosts)) {
    if (theater === 'P0876') continue
    const url = `https://${host}/r/123`
    assert.equal(safeBookingUrl(url), null)
    assert.equal(
      safeBookingUrl(
        url,
        'cinemovida',
        `cinemovida-showing-${theater}-${hash}`,
        `cinemovida-${theater}`,
      ),
      null,
    )
    assert.equal(
      safeBookingUrl(
        url,
        'cinemovida',
        `cinemovida-showing-P0876-${hash}`,
        'cinemovida-P0876',
      ),
      null,
    )
  }
})

test('Ciné-Movida d tokens roundtrip mixed shares and reject noncanonical/trailing bytes', () => {
  const keys = Object.keys(cinemovidaBookingHosts).map(
    (id) => `cinemovida:cinemovida-showing-${id}-${hash}`,
  )
  const mixed = [
    ...keys,
    'ugc:ugc-showing-12',
    `veo:veo-showing-P1234-${hash}`,
    'cineode:cineode-showing-991-atelier-local-1792188900',
  ]
  const encoded = serializeShowtimeSelection(mixed)!
  assert.deepEqual(parseShowtimeSelection(encoded), mixed.sort())
  assert.deepEqual(
    parseShowtimeSelection(`${encoded},${encoded}`),
    mixed.sort(),
  )
  const token = `dP0176-${Buffer.from(hash, 'hex').toString('base64url')}`
  assert.equal(serializeShowtimeSelection([keys[0]!]), token)
  for (const value of [
    token + '\n',
    token + 'A',
    token + '=',
    token.replace('P0176', 'P9999'),
    token.replace('P0176', 'p0176'),
    token.slice(0, -1) + 't',
  ])
    assert.deepEqual(parseShowtimeSelection(value), [], value)
  for (const key of [
    keys[0] + '\n',
    keys[0]!.replace(hash, hash.toUpperCase()),
    keys[0]!.replace('P0176', 'P9999'),
    keys[0]!.replace('cinemovida:', 'veo:'),
  ])
    assert.equal(serializeShowtimeSelection([key]), undefined)
})

test('Ciné-Movida fixture mapping preserves canonical ends over estimates, explicit estimates and unknown metadata', () => {
  const selectedDate = '2026-07-15'
  const result = toTheaterShowtimeResults(
    cinemovidaShowtimes(cinemovidaTheaters[0], selectedDate),
  )
  assert.deepEqual(
    result.map((item) => item.end),
    [
      {
        time: `${selectedDate}T15:07:00+02:00`,
        estimated: false,
        adsMinutes: null,
      },
      {
        time: `${selectedDate}T17:55:00+02:00`,
        estimated: true,
        adsMinutes: 15,
      },
      null,
    ],
  )
  assert.deepEqual(
    result.map((item) => [item.format, item.room, item.language]),
    [
      ['4DX', 'Salle 10 PREMIUM MX4D', 'VOSTFR'],
      ['2D', 'Salle 6 PREMIUM ICE', 'VFSTF'],
      ['2D', 'CASTILLET 2', 'VF'],
    ],
  )
  assert.equal(result[2]!.movieRuntimeMinutes, 0)
  assert.equal(result[2]!.posterUrl, null)
})

for (const [selectedDate, offset] of [
  ['2026-01-15', '+01:00'],
  ['2026-07-15', '+02:00'],
  ['2026-03-28', '+01:00'],
  ['2026-03-29', '+02:00'],
  ['2026-10-24', '+02:00'],
  ['2026-10-25', '+01:00'],
]) {
  test(`Ciné-Movida fixtures preserve Paris wall-clock times on ${selectedDate}`, () => {
    const payload = cinemovidaShowtimes(cinemovidaTheaters[0], selectedDate)
    assert.deepEqual(
      payload.showtimes.map((session) => session.start_time),
      [14, 16, 18].map((hour) => `${selectedDate}T${hour}:00:00${offset}`),
    )
    const result = toTheaterShowtimeResults(payload)
    assert.deepEqual(
      result.map((item) => item.end),
      [
        {
          time: `${selectedDate}T15:07:00${offset}`,
          estimated: false,
          adsMinutes: null,
        },
        {
          time: `${selectedDate}T17:55:00${offset}`,
          estimated: true,
          adsMinutes: 15,
        },
        null,
      ],
    )
    const localTime = new Intl.DateTimeFormat('fr-FR', {
      timeZone: 'Europe/Paris',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    })
    assert.equal(localTime.format(new Date(result[0]!.end!.time)), '15:07')
    assert.equal(localTime.format(new Date(result[1]!.end!.time)), '17:55')
  })
}
