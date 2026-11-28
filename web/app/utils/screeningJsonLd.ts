import type { Showtime } from '../types/api.ts'
import { safeBookingUrl } from './bookingUrl.ts'
import { formatLabel } from './formats.ts'
import type { JsonLdNode, JsonLdPostalAddress } from './jsonLd.ts'

export interface ScreeningEventContext {
  theaterId: string
  theaterName: string
  originalLanguage: string | null | undefined
  image?: string | null
}

type ScreeningEventDetails = Pick<
  JsonLdNode,
  | 'name'
  | 'eventStatus'
  | 'eventAttendanceMode'
  | 'inLanguage'
  | 'subtitleLanguage'
  | 'videoFormat'
  | 'offers'
  | 'image'
>

const LANGUAGE_CODE = /^[a-z]{2,3}$/

// Screening language codes describe audio and subtitles separately.
function screeningLanguages(
  language: Showtime['language'],
  originalLanguage: string | null | undefined,
): Pick<JsonLdNode, 'inLanguage' | 'subtitleLanguage'> {
  const original = originalLanguage?.trim().toLowerCase() ?? ''
  const originalAudio = LANGUAGE_CODE.test(original) ? original : undefined
  switch (language) {
    case 'VF':
      return { inLanguage: 'fr' }
    case 'VF_SME':
    case 'VFSTF':
      return { inLanguage: 'fr', subtitleLanguage: 'fr' }
    case 'VOSTFR':
      return originalAudio
        ? { inLanguage: originalAudio, subtitleLanguage: 'fr' }
        : { subtitleLanguage: 'fr' }
    case 'VO':
      return originalAudio ? { inLanguage: originalAudio } : {}
    default:
      return {}
  }
}

export function screeningEventDetails(
  showtime: Showtime,
  context: ScreeningEventContext,
): ScreeningEventDetails {
  const details: ScreeningEventDetails = {
    name: `${showtime.movie.title.trim()} à ${context.theaterName.trim()}`,
    eventStatus: 'https://schema.org/EventScheduled',
    eventAttendanceMode: 'https://schema.org/OfflineEventAttendanceMode',
    ...screeningLanguages(showtime.language, context.originalLanguage),
  }
  if (showtime.format) details.videoFormat = formatLabel(showtime.format)
  const booking = safeBookingUrl(
    showtime.booking_url,
    showtime.provider,
    showtime.id,
    context.theaterId,
  )
  // Generic cinema websites are not offers for this screening.
  if (booking?.kind === 'booking')
    details.offers = { '@type': 'Offer', url: booking.url }
  if (context.image) details.image = context.image
  return details
}

export function theaterPostalAddress(theater: {
  address?: string | null
  postal_code?: string | null
  city?: string | null
}): JsonLdPostalAddress | undefined {
  const street = theater.address?.trim()
  const postalCode = theater.postal_code?.trim()
  const city = theater.city?.trim()
  if (!city) return undefined
  const address: JsonLdPostalAddress = {
    '@type': 'PostalAddress',
    addressLocality: city,
    addressCountry: 'FR',
  }
  if (street) address.streetAddress = street
  if (postalCode) address.postalCode = postalCode
  return address
}
