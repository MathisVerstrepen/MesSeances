import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { Theater } from '../app/types/api.ts'
import {
  groupTheatersByCityIdentity,
  groupSelectedTheatersFirst,
  selectedFirst,
  isBroadTheaterSelection,
  updateTheaterSelection,
} from '../app/utils/cinemaSelection.ts'

function theater(id: string, city: string, citySlug: string): Theater {
  return {
    provider: 'ugc',
    id,
    slug: id,
    name: id,
    address: '1 rue du cinéma',
    city,
    city_slug: citySlug,
    postal_code: '75000',
    available_dates: [],
    accepted_passes: [],
  }
}

test('groups theaters by exact city slug with stable first labels and orders', () => {
  const theaters = [
    theater('paris-first', 'Paris', 'paris'),
    theater('evry-accent', 'Évry', 'evry--b6b4bc41'),
    theater('paris-case', 'PARIS', 'paris'),
    theater('evry-plain', 'Évry', 'evry--5a2839c1'),
  ]
  const before = structuredClone(theaters)

  const groups = groupTheatersByCityIdentity(theaters)

  assert.deepEqual(
    groups.map(({ city, citySlug, theaters: grouped }) => ({
      city,
      citySlug,
      ids: grouped.map((item) => item.id),
    })),
    [
      { city: 'Paris', citySlug: 'paris', ids: ['paris-first', 'paris-case'] },
      { city: 'Évry', citySlug: 'evry--b6b4bc41', ids: ['evry-accent'] },
      { city: 'Évry', citySlug: 'evry--5a2839c1', ids: ['evry-plain'] },
    ],
  )
  assert.deepEqual(theaters, before)
})

test('selects only target theaters while preserving hidden selection and input order', () => {
  const currentIds = ['hidden', 'visible-selected']
  const targets = [{ id: 'visible-selected' }, { id: 'visible-new' }]
  const beforeIds = [...currentIds]
  const beforeTargets = structuredClone(targets)

  assert.deepEqual(updateTheaterSelection(currentIds, targets, true), [
    'hidden',
    'visible-selected',
    'visible-new',
  ])
  assert.deepEqual(currentIds, beforeIds)
  assert.deepEqual(targets, beforeTargets)
})

test('deselects only target theaters, keeps unmatched IDs, and permits zero', () => {
  assert.deepEqual(
    updateTheaterSelection(
      ['outside-search', 'shown-a', 'shown-b'],
      [{ id: 'shown-a' }, { id: 'shown-b' }],
      false,
    ),
    ['outside-search'],
  )
  assert.deepEqual(
    updateTheaterSelection(['shown-a'], [{ id: 'shown-a' }], false),
    [],
  )
})

test('frontend selection has no geographic defaults; unrelated metadata stays unchanged', async () => {
  const [preferences, config] = await Promise.all([
    readFile(
      new URL('../app/composables/useCinemaPreferences.ts', import.meta.url),
      'utf8',
    ),
    readFile(new URL('../nuxt.config.ts', import.meta.url), 'utf8'),
  ])

  assert.doesNotMatch(preferences, /api\.theaters\(\{ city: 'Paris' \}\)/)
  assert.doesNotMatch(preferences, /api\.theaters\(\{ city: 'Lille' \}\)/)
  assert.equal(
    config.match(/séances de cinéma de Paris sur une frise horaire/g)?.length,
    2,
  )
  assert.doesNotMatch(
    config,
    /séances de cinéma de Lille sur une frise horaire/,
  )
})

test('header groups saved favorite cities by city slug with first label retained', async () => {
  const header = await readFile(
    new URL('../app/components/AppHeader.vue', import.meta.url),
    'utf8',
  )

  assert.match(header, /const cityLabels = new Map<string, string>\(\)/)
  assert.match(
    header,
    /if \(!cityLabels\.has\(theater\.city_slug\)\)\s+cityLabels\.set\(theater\.city_slug, theater\.city\)/,
  )
  assert.match(header, /const cities = \[\.\.\.cityLabels\.values\(\)\]/)
  assert.doesNotMatch(
    header,
    /new Set\(favoriteTheaters\.value\.map\(\(theater\) => theater\.city\)\)/,
  )
})

test('selected cinemas precede every unselected city group with unique keys and precise group actions', () => {
  const rows = [
    theater('a', 'Paris', 'paris'),
    theater('b', 'Paris', 'paris'),
    theater('c', 'Lille', 'lille'),
    theater('d', 'Lille', 'lille'),
  ]
  const groups = groupSelectedTheatersFirst(rows, new Set(['b', 'd']))
  assert.deepEqual(
    groups.flatMap((group) => group.theaters.map((row) => row.id)),
    ['b', 'd', 'a', 'c'],
  )
  assert.equal(new Set(groups.map((group) => group.key)).size, 4)
  assert.deepEqual(
    updateTheaterSelection(['b', 'd'], groups[0]!.theaters, false),
    ['d'],
  )
  const distances = rows.map((theater, index) => ({
    theater,
    distance: index,
    nearest: index === 0,
  }))
  const ordered = selectedFirst(
    distances,
    new Set(['d', 'b']),
    (row) => row.theater.id,
  )
  assert.deepEqual(
    ordered.map((row) => row.distance),
    [1, 3, 0, 2],
  )
  assert.equal(ordered[2]?.nearest, true)
  assert.equal(isBroadTheaterSelection([], rows), true)
  assert.equal(isBroadTheaterSelection(['a', 'b', 'c', 'd'], rows), true)
  assert.equal(isBroadTheaterSelection(['a'], rows), false)
})
