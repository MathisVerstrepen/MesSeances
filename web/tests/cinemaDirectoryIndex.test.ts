import assert from 'node:assert/strict'
import test from 'node:test'
import type { Provider, Theater } from '../app/types/api.ts'
import {
  buildCinemaDirectoryIndex,
  cinemaDirectorySuggestions,
  directoryFacets,
  matchesDirectoryFilters,
  matchesDirectorySearch,
  normalizeDirectoryText,
  theaterInPlace,
} from '../app/utils/cinemaDirectoryIndex.ts'
import {
  activeDirectoryPlace,
  cinemaDirectoryQuery,
  directoryPlaceChanges,
  isDirectoryHome,
  parseCinemaDirectoryQuery,
} from '../app/utils/cinemaDirectoryQuery.ts'
import {
  departmentCodeFromPostalCode,
  FRENCH_DEPARTMENTS,
  FRENCH_REGIONS,
  frenchRegion,
  parisArrondissement,
  parisArrondissementLabel,
} from '../app/utils/frenchTerritories.ts'

function theater(
  id: string,
  overrides: Partial<Theater> & { provider?: Provider } = {},
): Theater {
  return {
    id,
    provider: 'ugc',
    slug: id,
    name: id,
    address: '',
    city: 'Bordeaux',
    city_slug: 'bordeaux',
    postal_code: '33000',
    available_dates: [],
    accepted_passes: [],
    latitude: 44.84,
    longitude: -0.58,
    ...overrides,
  }
}

const directory = [
  theater('utopia', {
    provider: 'ticketingcine',
    name: 'Utopia Bordeaux',
    technologies: [],
  }),
  theater('megarama', {
    provider: 'megarama',
    name: 'Megarama Bordeaux',
    postal_code: '33100',
    technologies: ['IMAX', '4DX'],
  }),
  theater('merignac', {
    provider: 'ugc',
    name: 'UGC Mérignac',
    city: 'Mérignac',
    city_slug: 'merignac',
    postal_code: '33700',
  }),
  theater('bastille', {
    provider: 'pathe',
    name: 'Pathé Bastille',
    city: 'Paris',
    city_slug: 'paris',
    postal_code: '75011',
    available_formats: ['2D', 'DOLBY'],
  }),
  theater('passy', {
    provider: 'ugc',
    name: 'UGC Passy',
    city: 'Paris',
    city_slug: 'paris',
    postal_code: '75116',
  }),
  theater('saint-denis', {
    provider: 'cgr',
    name: 'CGR Saint-Denis',
    city: 'Saint-Denis',
    city_slug: 'saint-denis',
    postal_code: '97400',
    latitude: null,
    longitude: null,
  }),
  theater('nowhere', { postal_code: 'inconnu', city_slug: 'bordeaux' }),
]
const index = buildCinemaDirectoryIndex(directory)

test('territories map postal codes to departments, regions and Paris arrondissements', () => {
  assert.equal(FRENCH_DEPARTMENTS.length, 100)
  for (const department of FRENCH_DEPARTMENTS)
    assert.ok(frenchRegion(department.region), department.code)
  assert.equal(new Set(FRENCH_REGIONS.map((region) => region.slug)).size, 14)
  assert.equal(departmentCodeFromPostalCode('33000'), '33')
  assert.equal(departmentCodeFromPostalCode('20000'), '20')
  assert.equal(departmentCodeFromPostalCode('97400'), '974')
  assert.equal(departmentCodeFromPostalCode('98000'), undefined)
  assert.equal(departmentCodeFromPostalCode('3300'), undefined)
  assert.equal(departmentCodeFromPostalCode(' 75011 '), '75')
  assert.equal(parisArrondissement('75011'), 11)
  assert.equal(parisArrondissement('75116'), 16)
  assert.equal(parisArrondissement('75000'), undefined)
  assert.equal(parisArrondissement('75021'), undefined)
  assert.equal(parisArrondissement('69001'), undefined)
  assert.equal(parisArrondissementLabel(1), 'Paris 1er')
  assert.equal(parisArrondissementLabel(11), 'Paris 11e')
})

test('normalization folds accents, case, punctuation and saint abbreviations', () => {
  assert.equal(normalizeDirectoryText('  Mérignac '), 'merignac')
  assert.equal(normalizeDirectoryText('St-Étienne'), 'saint etienne')
  assert.equal(normalizeDirectoryText('Côte-d’Or'), 'cote d or')
  assert.equal(normalizeDirectoryText('Ste Foy'), 'sainte foy')
})

