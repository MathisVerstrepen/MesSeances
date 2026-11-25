import type { CinemaChain, Provider, Theater } from '../types/api'
import {
  INDEPENDENT_CHAIN,
  INDEPENDENT_CHAIN_LABEL,
  publicChain,
} from './cinemaChains.ts'
import { THEATER_PROVIDER_LABELS } from './theaterMap.ts'
import type { FormatBrand } from './formats.ts'

// SAFETY: the existing label record exhaustively covers Provider identifiers.
const providers = Object.keys(THEATER_PROVIDER_LABELS) as Provider[]
const chains = [...new Set(providers.map(publicChain))]

export function cinemaChainLabel(chain: CinemaChain): string {
  return chain === INDEPENDENT_CHAIN
    ? INDEPENDENT_CHAIN_LABEL
    : THEATER_PROVIDER_LABELS[chain]
}

// Accepts public chains and, for old links, the providers grouped under one.
export function normalizeCinemaChains(
  values: readonly string[],
): CinemaChain[] {
  const selected = new Set<string>()
  for (const value of values) {
    const key = value.trim().toLowerCase()
    const provider = providers.find((candidate) => candidate === key)
    selected.add(provider ? publicChain(provider) : key)
  }
  return chains.filter((chain) => selected.has(chain))
}

export function cinemaDirectoryChains(
  theaters: readonly Theater[],
): CinemaChain[] {
  const present = new Set(
    theaters.map((theater) => publicChain(theater.provider)),
  )
  return chains
    .filter((chain) => present.has(chain))
    .sort((left, right) =>
      cinemaChainLabel(left).localeCompare(cinemaChainLabel(right), 'fr-FR'),
    )
}

export function toggleCinemaChain(
  selected: readonly CinemaChain[],
  chain: CinemaChain,
): CinemaChain[] {
  return normalizeCinemaChains(
    selected.includes(chain)
      ? selected.filter((value) => value !== chain)
      : [...selected, chain],
  )
}

export function filterCinemaDirectory(
  theaters: readonly Theater[],
  search: string,
  chains: readonly CinemaChain[],
  technologies: readonly FormatBrand[] = [],
): Theater[] {
  const normalizedSearch = search.trim().toLocaleLowerCase('fr-FR')
  const selected = new Set(chains)
  return theaters.filter((theater) => {
    if (selected.size > 0 && !selected.has(publicChain(theater.provider)))
      return false
    if (
      technologies.length > 0 &&
      !technologies.some((technology) =>
        (theater.technologies ?? theater.available_formats)?.includes(
          technology,
        ),
      )
    )
      return false
    const searchable = `${theater.name} ${theater.city}`.toLocaleLowerCase(
      'fr-FR',
    )
    return !normalizedSearch || searchable.includes(normalizedSearch)
  })
}
