import type { Theater, TheaterActivityItem } from './api'

export interface ActivityVisit {
  username: string
  scope: string
  previous_last_event_id: string
}

export interface AccountTheaterFollows {
  username: string
  revision: string
  theater_ids: string[]
}

export interface SaveAccountTheaterFollow {
  expected_username: string
  expected_revision: string
  theater_id: string
  followed: 'true' | 'false'
}

export interface AccountActivityItem extends TheaterActivityItem {
  theater: Pick<Theater, 'id' | 'slug' | 'name' | 'city' | 'provider'>
}

export interface AccountActivityResponse {
  username: string
  follows_revision: string
  followed_theater_count: number
  generated_at: string
  timezone: 'Europe/Paris'
  coverage: {
    initialized_theater_count: number
    completeness: 'unknown' | 'partial'
    bootstrap: 'baseline'
    return_minimum_break_days: 28
  }
  items: AccountActivityItem[]
  limit: number
  next_cursor: string | null
}