test('index aggregates cities, departments, regions and arrondissements without inventing locations', () => {
  const before = structuredClone(directory)
  assert.deepEqual(
    index.cities.map((city) => [city.slug, city.count, city.departmentCode]),
    [
      ['bordeaux', 3, '33'],
      ['paris', 2, '75'],
      ['merignac', 1, '33'],
      ['saint-denis', 1, '974'],
    ],
  )
  const saintDenis = index.citiesBySlug.get('saint-denis')
  assert.equal(saintDenis?.latitude, null)
  assert.equal(saintDenis?.departmentName, 'La Réunion')
  assert.deepEqual(
    index.departments.map((department) => [department.code, department.count]),
    [
      ['33', 3],
      ['974', 1],
      ['75', 2],
    ],
  )
  assert.deepEqual(
    index.regions.map((region) => [
      region.slug,
      region.count,
      region.departmentCount,
    ]),
    [
      ['ile-de-france', 2, 1],
      ['nouvelle-aquitaine', 3, 1],
      ['outre-mer', 1, 1],
    ],
  )
  assert.deepEqual(
    index.arrondissements.map((row) => [row.number, row.count]),
    [
      [11, 1],
      [16, 1],
    ],
  )
  assert.equal(index.departmentOf.has('nowhere'), false)
  assert.deepEqual(directory, before)
})

test('places filter by arrondissement, city slug, department and region', () => {
  const ids = (place: Parameters<typeof theaterInPlace>[2]) =>
    directory
      .filter((item) => theaterInPlace(index, item, place))
      .map((item) => item.id)
  assert.deepEqual(ids({ kind: 'arrondissement', number: 11 }), ['bastille'])
  assert.deepEqual(ids({ kind: 'city', slug: 'paris' }), ['bastille', 'passy'])
  assert.deepEqual(ids({ kind: 'department', code: '33' }), [
    'utopia',
    'megarama',
    'merignac',
  ])
  assert.deepEqual(ids({ kind: 'region', slug: 'outre-mer' }), ['saint-denis'])
  assert.deepEqual(ids({ kind: 'region', slug: 'corse' }), [])
})

test('search matches every word across display name, city and postal code', () => {
  const ids = (query: string) =>
    directory
      .filter((item) => matchesDirectorySearch(index, item, query))
      .map((item) => item.id)
  assert.deepEqual(ids('merignac'), ['merignac'])
  assert.deepEqual(ids('ugc paris'), ['passy'])
  assert.deepEqual(ids('75011'), ['bastille'])
  assert.equal(ids('  ').length, directory.length)
  assert.deepEqual(ids('lyon'), [])
})

test('filters combine chains and any-of technologies; facets preview the other filter', () => {
  assert.equal(matchesDirectoryFilters(directory[1]!, [], ['IMAX']), true)
  assert.equal(matchesDirectoryFilters(directory[1]!, ['ugc'], []), false)
  assert.equal(
    matchesDirectoryFilters(directory[3]!, [], ['DOLBY', 'IMAX']),
    true,
  )
  assert.equal(
    matchesDirectoryFilters(directory[0]!, ['independants'], []),
    true,
  )
  const facets = directoryFacets(directory, ['ugc'], ['IMAX'])
  const ugc = facets.chains.find((facet) => facet.value === 'ugc')
  assert.deepEqual(ugc, {
    value: 'ugc',
    label: 'UGC',
    count: 0,
    selected: true,
  })
  assert.equal(
    facets.chains.find((facet) => facet.value === 'megarama')?.count,
    1,
  )
  assert.deepEqual(
    facets.technologies.map((facet) => [facet.value, facet.count]),
    [['IMAX', 0]],
  )
})

test('numeric suggestions put department and arrondissement before cities', () => {
  const department = cinemaDirectorySuggestions(index, '33')
  assert.deepEqual(
    department.map((group) => group.key),
    ['departments', 'cities', 'theaters', 'search'],
  )
  assert.deepEqual(department[0]?.items[0], {
    kind: 'department',
    code: '33',
    label: 'Gironde (33)',
    detail: 'Nouvelle-Aquitaine',
    count: 3,
  })
  const arrondissement = cinemaDirectorySuggestions(index, '75011')
  assert.equal(arrondissement[0]?.key, 'arrondissements')
  assert.deepEqual(arrondissement[0]?.items, [
    { kind: 'arrondissement', number: 11, label: 'Paris 11e', count: 1 },
  ])
})

