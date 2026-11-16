import type { Theater, TheaterActivityItem } from './api'

export interface ActivityNotification {
  username: string
  follows_revision: string
  has_new_activity: boolean
}

export interface ActivityVisit {
  username: string
  scope: string
  previous_last_event_id: string
}

export interface ActivityReading {
  username: string
  manual_reading: boolean
  last_read_event_id: string
}

export interface ActivityRead {
  username: string
  event_id: string
  consulted: boolean
}

export interface ActivityReadingSnapshot {
  reading: ActivityReading
  items: ActivityRead[]
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
  consulted: boolean
  theater: Pick<Theater, 'id' | 'slug' | 'name' | 'city' | 'provider'>
}

export interface AccountActivityResponse {
  reading: ActivityReading
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
