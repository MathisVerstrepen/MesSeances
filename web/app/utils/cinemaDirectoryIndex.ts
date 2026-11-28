import type { CinemaChain, Theater } from '../types/api'
import { publicChain } from './cinemaChains.ts'
import { cinemaChainLabel } from './cinemaDirectoryChains.ts'
import { cinemaTechnologyOptions } from './cinemaDirectoryTechnologies.ts'
import type { FormatBrand } from './formats.ts'
import {
  departmentCodeFromPostalCode,
  frenchDepartment,
  FRENCH_REGIONS,
  parisArrondissement,
  parisArrondissementLabel,
  type FrenchDepartment,
  type FrenchRegion,
} from './frenchTerritories.ts'
import { theaterDisplayName } from './theaterDisplayName.ts'

export const DIRECTORY_RADIUS_OPTIONS = [5, 10, 25, 50, 100] as const
export type DirectoryRadius = (typeof DIRECTORY_RADIUS_OPTIONS)[number]
export const DEFAULT_DIRECTORY_RADIUS: DirectoryRadius = 10

export interface DirectoryCity {
  slug: string
  name: string
  departmentCode: string | undefined
  departmentName: string
  count: number
  latitude: number | null
  longitude: number | null
  postalCodes: string[]
  search: string
}

export interface DirectoryDepartment extends FrenchDepartment {
  count: number
  search: string
}

export interface DirectoryRegion extends FrenchRegion {
  count: number
  departmentCount: number
  search: string
}

export interface DirectoryArrondissement {
  number: number
  label: string
  count: number
}

export interface CinemaDirectoryIndex {
  theaters: readonly Theater[]
  cities: DirectoryCity[]
  citiesBySlug: Map<string, DirectoryCity>
  departments: DirectoryDepartment[]
  departmentsByCode: Map<string, DirectoryDepartment>
  regions: DirectoryRegion[]
  regionsBySlug: Map<string, DirectoryRegion>
  arrondissements: DirectoryArrondissement[]
  chains: Map<CinemaChain, number>
  technologies: Map<FormatBrand, number>
  departmentOf: Map<string, string>
  searchOf: Map<string, string>
}

export type DirectoryPlace =
  | { kind: 'arrondissement'; number: number }
  | { kind: 'city'; slug: string }
  | { kind: 'department'; code: string }
  | { kind: 'region'; slug: string }

export type DirectorySuggestion =
  | { kind: 'arrondissement'; number: number; label: string; count: number }
  | {
      kind: 'city'
      slug: string
      label: string
      detail: string
      count: number
    }
  | {
      kind: 'department'
      code: string
      label: string
      detail: string
      count: number
    }
  | { kind: 'region'; slug: string; label: string; count: number }
  | { kind: 'chain'; chain: CinemaChain; label: string; count: number }
  | {
      kind: 'technology'
      technology: FormatBrand
      label: string
      count: number
    }
  | { kind: 'theater'; theater: Theater; label: string; detail: string }
  | { kind: 'search'; query: string; label: string }

export interface DirectorySuggestionGroup {
  key: string
  title: string
  items: DirectorySuggestion[]
}

