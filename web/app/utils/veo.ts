import { digest } from 'ohash/crypto'

export function veoShowingId(
  theaterId: string,
  sourceSessionId: string,
): string {
  const encoded = digest(`${theaterId}\0${sourceSessionId}`)
  const hex = Array.from(
    atob(encoded.replaceAll('-', '+').replaceAll('_', '/') + '='),
    (byte) => byte.charCodeAt(0).toString(16).padStart(2, '0'),
  ).join('')
  return `veo-showing-${theaterId}-${hex}`
}

export function safeVeoBooking(
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'website' | 'booking' } | null {
  if (raw.length > 4096) return null
  let theater: string | undefined
  if (theaterId !== undefined) {
    const match = /^veo-([A-Z0-9]{5}|EMS1361)$/.exec(theaterId ?? '')
    if (!match || match[0] !== theaterId) return null
    theater = match[1]
  }
  if (showtimeId !== undefined) {
    const match = /^veo-showing-([A-Z0-9]{5}|EMS1361)-[a-f0-9]{64}$/.exec(
      showtimeId ?? '',
    )
    if (!match || match[0] !== showtimeId || (theater && theater !== match[1]))
      return null
    theater = match[1]
  }
  // Exact original bytes only: no trimming, URL normalization or shared-host inference.
  const checkout =
    /^https:\/\/achat\.veocinemas\.fr\/[a-z0-9]+(?:-[a-z0-9]+)*\/r\/[1-9][0-9]*$/.exec(
      raw,
    )
  if (checkout?.[0] === raw)
    return theater === 'EMS1361' ? null : { url: raw, kind: 'booking' }
  if (theater && theater !== 'EMS1361') return null
  const root = /^https:\/\/castelginest\.miracinemas\.fr\/?$/.exec(raw)
  if (root?.[0] === raw) return { url: raw, kind: 'website' }
  const session =
    /^https:\/\/castelginest\.miracinemas\.fr\/?#showsession\?id=(emsx1361[0-9]{8})$/.exec(
      raw,
    )
  if (!session || session[0] !== raw) return null
  if (
    showtimeId !== undefined &&
    showtimeId !== veoShowingId('EMS1361', session[1]!)
  )
    return null
  return { url: raw, kind: 'booking' }
}
