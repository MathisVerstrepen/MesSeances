import { randomBytes } from 'node:crypto'
import type { H3Event } from 'h3'

export const INTERNAL_API_TOKEN_HEADER = 'X-Messeances-Internal-Token'
export const REQUEST_ID_HEADER = 'X-Request-ID'

const REQUEST_ID_PATTERN = /^[0-9a-f]{32}$/
const REQUEST_ID_CONTEXT_KEY = 'messeancesRequestId'

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
  const candidate = event.context[REQUEST_ID_CONTEXT_KEY]
  const current = Object.prototype.toString.call(candidate) === '[object String]' ? String(candidate) : ''
  if (REQUEST_ID_PATTERN.test(current)) return current

  const requestId = generateRequestId()
  event.context[REQUEST_ID_CONTEXT_KEY] = requestId
  return requestId
}

export function buildInternalApiHeaders(secret: string, requestId: string): InternalApiHeaders {
  if (!REQUEST_ID_PATTERN.test(requestId)) throw new Error('Invalid internal request ID')

  const headers: InternalApiHeaders = { [REQUEST_ID_HEADER]: requestId }
  if (secret !== '') headers[INTERNAL_API_TOKEN_HEADER] = secret
  return headers
}

export function internalApiHeaders(event: H3Event, secret: string): InternalApiHeaders {
  return buildInternalApiHeaders(secret, requestIdForEvent(event))
}
