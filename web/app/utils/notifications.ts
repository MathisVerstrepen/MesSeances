import type {
  AccountNotification,
  NotificationKind,
  NotificationRule,
  NotificationScope,
} from '../types/notifications.ts'
import { cinemaMovieTarget } from './cinemaMovieTarget.ts'

export interface NotificationPresentation {
  context: string
  headline: string
  detail: string
  target: string
}

function plural(count: number, singular: string, pluralForm: string) {
  return count === 1 ? singular : pluralForm
}

function theaterLabel(item: AccountNotification): string {
  const first = item.theaters[0]?.name
  const others = item.theater_count - (first ? 1 : 0)
  if (!first)
    return `${item.theater_count} ${plural(item.theater_count, 'cinéma', 'cinémas')}`
  if (others <= 0) return first
  return `${first} et ${others} ${plural(others, 'autre cinéma', 'autres cinémas')}`
}

function movieList(item: AccountNotification): string {
  const titles = item.movies.map((movie) => movie.title)
  const others = item.movie_count - titles.length
  if (!titles.length) return ''
  if (others > 0)
    return `${titles.join(', ')} et ${others} ${plural(others, 'autre', 'autres')}`
  if (titles.length === 1) return titles[0] ?? ''
  return `${titles.slice(0, -1).join(', ')} et ${titles.at(-1)}`
}

export function presentNotification(
  item: AccountNotification,
): NotificationPresentation {
  const movie = item.movies[0]
  const theater = item.theaters[0]
  if (item.kind === 'watchlist_screening') {
    return {
      context: 'Watchlist',
      headline: `${movie?.title ?? 'Un film de votre watchlist'} est programmé à ${theaterLabel(item)}`,
      detail: '',
      target: movie
        ? theater
          ? cinemaMovieTarget(movie.slug, theater.id)
          : `/film/${encodeURIComponent(movie.slug)}`
        : '/compte/watchlist',
    }
  }
  const single = item.movie_count === 1 && movie
  return {
    context: 'Activité des cinémas',
    headline: single
      ? `${movie.title} arrive à ${theaterLabel(item)}`
      : `${item.movie_count} ${plural(item.movie_count, 'nouveau film', 'nouveaux films')} à ${theaterLabel(item)}`,
    detail: single ? '' : movieList(item),
    target: '/compte/activite',
  }
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

export function formatNotificationTime(createdAt: string, now: Date): string {
  const created = new Date(createdAt)
  const elapsed = now.getTime() - created.getTime()
  if (!Number.isFinite(elapsed)) return ''
  if (elapsed < MINUTE) return 'à l’instant'
  if (elapsed < HOUR) return `il y a ${Math.floor(elapsed / MINUTE)} min`
  if (elapsed < DAY) return `il y a ${Math.floor(elapsed / HOUR)} h`
  if (elapsed < 2 * DAY) return 'hier'
  if (elapsed < 7 * DAY) return `il y a ${Math.floor(elapsed / DAY)} j`
  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: 'Europe/Paris',
    day: 'numeric',
    month: 'short',
  }).format(created)
}

export function notificationKindLabel(kind: NotificationKind): string {
  return kind === 'watchlist_screening'
    ? 'Film de ma watchlist programmé'
    : 'Nouveautés dans des cinémas'
}

export function notificationScopeLabel(
  scope: NotificationScope,
  theaterCount: number,
): string {
  const count = `${theaterCount} ${plural(theaterCount, 'cinéma', 'cinémas')}`
  if (scope === 'followed') return `Mes cinémas suivis (${count})`
  if (scope === 'preferred') return `Mes cinémas sélectionnés (${count})`
  return `Sélection personnalisée (${count})`
}

export function notificationRuleTheaterCount(
  rule: Pick<NotificationRule, 'theater_scope' | 'theater_ids'>,
  followedCount: number,
  preferredCount: number,
): number {
  if (rule.theater_scope === 'followed') return followedCount
  if (rule.theater_scope === 'preferred') return preferredCount
  return rule.theater_ids.length
}

export function unreadBadge(count: number): string {
  if (count <= 0) return ''
  return count > 99 ? '99+' : String(count)
}

export function normalizeTheaterSearch(value: string): string {
  return value
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim()
}

export function filterNotificationTheaters<
  T extends { id: string; name: string; city: string },
>(theaters: readonly T[], query: string, limit = 30): T[] {
  const terms = normalizeTheaterSearch(query).split(' ').filter(Boolean)
  if (!terms.length) return []
  const matches: T[] = []
  for (const theater of theaters) {
    const haystack = normalizeTheaterSearch(`${theater.name} ${theater.city}`)
    if (terms.every((term) => haystack.includes(term))) {
      matches.push(theater)
      if (matches.length >= limit) break
    }
  }
  return matches
}
