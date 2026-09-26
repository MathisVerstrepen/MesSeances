import type { AccountContinuation } from '../types/account'

export function googleAuthorizationUrl(value: string): string {
  const url = new URL(value)
  if (
    url.origin !== 'https://accounts.google.com' ||
    url.username ||
    url.password
  )
    throw new Error('Invalid provider URL')
  return url.href
}

// Accept a raw opaque token or the exact same-origin mail link, never a redirect.
export function accountEmailConfirmationToken(
  value: string,
  origin: string,
): string {
  const input = value.trim()
  if (/^[A-Za-z0-9_-]{43}$/.test(input)) return input
  try {
    const url = new URL(input)
    if (
      url.origin !== origin ||
      url.username ||
      url.password ||
      url.pathname !== '/compte/confirmer-email' ||
      url.search
    )
      return ''
    const match = /^#token=([A-Za-z0-9_-]{43})$/.exec(url.hash)
    return match?.[1] ?? ''
  } catch {
    return ''
  }
}

export function validAccountContinuation(
  value: AccountContinuation,
  now = Date.now(),
): boolean {
  if (
    ![
      'password_add',
      'email_change',
      'google_link',
      'google_unlink',
      'delete_account',
    ].includes(value.action)
  )
    return false
  if (value.action === 'email_change' ? !value.target : value.target !== null)
    return false
  return (
    Number.isFinite(Date.parse(value.expires_at)) &&
    Date.parse(value.expires_at) > now
  )
}
