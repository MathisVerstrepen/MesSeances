// Verified Ociné theaterID -> programme slug. Shared host is not identity.
export const ocineSlugs = {
  '309': 'maubeuge',
  '310': 'st-omer',
  '461': 'dunkerque',
} as const

export function ocineShowingParts(value: string): {
  theater: keyof typeof ocineSlugs
  movie: string
  epoch: string
  hall: string
} | null {
  if (value.length > 128 - 'ocine-showing-'.length) return null
  const match =
    /^(309|310|461)-([1-9][0-9]*|[a-z][a-z0-9]*(?:-[a-z0-9]+)*)-([1-9][0-9]*)-([1-9][0-9]*|past)$/.exec(
      value,
    )
  if (!match || match[0] !== value || BigInt(match[3]!) > 9223372036854775807n)
    return null
  return {
    // SAFETY: the matched theater group accepts exactly the three manifest keys.
    theater: match[1] as keyof typeof ocineSlugs,
    movie: match[2]!,
    epoch: match[3]!,
    hall: match[4]!,
  }
}

export function safeOcineBooking(
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'booking' } | null {
  // Checkout requires both identities; match original bytes, never URL-normalized aliases.
  if (!showtimeId?.startsWith('ocine-showing-') || raw.length > 2048)
    return null
  const parts = ocineShowingParts(showtimeId.slice('ocine-showing-'.length))
  if (!parts || parts.hall === 'past' || theaterId !== `ocine-${parts.theater}`)
    return null
  const match =
    /^https:\/\/www\.ocine\.fr\/(maubeuge|st-omer|dunkerque)\/reserver\/F([1-9][0-9]*)\/D([1-9][0-9]*)\/(VF|VO)\/([1-9][0-9]*)\/$/.exec(
      raw,
    )
  if (
    !match ||
    match[0] !== raw ||
    match[1] !== ocineSlugs[parts.theater] ||
    match[3] !== parts.epoch ||
    match[5] !== parts.hall ||
    (/^[0-9]+$/.test(parts.movie) && match[2] !== parts.movie)
  )
    return null
  // Local slugs bind to numeric F tokens during acquisition, not by guessing here.
  return { url: raw, kind: 'booking' }
}
