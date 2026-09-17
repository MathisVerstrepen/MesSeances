import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = await readFile(new URL('../app/components/ShowtimeResultLine.vue', import.meta.url), 'utf8')

test('movie-grouped mobile lines keep booking beside session details in both scopes', () => {
  assert.match(source, /<li v-else class="[^"]*grid-cols-\[minmax\(0,1fr\)_auto\] items-center/)
})

test('cinema chronological mobile lines place booking beside both detail rows', () => {
  assert.match(source, /scope === 'single-theater' \? 'grid-cols-\[3rem_minmax\(0,1fr\)_auto\]' : 'grid-cols-\[3rem_minmax\(0,1fr\)\]'/)
  assert.match(source, /col-start-3 row-start-1 row-span-2 min-h-11 max-w-28 self-center text-right sm:col-start-auto sm:row-start-auto sm:row-span-1 sm:max-w-none/)
  assert.match(source, /: 'col-span-2 mt-1 min-h-10 sm:col-span-1 sm:mt-0'/)
})
