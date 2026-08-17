import type { LocationQuery, LocationQueryValue } from 'vue-router'

export function singularQueryValue(value: LocationQueryValue | LocationQueryValue[] | undefined): string | undefined {
  return Array.isArray(value) || value === null ? undefined : value
}

export function positiveSafeInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) return undefined
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined
}

export function calendarDate(value: string | undefined): string | undefined {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined
  const [year = Number.NaN, month = Number.NaN, day = Number.NaN] = value.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, day, 12))
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day ? value : undefined
}

export function enumQueryValue<const T extends string>(value: string | undefined, values: readonly T[]): T | undefined {
  return values.find((candidate) => candidate === value)
}

export function mergeOwnedQuery(
  query: LocationQuery,
  ownedKeys: readonly string[],
  values: Readonly<Record<string, string | null | undefined>>
): LocationQuery {
  const next: LocationQuery = { ...query }
  for (const key of ownedKeys) delete next[key]
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined) next[key] = value
  }
  return next
}

function comparableEntries(query: LocationQuery): Array<[string, string[]]> {
  return Object.keys(query).sort().map((key) => {
    const raw = query[key]
    const values = Array.isArray(raw) ? raw : [raw]
    return [key, values.map((value) => value === null || value === undefined ? '' : String(value))]
  })
}

export function queriesEqual(left: LocationQuery, right: LocationQuery): boolean {
  return JSON.stringify(comparableEntries(left)) === JSON.stringify(comparableEntries(right))
}
