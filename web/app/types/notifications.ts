export type NotificationKind = 'cinema_activity' | 'watchlist_screening'
export type NotificationScope = 'followed' | 'preferred' | 'custom'

export interface NotificationMovie {
  slug: string
  title: string
  poster_url: string | null
  updated_at: string
}

export interface NotificationTheater {
  id: string
  slug: string
  name: string
  city: string
}

export interface NotificationStatus {
  username: string
  unread_count: number
  latest_id: string
}

export interface AccountNotification {
  id: string
  kind: NotificationKind
  rule_id: string | null
  created_at: string
  read: boolean
  event_count: number
  movie_count: number
  theater_count: number
  movies: NotificationMovie[]
  theaters: NotificationTheater[]
}

export interface AccountNotifications extends NotificationStatus {
  items: AccountNotification[]
  limit: number
  next_cursor: string | null
}

export interface NotificationRule {
  id: string
  kind: NotificationKind
  theater_scope: NotificationScope
  theater_ids: string[]
  include_returns: boolean
  enabled: boolean
  push: boolean
  email: boolean
  created_at: string
}

export interface NotificationRules {
  username: string
  rules: NotificationRule[]
  max_rules: number
  max_custom_theaters: number
}

export interface NotificationRuleDraft {
  kind: NotificationKind
  theater_scope: NotificationScope
  theater_ids: string[]
  include_returns: boolean
  enabled: boolean
  push: boolean
  email: boolean
}

export type DigestFrequency = 'off' | 'daily' | 'weekly'

export interface NotificationDigest {
  username: string
  frequency: DigestFrequency
  next_due_at: string | null
  last_sent_at: string | null
  available: boolean
}

export interface PushDevice {
  id: string
  label: string
  created_at: string
  last_success_at: string | null
}

export interface PushDevices {
  username: string
  available: boolean
  public_key: string
  devices: PushDevice[]
  max_devices: number
  current_id: string | null
}
