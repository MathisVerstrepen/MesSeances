import type { Provider } from '../types/api'

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`
}

function providerLabel(provider: Provider): string {
  return provider === 'ugc' ? 'UGC' : 'Kinepolis'
}

export function cityDescription(name: string, theaterCount: number, movieCount: number): string {
  const theaters = countLabel(theaterCount, 'cinéma', 'cinémas')
  const movies = countLabel(movieCount, 'film', 'films')
  return `À ${name}, ${theaters} ${theaterCount === 1 ? 'programme' : 'programment'} actuellement ${movies}.`
}

interface CinemaDescriptionInput {
  name: string
  provider: Provider
  city: string
  address?: string
  postalCode?: string
  availableDateCount: number
}

export function cinemaDescription(input: CinemaDescriptionInput): string {
  const location = [(input.address ?? '').trim(), (input.postalCode ?? '').trim(), input.city.trim()].filter(Boolean).join(', ')
  const dates = countLabel(input.availableDateCount, 'date disponible', 'dates disponibles')
  return `${input.name} est un cinéma ${providerLabel(input.provider)} à ${location}. Sa programmation compte ${dates}.`
}
