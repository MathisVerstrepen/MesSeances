import assert from 'node:assert/strict'
import test from 'node:test'
import type { ResolvedTrailer } from '../app/types/api.ts'
import { providerTrailerPlayback } from '../app/utils/providerTrailer.ts'

const mp4: ResolvedTrailer = {
  type: 'mp4',
  url: 'https://fr.vid.web.acsta.net/nmedia/33/trailer.mp4',
  source: 'cgr',
  language: null,
}

test('plays provider MP4s and converts Cineville references to privacy enhanced embeds', () => {
  assert.deepEqual(providerTrailerPlayback(mp4), { type: 'mp4', url: mp4.url })
  assert.deepEqual(
    providerTrailerPlayback({
      type: 'youtube',
      source: 'cineville',
      url: 'https://www.youtube.com/watch?v=AbC-12_xYz9',
      language: null,
    }),
    {
      type: 'youtube',
      url: 'https://www.youtube-nocookie.com/embed/AbC-12_xYz9?autoplay=1',
    },
  )
})

test('rejects unsafe or unsupported media without loading an external resource', () => {
  for (const url of [
    'javascript:alert(1)',
    'https://evil.example/a.mp4',
    `${mp4.url}?token=x`,
    `${mp4.url}#`,
    mp4.url.replace('/nmedia/', '/nmedia/../'),
    mp4.url.replace('/nmedia/', '/nmedia//'),
  ]) {
    assert.equal(providerTrailerPlayback({ ...mp4, url }), null)
  }
  assert.equal(providerTrailerPlayback({ ...mp4, source: 'cineville' }), null)
  assert.equal(providerTrailerPlayback(null), null)
})
