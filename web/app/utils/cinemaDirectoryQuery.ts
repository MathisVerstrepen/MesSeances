import type { LocationQuery } from 'vue-router'
import type { Provider } from '../types/api'
import { normalizeCinemaChains } from './cinemaDirectoryChains.ts'
import { normalizeCinemaTechnologies } from './cinemaDirectoryTechnologies.ts'
import type { FormatBrand } from './formats.ts'
import { mergeOwnedQuery, singularQueryValue } from './routeQuery.ts'

export interface CinemaDirectoryState {
  search: string
  view: 'list' | 'map'
  location: 'city' | 'nearby'
  chains: Provider[]
  technologies: FormatBrand[]
}

export function parseCinemaDirectoryQuery(
  query: LocationQuery,
): CinemaDirectoryState {
  const rawChains = Array.isArray(query.chains) ? query.chains : [query.chains]
  const rawTechnologies = Array.isArray(query.technologies)
    ? query.technologies
    : [query.technologies]
  return {
    search: singularQueryValue(query.q)?.trim() ?? '',
    view: singularQueryValue(query.view) === 'map' ? 'map' : 'list',
    location:
      singularQueryValue(query.location) === 'nearby' ? 'nearby' : 'city',
    chains: normalizeCinemaChains(
      rawChains.flatMap((value) => value?.split(',') ?? []),
    ),
    technologies: normalizeCinemaTechnologies(
      rawTechnologies.flatMap((value) => value?.split(',') ?? []),
    ),
  }
}

export function cinemaDirectoryQuery(
  query: LocationQuery,
  changes: Partial<CinemaDirectoryState> = {},
): LocationQuery {
  const state = { ...parseCinemaDirectoryQuery(query), ...changes }
  return mergeOwnedQuery(
    query,
    ['q', 'view', 'location', 'chains', 'technologies'],
    {
      q: state.search.trim() || undefined,
      view: state.view === 'map' ? 'map' : undefined,
      location: state.location === 'nearby' ? 'nearby' : undefined,
      chains: normalizeCinemaChains(state.chains).join(',') || undefined,
      technologies:
        normalizeCinemaTechnologies(state.technologies).join(',') || undefined,
    },
  )
}
