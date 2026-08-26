const MAX_TARGET_BYTES = 2048
const CODE_PATTERN = /^[A-Za-z0-9_-]{22}$/
const SLUG_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]*$/
const CONTROL_PATTERN = /\p{Cc}/u
const SHARED_THEATERS_KEY = 'shared_theaters'
const THEATER_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/

type RoutePattern = '/' | '/planning' | '/recherche' | '/films' | '/credits' | '/film/:slug' | '/cinema/:slug' | '/ville/:slug/cinemas'

const QUERY_KEYS = {
  '/': new Set(),
  '/planning': new Set(['date', 'language', 'format', 'mode', 'zoom', SHARED_THEATERS_KEY]),
  '/recherche': new Set(['theaters', 'date', 'start_after', 'finish_before', 'language', 'format', 'include_ads', 'buffer_ads', 'grouping', 'layout', SHARED_THEATERS_KEY]),
  '/films': new Set(['q', 'sort', 'page', SHARED_THEATERS_KEY]),
  '/credits': new Set([SHARED_THEATERS_KEY]),
  '/film/:slug': new Set(['date', 'language', 'format', 'sort', SHARED_THEATERS_KEY]),
  '/cinema/:slug': new Set(['date', 'grouping', 'layout', 'view', SHARED_THEATERS_KEY]),
  '/ville/:slug/cinemas': new Set([SHARED_THEATERS_KEY])
} satisfies Record<RoutePattern, ReadonlySet<string>>

export function isValidShortLinkCode(code: string): boolean {
  return CODE_PATTERN.test(code)
}

export function isValidShortLinkTarget(target: string): boolean {
  if (!target || utf8ByteLength(target) > MAX_TARGET_BYTES || !isValidUnicode(target)) return false
  if (!target.startsWith('/') || target.startsWith('//') || target.includes('\\') || target.includes('#') || CONTROL_PATTERN.test(target)) return false

  const queryIndex = target.indexOf('?')
  const path = queryIndex === -1 ? target : target.slice(0, queryIndex)
  const rawQuery = queryIndex === -1 ? null : target.slice(queryIndex + 1)
  if (path.includes('%') || !isAllowedPath(path)) return false

  const pattern = routePattern(path)
  if (!pattern) return false
  if (rawQuery === null) return true
  return isValidQuery(rawQuery, QUERY_KEYS[pattern]!)
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

function isValidUnicode(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index)
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1)
      if (index + 1 >= value.length || next < 0xdc00 || next > 0xdfff) return false
      index++
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return false
    }
  }
  return true
}

function isAllowedPath(path: string): boolean {
  if (path === '/') return true
  if (path.endsWith('/') || path.includes('//')) return false
  return path.slice(1).split('/').every((segment) => segment !== '' && segment !== '.' && segment !== '..')
}

function routePattern(path: string): RoutePattern | null {
  switch (path) {
    case '/':
    case '/planning':
    case '/recherche':
    case '/films':
    case '/credits':
      return path
  }
  const segments = path.slice(1).split('/')
  if (segments.length === 2 && segments[0] === 'film' && SLUG_PATTERN.test(segments[1]!)) return '/film/:slug'
  if (segments.length === 2 && segments[0] === 'cinema' && SLUG_PATTERN.test(segments[1]!)) return '/cinema/:slug'
  if (segments.length === 3 && segments[0] === 'ville' && SLUG_PATTERN.test(segments[1]!) && segments[2] === 'cinemas') return '/ville/:slug/cinemas'
  return null
}

function isValidQuery(rawQuery: string, allowedKeys: ReadonlySet<string>): boolean {
  if (!rawQuery) return false
  const seen = new Set<string>()
  for (const field of rawQuery.split('&')) {
    if (!field) return false
    const separator = field.indexOf('=')
    const rawKey = separator === -1 ? field : field.slice(0, separator)
    const rawValue = separator === -1 ? '' : field.slice(separator + 1)
    const key = decodeQueryPart(rawKey)
    const value = decodeQueryPart(rawValue)
    if (key === null || value === null || CONTROL_PATTERN.test(key) || CONTROL_PATTERN.test(value)) return false
    if (!allowedKeys.has(key) || seen.has(key)) return false
    if (key === SHARED_THEATERS_KEY && !isValidSharedTheaters(value)) return false
    seen.add(key)
  }
  return true
}

function isValidSharedTheaters(value: string): boolean {
  if (!value) return false
  const ids = value.split(',')
  return ids.every((id) => THEATER_ID_PATTERN.test(id)) && new Set(ids).size === ids.length
}

function decodeQueryPart(value: string): string | null {
  try {
    return decodeURIComponent(value.replaceAll('+', ' '))
  } catch {
    return null
  }
}
