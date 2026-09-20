import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const sources = [
  ['components/MovieCatalogPagination.vue', 1],
  ['pages/admin/tmdb-matches.vue', 2],
  ['pages/admin/theater-locations.vue', 1],
  ['pages/admin/upcoming-movies.vue', 1],
] as const

for (const [path, count] of sources) {
  test(`${path} keeps mobile pagination actions paired below a full-width count`, async () => {
    const source = await readFile(
      new URL(`../app/${path}`, import.meta.url),
      'utf8',
    )
    const pagers = [...source.matchAll(/<nav\b[\s\S]*?<\/nav>/g)].filter(
      ([nav]) => /[Pp]agination/.test(nav),
    )
    assert.equal(pagers.length, count)
    for (const [pager] of pagers) {
      const navClass = pager.match(/\bclass="([^"]+)"/)?.[1]?.split(' ') ?? []
      for (const token of ['grid', 'grid-cols-2', 'sm:flex'])
        assert.ok(navClass.includes(token), token)
      assert.ok(!navClass.includes('flex-col'))
      assert.ok(!navClass.includes('flex-wrap'))
      const countClass =
        pager
          .match(/<span\s+class="([^"]+)"[^>]*>\s*Page\s+/)?.[1]
          ?.split(' ') ?? []
      for (const token of [
        'order-first',
        'col-span-2',
        'min-w-0',
        'wrap-anywhere',
        'sm:order-none',
      ])
        assert.ok(countClass.includes(token), token)
    }
  })
}

test('public catalogs share pagination without a Films-only mobile override', async () => {
  for (const path of [
    'films/index.vue',
    'films/prochainement.vue',
    'ville/[slug]/cinemas.vue',
  ]) {
    const source = await readFile(
      new URL(`../app/pages/${path}`, import.meta.url),
      'utf8',
    )
    assert.match(source, /<MovieCatalogPagination\b/)
    assert.doesNotMatch(source, /films-pagination/)
  }
})

test('shared pager retains native links, unavailable spans and single navigation events', async () => {
  const source = await readFile(
    new URL('../app/components/MovieCatalogPagination.vue', import.meta.url),
    'utf8',
  )
  assert.equal([...source.matchAll(/<NuxtLink\b/g)].length, 2)
  assert.match(source, /<span\s+v-if="!previousTo"[^>]+aria-disabled="true"/)
  assert.match(source, /<span\s+v-if="!nextTo"[^>]+aria-disabled="true"/)
  assert.equal(
    [...source.matchAll(/:aria-disabled="pending \|\| undefined"/g)].length,
    2,
  )
  assert.match(source, /@click="emit\('navigate', \$event, page - 1\)"/)
  assert.match(source, /@click="emit\('navigate', \$event, page \+ 1\)"/)
  assert.match(source, /aria-live="polite"/)
  assert.doesNotMatch(source, /@click\.prevent|router\.(push|replace)/)
})
