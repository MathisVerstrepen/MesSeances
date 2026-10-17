// Public original booking routes only. Checkout redirects are not public links.
export const cinemovidaBookingHosts = {
  P0176: 'achat.cinema-castillet.com',
  P0218: 'achat.cinema-megacastillet.com',
  P7951: 'achat.cinemaleselect-granville.com',
  P0876: 'achat.concordecinema.com',
} as const

export function safeCinemovidaBooking(
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'booking' } | null {
  if (raw.length > 4096) return null
  for (const [theater, host] of Object.entries(cinemovidaBookingHosts)) {
    const root = `https://${host}`
    // Concorde also publishes original /r/ links; other venues do not.
    const prefix =
      theater === 'P0876' && raw.startsWith(`${root}/r/`)
        ? `${root}/r/`
        : `${root}/reserver/r/`
    if (!raw.startsWith(prefix)) continue
    const session = raw.slice(prefix.length)
    const match = /^[1-9][0-9]*$/.exec(session)
    if (!match || match[0] !== session) return null
    if (theaterId !== undefined && theaterId !== `cinemovida-${theater}`)
      return null
    if (showtimeId !== undefined) {
      const showing =
        /^cinemovida-showing-(P0176|P0218|P7951|P0876)-[a-f0-9]{64}$/.exec(
          showtimeId ?? '',
        )
      if (!showing || showing[0] !== showtimeId || showing[1] !== theater)
        return null
    }
    return { url: raw, kind: 'booking' }
  }
  return null
}
