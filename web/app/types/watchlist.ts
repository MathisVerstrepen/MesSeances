export interface WatchlistMovie {
  slug: string
  title: string
  poster_url?: string | null
  release_date?: string | null
}

export interface WatchlistItem extends WatchlistMovie {
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

export interface AccountWatchlist {
  username: string
  revision: string
  sort_order: WatchlistSortOrder
  items: WatchlistItem[]
  external_search_available: boolean
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

export interface ImportedWatchlist {
  watchlist: AccountWatchlist
  movie_slug: string
}
