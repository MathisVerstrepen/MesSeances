import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import {
  CINEWEST_THEATER_HOSTS,
  cinewestTicketShowingId,
  isSafeCinewestPoster,
} from '../app/utils/cinewest.ts'
import { posterImageSources, safePosterUrl } from '../app/utils/safeImageUrl.ts'

test('all thirteen frozen Cinewest roots support website fallback and reject foreign cinema context', () => {
  assert.equal(Object.keys(CINEWEST_THEATER_HOSTS).length, 13)
  assert.equal(
    Object.keys(CINEWEST_THEATER_HOSTS).filter((id) =>
      id.startsWith('cineoffice-'),
    ).length,
    9,
  )
  assert.deepEqual(
    Object.keys(CINEWEST_THEATER_HOSTS).filter((id) =>
      id.startsWith('ticketingcine-'),
    ),
    [
      'ticketingcine-EMS1185',
      'ticketingcine-EMS1317',
      'ticketingcine-EMS0042',
      'ticketingcine-EMS1378',
    ],
  )
  for (const [theater, host] of Object.entries(CINEWEST_THEATER_HOSTS)) {
    for (const url of [`https://${host}`, `https://${host}/`]) {
      const expected = { provider: 'cinewest', url, kind: 'website' }
      assert.deepEqual(
        safeBookingUrl(url, 'cinewest', null, `cinewest-${theater}`),
        expected,
      )
      assert.deepEqual(safeBookingUrl(url), expected)
      assert.equal(
        safeBookingUrl(url, 'cinewest', null, 'cinewest-cineoffice-unknown'),
        null,
      )
      assert.equal(safeBookingUrl(url, 'cgr'), null)
      assert.equal(safeBookingUrl(url, 'megarama'), null)
      assert.equal(
        safeBookingUrl(url, 'cinewest', null, 'cinewest-webediamovies-W8400'),
        null,
      )
    }
    for (const url of [
      `https://${host}/?api_token=x`,
      `https://${host}/?`,
      `https://${host}/#`,
      `https://${host}/vad/shows`,
      `https://${host}:443/`,
      `https://user@${host}/`,
      `http://${host}/`,
      `https://${host}.evil.test/`,
      `https://${host}/%2e/`,
      `https://${host}/a/../`,
      `https://${host}\\`,
      ` https://${host}/`,
      `https://${host}/\n`,
    ])
      assert.equal(safeBookingUrl(url, 'cinewest'), null, url)
  }
})

test('ticketingcine links retain Cinewest and bind exact source session to cinema-scoped SHA-256 identity', () => {
  for (const [theater, host] of Object.entries(CINEWEST_THEATER_HOSTS).filter(
    ([id]) => id.startsWith('ticketingcine-'),
  )) {
    const source = `emsx${theater.slice(-4)}00000001`
    const id = `cinewest-showing-ticketingcine-${createHash('sha256').update(`${theater}\0${source}`).digest('hex')}`
    assert.equal(cinewestTicketShowingId(theater, source), id)
    for (const slash of ['', '/']) {
      const url = `https://${host}${slash}#showsession?id=${source}`
      assert.deepEqual(
        safeBookingUrl(url, 'cinewest', id, `cinewest-${theater}`),
        { provider: 'cinewest', url, kind: 'booking' },
      )
      assert.equal(
        safeBookingUrl(url, 'cinewest')?.kind,
        'booking',
        'admin source without session context',
      )
      assert.equal(
        safeBookingUrl(
          url,
          'cinewest',
          `${id.slice(0, -1)}${id.endsWith('0') ? '1' : '0'}`,
        ),
        null,
      )
      assert.equal(
        safeBookingUrl(url, 'cinewest', id, 'cinewest-webediamovies-W8400'),
        null,
      )
      assert.equal(safeBookingUrl(url, 'megarama', id), null)
      for (const other of Object.keys(CINEWEST_THEATER_HOSTS)) {
        if (other !== theater)
          assert.equal(
            safeBookingUrl(url, 'cinewest', id, `cinewest-${other}`),
            null,
          )
      }
      for (const badId of [
        id + '\n',
        id.replace('ticketingcine', 'webediamovies'),
        id.replace('cinewest-', 'megarama-'),
        cinewestTicketShowingId('ticketingcine-EMS9999', source),
        cinewestTicketShowingId(theater, source.slice(0, -1) + '2'),
      ])
        assert.equal(
          safeBookingUrl(url, 'cinewest', badId, `cinewest-${theater}`),
          null,
          badId,
        )
      for (const bad of [
        url + '&token=x',
        url + '\n',
        url + '\r',
        url + '\t',
        url.replace('https://', 'https://user:password@'),
        url.replace(host, `${host}:443`),
        url.replace(host, `${host}.evil.test`),
        url.replace(source, source.toUpperCase()),
        url.replace(source, source.slice(0, -1)),
        url.replace(source, source + '0'),
        url.replace(source, 'emsx999900000001'),
        url.replace('showsession', '%73howsession'),
        url.replace('#', '?q=x#'),
        url.replace('?id=', '?id=%65'),
        url.replace('/#', '/page#'),
      ]) {
        if (bad !== url)
          assert.equal(safeBookingUrl(bad, 'cinewest', id), null, bad)
      }
    }
  }
})

