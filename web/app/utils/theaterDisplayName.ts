const BRAND_ONLY_NAME = /^(UGC|CGR|Megarama|IMAX|Kinepolis|Pathé|Pathe|Cinéville|Cineville|MK2|Cinewest|Grand [EÉ]cran|No[eé](?:\u0301)?\s+Cin[eé](?:\u0301)?mas)$/iu

export function theaterDisplayName(theater: { name: string; city: string }): string {
  const name = theater.name.trim()
  const city = theater.city.trim()
  return city && BRAND_ONLY_NAME.test(name) ? `${name} ${city}` : theater.name
}
