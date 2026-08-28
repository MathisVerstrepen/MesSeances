import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { validatedYouTubeKey, youtubeNoCookieTrailerEmbedUrl } from '../app/utils/youtubeTrailer.ts'

test('accepts only canonical 11-character YouTube video keys', () => {
  assert.equal(validatedYouTubeKey('AbC-12_xYz9'), 'AbC-12_xYz9')

  const invalidKeys = [
    null,
    undefined,
    '',
    'AbC-12_xYz',
    'AbC-12_xYz90',
    ' AbC-12_xYz9',
    'AbC-12_xYz9 ',
    'AbC/12_xYz9',
    'AbC?12_xYz9',
    'https://youtu.be/AbC-12_xYz9'
  ]

  for (const key of invalidKeys) assert.equal(validatedYouTubeKey(key), null, String(key))
})

test('constructs only privacy-enhanced autoplay embed URLs from valid keys', () => {
  assert.equal(
    youtubeNoCookieTrailerEmbedUrl('AbC-12_xYz9'),
    'https://www.youtube-nocookie.com/embed/AbC-12_xYz9?autoplay=1'
  )
  assert.equal(youtubeNoCookieTrailerEmbedUrl('https://youtube.com/watch?v=AbC-12_xYz9'), null)
  assert.equal(youtubeNoCookieTrailerEmbedUrl('../AbC-12_xYz9'), null)
})

test('trailer modal mounts playback only on demand and exposes accessible close paths', async () => {
  const component = await readFile(new URL('../app/components/MovieTrailer.vue', import.meta.url), 'utf8')

  assert.match(component, /v-if="youtubeKey"/u)
  assert.match(component, /const embedUrl = computed\(\(\) => isOpen\.value \? youtubeNoCookieTrailerEmbedUrl/u)
  assert.match(component, /v-if="isOpen && embedUrl"/u)
  assert.match(component, /dialog\.value\.showModal\(\)/u)
  assert.match(component, /@cancel\.prevent="closeModal\(\)"/u)
  assert.match(component, /@click\.self="closeModal\(\)"/u)
  assert.match(component, /closeButton\.value\?\.focus/u)
  assert.match(component, /trigger\.value\?\.focus/u)
  assert.match(component, /<iframe/u)
  assert.match(component, /:title="`Bande-annonce de \$\{movieTitle\} sur YouTube`"/u)
})
