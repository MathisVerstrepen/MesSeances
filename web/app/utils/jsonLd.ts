export interface JsonLdReference {
  '@id': string
}

export interface JsonLdBreadcrumbListItem {
  '@type': 'ListItem'
  position: number
  name: string
  item: string
}

export interface JsonLdSummaryListItem {
  '@type': 'ListItem'
  position: number
  url: string
}

export interface JsonLdPostalAddress {
  '@type': 'PostalAddress'
  streetAddress?: string
  postalCode?: string
  addressLocality: string
  addressCountry: 'FR'
}

export interface JsonLdOffer {
  '@type': 'Offer'
  url: string
}

export interface JsonLdNode {
  '@type': string
  '@id'?: string
  name?: string
  url?: string
  inLanguage?: string
  publisher?: JsonLdReference
  duration?: string
  description?: string
  datePublished?: string
  dateModified?: string
  headline?: string
  mainEntityOfPage?: string
  author?: JsonLdReference
  genre?: string[]
  image?: string | string[]
  sameAs?: string
  itemListElement?: JsonLdBreadcrumbListItem[] | JsonLdSummaryListItem[]
  address?: string | JsonLdPostalAddress
  startDate?: string
  endDate?: string
  location?: JsonLdReference
  workPresented?: JsonLdReference
  eventStatus?: 'https://schema.org/EventScheduled'
  eventAttendanceMode?: 'https://schema.org/OfflineEventAttendanceMode'
  subtitleLanguage?: string
  videoFormat?: string
  offers?: JsonLdOffer
}

export interface JsonLdDocument {
  '@context': 'https://schema.org'
  '@graph': JsonLdNode[]
}

export function serializeJsonLd(value: JsonLdDocument): string {
  return JSON.stringify(value)
    .replaceAll('<', '\\u003C')
    .replaceAll('>', '\\u003E')
    .replaceAll('&', '\\u0026')
    .replaceAll('\u2028', '\\u2028')
    .replaceAll('\u2029', '\\u2029')
}
