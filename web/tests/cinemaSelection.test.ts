import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { Theater } from '../app/types/api.ts'
import {
  groupTheatersByCityIdentity,
  cinemaListSections,
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

test('selected summary copies share rows while the complete directory keeps source order', () => {
  const rows = [
    theater('a', 'Paris', 'paris'),
    theater('b', 'Paris', 'paris'),
    theater('c', 'Lille', 'lille'),
    theater('d', 'Lille', 'lille'),
  ]
  const sections = cinemaListSections(
    rows,
    new Set(['b', 'd']),
    (row) => row.id,
  )
  assert.deepEqual(
    sections.map((section) => [section.key, section.rows.map((row) => row.id)]),
    [
      ['selected', ['b', 'd']],
      ['all', ['a', 'b', 'c', 'd']],
    ],
  )
  assert.equal(sections[0]!.rows[0], rows[1])
  assert.equal(sections[1]!.rows, rows)
  const groups = groupTheatersByCityIdentity(sections[1]!.rows)
  assert.equal(groups.length, 2)
  assert.deepEqual(
    updateTheaterSelection(['b', 'd'], groups[0]!.theaters, false),
    ['d'],
  )
  const distances = rows.map((theater, index) => ({
    theater,
    distance: index,
    nearest: index === 0,
  }))
  const ordered = cinemaListSections(
    distances,
    new Set(['d', 'b']),
    (row) => row.theater.id,
  )
  assert.deepEqual(
    ordered.map((section) => section.rows.map((row) => row.distance)),
    [
      [1, 3],
      [0, 1, 2, 3],
    ],
  )
  assert.equal(
    ordered[0]!.rows.every((row) => !row.nearest),
    true,
  )
  assert.equal(ordered[1]!.rows[0]?.nearest, true)
  assert.equal(isBroadTheaterSelection([], rows), true)
  assert.equal(isBroadTheaterSelection(['a', 'b', 'c', 'd'], rows), true)
  assert.equal(isBroadTheaterSelection(['a'], rows), false)
})

test('selected-only has one section, no selection has only all, empty filtered rows never add a summary', () => {
  const rows = [theater('a', 'Paris', 'paris'), theater('b', 'Lille', 'lille')]
  const id = (row: Theater) => row.id
  assert.deepEqual(
    cinemaListSections(rows, new Set(['b', 'hidden']), id, true),
    [{ key: 'selected', rows: [rows[1]] }],
  )
  assert.deepEqual(cinemaListSections(rows, new Set(), id), [
    { key: 'all', rows },
  ])
  assert.deepEqual(cinemaListSections(rows, new Set(['hidden']), id, true), [])
  assert.deepEqual(cinemaListSections([], new Set(['b']), id), [
    { key: 'all', rows: [] },
  ])
  assert.deepEqual(
    cinemaListSections(rows, new Set(['a', 'b']), id).map((s) => s.key),
    ['selected', 'all'],
  )
})
