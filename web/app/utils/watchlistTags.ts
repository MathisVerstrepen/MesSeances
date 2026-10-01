import type { WatchlistTag, WatchlistTagColor } from '../types/watchlist.ts'

export const watchlistTagPalette = {
  neutral: {
    label: 'Neutre',
    backgroundColor: '#f4f4f5',
    color: '#3f3f46',
    borderColor: '#71717a',
  },
  red: {
    label: 'Rouge',
    backgroundColor: '#fee2e2',
    color: '#991b1b',
    borderColor: '#dc2626',
  },
  amber: {
    label: 'Ambre',
    backgroundColor: '#fef3c7',
    color: '#92400e',
    borderColor: '#b45309',
  },
  green: {
    label: 'Vert',
    backgroundColor: '#e2f3d2',
    color: '#315b1c',
    borderColor: '#4b7a2a',
  },
  teal: {
    label: 'Sarcelle',
    backgroundColor: '#cff5f6',
    color: '#155e63',
    borderColor: '#0e7490',
  },
  blue: {
    label: 'Bleu',
    backgroundColor: '#dbeafe',
    color: '#1e40af',
    borderColor: '#2563eb',
  },
  violet: {
    label: 'Violet',
    backgroundColor: '#ede9fe',
    color: '#5b21b6',
    borderColor: '#7c3aed',
  },
  rose: {
    label: 'Rose',
    backgroundColor: '#fce7f3',
    color: '#9d174d',
    borderColor: '#be185d',
  },
} as const satisfies Record<
  WatchlistTagColor,
  { label: string; backgroundColor: string; color: string; borderColor: string }
>

export function watchlistTagStyle(color: WatchlistTagColor) {
  const token = watchlistTagPalette[color]
  return {
    backgroundColor: token.backgroundColor,
    color: token.color,
    borderColor: token.borderColor,
  }
}

const collator = new Intl.Collator('fr', { sensitivity: 'base', numeric: true })

export function sortWatchlistTags(tags: readonly WatchlistTag[]) {
  return [...tags].sort(
    (a, b) =>
      collator.compare(a.name, b.name) ||
      (BigInt(a.id) < BigInt(b.id) ? -1 : BigInt(a.id) > BigInt(b.id) ? 1 : 0),
  )
}

export function tagNameError(name: string) {
  const normalized = name
    .replace(/\p{White_Space}+/gu, ' ')
    .replace(/^ | $/g, '')
    .normalize('NFC')
  if (
    /[\p{Cc}\p{Cs}\u2028\u2029]/u.test(name) ||
    [...normalized].length < 1 ||
    [...normalized].length > 40
  )
    return 'Saisissez un nom de 1 à 40 caractères, sans saut de ligne.'
  return ''
}
