import { randomBytes } from 'node:crypto'
import { isIP } from 'node:net'
import type { H3Event } from 'h3'

export const INTERNAL_API_TOKEN_HEADER = 'X-Messeances-Internal-Token'
export const REQUEST_ID_HEADER = 'X-Request-ID'
// The API gives each forwarded visitor its own server-rendering budget.
export const CLIENT_IP_HEADER = 'X-Messeances-Client-IP'

const REQUEST_ID_PATTERN = /^[0-9a-f]{32}$/
const REQUEST_ID_CONTEXT_KEY = 'messeancesRequestId'
const CLIENT_IP_CONTEXT_KEY = 'messeancesClientIp'

export type InternalApiHeaders = Record<string, string> & {
  'X-Request-ID': string
}

export function generateRequestId(): string {
  return randomBytes(16).toString('hex')
}

export function initializeRequestId(event: H3Event): string {
  const requestId = generateRequestId()
  event.context[REQUEST_ID_CONTEXT_KEY] = requestId
  return requestId
}

export function requestIdForEvent(event: H3Event): string {
  const current = contextString(event, REQUEST_ID_CONTEXT_KEY)
  if (REQUEST_ID_PATTERN.test(current)) return current

  const requestId = generateRequestId()
  event.context[REQUEST_ID_CONTEXT_KEY] = requestId
  return requestId
}

function contextString(event: H3Event, key: string): string {
  const candidate = event.context[key]
  return Object.prototype.toString.call(candidate) === '[object String]'
    ? String(candidate)
    : ''
}

// With a header name, the address comes from the reverse proxy, which must
// overwrite that header on every request. Without one, the socket peer is
// the visitor. Anything but one plain address yields an empty string.
export function resolveClientIp(event: H3Event, header: string): string {
  const request = event.node.req
  const raw =
    header === ''
      ? request.socket?.remoteAddress
      : request.headers[header.toLowerCase()]
  const value = Array.isArray(raw) ? '' : (raw ?? '').trim()
  return isIP(value) ? value : ''
}

// Runs before route caching, which hides request headers from the render.
export function initializeClientIp(event: H3Event, header: string): string {
  const clientIp = resolveClientIp(event, header)
  event.context[CLIENT_IP_CONTEXT_KEY] = clientIp
  return clientIp
}

export function clientIpForEvent(event: H3Event): string {
  const current = contextString(event, CLIENT_IP_CONTEXT_KEY)
  return isIP(current) ? current : ''
}

export function buildInternalApiHeaders(
  secret: string,
  requestId: string,
  clientIp = '',
): InternalApiHeaders {
  if (!REQUEST_ID_PATTERN.test(requestId))
    throw new Error('Invalid internal request ID')

  const headers: InternalApiHeaders = { [REQUEST_ID_HEADER]: requestId }
  if (secret !== '') {
    headers[INTERNAL_API_TOKEN_HEADER] = secret
    // The API only trusts the address next to a valid token.
    if (isIP(clientIp)) headers[CLIENT_IP_HEADER] = clientIp
  }
  return headers
}

export function internalApiHeaders(
  event: H3Event,
  secret: string,
): InternalApiHeaders {
  return buildInternalApiHeaders(
    secret,
    requestIdForEvent(event),
    clientIpForEvent(event),
  )
}
