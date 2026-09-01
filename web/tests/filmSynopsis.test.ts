import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const page = await readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8')

test('film synopsis clamps to three lines and exposes expansion only when text overflows', () => {
  assert.match(page, /const synopsisExpanded = ref\(false\)/u)
  assert.match(page, /element\.scrollHeight > element\.clientHeight \+ 1/u)
  assert.match(page, /synopsisExpanded \? 'block' : 'line-clamp-3'/u)
  assert.match(page, /:disabled="!synopsisOverflows"/u)
  assert.match(page, /:aria-expanded="synopsisOverflows \? synopsisExpanded : undefined"/u)
  assert.match(page, /@click="toggleSynopsis"/u)
})

test('film synopsis expansion resets when its movie changes', () => {
  assert.match(page, /watch\(\(\) => schedule\.value\?\.movie\.overview, async \(\) => \{\s+synopsisExpanded\.value = false/u)
  assert.match(page, /watch\(slug, \(\) => \{[\s\S]*?synopsisExpanded\.value = false/u)
})
