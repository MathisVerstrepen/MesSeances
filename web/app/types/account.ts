export type AccountState =
  | 'anonymous'
  | 'pending_email'
  | 'pending_username'
  | 'complete'

declare global {
  interface Window {
    __takeAccountToken?: (path?: string) => string
  }
}

export interface AccountView {
  email: string
  username: string | null
  has_password: boolean
  google_linked: boolean
}

export interface AccountSession {
  enabled: boolean
  state: AccountState
  account: AccountView | null
}

export interface AccountDetails extends AccountView {
  avatar_url: string | null
  google_email: string | null
  pending_email: string | null
  allowed_methods: ('password' | 'google')[]
}

export interface AccountAvatarResult {
  avatar_url: string | null
}

export interface AccountTheaterPreferences {
  username: string
  revision: string
  theater_ids: string[]
}

export interface SaveAccountTheaterPreferences {
  expected_username: string
  expected_revision: string
  theater_ids: string
}

export type AccountAction =
  | 'password_add'
  | 'password_change'
  | 'email_change'
  | 'google_link'
  | 'google_unlink'
  | 'delete_account'

export interface AccountContinuation {
  action: AccountAction
  target: string | null
  expires_at: string
}

export type GoogleStart =
  | { mode: 'login' }
  | { mode: 'link'; grant: string }
  | { mode: 'reauth'; action: AccountAction; target?: string }
