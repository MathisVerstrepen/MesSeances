import type { Provider } from '../types/api'

export interface SafeBookingUrl {
  provider: Provider
  url: string
}

function isSafeCgrBooking(value: string): boolean {
  return value.length <= 2048
    && /^https:\/\/achat\.cgrcinemas\.fr\/[a-z0-9-]+\/r\/[1-9][0-9]*$/.test(value)
}

export function safeBookingUrl(raw: string | null | undefined, expectedProvider?: Provider | null): SafeBookingUrl | null {
  const value = raw?.trim()
  if (!value) return null

  try {
    const parsed = new URL(value)
    const hostname = parsed.hostname.toLowerCase()
    const provider: Provider | null = hostname === 'www.ugc.fr'
      ? 'ugc'
      : hostname === 'kinepolis.fr'
        ? 'kinepolis'
        : hostname === 's.pathe.fr'
          ? 'pathe'
          : hostname === 'achat.cgrcinemas.fr' ? 'cgr' : null
    const isSafePatheBooking = provider !== 'pathe' || (
      !parsed.search
      && !parsed.hash
      && parsed.href === value
      && /^\/fr\/[A-Za-z0-9_-]*S[1-9][0-9]*\/booking$/.test(parsed.pathname)
    )
    const isSafeCgrBookingUrl = provider !== 'cgr' || isSafeCgrBooking(value)

    if (
      parsed.protocol !== 'https:'
      || !provider
      || (expectedProvider && expectedProvider !== provider)
      || parsed.username
      || parsed.password
      || parsed.port
      || !isSafePatheBooking
      || !isSafeCgrBookingUrl
    ) return null

    return { provider, url: parsed.href }
  } catch {
    return null
  }
}
