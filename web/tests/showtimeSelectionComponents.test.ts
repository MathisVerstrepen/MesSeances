import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const pageSource = await readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8')
const resultsSource = await readFile(new URL('../app/components/ShowtimeResults.vue', import.meta.url), 'utf8')
const lineSource = await readFile(new URL('../app/components/ShowtimeResultLine.vue', import.meta.url), 'utf8')
const boxSource = await readFile(new URL('../app/components/ShowtimeResultBox.vue', import.meta.url), 'utf8')

test('search page owns canonical route selection and filters only rendered results', () => {
  assert.match(pageSource, /const SELECTION_QUERY_KEYS = \['selected'\]/)
  assert.match(pageSource, /async function canonicalizeShowtimeSelection\(\)/)
  assert.match(pageSource, /filterCompatibleShowtimeResults\(normalizedResults\.value, selectedShowtimeKeys\.value\)/)
  assert.match(pageSource, /:results="visibleResults"/)
  assert.match(pageSource, /:selected-keys="selectedShowtimeKeys"/)
  assert.match(pageSource, /const preserveSelection = appliedSearch\.value !== null && searchKey\(search\) === searchKey\(appliedSearch\.value\)/)
})

test('selection state reaches every grouped and chronological result renderer', () => {
  assert.match(resultsSource, /:selected-keys="selectedKeys"/)
  assert.equal((resultsSource.match(/@toggle-selection=/g) ?? []).length, 3)
  assert.equal((resultsSource.match(/:selected="selectedKeySet\.has\(result\.key\)"/g) ?? []).length, 2)
})

test('line and box selection controls expose keyboard and screen-reader button state', () => {
  for (const source of [lineSource, boxSource]) {
    assert.match(source, /type="button"/)
    assert.match(source, /:aria-label="selectionLabel"/)
    assert.match(source, /:aria-pressed="selected"/)
    assert.match(source, /@click="emit\('toggleSelection', result\.key\)"/)
    assert.match(source, /relative z-20/)
  }
  assert.match(lineSource, /v-if="scope === 'multi-theater'"/)
  assert.match(boxSource, /v-else class="relative flex h-full/)
})
