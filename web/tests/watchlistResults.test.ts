import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
import { partitionWatchlistResults } from '../app/utils/watchlistResults.ts'
import {
  filterSelectedShowtimeResults,
  filterCompatibleShowtimeResults,
  groupShowtimeResults,
  sortShowtimeResults,
} from '../app/utils/showtimeResults.ts'

function result(
  id: string,
  slug: string,
  hour: string,
  provider: 'ugc' | 'kinepolis' = 'ugc',
): ShowtimeResultViewModel {
  return {
    key: `${provider}:${id}`,
    showtimeId: id,
    provider,
    movieKey: `${provider}:${slug}`,
    movieSlug: slug,
    movieTitle: slug,
    movieOriginalLanguage: null,
    movieRuntimeMinutes: 90,
    theaterName: 'Cinema',
    theaterId: 'theater-1',
    advertisedStartTime: `2026-09-26T${hour}:00:00Z`,
    effectiveStartTime: `2026-09-26T${hour}:00:00Z`,
    end: null,
    language: 'VF',
    format: '2D',
    room: '',
    bookingUrl: null,
    posterUrl: null,
    backdropUrl: null,
  }
}
const results = [
  result('z', 'film-other', '12'),
  result('b', 'film-saved', '18'),
  result('a', 'film-saved', '18', 'kinepolis'),
  result('c', 'film-second', '16'),
]

for (const grouping of ['movie', 'chronological'])
  for (const layout of ['lines', 'boxes']) {
    test(`${grouping}/${layout}: unconditional priority, chronological ties, canonical multiprovider groups`, () => {
      const sections = partitionWatchlistResults(
        results,
        new Set(['film-saved']),
        false,
      )
      assert.deepEqual(
        sections.map((section) => section.title),
        ['Ma watchlist', 'Autres films'],
      )
      assert.deepEqual(
        sections[0]!.results.map((item) => item.showtimeId),
        ['a', 'b'],
      )
      assert.deepEqual(
        sections[1]!.results.map((item) => item.showtimeId),
        ['z', 'c'],
      )
      if (grouping === 'movie') {
        assert.equal(groupShowtimeResults(sections[0]!.results).length, 1)
        assert.deepEqual(
          groupShowtimeResults(sections[1]!.results).map((group) => group.key),
          ['film-other', 'film-second'],
        )
      } else
        assert.deepEqual(
          sortShowtimeResults(sections[0]!.results),
          sections[0]!.results,
        )
      assert.equal(results[1]!.movieKey, 'ugc:film-saved')
    })
  }

test('zero/one/both partitions, only filtering and restoration do not mutate source or selection', () => {
  assert.deepEqual(partitionWatchlistResults([], new Set(), false), [])
  assert.deepEqual(partitionWatchlistResults(results, new Set(), true), [])
  assert.equal(
    partitionWatchlistResults(results, new Set(), false)[0]?.key,
    'others',
  )
  assert.equal(
    partitionWatchlistResults(results, new Set(['film-saved']), true).length,
    1,
  )
  const keys = [results[1]!.key]
  const selected = filterSelectedShowtimeResults(results, keys)
  assert.equal(
    partitionWatchlistResults(selected, new Set(['film-saved']), true)[0]
      ?.results.length,
    1,
  )
  const compatible = filterCompatibleShowtimeResults(results, keys)
  assert.ok(
    partitionWatchlistResults(compatible, new Set(['film-saved']), false).some(
      (section) => section.key === 'watchlist',
    ),
  )
  assert.deepEqual(keys, ['ugc:b'])
  assert.equal(
    partitionWatchlistResults(results, new Set(['film-saved']), false).flatMap(
      (section) => section.results,
    ).length,
    results.length,
  )
})

test('page keeps private filter out of request/share/metadata and renders separate result components', async () => {
  const page = await readFile(
    new URL('../app/pages/recherche.vue', import.meta.url),
    'utf8',
  )
  const share = page.slice(
    page.indexOf('const shareTarget'),
    page.indexOf('const activeFilterSummary'),
  )
  const request = page.slice(
    page.indexOf('async function runSearch'),
    page.indexOf('async function applyRoute'),
  )
  assert.doesNotMatch(share + request, /watchlist|slugs|owner/i)
  assert.match(page, /watch\(\s*watchlist.scopeKey/)
  assert.match(page, /v-for="section in resultSections"/)
  assert.match(page, /:results="section.results"/)
  assert.match(page, /results && visibleResults.length === 0/)
  const film = await readFile(
    new URL('../app/pages/film/[slug].vue', import.meta.url),
    'utf8',
  )
  assert.match(film, /<MovieTrailer[\s\S]*?\/>\s*<WatchlistButton/)
  const button = await readFile(
    new URL('../app/components/WatchlistButton.vue', import.meta.url),
    'utf8',
  )
  assert.match(button, /:aria-pressed="unknown \? undefined : saved"/)
  assert.match(button, /size-12/)
})
