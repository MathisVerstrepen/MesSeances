import type { LocationQuery } from 'vue-router'
import { mergeOwnedQuery, singularQueryValue } from './routeQuery.ts'

export interface CinemaDirectoryState {
  search: string
  view: 'list' | 'map'
  location: 'city' | 'nearby'
}

export function parseCinemaDirectoryQuery(query: LocationQuery): CinemaDirectoryState {
  return {
    search: singularQueryValue(query.q)?.trim() ?? '',
    view: singularQueryValue(query.view) === 'map' ? 'map' : 'list',
    location: singularQueryValue(query.location) === 'nearby' ? 'nearby' : 'city'
  }
}

export function cinemaDirectoryQuery(query: LocationQuery, changes: Partial<CinemaDirectoryState> = {}): LocationQuery {
  const state = { ...parseCinemaDirectoryQuery(query), ...changes }
  return mergeOwnedQuery(query, ['q', 'view', 'location'], {
    q: state.search.trim() || undefined,
    view: state.view === 'map' ? 'map' : undefined,
    location: state.location === 'nearby' ? 'nearby' : undefined
  })
}
