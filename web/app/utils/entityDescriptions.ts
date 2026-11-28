import type { Provider } from '../types/api'

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`
}

function providerLabel(provider: Provider): string {
  const labels = {
    ugc: 'UGC',
    kinepolis: 'Kinepolis',
    pathe: 'Pathé',
    cgr: 'CGR',
    megarama: 'Megarama',
    cineville: 'Cinéville',
    mk2: 'MK2',
    cinewest: 'Cinewest',
    grandecran: 'Grand Ecran',
    noecinemas: 'Noé Cinémas',
    veo: 'Véo Cinémas',
    majestic: 'Majestic Cinémas',
    cineode: 'Cinéode',
    cinemovida: 'Ciné-Movida',
    ocine: 'Ociné',
    ticketingcine: 'TicketingCiné',
    scare: 'SCARE',
    webedia: 'Webedia',
    cotecine: 'Cotecine',
  } satisfies Record<Provider, string>
  return labels[provider]
}

export function cityDescription(
  name: string,
  theaterCount: number,
  movieCount: number,
): string {
  const theaters = countLabel(theaterCount, 'cinéma', 'cinémas')
  const movies = countLabel(movieCount, 'film', 'films')
  return `À ${name}, ${theaters} ${theaterCount === 1 ? 'programme' : 'programment'} actuellement ${movies}.`
}

// Search snippets quote these: lead with the films, keep them short.
function titlesSummary(titles: readonly string[], shown = 3): string {
  const unique = [...new Set(titles.map((title) => title.trim()))].filter(
    Boolean,
  )
  const head = unique.slice(0, shown)
  const rest = unique.length - head.length
  if (rest > 0)
    return `${head.join(', ')} et ${countLabel(rest, 'autre film', 'autres films')}`
  if (head.length <= 1) return head.join('')
  return `${head.slice(0, -1).join(', ')} et ${head.at(-1)}`
}

export function cityShowtimesDescription(
  name: string,
  theaterCount: number,
  movieTitles: readonly string[],
): string {
  const theaters = countLabel(theaterCount, 'cinéma', 'cinémas')
  const films = titlesSummary(movieTitles)
  return films
    ? `Séances de cinéma à ${name} : horaires du jour dans ${theaters}. À l’affiche : ${films}. Versions VF et VOSTFR, réservation.`
    : `Séances de cinéma à ${name} : horaires du jour dans ${theaters}.`
}

interface CinemaDescriptionInput {
  name: string
  provider: Provider
  city: string
  address?: string
  postalCode?: string
  availableDateCount: number
  movieTitles?: readonly string[]
}

// Some providers already embed the postal code and city in the address.
function cinemaLocation(input: CinemaDescriptionInput): string {
  const address = (input.address ?? '').trim()
  const normalizedAddress = address.toLocaleLowerCase('fr')
  const postalCode = (input.postalCode ?? '').trim()
  const city = input.city.trim()
  const locality = [
    postalCode && !normalizedAddress.includes(postalCode) ? postalCode : '',
    city && !normalizedAddress.includes(city.toLocaleLowerCase('fr'))
      ? city
      : '',
  ]
    .filter(Boolean)
    .join(' ')
  return [address, locality].filter(Boolean).join(', ')
}

export function cinemaDescription(input: CinemaDescriptionInput): string {
  const intro = `${input.name} est un cinéma ${providerLabel(input.provider)} à ${cinemaLocation(input)}.`
  const films = titlesSummary(input.movieTitles ?? [])
  if (films) return `${intro} À l’affiche : ${films}.`
  const dates = countLabel(
    input.availableDateCount,
    'date disponible',
    'dates disponibles',
  )
  return `${intro} Sa programmation compte ${dates}.`
}
