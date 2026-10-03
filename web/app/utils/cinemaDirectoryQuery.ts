import type { LocationQuery } from 'vue-router'
import type { Provider } from '../types/api'
import { normalizeCinemaChains } from './cinemaDirectoryChains.ts'
import { mergeOwnedQuery, singularQueryValue } from './routeQuery.ts'

export interface CinemaDirectoryState {
  search: string
  view: 'list' | 'map'
  location: 'city' | 'nearby'
  chains: Provider[]
}

export function parseCinemaDirectoryQuery(
  query: LocationQuery,
): CinemaDirectoryState {
  const rawChains = Array.isArray(query.chains) ? query.chains : [query.chains]
  return {
    search: singularQueryValue(query.q)?.trim() ?? '',
    view: singularQueryValue(query.view) === 'map' ? 'map' : 'list',
    location:
      singularQueryValue(query.location) === 'nearby' ? 'nearby' : 'city',
    chains: normalizeCinemaChains(
      rawChains.flatMap((value) => value?.split(',') ?? []),
    ),
  }
}

export function cinemaDirectoryQuery(
  query: LocationQuery,
  changes: Partial<CinemaDirectoryState> = {},
): LocationQuery {
  const state = { ...parseCinemaDirectoryQuery(query), ...changes }
  return mergeOwnedQuery(query, ['q', 'view', 'location', 'chains'], {
    q: state.search.trim() || undefined,
    view: state.view === 'map' ? 'map' : undefined,
    location: state.location === 'nearby' ? 'nearby' : undefined,
    chains: normalizeCinemaChains(state.chains).join(',') || undefined,
  })
}
