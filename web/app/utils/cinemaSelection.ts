import type { Theater } from '../types/api'

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
