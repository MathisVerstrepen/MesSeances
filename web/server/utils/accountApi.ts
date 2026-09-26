import { generateRequestId } from './internalApi.ts'

export function accountCookieHeader(
  cookie: string,
  siteUrl: string,
): string | undefined {
  const url = new URL(siteUrl)
  const local =
    url.protocol === 'http:' &&
    ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  const name = local ? 'messeances_session_dev' : '__Host-messeances_session'
  const matches = cookie
    .split(';')
    .map((part) => part.trim())
    .filter((part) => part.startsWith(`${name}=`))
  if (matches.length !== 1) return undefined
  const value = matches[0]!.slice(name.length + 1)
  if (!/^[A-Za-z0-9_-]{43}$/.test(value)) return undefined
  return `${name}=${value}`
}

interface AccountRequestHeaders {
  'X-Request-ID': string
  Cookie?: string
}

export function accountRequestHeaders(
  cookie: string,
  siteUrl: string,
): AccountRequestHeaders {
  const headers: AccountRequestHeaders = { 'X-Request-ID': generateRequestId() }
  const selected = accountCookieHeader(cookie, siteUrl)
  if (selected) headers.Cookie = selected
  return headers
}
