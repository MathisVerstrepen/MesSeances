import type { Provider } from '../types/api'

export interface SafeBookingUrl {
  provider: Provider
  url: string
  kind: 'booking' | 'website'
}

const MEGARAMA_HOSTS = new Set([
  'boulogne.megarama.fr',
  'roubaix.megarama.fr',
  'nice.megarama.fr',
  'villeneuve.megarama.fr',
  'montigny.megarama.fr',
  'bordeaux.megarama.fr',
  'chalon.megarama.fr',
  'montpellier.megarama.fr',
  'besancon.megarama.fr',
  'beaux-arts.megarama.fr',
  'audincourt.megarama.fr',
  'arcueil.megarama.fr',
  'arras.megarama.fr',
  'pian.megarama.fr',
  'royalpalace-nogent.ticketingcine.com',
  'lons.megarama.fr',
  'lons-le-palace.megarama.fr',
  'studio66.megarama.fr',
  'chambly.megarama.fr',
  'garat.megarama.fr',
  'camion-rouge.megarama.fr',
  'alhambra.megarama.fr',
  'denain.megarama.fr',
  'orange.megarama.fr',
  'annecy.megarama.fr',
  'pince-vent.megarama.fr',
  'givors.megarama.fr',
  'dieppe.megarama.fr',
  'louviers.megarama.fr',
  'gaillon.megarama.fr',
  'cine-armentieres.fr',
  'lepalacecambrai.com',
  'les-ulis.megarama.fr',
  'cormeilles.megarama.fr'
])

// Verified booking-only aliases. These never become website-fallback roots.
const MEGARAMA_BOOKING_HOSTS = new Map([
  ['www.lepalacecambrai.com', 'EMS0592'],
  ['www.royalpalacenogent.fr', 'EMS0809'],
  ['www.cine-armentieres.fr', 'EMS1053'],
  ['jean-jaures.megarama.fr', 'EMS1204'],
  ['chavanelle.megarama.fr', 'EMS1205']
])

function isSafeMegaramaBooking(value: string, hostname: string): boolean {
  // Match raw bytes too: URL parsing alone normalizes ports, traversal and escapes.
  const root = `https://${hostname}`
  if (!value.startsWith(root)) return false
  const route = value.slice(root.length)
  const aliasCinema = MEGARAMA_BOOKING_HOSTS.get(hostname)
  if (aliasCinema) {
    const session = /^\/?#showsession\?id=emsx([0-9]{4})[0-9]{8}$/.exec(route)
    return session !== null && session[0] === route && `EMS${session[1]}` === aliasCinema
  }
  return /^\/?(?:#showsession\?id=[A-Za-z0-9][A-Za-z0-9_-]{0,110})?$/.test(route)
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
          : hostname === 'achat.cgrcinemas.fr' ? 'cgr' : MEGARAMA_HOSTS.has(hostname) || MEGARAMA_BOOKING_HOSTS.has(hostname) ? 'megarama' : null
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
      || (provider === 'megarama' && (raw !== value || !isSafeMegaramaBooking(value, hostname)))
    ) return null

    return { provider, url: parsed.href, kind: provider === 'megarama' && !parsed.hash ? 'website' : 'booking' }
  } catch {
    return null
  }
}
