import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { compileMovieTitleSearch } from '../app/utils/movieTitleSearch.ts'

const corpus: {
  name: string
  query: string
  title: string
  original_title: string | null
  expected: boolean
}[] = JSON.parse(
  await readFile(
    new URL('./fixtures/movie-title-search.json', import.meta.url),
    'utf8',
  ),
)

test('compiled title search matches the shared Go corpus', async (t) => {
  for (const fixture of corpus) {
    await t.test(fixture.name, () => {
      const query = compileMovieTitleSearch(fixture.query)
      assert.equal(
        query.matches(fixture.title, fixture.original_title),
        fixture.expected,
      )
    })
  }
})

test('blank raw queries differ from nonblank zero-word queries', () => {
  for (const raw of ['', ' \t\n\u00a0 ']) {
    const query = compileMovieTitleSearch(raw)
    assert.equal(query.blank(), true)
    assert.equal(query.matches('', null), true)
  }
  for (const raw of ['%_', '---', '\u034f']) {
    const query = compileMovieTitleSearch(raw)
    assert.equal(query.blank(), false)
    assert.equal(query.matches(raw, raw), false)
  }
})

test('all Unicode mark categories are removed, not only diacritics', () => {
  // Mn (grapheme joiner), Mc (vowel sign), Me (enclosing circle).
  for (const mark of ['\u034f', '\u093e', '\u20dd']) {
    assert.equal(compileMovieTitleSearch(`fi${mark}lm`).matches('Film'), true)
    assert.equal(compileMovieTitleSearch('film').matches(`Fi${mark}lm`), true)
  }
})

test('compiled queries are reusable without borrowing words across movies or titles', () => {
  const { matches } = compileMovieTitleSearch('man spider')
  assert.equal(matches('Spider-Man', 'Spider-Man'), true)
  assert.equal(matches('Spider', 'Man'), false)
  assert.equal(matches('Man'), false)
  assert.equal(matches('Spider'), false)
  assert.equal(matches('Un film', 'Spider-Man'), true)
  assert.equal(matches('Spider-Man'), true)
})
