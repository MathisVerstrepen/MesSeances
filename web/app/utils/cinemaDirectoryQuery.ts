import type { LocationQuery } from 'vue-router'
import type { CinemaChain } from '../types/api'
import { normalizeCinemaChains } from './cinemaDirectoryChains.ts'
import {
  DEFAULT_DIRECTORY_RADIUS,
  DIRECTORY_RADIUS_OPTIONS,
  type DirectoryPlace,
  type DirectoryRadius,
} from './cinemaDirectoryIndex.ts'
import { normalizeCinemaTechnologies } from './cinemaDirectoryTechnologies.ts'
import type { FormatBrand } from './formats.ts'
import { frenchDepartment, frenchRegion } from './frenchTerritories.ts'
import { mergeOwnedQuery, singularQueryValue } from './routeQuery.ts'

export interface CinemaDirectoryState {
  search: string
  view: 'list' | 'map'
  location: 'city' | 'nearby'
  chains: CinemaChain[]
  technologies: FormatBrand[]
  region: string
  department: string
  city: string
  arrondissement: number | null
  radius: DirectoryRadius
}

const OWNED_KEYS = [
  'q',
  'view',
  'location',
  'chains',
  'technologies',
  'region',
  'departement',
  'ville',
  'arrondissement',
  'rayon',
] as const

const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

function listValues(value: LocationQuery[string] | undefined): string[] {
  const values = Array.isArray(value) ? value : [value]
  return values.flatMap((item) => item?.split(',') ?? [])
}

function parseArrondissement(value: string | undefined): number | null {
  if (!value || !/^\d{1,2}$/.test(value)) return null
  const number = Number(value)
  return number >= 1 && number <= 20 ? number : null
}

function parseRadius(value: string | undefined): DirectoryRadius {
  return (
    DIRECTORY_RADIUS_OPTIONS.find((radius) => String(radius) === value) ??
    DEFAULT_DIRECTORY_RADIUS
  )
}

export function parseCinemaDirectoryQuery(
  query: LocationQuery,
): CinemaDirectoryState {
  const region = singularQueryValue(query.region)?.trim() ?? ''
  const department = singularQueryValue(query.departement)?.trim() ?? ''
  const city = singularQueryValue(query.ville)?.trim() ?? ''
  return {
    search: singularQueryValue(query.q)?.trim() ?? '',
    view: singularQueryValue(query.view) === 'map' ? 'map' : 'list',
    location:
      singularQueryValue(query.location) === 'nearby' ? 'nearby' : 'city',
    chains: normalizeCinemaChains(listValues(query.chains)),
    technologies: normalizeCinemaTechnologies(listValues(query.technologies)),
    region: frenchRegion(region) ? region : '',
    department: frenchDepartment(department) ? department : '',
    city: SLUG.test(city) ? city : '',
    arrondissement: parseArrondissement(
      singularQueryValue(query.arrondissement)?.trim(),
    ),
    radius: parseRadius(singularQueryValue(query.rayon)?.trim()),
  }
}

export function cinemaDirectoryQuery(
  query: LocationQuery,
  changes: Partial<CinemaDirectoryState> = {},
): LocationQuery {
  const state = { ...parseCinemaDirectoryQuery(query), ...changes }
  const nearby = state.location === 'nearby'
  return mergeOwnedQuery(query, OWNED_KEYS, {
    q: state.search.trim() || undefined,
    view: state.view === 'map' ? 'map' : undefined,
    location: nearby ? 'nearby' : undefined,
    chains: normalizeCinemaChains(state.chains).join(',') || undefined,
    technologies:
      normalizeCinemaTechnologies(state.technologies).join(',') || undefined,
    region: state.region || undefined,
    departement: state.department || undefined,
    ville: state.city || undefined,
    arrondissement: state.arrondissement
      ? String(state.arrondissement)
      : undefined,
    rayon:
      nearby && state.radius !== DEFAULT_DIRECTORY_RADIUS
        ? String(state.radius)
        : undefined,
  })
}

/** The single active place; nearby wins, then the most specific territory. */
export function activeDirectoryPlace(
  state: CinemaDirectoryState,
): DirectoryPlace | null {
  if (state.arrondissement)
    return { kind: 'arrondissement', number: state.arrondissement }
  if (state.city) return { kind: 'city', slug: state.city }
  if (state.department) return { kind: 'department', code: state.department }
  if (state.region) return { kind: 'region', slug: state.region }
  return null
}

const NO_PLACE: Pick<
  CinemaDirectoryState,
  'search' | 'location' | 'region' | 'department' | 'city' | 'arrondissement'
> = {
  search: '',
  location: 'city',
  region: '',
  department: '',
  city: '',
  arrondissement: null,
}

/** Changes that replace the current place (and free-text search) with one place. */
export function directoryPlaceChanges(
  place: DirectoryPlace | 'nearby' | null,
): Partial<CinemaDirectoryState> {
  if (place === null) return { ...NO_PLACE }
  if (place === 'nearby') return { ...NO_PLACE, location: 'nearby' }
  switch (place.kind) {
    case 'arrondissement':
      return { ...NO_PLACE, arrondissement: place.number }
    case 'city':
      return { ...NO_PLACE, city: place.slug }
    case 'department':
      return { ...NO_PLACE, department: place.code }
    case 'region':
      return { ...NO_PLACE, region: place.slug }
  }
}

export function isDirectoryHome(state: CinemaDirectoryState): boolean {
  return (
    !state.search &&
    state.view !== 'map' &&
    state.location !== 'nearby' &&
    state.chains.length === 0 &&
    state.technologies.length === 0 &&
    activeDirectoryPlace(state) === null
  )
}

/** Identity of a directory view; filters, radius and display mode stay in place. */
export function directoryScopeKey(query: LocationQuery): string {
  const state = parseCinemaDirectoryQuery(query)
  return JSON.stringify([
    isDirectoryHome(state),
    state.location,
    activeDirectoryPlace(state),
    state.search,
  ])
}