/** Accent-, case- and punctuation-insensitive form used for directory matching. */
export function normalizeDirectoryText(value: string): string {
  return value
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[-'’,.()/]/g, ' ')
    .replace(/\bst\b/g, 'saint')
    .replace(/\bste\b/g, 'sainte')
    .replace(/\s+/g, ' ')
    .trim()
}

export function theaterTechnologies(theater: Theater): FormatBrand[] {
  return cinemaTechnologyOptions
    .map((option) => option.value)
    .filter((value) =>
      (theater.technologies ?? theater.available_formats ?? []).includes(value),
    )
}

function increment<K>(map: Map<K, number>, key: K) {
  map.set(key, (map.get(key) ?? 0) + 1)
}

function compareFrench(left: string, right: string) {
  return left.localeCompare(right, 'fr-FR')
}

export function buildCinemaDirectoryIndex(
  theaters: readonly Theater[],
): CinemaDirectoryIndex {
  const departmentOf = new Map<string, string>()
  const searchOf = new Map<string, string>()
  const chains = new Map<CinemaChain, number>()
  const technologies = new Map<FormatBrand, number>()
  const departmentCounts = new Map<string, number>()
  const arrondissementCounts = new Map<number, number>()
  const cityRows = new Map<
    string,
    {
      name: string
      count: number
      departments: Map<string, number>
      latitude: number
      longitude: number
      located: number
      postalCodes: Set<string>
    }
  >()

  for (const theater of theaters) {
    const code = departmentCodeFromPostalCode(theater.postal_code)
    if (code) {
      departmentOf.set(theater.id, code)
      increment(departmentCounts, code)
    }
    const arrondissement = parisArrondissement(theater.postal_code)
    if (arrondissement) increment(arrondissementCounts, arrondissement)
    searchOf.set(
      theater.id,
      normalizeDirectoryText(
        `${theaterDisplayName(theater)} ${theater.city} ${theater.postal_code}`,
      ),
    )
    increment(chains, publicChain(theater.provider))
    for (const technology of theaterTechnologies(theater))
      increment(technologies, technology)

    if (!theater.city_slug) continue
    const city = cityRows.get(theater.city_slug) ?? {
      name: theater.city,
      count: 0,
      departments: new Map<string, number>(),
      latitude: 0,
      longitude: 0,
      located: 0,
      postalCodes: new Set<string>(),
    }
    city.count++
    if (code) increment(city.departments, code)
    if (theater.postal_code) city.postalCodes.add(theater.postal_code)
    const latitude = theater.latitude ?? Number.NaN
    const longitude = theater.longitude ?? Number.NaN
    if (Number.isFinite(latitude) && Number.isFinite(longitude)) {
      city.latitude += latitude
      city.longitude += longitude
      city.located++
    }
    cityRows.set(theater.city_slug, city)
  }

  const cities: DirectoryCity[] = [...cityRows.entries()].map(([slug, row]) => {
    const departmentCode = [...row.departments.entries()].sort(
      (left, right) => right[1] - left[1],
    )[0]?.[0]
    return {
      slug,
      name: row.name,
      departmentCode,
      departmentName: departmentCode
        ? (frenchDepartment(departmentCode)?.name ?? '')
        : '',
      count: row.count,
      latitude: row.located ? row.latitude / row.located : null,
      longitude: row.located ? row.longitude / row.located : null,
      postalCodes: [...row.postalCodes].sort(),
      search: normalizeDirectoryText(row.name),
    }
  })
  cities.sort(
    (left, right) =>
      right.count - left.count || compareFrench(left.name, right.name),
  )

  const departments: DirectoryDepartment[] = [...departmentCounts.entries()]
    .flatMap(([code, count]) => {
      const department = frenchDepartment(code)
      return department
        ? [
            {
              ...department,
              count,
              search: normalizeDirectoryText(department.name),
            },
          ]
        : []
    })
    .sort((left, right) => compareFrench(left.name, right.name))

  const regions: DirectoryRegion[] = FRENCH_REGIONS.flatMap((region) => {
    const members = departments.filter(
      (department) => department.region === region.slug,
    )
    const count = members.reduce((total, member) => total + member.count, 0)
    return count
      ? [
          {
            ...region,
            count,
            departmentCount: members.length,
            search: normalizeDirectoryText(region.name),
          },
        ]
      : []
  }).sort((left, right) => compareFrench(left.name, right.name))

  return {
    theaters,
    cities,
    citiesBySlug: new Map(cities.map((city) => [city.slug, city])),
    departments,
    departmentsByCode: new Map(
      departments.map((department) => [department.code, department]),
    ),
    regions,
    regionsBySlug: new Map(regions.map((region) => [region.slug, region])),
    arrondissements: [...arrondissementCounts.entries()]
      .sort((left, right) => left[0] - right[0])
      .map(([number, count]) => ({
        number,
        label: parisArrondissementLabel(number),
        count,
      })),
    chains,
    technologies,
    departmentOf,
    searchOf,
  }
}

export function theaterInPlace(
  index: CinemaDirectoryIndex,
  theater: Theater,
  place: DirectoryPlace,
): boolean {
  switch (place.kind) {
    case 'arrondissement':
      return parisArrondissement(theater.postal_code) === place.number
    case 'city':
      return theater.city_slug === place.slug
    case 'department':
      return index.departmentOf.get(theater.id) === place.code
    case 'region':
      return (
        frenchDepartment(index.departmentOf.get(theater.id) ?? '')?.region ===
        place.slug
      )
  }
}

export function matchesDirectorySearch(
  index: CinemaDirectoryIndex,
  theater: Theater,
  query: string,
): boolean {
  const words = normalizeDirectoryText(query).split(' ').filter(Boolean)
  if (words.length === 0) return true
  const searchable = index.searchOf.get(theater.id) ?? ''
  return words.every((word) => searchable.includes(word))
}

export function matchesDirectoryFilters(
  theater: Theater,
  chains: readonly CinemaChain[],
  technologies: readonly FormatBrand[],
): boolean {
  if (chains.length > 0 && !chains.includes(publicChain(theater.provider)))
    return false
  if (technologies.length === 0) return true
  const available = theaterTechnologies(theater)
  return technologies.some((technology) => available.includes(technology))
}

export interface DirectoryFacet<T extends string> {
  value: T
  label: string
  count: number
  selected: boolean
}

export interface DirectoryFacets {
  chains: DirectoryFacet<CinemaChain>[]
  technologies: DirectoryFacet<FormatBrand>[]
}

/** Facet counts honour the other facet so each count previews its result. */
export function directoryFacets(
  theaters: readonly Theater[],
  chains: readonly CinemaChain[],
  technologies: readonly FormatBrand[],
): DirectoryFacets {
  const chainCounts = new Map<CinemaChain, number>()
  const technologyCounts = new Map<FormatBrand, number>()
  for (const theater of theaters) {
    if (matchesDirectoryFilters(theater, [], technologies))
      increment(chainCounts, publicChain(theater.provider))
    if (matchesDirectoryFilters(theater, chains, []))
      for (const technology of theaterTechnologies(theater))
        increment(technologyCounts, technology)
  }
  for (const chain of chains)
    if (!chainCounts.has(chain)) chainCounts.set(chain, 0)
  for (const technology of technologies)
    if (!technologyCounts.has(technology)) technologyCounts.set(technology, 0)
  return {
    chains: [...chainCounts.entries()]
      .map(([value, count]) => ({
        value,
        label: cinemaChainLabel(value),
        count,
        selected: chains.includes(value),
      }))
      .sort(
        (left, right) =>
          right.count - left.count || compareFrench(left.label, right.label),
      ),
    technologies: cinemaTechnologyOptions.flatMap((option) => {
      const count = technologyCounts.get(option.value)
      return count === undefined
        ? []
        : [
            {
              value: option.value,
              label: option.label,
              count,
              selected: technologies.includes(option.value),
            },
          ]
    }),
  }
}

function matchRank(text: string, query: string): number {
  if (text.startsWith(query)) return 0
  if (text.includes(` ${query}`)) return 1
  if (text.includes(query)) return 2
  return Number.POSITIVE_INFINITY
}

const MAX_CITIES = 5
const MAX_THEATERS = 5

export function cinemaDirectorySuggestions(
  index: CinemaDirectoryIndex,
  rawQuery: string,
): DirectorySuggestionGroup[] {
  const query = normalizeDirectoryText(rawQuery)
  const trimmed = rawQuery.trim()
  if (!query) return []
  const groups: DirectorySuggestionGroup[] = []
  const numeric = /^\d{2,5}$/.test(query)

  const cityItem = (city: DirectoryCity): DirectorySuggestion => ({
    kind: 'city',
    slug: city.slug,
    label: city.name,
    detail: city.departmentCode
      ? `${city.departmentName} (${city.departmentCode})`
      : '',
    count: city.count,
  })
  const departmentItem = (
    department: DirectoryDepartment,
  ): DirectorySuggestion => ({
    kind: 'department',
    code: department.code,
    label: `${department.name} (${department.code})`,
    detail: index.regionsBySlug.get(department.region)?.name ?? '',
    count: department.count,
  })

  let cities: DirectoryCity[]
  let departments: DirectoryDepartment[] = []
  let theaters: Theater[]
  if (numeric) {
    const arrondissement =
      query.length === 5 ? parisArrondissement(query) : undefined
    const row = index.arrondissements.find(
      (item) => item.number === arrondissement,
    )
    if (row)
      groups.push({
        key: 'arrondissements',
        title: 'Arrondissement',
        items: [
          {
            kind: 'arrondissement',
            number: row.number,
            label: row.label,
            count: row.count,
          },
        ],
      })
    if (query.length <= 3) {
      const department = index.departmentsByCode.get(query)
      if (department) departments = [department]
    }
    cities = index.cities
      .filter((city) =>
        city.postalCodes.some((postalCode) => postalCode.startsWith(query)),
      )
      .slice(0, MAX_CITIES)
    theaters = index.theaters
      .filter((theater) => theater.postal_code.startsWith(query))
      .slice(0, MAX_THEATERS)
  } else {
    cities = index.cities
      .map((city) => ({ city, rank: matchRank(city.search, query) }))
      .filter((row) => row.rank <= 1)
      .sort((left, right) => left.rank - right.rank)
      .slice(0, MAX_CITIES)
      .map((row) => row.city)
    departments = index.departments
      .map((department) => ({
        department,
        rank: matchRank(department.search, query),
      }))
      .filter((row) => row.rank <= 1)
      .sort((left, right) => left.rank - right.rank)
      .slice(0, 2)
      .map((row) => row.department)
    const words = query.split(' ')
    theaters = index.theaters
      .filter((theater) => {
        const searchable = index.searchOf.get(theater.id) ?? ''
        return words.every((word) => searchable.includes(word))
      })
      .map((theater) => ({
        theater,
        rank: matchRank(
          normalizeDirectoryText(theaterDisplayName(theater)),
          query,
        ),
      }))
      .sort(
        (left, right) =>
          left.rank - right.rank ||
          compareFrench(
            theaterDisplayName(left.theater),
            theaterDisplayName(right.theater),
          ),
      )
      .slice(0, MAX_THEATERS)
      .map((row) => row.theater)
  }

  const cityGroup = cities.length
    ? { key: 'cities', title: 'Villes', items: cities.map(cityItem) }
    : null
  const departmentGroup = departments.length
    ? {
        key: 'departments',
        title: 'Départements',
        items: departments.map(departmentItem),
      }
    : null
  for (const group of numeric
    ? [departmentGroup, cityGroup]
    : [cityGroup, departmentGroup])
    if (group) groups.push(group)

  if (!numeric) {
    const regions = index.regions.filter(
      (region) => matchRank(region.search, query) <= (query.length > 3 ? 2 : 1),
    )
    if (regions.length)
      groups.push({
        key: 'regions',
        title: 'Régions',
        items: regions.slice(0, 2).map((region) => ({
          kind: 'region',
          slug: region.slug,
          label: region.name,
          count: region.count,
        })),
      })
    const brands: DirectorySuggestion[] = []
    if (query.length >= 2) {
      for (const option of cinemaTechnologyOptions) {
        const count = index.technologies.get(option.value)
        if (
          count &&
          (matchRank(normalizeDirectoryText(option.label), query) <= 1 ||
            normalizeDirectoryText(option.value).startsWith(query))
        )
          brands.push({
            kind: 'technology',
            technology: option.value,
            label: `Salles ${option.label}`,
            count,
          })
      }
      for (const [chain, count] of index.chains) {
        const label = cinemaChainLabel(chain)
        if (matchRank(normalizeDirectoryText(label), query) <= 1)
          brands.push({ kind: 'chain', chain, label, count })
      }
    }
    if (brands.length)
      groups.push({
        key: 'brands',
        title: 'Enseignes et formats',
        items: brands.slice(0, 3),
      })
  }

  if (theaters.length)
    groups.push({
      key: 'theaters',
      title: 'Cinémas',
      items: theaters.map((theater) => ({
        kind: 'theater',
        theater,
        label: theaterDisplayName(theater),
        detail: [theater.postal_code, theater.city].filter(Boolean).join(' '),
      })),
    })

  groups.push({
    key: 'search',
    title: 'Recherche libre',
    items: [
      {
        kind: 'search',
        query: trimmed,
        label: `Tous les cinémas correspondant à « ${trimmed} »`,
      },
    ],
  })
  return groups
}
