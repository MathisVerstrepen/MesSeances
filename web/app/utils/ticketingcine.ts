// Generic TicketingCiné (Monnaie Services) cinemas are configured as sync
// sources, so any EMSdddd site is a valid identity. Bookings always use exact
// www.ticketingcine.com session routes bound to the site and session.

const SESSION_PATTERN = /^emsx([0-9]{4})[0-9]{8}$/

export function isValidTicketingcineSessionId(value: string): boolean {
  return SESSION_PATTERN.exec(value)?.[0] === value
}

export function safeTicketingcineBooking(
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'booking' } | null {
  let site: string | undefined
  if (theaterId !== undefined) {
    const match = /^ticketingcine-(EMS[0-9]{4})$/.exec(theaterId ?? '')
    if (!match || match[0] !== theaterId) return null
    site = match[1]
  }
  if (showtimeId !== undefined) {
    const prefix = 'ticketingcine-showing-'
    if (!showtimeId?.startsWith(prefix)) return null
    const session = showtimeId.slice(prefix.length)
    if (!isValidTicketingcineSessionId(session)) return null
    const showingSite = `EMS${session.slice(4, 8)}`
    if (site && site !== showingSite) return null
    site = showingSite
  }
  // Validate original bytes, not URL-normalized paths, ports, escapes or fragments.
  const ticket =
    /^https:\/\/www\.ticketingcine\.com(?:\/(EMS[0-9]{4})\/FR[A-Z0-9]{5}|\?(EMS[0-9]{4}))#showsession\?id=(emsx[0-9]{12})(?:&ps=ticketingcine)?$/.exec(
      raw,
    )
  if (!ticket || ticket[0] !== raw || raw.length > 2048) return null
  const ticketSite = ticket[1] ?? ticket[2]
  const session = ticket[3]!
  if (
    ticketSite !== `EMS${session.slice(4, 8)}` ||
    (site && site !== ticketSite) ||
    (showtimeId !== undefined &&
      showtimeId !== `ticketingcine-showing-${session}`)
  )
    return null
  return { url: raw, kind: 'booking' }
}
