import assert from 'node:assert/strict'
import test from 'node:test'
import type { Theater } from '../app/types/api.ts'
import { filterCinemaDirectory } from '../app/utils/cinemaDirectoryChains.ts'
import {
  cinemaDirectoryTechnologies,
  normalizeCinemaTechnologies,
} from '../app/utils/cinemaDirectoryTechnologies.ts'
import {
  cinemaDirectoryQuery,
  parseCinemaDirectoryQuery,
} from '../app/utils/cinemaDirectoryQuery.ts'

const base: Theater = {
  provider: 'ugc',
  id: 'imax',
  slug: 'imax',
  name: 'UGC Lille',
  city: 'Lille',
  city_slug: 'lille',
  address: '',
  postal_code: '',
  available_dates: [],
  accepted_passes: [],
}
const theaters: Theater[] = [
  { ...base, available_formats: ['2D', 'IMAX', '3D'] },
  { ...base, id: 'ice', provider: 'cgr', available_formats: ['ICE'] },
  {
    ...base,
    id: 'paris',
    city: 'Paris',
    name: 'UGC Paris',
    available_formats: ['IMAX'],
  },
  { ...base, id: 'empty', available_formats: [] },
  { ...base, id: 'missing' },
]

test('technology options reflect the full directory, omit 2D and deduplicate formats', () => {
  assert.deepEqual(
    cinemaDirectoryTechnologies(theaters).map((option) => option.value),
    ['3D', 'IMAX', 'ICE'],
  )
  assert.deepEqual(cinemaDirectoryTechnologies([]), [])
  assert.deepEqual(
    normalizeCinemaTechnologies([
      ' imax ',
      'ICE',
      'IMAX',
      '2D',
      'ALL',
      'unknown',
    ]),
    ['IMAX', 'ICE'],
  )
})

test('technologies use a union intersected with chain and search; unknown availability never matches', () => {
  const before = structuredClone(theaters)
  const ids = (
    search: string,
    chains: Parameters<typeof filterCinemaDirectory>[2],
    technologies: Parameters<typeof filterCinemaDirectory>[3],
  ) =>
    filterCinemaDirectory(theaters, search, chains, technologies).map(
      (theater) => theater.id,
    )
  assert.deepEqual(ids('', [], ['IMAX', 'ICE']), ['imax', 'ice', 'paris'])
  assert.deepEqual(ids(' Lille ', ['ugc'], ['IMAX', 'ICE']), ['imax'])
  assert.deepEqual(ids('', ['cgr'], ['IMAX']), [])
  assert.deepEqual(ids('', [], ['DOLBY']), [])
  assert.deepEqual(filterCinemaDirectory(theaters, '', [], []), theaters)
  assert.deepEqual(theaters, before)
})

test('technology query normalization retains other state and clears cleanly', () => {
  const query = {
    technologies: [' imax,ICE,2D', null, 'imax,invalid'],
    q: ' Lille ',
    chains: 'ugc',
    view: 'map',
    location: 'nearby',
    tag: ['kept', null],
  }
  const before = structuredClone(query)
  assert.deepEqual(parseCinemaDirectoryQuery(query).technologies, [
    'IMAX',
    'ICE',
  ])
  const normalized = cinemaDirectoryQuery(query)
  assert.equal(normalized.technologies, 'IMAX,ICE')
  assert.equal(normalized.q, 'Lille')
  assert.equal(
    cinemaDirectoryQuery(normalized, { search: 'Paris', view: 'list' })
      .technologies,
    'IMAX,ICE',
  )
  assert.deepEqual(cinemaDirectoryQuery(normalized, { technologies: [] }), {
    q: 'Lille',
    chains: 'ugc',
    view: 'map',
    location: 'nearby',
    tag: ['kept', null],
  })
  for (const technologies of [
    undefined,
    null,
    '',
    '2D,ALL,unknown',
    [null, 'bad'],
  ]) {
    assert.deepEqual(
      parseCinemaDirectoryQuery({ technologies }).technologies,
      [],
    )
    assert.deepEqual(cinemaDirectoryQuery({ technologies }), {})
  }
  assert.deepEqual(query, before)
})
