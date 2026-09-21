import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = await readFile(
  new URL('../app/components/ShowtimeResultLine.vue', import.meta.url),
  'utf8',
)

test('movie-grouped mobile lines keep booking beside session details in both scopes', () => {
  assert.match(
    source,
    /<li\s+v-else\s+class="[^"]*grid-cols-\[minmax\(0,1fr\)_auto\] items-center/,
  )
})

test('chronological mobile lines keep booking at the end of the time row', () => {
  assert.match(
    source,
    /grid-cols-\[3rem_minmax\(0,1fr\)_auto\] gap-x-3 gap-y-2/,
  )
  assert.match(source, /col-start-2 self-center border-l-2 border-ink pl-3/)
  assert.match(
    source,
    /col-start-3 row-start-1 row-span-2 min-h-11 max-w-28 self-center text-right sm:col-start-auto sm:row-start-auto sm:row-span-1 sm:max-w-none/,
  )
  assert.match(
    source,
    /: 'col-start-3 row-start-1 min-h-10 self-center sm:col-start-auto sm:row-start-auto'/,
  )
  assert.match(
    source,
    /scope === 'multi-theater' \? 'col-span-2' : 'col-span-1'/,
  )
})
