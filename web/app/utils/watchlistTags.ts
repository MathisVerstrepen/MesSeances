import type { WatchlistTag } from '../types/watchlist.ts'

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
