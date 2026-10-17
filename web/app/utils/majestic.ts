// Frozen ticketingcine site identities. Never infer chain membership from its shared host.
const websites = {
  EMS0700: 'majestic-compiegne.fr',
  EMS0654: 'majestic-douai.com',
  EMS1333: 'dole.majestic-cinemas.com',
  EMS1212: 'vesoul.majestic-cinemas.com',
  EMS1299: 'cinelaon.fr',
  EMS1211: 'remiremont.majestic-cinemas.com',
  EMS0957: 'cinevendome.fr',
}

export function isValidMajesticSessionId(value: string): boolean {
  const match = /^emsx([0-9]{4})[0-9]{8}$/.exec(value)
  return match?.[0] === value && Object.hasOwn(websites, `EMS${match[1]}`)
}

export function safeMajesticBooking(
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'booking' | 'website' } | null {
  let site: string | undefined
  if (theaterId !== undefined) {
    const match = /^majestic-(EMS[0-9]{4})$/.exec(theaterId ?? '')
    if (!match || match[0] !== theaterId || !Object.hasOwn(websites, match[1]!))
      return null
    site = match[1]
  }
  if (showtimeId !== undefined) {
    const prefix = 'majestic-showing-'
    if (!showtimeId?.startsWith(prefix)) return null
    const session = showtimeId.slice(prefix.length)
    if (!isValidMajesticSessionId(session)) return null
    const showingSite = `EMS${session.slice(4, 8)}`
    if (site && site !== showingSite) return null
    site = showingSite
  }

  // Validate original bytes, not URL-normalized paths, ports, escapes or fragments.
  const ticket =
    /^https:\/\/www\.ticketingcine\.com(?:\/(EMS[0-9]{4})\/FR[A-Z0-9]{5}|\?(EMS[0-9]{4}))#showsession\?id=(emsx[0-9]{12})(?:&ps=ticketingcine)?$/.exec(
      raw,
    )
  if (ticket?.[0] === raw) {
    const ticketSite = ticket[1] ?? ticket[2]
    const session = ticket[3]!
    if (
      !isValidMajesticSessionId(session) ||
      ticketSite !== `EMS${session.slice(4, 8)}` ||
      (site && site !== ticketSite) ||
      (showtimeId !== undefined && showtimeId !== `majestic-showing-${session}`)
    )
      return null
    return { url: raw, kind: 'booking' }
  }

  for (const [websiteSite, host] of Object.entries(websites)) {
    if ((!site || site === websiteSite) && raw === `https://${host}/`)
      return { url: raw, kind: 'website' }
  }
  return null
}
