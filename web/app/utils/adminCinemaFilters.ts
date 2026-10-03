import type { LocationQuery } from 'vue-router'
import type { AdminTheatersQuery, Provider } from '../types/api.ts'
import {
  mergeOwnedQuery,
  positiveSafeInteger,
  singularQueryValue,
} from './routeQuery.ts'

export const ADMIN_CINEMA_PAGE_SIZE = 20
export const ADMIN_CINEMA_SEARCH_DELAY = 350
export const cinemaProviderLabels = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  cgr: 'CGR',
  megarama: 'Megarama',
  cineville: 'Cinéville',
  mk2: 'MK2',
  cinewest: 'Cinewest',
  grandecran: 'Grand Ecran',
  noecinemas: 'Noé Cinémas',
} satisfies Record<Provider, string>

export interface AdminCinemaRouteState {
  page: number
  q: string
  provider: Provider | ''
}

export function normalizeCinemaSearch(value: string): string {
  return Array.from(value.replace(/[\p{Cc}\p{Cs}]/gu, '').trim())
    .slice(0, 1024)
    .join('')
}

export function parseCinemaProvider(value: string | undefined): Provider | '' {
  return (
    Object.keys(cinemaProviderLabels).find(
      (provider): provider is Provider => provider === value,
    ) ?? ''
  )
}

export function parseCinemaRoute(query: LocationQuery): AdminCinemaRouteState {
  const requested = positiveSafeInteger(singularQueryValue(query.page)) ?? 1
  return {
    page:
      requested <= Math.floor(2_147_483_647 / ADMIN_CINEMA_PAGE_SIZE) + 1
        ? requested
        : 1,
    q: normalizeCinemaSearch(singularQueryValue(query.q) ?? ''),
    provider: parseCinemaProvider(singularQueryValue(query.provider)),
  }
}

export function cinemaRouteQuery(
  state: AdminCinemaRouteState,
  current: LocationQuery,
): LocationQuery {
  return mergeOwnedQuery(current, ['page', 'q', 'provider'], {
    page: state.page === 1 ? undefined : String(state.page),
    q: state.q || undefined,
    provider: state.provider || undefined,
  })
}

export function cinemaApiQuery(
  state: AdminCinemaRouteState,
): AdminTheatersQuery {
  const query: AdminTheatersQuery = {
    limit: ADMIN_CINEMA_PAGE_SIZE,
    offset: (state.page - 1) * ADMIN_CINEMA_PAGE_SIZE,
  }
  if (state.q) query.q = state.q
  if (state.provider) query.provider = state.provider
  return query
}
