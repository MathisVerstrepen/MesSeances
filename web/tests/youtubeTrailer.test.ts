import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { availableTrailerSelections, validatedYouTubeKey, youtubeNoCookieTrailerEmbedUrl } from '../app/utils/youtubeTrailer.ts'

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

test('orders VF first, falls back to VO, and hides unavailable variants', () => {
  const vfKey = 'AbC-12_xYz9'
  const voKey = 'VoT-98_rQp7'

  assert.deepEqual(availableTrailerSelections(vfKey, voKey), [
    { variant: 'VF', youtubeKey: vfKey },
    { variant: 'VO', youtubeKey: voKey }
  ])
  assert.deepEqual(availableTrailerSelections(vfKey, null), [
    { variant: 'VF', youtubeKey: vfKey }
  ])
  assert.deepEqual(availableTrailerSelections(null, voKey), [
    { variant: 'VO', youtubeKey: voKey }
  ])
  assert.deepEqual(availableTrailerSelections(null, null), [])
  assert.deepEqual(availableTrailerSelections('invalid', voKey), [
    { variant: 'VO', youtubeKey: voKey }
  ])
})

test('removes duplicate VF and VO controls defensively', () => {
  const duplicateKey = 'AbC-12_xYz9'

  assert.deepEqual(availableTrailerSelections(duplicateKey, duplicateKey), [
    { variant: 'VF', youtubeKey: duplicateKey }
  ])
})

test('trailer header exposes one preferred action and modal owns the optional VO switch', async () => {
  const component = await readFile(new URL('../app/components/MovieTrailer.vue', import.meta.url), 'utf8')

  assert.match(component, /v-if="primaryTrailer"/u)
  assert.equal(component.match(/aria-haspopup="dialog"/gu)?.length, 1)
  assert.match(component, /@click="openModal\(primaryTrailer, \$event\)"/u)
  assert.doesNotMatch(component, /openModal\(secondaryTrailer/u)
  assert.match(component, /v-if="secondaryTrailer && activeTrailer\?\.variant !== 'VO'"/u)
  assert.match(component, />\s*Voir en VO\s*<\/button>/u)
  assert.match(component, /@click="selectTrailer\(secondaryTrailer\)"/u)
  assert.match(component, /async function selectTrailer\(trailer: TrailerSelection \| null\)/u)
  assert.match(component, /const embedUrl = computed\(\(\) => isOpen\.value \? youtubeNoCookieTrailerEmbedUrl\(activeTrailer\.value\?\.youtubeKey\)/u)
  assert.match(component, /v-if="isOpen && embedUrl"/u)
  assert.match(component, /dialog\.value\.showModal\(\)/u)
  assert.match(component, /@cancel\.prevent="closeModal\(\)"/u)
  assert.match(component, /@click\.self="closeModal\(\)"/u)
  assert.match(component, /closeButton\.value\?\.focus/u)
  assert.match(component, /activeTrigger\?\.focus/u)
  assert.match(component, /activeTrailer\.value = null/u)
  assert.match(component, /<iframe/u)
  assert.match(component, /:key="`\$\{activeTrailer\?\.variant\}-\$\{activeTrailer\?\.youtubeKey\}`"/u)
  assert.equal(component.match(/>\s*Bande-annonce\s*<\/button>/gu)?.length, 1)
  assert.doesNotMatch(component, /Bande-annonce \{\{ primaryTrailer\.variant \}\}/u)
  assert.doesNotMatch(component, /Bande-annonce \{\{ secondaryTrailer\.variant \}\}/u)
  assert.match(component, /:aria-label="`Voir la bande-annonce \$\{primaryTrailer\.variant\} de \$\{movieTitle\}`"/u)
  assert.match(component, /:title="`Bande-annonce \$\{activeTrailer\?\.variant\} de \$\{movieTitle\} sur YouTube`"/u)
  assert.match(component, /:aria-label="`Voir la bande-annonce VO de \$\{movieTitle\}`"/u)
  assert.match(component, /:aria-label="`Fermer la bande-annonce \$\{activeTrailer\?\.variant\} de \$\{movieTitle\}`"/u)
})
