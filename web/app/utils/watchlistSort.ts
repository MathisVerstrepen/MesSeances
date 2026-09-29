import type { WatchlistItem, WatchlistSortOrder } from '../types/watchlist.ts'
import { isCalendarDate } from './date.ts'

export const watchlistSortOptions = [
  {
    value: 'added_desc',
    label: 'Ajouts les plus récents',
    shortLabel: 'Ajouts récents',
  },
  {
    value: 'added_asc',
    label: 'Ajouts les plus anciens',
    shortLabel: 'Ajouts anciens',
  },
  { value: 'title_asc', label: 'Titre : A-Z', shortLabel: 'Titre : A-Z' },
  { value: 'title_desc', label: 'Titre : Z-A', shortLabel: 'Titre : Z-A' },
  {
    value: 'release_desc',
    label: 'Sorties françaises les plus récentes',
    shortLabel: 'Sortie FR récente',
  },
  {
    value: 'release_asc',
    label: 'Sorties françaises les plus anciennes',
    shortLabel: 'Sortie FR ancienne',
  },
] satisfies { value: WatchlistSortOrder; label: string; shortLabel: string }[]

const titles = new Intl.Collator('fr-FR', {
  sensitivity: 'base',
  numeric: true,
})
const lexical = (left: string, right: string) =>
  left < right ? -1 : left > right ? 1 : 0

// API timestamps are UTC RFC3339Nano. Keep precision that Date.parse discards.
function additionKey(timestamp: string) {
  return timestamp.replace(
    /(?:\.(\d{1,9}))?Z$/,
    (_, fraction = '') => `.${fraction.padEnd(9, '0')}Z`,
  )
}

function releaseKey(item: WatchlistItem) {
  const date = item.french_release_date
  return date && isCalendarDate(date) ? date : ''
}

export function sortWatchlistItems(
  items: readonly WatchlistItem[],
  order: WatchlistSortOrder,
): WatchlistItem[] {
  const direction = order.endsWith('_asc') ? 1 : -1
  return [...items].sort((left, right) => {
    let primary: number
    if (order.startsWith('title_')) {
      primary = titles.compare(left.title, right.title)
    } else if (order.startsWith('release_')) {
      const a = releaseKey(left)
      const b = releaseKey(right)
      if (!!a !== !!b) return a ? -1 : 1
      primary = lexical(a, b)
    } else {
      primary = lexical(additionKey(left.added_at), additionKey(right.added_at))
    }
    return direction * primary || lexical(left.slug, right.slug)
  })
}
