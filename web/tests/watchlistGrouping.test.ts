import assert from 'node:assert/strict'
import test from 'node:test'
import type { WatchlistItem, WatchlistTag } from '../app/types/watchlist.ts'
import { groupWatchlistItems } from '../app/utils/watchlistGrouping.ts'
import {
  sortWatchlistItems,
  watchlistSortOptions,
} from '../app/utils/watchlistSort.ts'
import {
  sortWatchlistTags,
  watchlistTagPalette,
} from '../app/utils/watchlistTags.ts'

const tag = (id: string, name: string): WatchlistTag => ({
  id,
  name,
  color: 'neutral',
})
const tags = [
  tag('20', 'École 10'),
  tag('10', 'école 2'),
  tag('2', 'Ecole 02'),
  tag('1', 'Vide'),
]
const item = (
  slug: string,
  props: Partial<WatchlistItem> = {},
): WatchlistItem => ({
  slug,
  title: slug,
  added_at: '2026-09-01T00:00:00Z',
  tag_ids: ['20', '10', '2'],
  ...props,
})
const items = [
  item('b', { title: 'École 10', french_release_date: '2000-02-29' }),
  item('a', {
    title: 'ecole 2',
    french_release_date: '1998-10-14',
    added_at: '2026-08-31T23:59:59Z',
  }),
  item('c', {
    title: 'Zèbre',
    release_date: '1900-01-01',
    added_at: '2026-09-01T00:00:00.000000001Z',
  }),
]

test('nonempty sections follow stable French tag order, duplicate canonical items and place Sans tag last', () => {
  const untagged = item('untagged', { tag_ids: [] })
  const source = [...items, untagged]
  const before = structuredClone({ source, tags })
  const groups = groupWatchlistItems(source, sortWatchlistTags(tags))
  assert.deepEqual(
    groups.map((group) => group.id),
    ['tag-2', 'tag-10', 'tag-20', 'untagged'],
  )
  assert.deepEqual(
    groups.map((group) => group.items.length),
    [3, 3, 3, 1],
  )
  for (const group of groups.slice(0, 3)) {
    assert.equal(group.items[0], items[0])
    assert.deepEqual(group.items, items)
  }
  assert.equal(groups[3]?.name, 'Sans tag')
  assert.equal(groups[3]?.items[0], untagged)
  assert.deepEqual({ source, tags }, before)
})

test('tag filter selects only its section, suppressing untagged and empty sections', () => {
  const source = [...items, item('untagged', { tag_ids: [] })]
  const groups = groupWatchlistItems(source, sortWatchlistTags(tags), '10')
  assert.equal(groups.length, 1)
  assert.equal(groups[0]?.id, 'tag-10')
  assert.deepEqual(groups[0]?.items, items)
  assert.deepEqual(groupWatchlistItems(source, tags, '1'), [])
  assert.deepEqual(groupWatchlistItems(source, tags, '999'), [])
  assert.deepEqual(groupWatchlistItems([], tags), [])
  assert.deepEqual(groupWatchlistItems([], []), [])
})

test('groups carry stored palette colors after edits, while only synthetic Sans tag has no color', () => {
  const source = [
    item('tagged', { tag_ids: ['1'] }),
    item('none', { tag_ids: [] }),
  ]
  // SAFETY: This static palette defines exactly the WatchlistTag color keys.
  for (const color of Object.keys(
    watchlistTagPalette,
  ) as WatchlistTag['color'][]) {
    const definition: WatchlistTag = { id: '1', name: 'Sans tag', color }
    const groups = groupWatchlistItems(source, [definition])
    assert.equal(groups[0]?.color, color)
    assert.equal(groups[1]?.color, null)
    const renamed = groupWatchlistItems(source, [
      { ...definition, name: 'Amis' },
    ])
    assert.equal(renamed[0]?.id, groups[0]?.id)
    assert.equal(renamed[0]?.name, 'Amis')
    assert.equal(renamed[0]?.color, color)
    assert.deepEqual(renamed[0]?.items, groups[0]?.items)
  }
})

for (const { value: order } of watchlistSortOptions) {
  test(`each group preserves ${order}, with undated French releases last`, () => {
    const expected = {
      added_desc: ['c', 'b', 'a'],
      added_asc: ['a', 'b', 'c'],
      title_asc: ['a', 'b', 'c'],
      title_desc: ['c', 'b', 'a'],
      release_desc: ['b', 'a', 'c'],
      release_asc: ['a', 'b', 'c'],
    }
    const source = [
      ...items,
      ...items.map((movie) => ({
        ...movie,
        slug: `untagged-${movie.slug}`,
        tag_ids: [],
      })),
    ]
    const groups = groupWatchlistItems(
      sortWatchlistItems(source, order),
      sortWatchlistTags(tags),
    )
    assert.equal(groups.length, 4)
    for (const group of groups) {
      assert.deepEqual(
        group.items.map((movie) => movie.slug.replace('untagged-', '')),
        expected[order],
      )
    }
  })
}

test('title ties and tag renames keep stable canonical identities without primary-tag selection', () => {
  const source = [
    item('z', { title: 'École 2' }),
    item('a', { title: 'ecole 02' }),
  ]
  for (const order of ['title_asc', 'title_desc'] as const) {
    const sorted = sortWatchlistItems(source, order)
    const groups = groupWatchlistItems(sorted, sortWatchlistTags(tags))
    assert.ok(
      groups.every(
        (group) => group.items.map((movie) => movie.slug).join(',') === 'a,z',
      ),
    )
    const renamed = groupWatchlistItems(
      sorted,
      sortWatchlistTags(
        tags.map((definition) =>
          definition.id === '20'
            ? { ...definition, name: 'Aventure' }
            : definition,
        ),
      ),
    )
    assert.equal(renamed[0]?.id, 'tag-20')
    assert.deepEqual(renamed[0]?.items, groups[2]?.items)
  }
})

test('confirmed replacement snapshots move copies and remove the final emptied section', () => {
  const source = item('film', { tag_ids: ['2', '10'] })
  assert.deepEqual(
    groupWatchlistItems([source], tags).map((group) => group.id),
    ['tag-10', 'tag-2'],
  )
  const remaining = { ...source, tag_ids: ['10'] }
  assert.deepEqual(
    groupWatchlistItems([remaining], tags).map((group) => group.id),
    ['tag-10'],
  )
  const untagged = { ...source, tag_ids: [] }
  assert.deepEqual(
    groupWatchlistItems([untagged], tags).map((group) => group.id),
    ['untagged'],
  )
  assert.deepEqual(groupWatchlistItems([], tags), [])
  assert.deepEqual(source.tag_ids, ['2', '10'])
})
