const BRAND_ONLY_NAME = /^(UGC|CGR|Megarama|IMAX|Kinepolis|Pathé|Pathe)$/iu

export function theaterDisplayName(theater: { name: string; city: string }): string {
  const name = theater.name.trim()
  const city = theater.city.trim()
  return city && BRAND_ONLY_NAME.test(name) ? `${name} ${city}` : theater.name
}
