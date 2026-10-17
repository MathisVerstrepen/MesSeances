import type { AccountWatchlist, WatchlistMovie } from './watchlist'

export const watchedSections = [
  'search',
  'planning',
  'films',
  'upcoming',
  'home',
  'cinema_city',
] as const
export type WatchedSection = (typeof watchedSections)[number]
export type WatchedVisibility = 'hide' | 'show'
export type WatchedOverride = WatchedVisibility | 'inherit'
export type WatchedPreferences = {
  default_visibility: WatchedVisibility
  auto_remove_watchlist: boolean
} & Record<`${WatchedSection}_visibility`, WatchedOverride>
export interface WatchedItem extends WatchlistMovie {
  watched_at: string
}
export interface WatchedSnapshot {
  username: string
  revision: string
  items: WatchedItem[]
  preferences: WatchedPreferences
}
export interface SaveWatched {
  expected_username: string
  expected_revision: string
  movie_slug: string
  watched: 'true' | 'false'
  expected_watchlist_revision: string
}
export type SaveWatchedPreferences = {
  expected_username: string
  expected_revision: string
} & Omit<WatchedPreferences, 'auto_remove_watchlist'> & {
    auto_remove_watchlist: 'true' | 'false'
  }
export interface WatchedMutation {
  watched: WatchedSnapshot
  watchlist: AccountWatchlist | null
}
