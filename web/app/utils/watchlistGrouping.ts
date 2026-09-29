import type {
  WatchlistItem,
  WatchlistTag,
  WatchlistTagColor,
} from '../types/watchlist.ts'

export interface WatchlistGroup {
  id: string
  name: string
  color: WatchlistTagColor | null
  items: WatchlistItem[]
}

// Inputs already carry the selected film sort and French tag-name order.
// Partition once, keeping the same canonical item in each assigned section.
export function groupWatchlistItems(
  items: readonly WatchlistItem[],
  tags: readonly WatchlistTag[],
  selectedTag = '',
): WatchlistGroup[] {
  const groups = tags
    .filter((tag) => !selectedTag || tag.id === selectedTag)
    .map<WatchlistGroup>((tag) => ({
      id: `tag-${tag.id}`,
      name: tag.name,
      color: tag.color,
      items: [],
    }))
  const byId = new Map(groups.map((group) => [group.id, group]))
  const untagged: WatchlistGroup = {
    id: 'untagged',
    name: 'Sans tag',
    color: null,
    items: [],
  }
  for (const item of items) {
    if (!item.tag_ids.length && !selectedTag) untagged.items.push(item)
    for (const id of item.tag_ids) byId.get(`tag-${id}`)?.items.push(item)
  }
  if (!selectedTag) groups.push(untagged)
  return groups.filter((group) => group.items.length)
}
