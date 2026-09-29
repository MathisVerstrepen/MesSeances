export interface WatchlistMovie {
  slug: string
  title: string
  poster_url?: string | null
  release_date?: string | null
}

export interface WatchlistItem extends WatchlistMovie {
  tag_ids: string[]
  french_release_date?: string | null
  added_at: string
}

export type WatchlistSortOrder =
  | 'added_desc'
  | 'added_asc'
  | 'title_asc'
  | 'title_desc'
  | 'release_desc'
  | 'release_asc'

export type WatchlistViewMode = 'list' | 'tags'

export interface AccountWatchlist {
  username: string
  revision: string
  sort_order: WatchlistSortOrder
  view_mode: WatchlistViewMode
  filter_tag_id: string | null
  items: WatchlistItem[]
  tags: WatchlistTag[]
  external_search_available: boolean
}

export type WatchlistTagColor =
  | 'neutral'
  | 'red'
  | 'amber'
  | 'green'
  | 'teal'
  | 'blue'
  | 'violet'
  | 'rose'

export interface WatchlistTag {
  id: string
  name: string
  color: WatchlistTagColor
}

export interface CreateWatchlistTag {
  expected_username: string
  expected_revision: string
  name: string
  color: WatchlistTagColor
}

export interface DeleteWatchlistTag {
  expected_username: string
  expected_revision: string
  tag_id: string
}

export interface UpdateWatchlistTag extends DeleteWatchlistTag {
  name: string
  color: WatchlistTagColor
}

export interface AssignWatchlistTag extends DeleteWatchlistTag {
  movie_slug: string
  assigned: 'true' | 'false'
}

export interface ExternalWatchlistMovie {
  tmdb_id: string
  title: string
  original_title?: string | null
  poster_url?: string | null
  release_date?: string | null
}

export interface WatchlistSearch {
  username: string
  catalog: WatchlistMovie[]
  external: ExternalWatchlistMovie[]
  external_status: 'ready' | 'unavailable' | 'disabled'
  catalog_has_more: boolean
}

export interface SaveWatchlist {
  expected_username: string
  expected_revision: string
  movie_slug: string
  saved: 'true' | 'false'
}

export interface ImportWatchlist {
  expected_username: string
  expected_revision: string
  tmdb_id: string
}

export interface SaveWatchlistSort {
  expected_username: string
  expected_revision: string
  sort_order: WatchlistSortOrder
}

export interface SaveWatchlistPreferences {
  expected_username: string
  expected_revision: string
  view_mode: WatchlistViewMode
  filter_tag_id: string | null
}

export interface ImportedWatchlist {
  watchlist: AccountWatchlist
  movie_slug: string
}
