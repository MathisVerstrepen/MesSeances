import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const pageSource = await readFile(
  new URL('../app/pages/recherche.vue', import.meta.url),
  'utf8',
)
const resultsSource = await readFile(
  new URL('../app/components/ShowtimeResults.vue', import.meta.url),
  'utf8',
)
const lineSource = await readFile(
  new URL('../app/components/ShowtimeResultLine.vue', import.meta.url),
  'utf8',
)
const boxSource = await readFile(
  new URL('../app/components/ShowtimeResultBox.vue', import.meta.url),
  'utf8',
)

test('search page owns canonical route selection and filters only rendered results', () => {
  assert.match(
    pageSource,
    /const SELECTION_QUERY_KEYS = \['selected', 'selected_only'\]/,
  )
  assert.match(pageSource, /async function canonicalizeShowtimeSelection\(\)/)
  assert.match(
    pageSource,
    /filterCompatibleShowtimeResults\(\s*normalizedResults\.value,\s*selectedShowtimeKeys\.value,?\s*\)/,
  )
  assert.match(
    pageSource,
    /filterSelectedShowtimeResults\(\s*normalizedResults\.value,\s*selectedShowtimeKeys\.value,?\s*\)/,
  )
  assert.match(pageSource, /:results="visibleResults"/)
  assert.match(pageSource, /:selected-keys="selectedShowtimeKeys"/)
  assert.match(
    pageSource,
    /const preserveSelection =\s*appliedSearch\.value !== null &&\s*searchKey\(search\) === searchKey\(appliedSearch\.value\)/,
  )
})

test('selected-only setting is a labeled checkbox shown only for valid selections', () => {
  assert.match(
    pageSource,
    /const selectedCount = computed\(\(\) => selectedShowtimeKeys\.value\.length\)/,
  )
  assert.match(
    pageSource,
    /const selectedOnly = computed\(\s*\(\) => selectedCount\.value > 0 && routeSelectedOnly\.value,?\s*\)/,
  )
  assert.match(
    pageSource,
    /<label\s+v-if="selectedCount"[^>]*>\s*<input\s+:checked="selectedOnly"\s+type="checkbox"[^>]*@change="setSelectedOnly"\s*\/?>\s*<span>Afficher uniquement les séances sélectionnées<\/span>\s*<\/label>/,
  )
})

test('selection state reaches every grouped and chronological result renderer', () => {
  assert.match(resultsSource, /:selected-keys="selectedKeys"/)
  assert.equal((resultsSource.match(/@toggle-selection=/g) ?? []).length, 3)
  assert.equal(
    (
      resultsSource.match(/:selected="selectedKeySet\.has\(result\.key\)"/g) ??
      []
    ).length,
    2,
  )
})

test('every result layout stays below the sticky search summary', () => {
  assert.equal((resultsSource.match(/'relative isolate/g) ?? []).length, 3)
})

test('box time ranges stay on one line and scale with card width', () => {
  assert.match(boxSource, /class="@container relative flex h-full/)
  assert.match(
    boxSource,
    /whitespace-nowrap text-\[min\(1\.5rem,12cqi\)\] leading-tight/,
  )
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
  assert.match(boxSource, /v-else\s+class="@container relative flex h-full/)
})
