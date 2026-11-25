// Generic open-data and platform providers of independent cinemas (SCARE,
// Webedia, Cotecine). Their booking links point to each cinema's own ticketing pages,
// which no manifest can list, so they are accepted only as canonical public
// HTTPS URLs bound to the showing's cinema. Mirrors schedule.ValidPublicHTTPSURL.

export type IndependentProvider = 'scare' | 'webedia' | 'cotecine'

const PUBLIC_HOST = /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/

const SHOWING_PATTERNS = {
  scare: /^scare-showing-([1-9][0-9]{0,18})-[A-Za-z0-9]{1,64}$/,
  webedia: /^webedia-showing-([A-Z][A-Z0-9]{4})-[a-f0-9]{32}$/,
  cotecine:
    /^cotecine-showing-([1-9][0-9]{0,6})-(?:[1-9][0-9]{0,9}|[1-9][0-9]{0,6}-[a-z0-9]+(?:-[a-z0-9]+)*)-[1-9][0-9]{0,11}$/,
} as const satisfies Record<IndependentProvider, RegExp>

export function isPublicHttpsUrl(raw: string): boolean {
  if (raw.length > 2048 || /[\s\\]/.test(raw)) return false
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return false
  }
  return (
    url.protocol === 'https:' &&
    !url.username &&
    !url.password &&
    !url.port &&
    url.href === raw &&
    PUBLIC_HOST.test(url.hostname)
  )
}

/** Returns the cinema source id of a showing, or null when malformed. */
export function independentShowingTheater(
  provider: IndependentProvider,
  showtimeId: string,
): string | null {
  const match = SHOWING_PATTERNS[provider].exec(showtimeId)
  return match?.[0] === showtimeId ? match[1]! : null
}

export function safeIndependentBooking(
  provider: IndependentProvider,
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'booking' } | null {
  if (!isPublicHttpsUrl(raw)) return null
  if (showtimeId !== undefined) {
    const theater = showtimeId
      ? independentShowingTheater(provider, showtimeId)
      : null
    if (!theater) return null
    if (theaterId !== undefined && theaterId !== `${provider}-${theater}`)
      return null
  } else if (
    theaterId !== undefined &&
    !theaterId?.startsWith(`${provider}-`)
  ) {
    return null
  }
  return { url: raw, kind: 'booking' }
}
