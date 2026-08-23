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
  genre?: string[]
  image?: string | string[]
  sameAs?: string
  itemListElement?: JsonLdBreadcrumbListItem[] | JsonLdSummaryListItem[]
  address?: string
  startDate?: string
  endDate?: string
  location?: JsonLdReference
  workPresented?: JsonLdReference
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
