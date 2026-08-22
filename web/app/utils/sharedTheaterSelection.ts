export const SHARED_THEATERS_QUERY_KEY = 'shared_theaters'

const THEATER_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/

export interface TheaterIdentity {
  id: string
}

type SharedTheaterQueryValue = string | null | (string | null)[] | undefined

export function parseSharedTheaterSelection(value: SharedTheaterQueryValue): string[] | null {
  if (value === undefined || value === null || Array.isArray(value) || value.length === 0) return null

  const ids = value.split(',')
  if (ids.some((id) => !THEATER_ID_PATTERN.test(id))) return null
  if (new Set(ids).size !== ids.length) return null
  return ids
}

export function canonicalizeTheaterSelection(ids: readonly string[], catalog: readonly TheaterIdentity[]): string[] {
  const selected = new Set(ids)
  return catalog.filter((theater) => selected.has(theater.id)).map((theater) => theater.id)
}

export function theaterSelectionsEqual(left: readonly string[], right: readonly string[]): boolean {
  if (left.length !== right.length) return false
  const rightIds = new Set(right)
  return left.every((id) => rightIds.has(id))
}

export function withSharedTheaterSelection(target: string, ids: readonly string[]): string | null {
  if (ids.length === 0 || parseSharedTheaterSelection(ids.join(',')) === null) return null

  const queryIndex = target.indexOf('?')
  const path = queryIndex === -1 ? target : target.slice(0, queryIndex)
  const rawQuery = queryIndex === -1 ? '' : target.slice(queryIndex + 1)
  const fields: string[] = []

  for (const field of rawQuery ? rawQuery.split('&') : []) {
    const separator = field.indexOf('=')
    const rawKey = separator === -1 ? field : field.slice(0, separator)
    let key: string
    try {
      key = decodeURIComponent(rawKey.replaceAll('+', ' '))
    } catch {
      return null
    }
    if (key !== SHARED_THEATERS_QUERY_KEY) fields.push(field)
  }

  fields.push(`${SHARED_THEATERS_QUERY_KEY}=${encodeURIComponent(ids.join(','))}`)
  return `${path}?${fields.join('&')}`
}
