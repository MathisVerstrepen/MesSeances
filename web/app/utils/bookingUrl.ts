import type { Provider } from '../types/api'
import { isValidMk2ShowingId } from './mk2.ts'
import { safeCinewestBooking } from './cinewest.ts'

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

function isSafeGrandEcranBooking(value: string): boolean {
  const match = /^https:\/\/achat\.grandecran\.fr\/[a-z0-9]+(?:-[a-z0-9]+)*\/r\/[1-9][0-9]*$/.exec(value)
  return value.length <= 4096 && match !== null && match[0] === value
}

// Frozen default:DESKTOP entries. Room-enrichment redirect destinations are not public links.
const NOE_CINEMAS_BOOKING_PREFIXES = {
  B0158: 'https://achat.noecinemas.com/domont-ermitage/r/',
  B0181: 'https://achat.cinepal.fr/reserver/r/',
  P0089: 'https://achat.noecinemas.com/pithiviers/reserver/r/',
  P0101: 'https://achat.omnia-cinemas.com/reserver/r/',
  P0276: 'https://achat.cinemamorny.fr/reserver/r/',
  P0290: 'https://achat.les-arts-cinema.com/reserver/r/',
  P0297: 'https://achat.cinema-nogent-le-rotrou.fr/reserver/r/',
  P0542: 'https://achat.noecinemas.com/houlgate/reserver/r/',
  P0613: 'https://achat.noecinemas.com/elbeuf/reserver/r/',
  P0713: 'https://achat.noecinemas.com/fecamp/reserver/r/',
  P0714: 'https://achat.3colombiers-gravenchon.fr/r/',
  P0733: 'https://achat.noecinemas.com/caudebec-en-caux/reserver/r/',
  P0975: 'https://achat.cinemas-vernon.fr/reserver/r/',
  P0997: 'https://achat.noecinemas.com/les-andelys/reserver/r/',
  P2132: 'https://achat.noecinemas.com/gisors/r/',
  P2425: 'https://achat.cinema-senonches.com/reserver/r/',
  P2478: 'https://achat.noecinemas.com/carentan/reserver/r/',
  P7898: 'https://achat.cinema-altkirch.com/reserver/r/',
  P8088: 'https://achat.cinema-laigle.com/reserver/r/',
  P9554: 'https://achat.cinemas-bernay.fr/reserver/r/',
  W2750: 'https://achat.noecinemas.com/pont-audemer/reserver/r/',
  W5200: 'https://achat.chaumont-cinemas.com/reserver/r/',
  W7619: 'https://achat.noecinemas.com/yvetot-arches-lumiere/reserver/r/',
  W8390: 'https://achat.cinemasdulavandou.fr/grand-bleu/reserver/r/'
}

function isSafeNoeCinemasBooking(value: string, showtimeId?: string | null, theaterId?: string | null): boolean {
  if (value.length > 4096) return false
  const entry = Object.entries(NOE_CINEMAS_BOOKING_PREFIXES).find(([, prefix]) => value.startsWith(prefix))
  if (!entry) return false
  const [theater, prefix] = entry
  const session = value.slice(prefix.length)
  const match = /^[1-9][0-9]*$/.exec(session)
  if (!match || match[0] !== session) return false
  if (theaterId !== undefined && theaterId !== `noecinemas-${theater}`) return false
  if (showtimeId !== undefined) {
    if (showtimeId === null) return false
    const showing = /^noecinemas-showing-([A-Z0-9]{5})-([a-f0-9]{64})$/.exec(showtimeId)
    if (!showing || showing[0] !== showtimeId || showing[1] !== theater) return false
  }
  return true
}

function isSafeCinevilleBooking(value: string): boolean {
  const match = /^https:\/\/www\.cineville\.fr\/vad\/([1-9][0-9]{0,18})\/([1-9][0-9]{0,18})\/([1-9][0-9]{0,18})$/.exec(value)
  return match !== null && match[0] === value && match.slice(1).every((id) => BigInt(id) <= 9223372036854775807n)
}

function isSafeMk2Booking(value: string, showtimeId?: string | null): boolean {
  const match = /^https:\/\/www\.mk2\.com\/panier\/seance\/tickets\?cinemaId=([0-9]+)&sessionId=([1-9][0-9]*)$/.exec(value)
  if (!match || match[0] !== value || value.length > 2048) return false
  const showingId = `${match[1]}-${match[2]}`
  return isValidMk2ShowingId(showingId)
    && (showtimeId === undefined || showtimeId === `mk2-showing-${showingId}`)
}

export function safeBookingUrl(raw: string | null | undefined, expectedProvider?: Provider | null, showtimeId?: string | null, theaterId?: string | null): SafeBookingUrl | null {
  // Validate original bytes before generic URL normalization or shared-platform inference.
  if (raw && (expectedProvider === 'noecinemas' || !expectedProvider) && isSafeNoeCinemasBooking(raw, showtimeId, theaterId)) {
    return { provider: 'noecinemas', url: raw, kind: 'booking' }
  }
  if (expectedProvider === 'noecinemas') return null
  // Resolve Cinewest before generic shared-platform host inference. Never trim it.
  if (raw && (expectedProvider === 'cinewest' || !expectedProvider)) {
    const cinewest = safeCinewestBooking(raw, showtimeId, theaterId)
    if (cinewest) return { provider: 'cinewest', ...cinewest }
  }
  if (expectedProvider === 'cinewest') return null
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
          : hostname === 'achat.cgrcinemas.fr' ? 'cgr' : hostname === 'achat.grandecran.fr' ? 'grandecran' : hostname === 'www.cineville.fr' ? 'cineville' : hostname === 'www.mk2.com' ? 'mk2' : MEGARAMA_HOSTS.has(hostname) || MEGARAMA_BOOKING_HOSTS.has(hostname) ? 'megarama' : null
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
      || (provider === 'grandecran' && (raw !== value || parsed.href !== value || !isSafeGrandEcranBooking(value)))
      || (provider === 'mk2' && (raw !== value || parsed.href !== value || !isSafeMk2Booking(value, showtimeId)))
      || (provider === 'cineville' && (raw !== value || parsed.href !== value || !isSafeCinevilleBooking(value)))
      || (provider === 'megarama' && (raw !== value || !isSafeMegaramaBooking(value, hostname)))
    ) return null

    return { provider, url: parsed.href, kind: provider === 'megarama' && !parsed.hash ? 'website' : 'booking' }
  } catch {
    return null
  }
}
