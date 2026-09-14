import { digest } from 'ohash/crypto'

// Frozen public roots, shared with schedule.CinewestWebsite. No API/token URLs.
export const CINEWEST_THEATER_HOSTS = {
  'cineoffice-cognaclegalaxy': 'www.cine-cognac.com',
  'cineoffice-neverscinemazarin': 'www.cinemazarin-nevers.fr',
  'cineoffice-mouanssartouxlastrada': 'lastrada.cinewest06.fr',
  'cineoffice-vitreaurore': 'www.aurorecinema.fr',
  'cineoffice-mouginslesbalcons': 'lesbalcons.cinewest06.fr',
  'cineoffice-royanlelido': 'www.cine-royan.com',
  'cineoffice-ploermelcinelac': 'cinelac.fr',
  'cineoffice-saintesatlanticcine': 'www.atlantic-cine.fr',
  'cineoffice-aurillaclecristal': 'cineaurillac.fr',
  'ticketingcine-EMS1185': 'www.etoilecinemas-bethune.fr',
  'ticketingcine-EMS1317': 'www.cinema-liberte.fr',
  'ticketingcine-EMS0042': 'www.toilesdumoun.fr',
  'webediamovies-W8400': 'www.capitolestudios.com'
} as const

export function cinewestTicketShowingId(theaterId: string, sourceSessionId: string): string {
  // ohash's public digest API is SHA-256 in both its Node and browser exports.
  const encoded = digest(`${theaterId}\0${sourceSessionId}`)
  const hex = Array.from(atob(encoded.replaceAll('-', '+').replaceAll('_', '/') + '='), (byte) => byte.charCodeAt(0).toString(16).padStart(2, '0')).join('')
  return `cinewest-showing-ticketingcine-${hex}`
}

export function safeCinewestBooking(raw: string, showtimeId?: string | null, expectedTheaterId?: string | null): { url: string; kind: 'website' | 'booking' } | null {
  if (raw.length > 4096) return null
  const providerTheaterId = expectedTheaterId?.replace(/^cinewest-/, '')
  for (const [theaterId, host] of Object.entries(CINEWEST_THEATER_HOSTS)) {
    if (providerTheaterId && providerTheaterId !== theaterId) continue
    const root = `https://${host}`
    if (raw === root || raw === `${root}/`) return { url: raw, kind: 'website' }
    if (!theaterId.startsWith('ticketingcine-') || !raw.startsWith(root)) continue
    const route = raw.slice(root.length)
    const match = /^\/?#showsession\?id=(emsx([0-9]{4})[0-9]{8})$/.exec(route)
    if (!match || match[0] !== route || `ticketingcine-EMS${match[2]}` !== theaterId) continue
    if (showtimeId && showtimeId !== cinewestTicketShowingId(theaterId, match[1]!)) return null
    return { url: raw, kind: 'booking' }
  }
  if (providerTheaterId && providerTheaterId !== 'webediamovies-W8400') return null
  const capitole = /^https:\/\/www\.capitolestudios-reserver\.cotecine\.fr\/reserver\/r\/[1-9][0-9]*$/.exec(raw)
  return capitole?.[0] === raw ? { url: raw, kind: 'booking' } : null
}

export function isSafeCinewestPoster(raw: string): boolean {
  if (raw.length > 4096) return false
  const match = /^(?:https:\/\/cinemedia\.cine\.digital\/medias\/[0-9]+\/[a-f0-9]{2}\/[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\.(?:jpeg|jpg|png|webp)|https:\/\/images\.monnaie-services\.com\/movie_poster\/(?:120|600)\/FR[A-Z0-9]{5}\/[A-Z0-9]{8}\.webp|https:\/\/all\.web\.img\.acsta\.net\/img\/[a-f0-9]{2}\/[a-f0-9]{2}\/[a-f0-9]{24,64}\.(?:jpeg|jpg|png|webp))$/.exec(raw)
  return match?.[0] === raw
}
