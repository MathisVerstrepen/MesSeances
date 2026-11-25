import type { CinemaChain, IndependentProvider, Provider } from '../types/api'

// TicketingCiné, SCARE, Webedia and Cotecine serve independent cinemas, not chains:
// public filters, map legend and statistics group them as one chain.
export const INDEPENDENT_CHAIN = 'independants'
export const INDEPENDENT_CHAIN_LABEL = 'Cinémas indépendants'

const independentProviders: ReadonlySet<string> = new Set([
  'ticketingcine',
  'scare',
  'webedia',
  'cotecine',
] satisfies IndependentProvider[])

export function isIndependentProvider(
  provider: Provider,
): provider is IndependentProvider {
  return independentProviders.has(provider)
}

export function publicChain(provider: Provider): CinemaChain {
  return isIndependentProvider(provider) ? INDEPENDENT_CHAIN : provider
}

/** Provider whose brand logo represents the chain (the INDÉ badge). */
export function chainDisplayProvider(chain: CinemaChain): Provider {
  return chain === INDEPENDENT_CHAIN ? 'ticketingcine' : chain
}
