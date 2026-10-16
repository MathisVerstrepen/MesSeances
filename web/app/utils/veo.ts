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
    /^https:\/\/achat\.veocinemas\.fr\/[a-z0-9]+(?:-[a-z0-9]+)*\/(?:reserver\/)?r\/[1-9][0-9]*$/.exec(
      raw,
    )
  if (checkout?.[0] === raw)
    return theater === 'EMS1361' ? null : { url: raw, kind: 'booking' }
  if (theater === 'G06DB' && validGueretRelay(raw))
    return { url: raw, kind: 'booking' }
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

// Structural only. Source acquisition binds start/tags; transport binds final D.
// Single ASCII decode, mirrored by Go, SQL 065 and crawlability tooling.
function validGueretRelay(raw: string): boolean {
  const prefix = 'https://relay.mvtx.us/ticketing/dbz?'
  if (raw.length > 4096 || !raw.startsWith(prefix)) return false
  const values = new Map<string, string>()
  for (const pair of raw.slice(prefix.length).split('&')) {
    const parts = pair.split('=')
    const key = parts[0]!
    const value = parts[1] ?? ''
    if (
      parts.length !== 2 ||
      values.has(key) ||
      !['code_theater', 'gid_entity', 'tags', 'perfd', 'perft'].includes(key)
    )
      return false
    if (
      !/^[A-Za-z0-9._~,:%-]{1,3072}$/.test(value) ||
      /[^A-Za-z0-9._~,:%-]/.test(value)
    )
      return false
    let decoded: string
    try {
      decoded = decodeURIComponent(value)
    } catch {
      return false
    }
    if (
      !/^[A-Za-z0-9._~,:-]{1,1024}$/.test(decoded) ||
      /[^A-Za-z0-9._~,:-]/.test(decoded)
    )
      return false
    values.set(key, decoded)
  }
  const day = values.get('perfd') ?? ''
  const clock = values.get('perft') ?? ''
  if (
    values.size !== 5 ||
    values.get('code_theater') !== 'P1394' ||
    !/^[0-9]{8}$/.test(day) ||
    day.length !== 8 ||
    !/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(clock) ||
    clock.length !== 5
  )
    return false
  const year = Number(day.slice(0, 4)),
    month = Number(day.slice(4, 6)),
    date = Number(day.slice(6))
  const days = [
    31,
    year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ]
  return (
    year >= 1 &&
    month >= 1 &&
    month <= 12 &&
    date >= 1 &&
    date <= days[month - 1]!
  )
}
