import type { Provider, Theater } from '../types/api'
import { THEATER_PROVIDER_LABELS } from './theaterMap.ts'
import type { FormatBrand } from './formats.ts'

// SAFETY: the existing label record exhaustively covers Provider identifiers.
const providers = Object.keys(THEATER_PROVIDER_LABELS) as Provider[]

export function normalizeCinemaChains(values: readonly string[]): Provider[] {
  const selected = new Set(values.map((value) => value.trim().toLowerCase()))
  return providers.filter((provider) => selected.has(provider))
}

export function cinemaDirectoryChains(
  theaters: readonly Theater[],
): Provider[] {
  const present = new Set(theaters.map((theater) => theater.provider))
  return providers
    .filter((provider) => present.has(provider))
    .sort((left, right) =>
      THEATER_PROVIDER_LABELS[left].localeCompare(
        THEATER_PROVIDER_LABELS[right],
        'fr-FR',
      ),
    )
}

export function toggleCinemaChain(
  selected: readonly Provider[],
  provider: Provider,
): Provider[] {
  return normalizeCinemaChains(
    selected.includes(provider)
      ? selected.filter((value) => value !== provider)
      : [...selected, provider],
  )
}

export function filterCinemaDirectory(
  theaters: readonly Theater[],
  search: string,
  chains: readonly Provider[],
  technologies: readonly FormatBrand[] = [],
): Theater[] {
  const normalizedSearch = search.trim().toLocaleLowerCase('fr-FR')
  const selected = new Set(chains)
  return theaters.filter((theater) => {
    if (selected.size > 0 && !selected.has(theater.provider)) return false
    if (
      technologies.length > 0 &&
      !technologies.some((technology) =>
        theater.available_formats?.includes(technology),
      )
    )
      return false
    const searchable = `${theater.name} ${theater.city}`.toLocaleLowerCase(
      'fr-FR',
    )
    return !normalizedSearch || searchable.includes(normalizedSearch)
  })
}
