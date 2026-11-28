import assert from 'node:assert/strict'
import test from 'node:test'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Provider, Theater } from '../app/types/api.ts'
import {
  cinemaDirectoryChains,
  filterCinemaDirectory,
  normalizeCinemaChains,
  toggleCinemaChain,
} from '../app/utils/cinemaDirectoryChains.ts'
import {
  cinemaDirectoryQuery,
  parseCinemaDirectoryQuery,
} from '../app/utils/cinemaDirectoryQuery.ts'
import { updateTheaterSelection } from '../app/utils/cinemaSelection.ts'
import { sortTheatersByDistance } from '../app/utils/theaterDistance.ts'

function theater(id: string, provider: Provider, city = 'Lille'): Theater {
  return {
    id,
    provider,
    slug: id,
    name: `${provider} Centre`,
    city,
    city_slug: city.toLowerCase(),
    address: '',
    postal_code: '',
    available_dates: [],
    accepted_passes: [],
    latitude: 50,
    longitude: 3,
  }
}
const directory = [
  theater('u', 'ugc'),
  theater('p', 'pathe'),
  theater('c', 'cgr', 'Paris'),
  theater('u-paris', 'ugc', 'Paris'),
]

test('chain query accepts all allowed identifiers, normalizes case, whitespace and duplicates', () => {
  const providers: Provider[] = [
    'ugc',
    'kinepolis',
    'pathe',
    'cgr',
    'megarama',
    'cineville',
    'mk2',
    'cinewest',
    'grandecran',
    'noecinemas',
    'veo',
  ]
  assert.deepEqual(normalizeCinemaChains(providers), providers)
  assert.deepEqual(
    parseCinemaDirectoryQuery({
      chains: [' CGR,ugc,unknown', null, 'UGC, pathe '],
    }).chains,
    ['ugc', 'pathe', 'cgr'],
  )
  const query = {
    chains: ['cgr,ugc', 'UGC'],
    view: 'map',
    flag: null,
    tag: ['one', 'two'],
  }
  const before = structuredClone(query)
  assert.deepEqual(cinemaDirectoryQuery(query), {
    chains: 'ugc,cgr',
    view: 'map',
    flag: null,
    tag: ['one', 'two'],
  })
  assert.deepEqual(query, before)
  assert.deepEqual(cinemaDirectoryQuery(query, { chains: [] }), {
    view: 'map',
    flag: null,
    tag: ['one', 'two'],
  })
  assert.equal(
    cinemaDirectoryQuery({}, { chains: ['cgr', 'ugc', 'cgr'] }).chains,
    'ugc,cgr',
  )
  for (const chains of [
    undefined,
    null,
    '',
    'all',
    'invalid',
    [null, 'invalid'],
  ]) {
    assert.deepEqual(cinemaDirectoryQuery({ chains }), {})
    assert.deepEqual(parseCinemaDirectoryQuery({ chains }).chains, [])
  }
})

test('chain options use full directory and selections are a union intersected with search', () => {
  const before = structuredClone(directory)
  assert.deepEqual(cinemaDirectoryChains(directory), ['cgr', 'pathe', 'ugc'])
  assert.deepEqual(cinemaDirectoryChains([]), [])
  assert.deepEqual(filterCinemaDirectory(directory, '', []), directory)
  assert.deepEqual(
    filterCinemaDirectory(directory, '', ['ugc']).map((row) => row.id),
    ['u', 'u-paris'],
  )
  assert.deepEqual(
    filterCinemaDirectory(directory, ' LILLE ', ['ugc', 'pathe']).map(
      (row) => row.id,
    ),
    ['u', 'p'],
  )
  assert.deepEqual(filterCinemaDirectory(directory, 'cgr', ['ugc']), [])
  assert.deepEqual(filterCinemaDirectory(directory, '', ['kinepolis']), [])
  assert.deepEqual(filterCinemaDirectory([], '', []), [])
  assert.deepEqual(directory, before)
})

test('selection toggles preserve other chains and removing final checkbox restores all', () => {
  const selected: Provider[] = ['ugc']
  const next = toggleCinemaChain(selected, 'cgr')
  assert.deepEqual(selected, ['ugc'])
  assert.deepEqual(next, ['ugc', 'cgr'])
  assert.deepEqual(toggleCinemaChain(next, 'ugc'), ['cgr'])
  const cleared = toggleCinemaChain(['cgr'], 'cgr')
  assert.deepEqual(cleared, [])
  assert.deepEqual(filterCinemaDirectory(directory, '', cleared), directory)
})

test('filtered list, nearby rows and bulk actions keep hidden favorites intact', () => {
  const favorites = ['c', 'u-paris', 'missing']
  const visible = filterCinemaDirectory(directory, 'Lille', ['ugc', 'pathe'])
  assert.deepEqual(
    sortTheatersByDistance(visible, { latitude: 50, longitude: 3 }).map(
      (row) => row.theater.id,
    ),
    ['p', 'u'],
  )
  const selected = updateTheaterSelection(favorites, visible, true)
  assert.deepEqual(selected, ['c', 'u-paris', 'missing', 'u', 'p'])
  assert.deepEqual(updateTheaterSelection(selected, visible, false), favorites)
  assert.deepEqual(favorites, ['c', 'u-paris', 'missing'])
})

test('chain URL history restores selection and retains search, modes and unrelated keys', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/cinemas', component: {} }],
  })
  await router.push('/cinemas?q=Lille&view=map&location=nearby&tag=kept')
  const query = () => router.currentRoute.value.query
  const modes = () => {
    const { search, view, location, chains, technologies } =
      parseCinemaDirectoryQuery(query())
    return { search, view, location, chains, technologies }
  }
  async function traverse(direction: 'back' | 'forward') {
    await new Promise<void>((resolve) => {
      const remove = router.afterEach(() => {
        remove()
        resolve()
      })
      router[direction]()
    })
  }
  await router.push({
    query: cinemaDirectoryQuery(query(), { chains: ['ugc'] }),
  })
  await router.push({
    query: cinemaDirectoryQuery(query(), { chains: ['ugc', 'cgr'] }),
  })
  await router.replace({
    query: cinemaDirectoryQuery(query(), { search: 'Paris' }),
  })
  await router.push({
    query: cinemaDirectoryQuery(query(), { view: 'list', location: 'city' }),
  })
  assert.deepEqual(modes(), {
    search: 'Paris',
    view: 'list',
    location: 'city',
    chains: ['ugc', 'cgr'],
    technologies: [],
  })
  await traverse('back')
  assert.deepEqual(modes(), {
    search: 'Paris',
    view: 'map',
    location: 'nearby',
    chains: ['ugc', 'cgr'],
    technologies: [],
  })
  await traverse('back')
  assert.deepEqual(modes(), {
    search: 'Lille',
    view: 'map',
    location: 'nearby',
    chains: ['ugc'],
    technologies: [],
  })
  await traverse('forward')
  assert.equal(query().tag, 'kept')
  await router.push({ query: cinemaDirectoryQuery(query(), { chains: [] }) })
  assert.equal(query().chains, undefined)
  assert.equal(query().q, 'Paris')
  assert.equal(query().view, 'map')
  assert.equal(query().location, 'nearby')
})
