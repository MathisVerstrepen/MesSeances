import assert from 'node:assert/strict'
import test from 'node:test'
import type { Provider, Theater } from '../app/types/api.ts'
import { chainDisplayProvider, publicChain } from '../app/utils/cinemaChains.ts'
import {
  cinemaChainLabel,
  cinemaDirectoryChains,
  filterCinemaDirectory,
  normalizeCinemaChains,
  toggleCinemaChain,
} from '../app/utils/cinemaDirectoryChains.ts'
import {
  parseCinemaDirectoryQuery,
  cinemaDirectoryQuery,
} from '../app/utils/cinemaDirectoryQuery.ts'
import {
  parseStatisticsQuery,
  statisticsChainLabels,
} from '../app/utils/statistics.ts'

function theater(id: string, provider: Provider): Theater {
  return {
    id,
    slug: id,
    provider,
    name: id,
    address: '',
    city: 'Ville',
    city_slug: 'ville',
    postal_code: '00000',
    available_dates: [],
    accepted_passes: [],
  }
}

const theaters = [
  theater('ticketingcine-EMS0963', 'ticketingcine'),
  theater('scare-1854', 'scare'),
  theater('webedia-P0137', 'webedia'),
  theater('ugc-1', 'ugc'),
]

test('independent providers form one public chain', () => {
  for (const provider of ['ticketingcine', 'scare', 'webedia'] as const) {
    assert.equal(publicChain(provider), 'independants')
  }
  assert.equal(publicChain('ugc'), 'ugc')
  assert.equal(chainDisplayProvider('independants'), 'ticketingcine')
  assert.equal(cinemaChainLabel('independants'), 'Cinémas indépendants')
  assert.deepEqual(cinemaDirectoryChains(theaters), ['independants', 'ugc'])
})

test('the chain filter selects every independent cinema and reads old links', () => {
  assert.deepEqual(
    filterCinemaDirectory(theaters, '', ['independants']).map((t) => t.id),
    ['ticketingcine-EMS0963', 'scare-1854', 'webedia-P0137'],
  )
  assert.deepEqual(normalizeCinemaChains(['scare', 'Webedia', 'ugc']), [
    'ugc',
    'independants',
  ])
  assert.deepEqual(toggleCinemaChain(['independants'], 'independants'), [])
  const state = parseCinemaDirectoryQuery({ chains: 'ticketingcine,ugc' })
  assert.deepEqual(state.chains, ['ugc', 'independants'])
  assert.equal(
    cinemaDirectoryQuery({}, { chains: ['independants'] }).chains,
    'independants',
  )
})

test('statistics accept and label the independent chain', () => {
  assert.equal(statisticsChainLabels.independants, 'Cinémas indépendants')
  assert.equal(statisticsChainLabels.scare, 'SCARE')
  assert.equal(
    parseStatisticsQuery({ chain: 'independants' }).query.chain,
    'independants',
  )
})
