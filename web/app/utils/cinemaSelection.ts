import type { Theater } from '../types/api'

export interface CinemaCityGroup {
  key?: string
  city: string
  citySlug: string
  theaters: Theater[]
}

export function selectedFirst<T>(
  rows: readonly T[],
  selectedIds: ReadonlySet<string>,
  id: (row: T) => string,
): T[] {
  return [
    ...rows.filter((row) => selectedIds.has(id(row))),
    ...rows.filter((row) => !selectedIds.has(id(row))),
  ]
}

export function groupSelectedTheatersFirst(
  theaters: readonly Theater[],
  selectedIds: ReadonlySet<string>,
): CinemaCityGroup[] {
  return [true, false].flatMap((selected) =>
    groupTheatersByCityIdentity(
      theaters.filter((theater) => selectedIds.has(theater.id) === selected),
    ).map((group) => ({
      ...group,
      key: `${selected ? 'selected' : 'unselected'}:${group.citySlug}`,
    })),
  )
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
