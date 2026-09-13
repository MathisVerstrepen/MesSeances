import assert from 'node:assert/strict'
import test from 'node:test'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'

test('accepts the captured CGR booking host and route', () => {
  assert.deepEqual(safeBookingUrl('https://achat.cgrcinemas.fr/cgr-abbeville-la-sucrerie/r/123456', 'cgr'), {
    provider: 'cgr',
    kind: 'booking',
    url: 'https://achat.cgrcinemas.fr/cgr-abbeville-la-sucrerie/r/123456'
  })
})

test('rejects unsafe or mismatched CGR booking URLs', () => {
  const invalidUrls = [
    'http://achat.cgrcinemas.fr/cgr-lille/r/123',
    'https://user@achat.cgrcinemas.fr/cgr-lille/r/123',
    'https://achat.cgrcinemas.fr:443/cgr-lille/r/123',
    'https://www.cgrcinemas.fr/cgr-lille/r/123',
    'https://cgrcinemas.fr/cgr-lille/r/123',
    'https://tickets.cgrcinemas.fr/cgr-lille/r/123',
    'https://achat.cgrcinemas.fr.evil.test/cgr-lille/r/123',
    'https://ACHAT.CGRCINEMAS.FR/cgr-lille/r/123',
    'https://achat.cgrcinemas.fr/',
    'https://achat.cgrcinemas.fr//r/123',
    'https://achat.cgrcinemas.fr/cgr-lille//r/123',
    'https://achat.cgrcinemas.fr/cgr-lille/r/0',
    'https://achat.cgrcinemas.fr/cgr-lille/r/0123',
    'https://achat.cgrcinemas.fr/cgr-lille/r/not-numeric',
    'https://achat.cgrcinemas.fr/CGR-Lille/r/123',
    'https://achat.cgrcinemas.fr/cgr_lille/r/123',
    'https://achat.cgrcinemas.fr/cgr-lille/../secret/r/123',
    'https://achat.cgrcinemas.fr/cgr-lille/%2e%2e/r/123',
    'https://achat.cgrcinemas.fr/cgr-lille\\r\\123',
    'https://achat.cgrcinemas.fr/cgr-lille/r/123?source=programme',
    'https://achat.cgrcinemas.fr/cgr-lille/r/123#details'
  ]

  for (const url of invalidUrls) assert.equal(safeBookingUrl(url, 'cgr'), null, url)
  assert.equal(safeBookingUrl('https://achat.cgrcinemas.fr/cgr-lille/r/123', 'ugc'), null)
})

test('accepts only the 34 verified Megarama cinema roots and classifies reservation fragments', () => {
  const hosts = [
    'boulogne.megarama.fr', 'roubaix.megarama.fr', 'nice.megarama.fr', 'villeneuve.megarama.fr',
    'montigny.megarama.fr', 'bordeaux.megarama.fr', 'chalon.megarama.fr', 'montpellier.megarama.fr',
    'besancon.megarama.fr', 'beaux-arts.megarama.fr', 'audincourt.megarama.fr', 'arcueil.megarama.fr',
    'arras.megarama.fr', 'pian.megarama.fr', 'royalpalace-nogent.ticketingcine.com', 'lons.megarama.fr',
    'lons-le-palace.megarama.fr', 'studio66.megarama.fr', 'chambly.megarama.fr', 'garat.megarama.fr',
    'camion-rouge.megarama.fr', 'alhambra.megarama.fr', 'denain.megarama.fr', 'orange.megarama.fr',
    'annecy.megarama.fr', 'pince-vent.megarama.fr', 'givors.megarama.fr', 'dieppe.megarama.fr',
    'louviers.megarama.fr', 'gaillon.megarama.fr', 'cine-armentieres.fr', 'lepalacecambrai.com',
    'les-ulis.megarama.fr', 'cormeilles.megarama.fr'
  ]
  assert.equal(new Set(hosts).size, 34)
  for (const host of hosts) {
    for (const slash of ['', '/']) {
      const root = `https://${host}${slash}`
      assert.deepEqual(safeBookingUrl(root, 'megarama'), { provider: 'megarama', kind: 'website', url: `https://${host}/` })
      const fragment = '#showsession?id=emsx056500123456'
      assert.deepEqual(safeBookingUrl(`${root}${fragment}`, 'megarama'), {
        provider: 'megarama', kind: 'booking', url: `https://${host}/${fragment}`
      })
      for (const provider of ['ugc', 'kinepolis', 'pathe', 'cgr'] as const) assert.equal(safeBookingUrl(root, provider), null)
    }
  }
})

