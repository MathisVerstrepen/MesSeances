import assert from 'node:assert/strict'
import test from 'node:test'
import { safePosterUrl } from '../app/utils/safeImageUrl.ts'

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
