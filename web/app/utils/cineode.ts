// Verified dataLayer theaterID -> programme slug. Shared host never proves identity.
export const cineodeSlugs = {
  '4640': 'allevard-bel-donne',
  '597': 'arletty',
  '4077': 'benodet-cinemarine',
  '593': 'casino',
  '4626': 'chatillon-chalaronne-etoile',
  '592': 'cine-lumieres',
  '4388': 'cine7',
  '4582': 'cinema-casino',
  '4622': 'doullens-tivoli',
  '3524': 'joigny',
  '981': 'l-arc-en-ciel',
  '3955': 'la-grange',
  '4426': 'le-7eme-art',
  '991': 'le-don-camillo',
  '600': 'le-moderne',
  '595': 'le-palace',
  '636': 'le-paris',
  '4420': 'le-raimu',
  '603': 'le-riounet',
  '601': 'le-roc',
  '594': 'les-clubs',
  '3859': 'le-vigan-le-palace',
  '4444': 'le-zoetrope',
  '4624': 'marly-fontenelle',
  '4638': 'mediatheque-de-nay',
  '4277': 'metabief',
  '4386': 'novelty',
  '4384': 'pirou-le-cotentin',
  '3064': 'poligny',
  '4075': 'reville-richelieu',
  '3074': 'saint-cyr-sur-mer',
  '4592': 'saint-marcellin',
  '4181': 'salle-michel-legrand',
  '4281': 'salles-le-7eme-art',
  '3857': 'sunset',
  '3945': 'tour-du-pin-equinoxe',
  '3104': 'villars-les-dombes',
} as const

export function isValidCineodeMovieId(value: string): boolean {
  const match = /^(?:[1-9][0-9]*|[a-z][a-z0-9]*(?:-[a-z0-9]+)*)$/.exec(value)
  return value.length <= 128 - 'cineode-film-'.length && match?.[0] === value
}

export function cineodeShowingParts(
  value: string,
): { theater: string; movie: string; epoch: string } | null {
  if (value.length > 128 - 'cineode-showing-'.length) return null
  const match = /^([1-9][0-9]*)-(.+)-([1-9][0-9]*)$/.exec(value)
  if (
    !match ||
    match[0] !== value ||
    !Object.hasOwn(cineodeSlugs, match[1]!) ||
    !isValidCineodeMovieId(match[2]!) ||
    BigInt(match[3]!) > 9223372036854775807n
  )
    return null
  return { theater: match[1]!, movie: match[2]!, epoch: match[3]! }
}

export function safeCineodeBooking(
  raw: string,
  showtimeId?: string | null,
  theaterId?: string | null,
): { url: string; kind: 'website' } | null {
  let theater: string | undefined
  if (theaterId !== undefined) {
    if (!theaterId?.startsWith('cineode-')) return null
    theater = theaterId.slice('cineode-'.length)
    if (!Object.hasOwn(cineodeSlugs, theater)) return null
  }
  if (showtimeId !== undefined) {
    if (!showtimeId?.startsWith('cineode-showing-')) return null
    const parts = cineodeShowingParts(
      showtimeId.slice('cineode-showing-'.length),
    )
    if (!parts || (theater && theater !== parts.theater)) return null
    theater = parts.theater
  }
  for (const [id, slug] of Object.entries(cineodeSlugs)) {
    if (
      (!theater || theater === id) &&
      raw === `https://www.cineode.fr/${slug}/seances/`
    )
      return { url: raw, kind: 'website' }
  }
  return null
}