test('rejects unsafe Megarama authorities, paths, fragments and identity lengths before URL normalization', () => {
  const root = 'https://bordeaux.megarama.fr'
  const fragment = '#showsession?id=emsx056500123456'
  const invalid = [
    'https://megarama.fr', 'https://www.megarama.fr', 'https://unknown.megarama.fr',
    'https://other.ticketingcine.com', 'https://www.cine-armentieres.fr',
    `${root}.evil.test`, `${root}:443`, `${root}:8443`, `${root}.`,
    'https://user@bordeaux.megarama.fr', 'https://user:password@bordeaux.megarama.fr',
    'http://bordeaux.megarama.fr', '//bordeaux.megarama.fr', 'javascript:alert(1)',
    'https://BORDEAUX.MEGARAMA.FR', `${root}/foo/..`, `${root}/%2e%2e`, `${root}\\`,
    `${root}//`, `${root}/?x=1`, `${root}?`, `${root}#`, `${root}#other`,
    `${root}${fragment}&other=1`, `${root}${fragment}?other=1`, `${root}${fragment}/`,
    `${root}#showsession?id=`, `${root}#showsession?id=_bad`, `${root}#showsession?id=-bad`,
    `${root}#showsession?id=bad.id`, `${root}#showsession?id=bad%2fid`,
    `${root}#showsession?id=${'a'.repeat(112)}`, ` ${root}`, `${root}\n`,
    'https://bord\neaux.megarama.fr'
  ]
  for (const url of invalid) assert.equal(safeBookingUrl(url, 'megarama'), null, url)
  const maximum = `${root}#showsession?id=${'a'.repeat(111)}`
  assert.equal(safeBookingUrl(maximum, 'megarama')?.kind, 'booking')
  assert.equal(safeBookingUrl(`${root}#showsession?id=Ab_1-2`, 'megarama')?.kind, 'booking')
})

test('accepts exactly five booking-only aliases for their matching session cinema', () => {
  const aliases = [
    ['www.lepalacecambrai.com', '0592'],
    ['www.royalpalacenogent.fr', '0809'],
    ['www.cine-armentieres.fr', '1053'],
    ['jean-jaures.megarama.fr', '1204'],
    ['chavanelle.megarama.fr', '1205']
  ] as const
  for (const [host, cinema] of aliases) {
    for (const slash of ['', '/']) {
      const root = `https://${host}${slash}`
      const fragment = `#showsession?id=emsx${cinema}00123456`
      assert.deepEqual(safeBookingUrl(`${root}${fragment}`, 'megarama'), {
        provider: 'megarama', kind: 'booking', url: `https://${host}/${fragment}`
      })
      assert.equal(safeBookingUrl(`${root}${fragment}`)?.kind, 'booking')
      for (const provider of ['ugc', 'kinepolis', 'pathe', 'cgr'] as const) assert.equal(safeBookingUrl(`${root}${fragment}`, provider), null)
      // Neither an alias root nor an alias with a wrong-cinema fragment is a website fallback.
      assert.equal(safeBookingUrl(root), null)
      for (const [, otherCinema] of aliases) {
        if (otherCinema !== cinema) assert.equal(safeBookingUrl(`${root}#showsession?id=emsx${otherCinema}00123456`), null)
      }
      for (const suffix of ['#', '#showsession', '#showsession?id=', '#showsession?id=emsx056500123456',
        `#showsession?id=emsx${cinema}1234567`, `#showsession?id=emsx${cinema}123456789`,
        `#showsession?id=EMSX${cinema}00123456`, `#showsession?id=emsx${cinema}0012345a`,
        `${fragment}&extra=1`, `${fragment}/`, `${fragment}%0A`, `${fragment}\n`, `?x=1${fragment}`]) {
        assert.equal(safeBookingUrl(`${root}${suffix}`), null, `${host}: ${suffix}`)
      }
    }
    const fragment = `#showsession?id=emsx${cinema}00123456`
    for (const root of [`http://${host}`, `//${host}`, `https://${host}:443`, `https://${host}:8443`,
      `https://user@${host}`, `https://${host}.evil.test`, `https://${host.toUpperCase()}`,
      `https://${host}/x/..`, `https://${host}/%2e%2e`, `https://${host}\\`, `https://${host}//`]) {
      assert.equal(safeBookingUrl(`${root}${fragment}`), null, root)
    }
  }
  assert.equal(safeBookingUrl('https://royalpalacenogent.fr/#showsession?id=emsx080900123456'), null)
  assert.equal(safeBookingUrl('https://www.jean-jaures.megarama.fr/#showsession?id=emsx120400123456'), null)
})
