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