test('text suggestions cover cities, regions, chains, formats, theaters and free search', () => {
  const city = cinemaDirectorySuggestions(index, 'bord')
  assert.equal(city[0]?.key, 'cities')
  assert.equal(city[0]?.items[0]?.label, 'Bordeaux')
  assert.equal(city.at(-1)?.items[0]?.kind, 'search')

  const imax = cinemaDirectorySuggestions(index, 'imax')
  assert.deepEqual(imax.find((group) => group.key === 'brands')?.items, [
    { kind: 'technology', technology: 'IMAX', label: 'Salles IMAX', count: 1 },
  ])
  const chain = cinemaDirectorySuggestions(index, 'megar')
  assert.ok(
    chain
      .find((group) => group.key === 'brands')
      ?.items.some(
        (item) => item.kind === 'chain' && item.chain === 'megarama',
      ),
  )
  const region = cinemaDirectorySuggestions(index, 'aquitaine')
  assert.equal(
    region.find((group) => group.key === 'regions')?.items[0]?.label,
    'Nouvelle-Aquitaine',
  )
  const theaters = cinemaDirectorySuggestions(index, 'utopia')
  assert.equal(
    theaters.find((group) => group.key === 'theaters')?.items[0]?.label,
    'Utopia Bordeaux',
  )
  assert.deepEqual(cinemaDirectorySuggestions(index, '   '), [])
})

test('directory query owns place, radius and legacy keys while dropping invalid values', () => {
  const parsed = parseCinemaDirectoryQuery({
    region: 'nouvelle-aquitaine',
    departement: '33',
    ville: 'bordeaux',
    arrondissement: '11',
    rayon: '25',
    technologies: 'imax',
    chains: 'UGC',
  })
  assert.equal(parsed.region, 'nouvelle-aquitaine')
  assert.equal(parsed.department, '33')
  assert.equal(parsed.city, 'bordeaux')
  assert.equal(parsed.arrondissement, 11)
  assert.equal(parsed.radius, 25)
  assert.deepEqual(parsed.technologies, ['IMAX'])
  assert.deepEqual(parsed.chains, ['ugc'])
  assert.deepEqual(activeDirectoryPlace(parsed), {
    kind: 'arrondissement',
    number: 11,
  })

  const invalid = parseCinemaDirectoryQuery({
    region: 'atlantide',
    departement: '99',
    ville: 'Bad Slug',
    arrondissement: '21',
    rayon: '7',
  })
  assert.equal(activeDirectoryPlace(invalid), null)
  assert.equal(invalid.radius, 10)
  assert.equal(isDirectoryHome(invalid), true)
  assert.deepEqual(
    cinemaDirectoryQuery({ region: 'atlantide', rayon: '7', tag: 'kept' }),
    { tag: 'kept' },
  )
  assert.deepEqual(cinemaDirectoryQuery({ rayon: '25' }), {})
  assert.deepEqual(cinemaDirectoryQuery({ location: 'nearby', rayon: '25' }), {
    location: 'nearby',
    rayon: '25',
  })
})

test('choosing a place replaces the previous place and free search but keeps filters and view', () => {
  const query = {
    q: 'utopia',
    region: 'bretagne',
    technologies: 'IMAX',
    view: 'map',
  }
  assert.deepEqual(
    cinemaDirectoryQuery(
      query,
      directoryPlaceChanges({ kind: 'city', slug: 'bordeaux' }),
    ),
    { technologies: 'IMAX', view: 'map', ville: 'bordeaux' },
  )
  assert.deepEqual(
    cinemaDirectoryQuery(query, directoryPlaceChanges('nearby')),
    { technologies: 'IMAX', view: 'map', location: 'nearby' },
  )
  assert.deepEqual(cinemaDirectoryQuery(query, directoryPlaceChanges(null)), {
    technologies: 'IMAX',
    view: 'map',
  })
  assert.equal(isDirectoryHome(parseCinemaDirectoryQuery({})), true)
  assert.equal(
    isDirectoryHome(parseCinemaDirectoryQuery({ view: 'map' })),
    false,
  )
  assert.equal(
    isDirectoryHome(parseCinemaDirectoryQuery({ technologies: 'IMAX' })),
    false,
  )
})
