import assert from 'node:assert/strict'
import test from 'node:test'
import { posterImageSources, safePosterUrl } from '../app/utils/safeImageUrl.ts'

test('builds the six responsive TMDB poster candidates from a validated canonical source', () => {
  assert.deepEqual(
    posterImageSources('https://image.tmdb.org/t/p/w500/path/poster.jpg'),
    {
      src: 'https://image.tmdb.org/t/p/w500/path/poster.jpg',
      srcset: [
        'https://image.tmdb.org/t/p/w92/path/poster.jpg 92w',
        'https://image.tmdb.org/t/p/w154/path/poster.jpg 154w',
        'https://image.tmdb.org/t/p/w185/path/poster.jpg 185w',
        'https://image.tmdb.org/t/p/w342/path/poster.jpg 342w',
        'https://image.tmdb.org/t/p/w500/path/poster.jpg 500w',
        'https://image.tmdb.org/t/p/w780/path/poster.jpg 780w'
      ].join(', ')
    }
  )
})

test('never synthesizes candidates for absent, unsafe, or non-TMDB poster sources', () => {
  assert.deepEqual(posterImageSources(null), { src: null, srcset: null })
  assert.deepEqual(posterImageSources('https://image.tmdb.org.evil.test/t/p/w500/poster.jpg'), { src: null, srcset: null })
  assert.deepEqual(posterImageSources('https://image.tmdb.org/t/p/w500/%2e%2e/secret.jpg'), { src: null, srcset: null })
  assert.deepEqual(
    posterImageSources('https://www.ugc.fr/posters/film.jpg'),
    { src: 'https://www.ugc.fr/posters/film.jpg', srcset: null }
  )
})

test('accepts canonical Pathé poster URLs on the apex host and subdomains', () => {
  assert.equal(safePosterUrl('https://www.pathe.fr/media/posters/film.webp'), 'https://www.pathe.fr/media/posters/film.webp')
  assert.equal(safePosterUrl('https://pathe.fr/posters/film.jpg'), 'https://pathe.fr/posters/film.jpg')
  assert.equal(safePosterUrl('https://cdn.pathe.fr/a/b/film.jpg'), 'https://cdn.pathe.fr/a/b/film.jpg')
  assert.equal(safePosterUrl('https://WWW.PATHE.FR/media/posters/film.webp'), 'https://www.pathe.fr/media/posters/film.webp')
})

test('rejects noncanonical or unsafe Pathé poster URLs', () => {
  const unsafeUrls = [
    'http://www.pathe.fr/media/posters/film.webp',
    'https://pathe.fr/',
    'https://notpathe.fr/media/posters/film.webp',
    'https://pathe.fr.evil.test/media/posters/film.webp',
    'https://user@www.pathe.fr/media/posters/film.webp',
    'https://www.pathe.fr:8443/media/posters/film.webp',
    'https://www.pathe.fr/media/posters/film.webp?size=large',
    'https://www.pathe.fr/media/posters/film.webp#poster',
    'https://www.pathe.fr/media//posters/film.webp',
    'https://www.pathe.fr/media/../posters/film.webp',
    'https://www.pathe.fr/media/%66ilm.webp',
    'https://www.pathe.fr/media%2F..%2Fsecret.webp',
    'https://www.pathe.fr\\media\\poster.webp'
  ]

  for (const url of unsafeUrls) assert.equal(safePosterUrl(url), null, url)
})

test('accepts canonical CGR poster URLs on acsta.net and rejects unsafe variants', () => {
  assert.equal(safePosterUrl('https://images.acsta.net/pictures/film.jpg'), 'https://images.acsta.net/pictures/film.jpg')
  assert.equal(safePosterUrl('https://ACSTA.NET/posters/film.webp'), 'https://acsta.net/posters/film.webp')

  const unsafeUrls = [
    'http://images.acsta.net/pictures/film.jpg',
    'https://acsta.net/',
    'https://notacsta.net/pictures/film.jpg',
    'https://acsta.net.evil.test/pictures/film.jpg',
    'https://user@images.acsta.net/pictures/film.jpg',
    'https://images.acsta.net:8443/pictures/film.jpg',
    'https://images.acsta.net/pictures/film.jpg?size=large',
    'https://images.acsta.net/pictures/%66ilm.jpg'
  ]

  for (const url of unsafeUrls) assert.equal(safePosterUrl(url), null, url)
})

test('accepts only verified Megarama global and local poster families without inventing responsive sizes', () => {
  for (const path of [
    '/movie_poster/120/FRAB123/AB12CD34.webp',
    '/movie_poster/600/FRAB123/AB12CD34.webp',
    '/ems_spectacle/120/0565/HC123456.jpg',
    '/ems_spectacle/120/0565/HC123456.jpg?ts=1234567890'
  ]) {
    const url = `https://images.monnaie-services.com${path}`
    assert.equal(safePosterUrl(url), url)
    assert.deepEqual(posterImageSources(url), { src: url, srcset: null })
  }
})

test('rejects unsafe Megarama poster hosts, paths, sizes, and queries', () => {
  const origin = 'https://images.monnaie-services.com'
  const global = '/movie_poster/600/FRAB123/AB12CD34.webp'
  const local = '/ems_spectacle/120/0565/HC123456.jpg'
  const urls = [
    `http://images.monnaie-services.com${global}`, `//images.monnaie-services.com${global}`,
    `https://user@images.monnaie-services.com${global}`, `${origin}:443${global}`, `${origin}:8443${global}`,
    `${origin}.evil.test${global}`, `https://other.monnaie-services.com${global}`,
    `https://IMAGES.MONNAIE-SERVICES.COM${global}`, ` ${origin}${global}`, `${origin}${global}\n`,
    `${origin}${global}?ts=123`, `${origin}${global}?`, `${origin}${global}#`,
    `${origin}${global.replace('/600/', '/500/')}`, `${origin}${global.replace('FRAB123', 'AB123')}`,
    `${origin}${global.replace('AB12CD34', 'ab12cd34')}`, `${origin}${global.replace('.webp', '.jpg')}`,
    `${origin}${local.replace('/120/', '/600/')}`, `${origin}${local.replace('0565', '565')}`,
    `${origin}${local}?ts=123&ts=456`, `${origin}${local}?other=123`, `${origin}${local}?ts=1.5`,
    `${origin}${local}?ts=-1`, `${origin}${local}?ts=`, `${origin}${local}?ts=%31`,
    `${origin}${local}?ts=1#poster`, `${origin}${local}/../HC1.jpg`,
    `${origin}/movie_poster/600/FRAB123/../FRAB123/AB12CD34.webp`,
    `${origin}${global.replace('AB12CD34', '%41B12CD34')}`, `${origin}\\${global}`
  ]
  for (const url of urls) assert.equal(safePosterUrl(url), null, url)
  assert.equal(safePosterUrl(null), null)
})
