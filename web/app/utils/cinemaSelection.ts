import type { Theater } from '../types/api'

export interface CinemaCityGroup {
  city: string
  citySlug: string
  theaters: Theater[]
}

export function cinemaListSections<T>(
  rows: readonly T[],
  selectedIds: ReadonlySet<string>,
  id: (row: T) => string,
  selectedOnly = false,
): { key: 'selected' | 'all'; rows: readonly T[] }[] {
  const selected = rows.filter((row) => selectedIds.has(id(row)))
  return [
    ...(selected.length > 0
      ? [{ key: 'selected' as const, rows: selected }]
      : []),
    ...(!selectedOnly ? [{ key: 'all' as const, rows }] : []),
  ]
}

export function isBroadTheaterSelection(
  ids: readonly string[],
  catalog: readonly Pick<Theater, 'id'>[],
): boolean {
  const selected = new Set(ids)
  return (
    selected.size === 0 ||
    (catalog.length > 0 && catalog.every((theater) => selected.has(theater.id)))
  )
}

export function groupTheatersByCityIdentity(
  theaters: readonly Theater[],
): CinemaCityGroup[] {
  const groups = new Map<string, CinemaCityGroup>()

  for (const theater of theaters) {
    const existing = groups.get(theater.city_slug)
    if (existing) {
      existing.theaters.push(theater)
      continue
    }

    groups.set(theater.city_slug, {
      city: theater.city,
      citySlug: theater.city_slug,
      theaters: [theater],
    })
  }

  return [...groups.values()]
}

export function updateTheaterSelection(
  currentIds: readonly string[],
  targetTheaters: readonly Pick<Theater, 'id'>[],
  select: boolean,
): string[] {
  const targetIds = new Set(targetTheaters.map((theater) => theater.id))
  const nextIds = [...new Set(currentIds)]

  if (!select) return nextIds.filter((id) => !targetIds.has(id))

  const selectedIds = new Set(nextIds)
  for (const theater of targetTheaters) {
    if (selectedIds.has(theater.id)) continue
    selectedIds.add(theater.id)
    nextIds.push(theater.id)
  }
  return nextIds
}