test('migrated Capitole accepts historical checkout only with full legacy showing identity and current cinema context', () => {
  const url =
    'https://www.capitolestudios-reserver.cotecine.fr/reserver/r/244471'
  const id = `cinewest-showing-webediamovies-${'a'.repeat(64)}`
  const theater = 'cinewest-ticketingcine-EMS1378'
  assert.deepEqual(safeBookingUrl(url, 'cinewest', id, theater), {
    provider: 'cinewest',
    url,
    kind: 'booking',
  })
  assert.deepEqual(safeBookingUrl(url, undefined, id, theater), {
    provider: 'cinewest',
    url,
    kind: 'booking',
  })
  assert.equal(safeBookingUrl(url), null)
  assert.equal(safeBookingUrl(url, 'cinewest'), null)
  assert.equal(safeBookingUrl(url, 'megarama', id, theater), null)
  for (const context of [undefined, null, '', 'cinewest-webediamovies-W8400'])
    assert.equal(safeBookingUrl(url, 'cinewest', id, context), null)
  for (const currentTheater of Object.keys(CINEWEST_THEATER_HOSTS)) {
    if (currentTheater !== 'ticketingcine-EMS1378')
      assert.equal(
        safeBookingUrl(url, 'cinewest', id, `cinewest-${currentTheater}`),
        null,
      )
  }
  for (const showing of [
    undefined,
    null,
    '',
    id + '\n',
    id + '\r',
    id + '\t',
    id.slice(0, -1),
    id + 'a',
    id.toUpperCase(),
    id.replace('cinewest-', 'megarama-'),
    id.replace('webediamovies', 'cineoffice'),
    cinewestTicketShowingId('ticketingcine-EMS1378', 'emsx137800000001'),
  ])
    assert.equal(safeBookingUrl(url, 'cinewest', showing, theater), null)
  assert.equal(
    safeBookingUrl(
      'https://www.capitolestudios.com/#showsession?id=emsx137800000001',
      'cinewest',
      id,
      theater,
    ),
    null,
  )
  for (const bad of [
    url + '?',
    url + '#',
    url + '?api_token=x',
    url + '\n',
    url + '\r',
    url + '\t',
    url + '/',
    url.replace('244471', '0'),
    url.replace('244471', '0244471'),
    url.replace('/r/', '/r/%32'),
    url.replace('.fr/', '.fr:443/'),
    url.replace('https:', 'http:'),
    url.replace('www.', 'user@www.'),
    url.replace('.fr/', '.fr.evil.test/'),
    url + '1'.repeat(4096),
    'https://ws.ticketingcine.com/site',
    'https://cinewest.cineoffice.fr/vad/shows?api_token=x',
  ]) {
    assert.equal(safeBookingUrl(bad, 'cinewest', id, theater), null, bad)
  }
  assert.equal(
    safeBookingUrl('https://nice.megarama.fr/', 'megarama')?.kind,
    'website',
  )
  assert.equal(safeBookingUrl('https://nice.megarama.fr/', 'cinewest'), null)
})

test('Cinewest poster contracts match backend fixtures without resizing provider artwork', () => {
  const posters = [
    'https://cinemedia.cine.digital/medias/1/29/290f220e-8ab4-4ed2-afd5-141fcb6dd229.jpeg',
    'https://images.monnaie-services.com/movie_poster/600/FRABCDE/ABCDEFGH.webp',
    'https://all.web.img.acsta.net/img/6f/af/6faf7d9aa879bd9374e773f31db44956.jpg',
  ]
  for (const poster of posters) {
    assert.equal(isSafeCinewestPoster(poster), true)
    assert.deepEqual(posterImageSources(poster), { src: poster, srcset: null })
    for (const bad of [
      poster + '?',
      poster + '#',
      poster + '?api_token=x',
      poster + '\n',
      poster.replace('https:', 'http:'),
      poster.replace('https://', 'https://user@'),
      poster.replace('.com/', '.com:443/'),
      poster.replace('/medias/', '/%6dedias/'),
      poster.replace('/img/', '/img/../img/'),
    ]) {
      if (bad !== poster) assert.equal(isSafeCinewestPoster(bad), false, bad)
    }
  }
  const office = posters[0]!
  for (const bad of [
    office + '#',
    office + '?',
    office + '\n',
    office.replace('.digital/', '.digital:443/'),
    office.replace('.digital/', '.digital.evil.test/'),
    office.replace('/1/', '/-1/'),
    office.replace('/29/', '/2G/'),
    office.replace('/medias/', '/%6dedias/'),
    office.replace('/medias/', '/a/../medias/'),
    office.replace('.jpeg', '.svg'),
    office.replace('/1/', `/${'1'.repeat(4096)}/`),
  ]) {
    assert.equal(safePosterUrl(bad), null, bad)
  }
})
