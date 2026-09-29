import assert from 'node:assert/strict'
import test from 'node:test'
import type {
  WatchlistItem,
  WatchlistSortOrder,
} from '../app/types/watchlist.ts'
import {
  sortWatchlistItems,
  watchlistSortOptions,
} from '../app/utils/watchlistSort.ts'

const item = (
  slug: string,
  props: Partial<WatchlistItem> = {},
): WatchlistItem => ({
  slug,
  title: slug,
  added_at: '2026-09-01T00:00:00Z',
  tag_ids: [],
  ...props,
})
const slugs = (items: WatchlistItem[]) => items.map((movie) => movie.slug)

test('six distinct compact choices retain original order, values and accessible full names', () => {
  assert.deepEqual(
    watchlistSortOptions.map(({ value, shortLabel, label }) => [
      value,
      shortLabel,
      label,
    ]),
    [
      ['added_desc', 'Ajouts récents', 'Ajouts les plus récents'],
      ['added_asc', 'Ajouts anciens', 'Ajouts les plus anciens'],
      ['title_asc', 'Titre : A-Z', 'Titre : A-Z'],
      ['title_desc', 'Titre : Z-A', 'Titre : Z-A'],
      [
        'release_desc',
        'Sortie FR récente',
        'Sorties françaises les plus récentes',
      ],
      [
        'release_asc',
        'Sortie FR ancienne',
        'Sorties françaises les plus anciennes',
      ],
    ],
  )
  assert.equal(
    new Set(watchlistSortOptions.map(({ shortLabel }) => shortLabel)).size,
    6,
  )
})

test('all six modes sort a copy, using French theatrical evidence only', () => {
  const items = [
    item('film-b', { title: 'École 10', french_release_date: '2000-02-29' }),
    item('film-a', {
      title: 'ecole 2',
      french_release_date: '1998-10-14',
      added_at: '2026-08-31T23:59:59.999999999Z',
    }),
    item('film-c', {
      title: 'Zèbre',
      release_date: '1900-01-01',
      added_at: '2026-09-01T00:00:00.000000001Z',
    }),
  ]
  const before = structuredClone(items)
  const expected = {
    added_desc: ['film-c', 'film-b', 'film-a'],
    added_asc: ['film-a', 'film-b', 'film-c'],
    title_asc: ['film-a', 'film-b', 'film-c'],
    title_desc: ['film-c', 'film-b', 'film-a'],
    release_desc: ['film-b', 'film-a', 'film-c'],
    release_asc: ['film-a', 'film-b', 'film-c'],
  } satisfies Record<WatchlistSortOrder, string[]>
  for (const { value: order } of watchlistSortOptions) {
    const sorted = sortWatchlistItems(items, order)
    assert.deepEqual(slugs(sorted), expected[order])
    assert.notEqual(sorted, items)
    assert.deepEqual(items, before)
  }
})

test('addition keys preserve nanoseconds and normalize omitted or variable fractions', () => {
  const items = [
    item('f', { added_at: '2026-09-01T00:00:00.123456789Z' }),
    item('e', { added_at: '2026-09-01T00:00:00.123456788Z' }),
    item('d', { added_at: '2026-09-01T00:00:00.1Z' }),
    item('c', { added_at: '2026-09-01T00:00:00.100000000Z' }),
    item('b', { added_at: '2026-09-01T00:00:00.000Z' }),
    item('a'),
  ]
  assert.deepEqual(slugs(sortWatchlistItems(items, 'added_asc')), [
    'a',
    'b',
    'c',
    'd',
    'e',
    'f',
  ])
  assert.deepEqual(slugs(sortWatchlistItems(items, 'added_desc')), [
    'f',
    'e',
    'c',
    'd',
    'a',
    'b',
  ])
})

test('French case/accent and numeric-equivalent title ties use ascending slug in both directions', () => {
  const items = [
    item('z', { title: 'ÉCOLE 2' }),
    item('a', { title: 'ecole 02' }),
    item('m', { title: 'école 2' }),
  ]
  for (const order of ['title_asc', 'title_desc'] as const) {
    assert.deepEqual(slugs(sortWatchlistItems(items, order)), ['a', 'm', 'z'])
  }
})

test('missing/null/empty/invalid French dates stay last, tied by slug regardless of direction', () => {
  const invalid = [
    undefined,
    null,
    '',
    '1998',
    '1998-02-30',
    '2025-02-29',
    '1998-13-14',
    '1998-10-14T00:00:00Z',
    'invalid',
  ]
  const items = invalid
    .map((date, index) =>
      item(`undated-${index}`, {
        french_release_date: date,
        release_date: '2026-12-01',
      }),
    )
    .reverse()
  items.push(
    item('b', { french_release_date: '2000-02-29' }),
    item('a', { french_release_date: '2000-02-29' }),
  )
  for (const order of ['release_asc', 'release_desc'] as const) {
    assert.deepEqual(slugs(sortWatchlistItems(items, order)), [
      'a',
      'b',
      ...invalid.map((_, index) => `undated-${index}`),
    ])
  }
})

test('empty lists and canonical rows are unchanged except order', () => {
  assert.deepEqual(sortWatchlistItems([], 'added_desc'), [])
  const canonical = item('film-42', { added_at: '1998-10-14T00:00:00Z' })
  assert.equal(sortWatchlistItems([canonical], 'title_asc')[0], canonical)
})
