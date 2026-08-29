import type { SlotResult, TheaterShowtimesResponse } from '../types/api'
import type { ResultGrouping, ResultLayout, ShowtimeMovieResultGroup, ShowtimeResultViewModel } from '../types/showtimeResults'

export const resultGroupingOptions: [{ value: ResultGrouping; label: string }, { value: ResultGrouping; label: string }] = [
  { value: 'movie', label: 'Par film' },
  { value: 'chronological', label: 'Chronologique' }
]

export const resultLayoutOptions: [{ value: ResultLayout; label: string }, { value: ResultLayout; label: string }] = [
  { value: 'lines', label: 'Lignes' },
  { value: 'boxes', label: 'Boîtes' }
]

export function toSlotShowtimeResults(results: readonly SlotResult[]): ShowtimeResultViewModel[] {
  return results.map((result) => ({
    key: `${result.showtime.provider}:${result.showtime.id}`,
    showtimeId: result.showtime.id,
    provider: result.showtime.provider,
    movieKey: `${result.showtime.provider}:${result.showtime.movie.slug}`,
    movieSlug: result.showtime.movie.slug,
    movieTitle: result.showtime.movie.title,
    movieRuntimeMinutes: result.showtime.movie.runtime_minutes,
    theaterName: result.theater.name,
    advertisedStartTime: result.showtime.start_time,
    effectiveStartTime: result.effective_start_time,
    endTime: result.showtime.end_time,
    language: result.showtime.language,
    format: result.showtime.format,
    room: result.showtime.room,
    bookingUrl: result.showtime.booking_url,
    posterUrl: result.poster_url,
    backdropUrl: result.backdrop_url
  }))
}

export function toTheaterShowtimeResults(response: TheaterShowtimesResponse): ShowtimeResultViewModel[] {
  return response.showtimes.map((showtime) => ({
    key: `${showtime.provider}:${showtime.id}`,
    showtimeId: showtime.id,
    provider: showtime.provider,
    movieKey: `${showtime.provider}:${showtime.movie.slug}`,
    movieSlug: showtime.movie.slug,
    movieTitle: showtime.movie.title,
    movieRuntimeMinutes: showtime.movie.runtime_minutes,
    theaterName: response.theater.name,
    advertisedStartTime: showtime.start_time,
    effectiveStartTime: showtime.start_time,
    endTime: showtime.end_time,
    language: showtime.language,
    format: showtime.format,
    room: showtime.room,
    bookingUrl: showtime.booking_url,
    posterUrl: showtime.poster_url,
    backdropUrl: showtime.backdrop_url
  }))
}

export function sortShowtimeResults(results: readonly ShowtimeResultViewModel[]): ShowtimeResultViewModel[] {
  return [...results].sort((first, second) => {
    const timeDifference = Date.parse(first.advertisedStartTime) - Date.parse(second.advertisedStartTime)
    return timeDifference || first.showtimeId.localeCompare(second.showtimeId)
  })
}

export function groupShowtimeResults(results: readonly ShowtimeResultViewModel[]): ShowtimeMovieResultGroup[] {
  const groups = new Map<string, ShowtimeResultViewModel[]>()
  for (const result of results) {
    const group = groups.get(result.movieKey)
    if (group) group.push(result)
    else groups.set(result.movieKey, [result])
  }
  return [...groups.entries()].map(([key, groupedResults]) => ({ key, results: groupedResults }))
}

const BASE64URL_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
const MAX_UINT64_DECIMAL = '18446744073709551615'
const UGC_SELECTION_KEY_PATTERN = /^ugc:ugc-showing-([0-9]{1,128})$/
const KINEPOLIS_SELECTION_KEY_PATTERN = /^kinepolis:kinepolis-showing-([A-Za-z0-9][A-Za-z0-9_-]{0,127})$/
const PATHE_SELECTION_KEY_PATTERN = /^pathe:pathe-showing-(V[1-9][0-9]*S[1-9][0-9]*)$/
const CGR_SELECTION_KEY_PATTERN = /^cgr:cgr-showing-([A-Z][0-9]{4})-([a-f0-9]{64})$/
const KINEPOLIS_PROVIDER_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/
const PATHE_PROVIDER_ID_PATTERN = /^V[1-9][0-9]*S[1-9][0-9]*$/
const CGR_TOKEN_PATTERN = /^c([A-Z][0-9]{4})-([A-Za-z0-9_-]{43})$/

function isValidUgcProviderID(value: string): boolean {
  if (!/^[0-9]{1,128}$/.test(value)) return false
  const normalized = value.replace(/^0+/, '')
  if (!normalized) return false
  return normalized.length < MAX_UINT64_DECIMAL.length
    || (normalized.length === MAX_UINT64_DECIMAL.length && normalized <= MAX_UINT64_DECIMAL)
}

function hexToBase64Url(hex: string): string {
  let output = ''
  let buffer = 0
  let bitCount = 0
  for (let index = 0; index < hex.length; index += 2) {
    buffer = (buffer << 8) | Number.parseInt(hex.slice(index, index + 2), 16)
    bitCount += 8
    while (bitCount >= 6) {
      bitCount -= 6
      output += BASE64URL_ALPHABET[(buffer >> bitCount) & 0x3f]
      buffer &= (1 << bitCount) - 1
    }
  }
  if (bitCount > 0) output += BASE64URL_ALPHABET[(buffer << (6 - bitCount)) & 0x3f]
  return output
}

function base64UrlToHex(value: string): string | null {
  let output = ''
  let buffer = 0
  let bitCount = 0
  for (const character of value) {
    const sextet = BASE64URL_ALPHABET.indexOf(character)
    if (sextet < 0) return null
    buffer = (buffer << 6) | sextet
    bitCount += 6
    if (bitCount >= 8) {
      bitCount -= 8
      output += ((buffer >> bitCount) & 0xff).toString(16).padStart(2, '0')
      buffer &= (1 << bitCount) - 1
    }
  }
  if (bitCount > 0 && buffer !== 0) return null
  return output
}

function encodeShowtimeSelectionKey(key: string): string | null {
  const ugcMatch = UGC_SELECTION_KEY_PATTERN.exec(key)
  if (ugcMatch?.[1] && isValidUgcProviderID(ugcMatch[1])) return `u${ugcMatch[1]}`

  const kinepolisMatch = KINEPOLIS_SELECTION_KEY_PATTERN.exec(key)
  if (kinepolisMatch?.[1]) return `k${kinepolisMatch[1]}`

  const patheMatch = PATHE_SELECTION_KEY_PATTERN.exec(key)
  if (patheMatch?.[1] && patheMatch[1].length <= 115) return `p${patheMatch[1]}`

  const cgrMatch = CGR_SELECTION_KEY_PATTERN.exec(key)
  if (cgrMatch?.[1] && cgrMatch[2]) return `c${cgrMatch[1]}-${hexToBase64Url(cgrMatch[2])}`
  return null
}

function decodeShowtimeSelectionToken(token: string): string | null {
  if (token.startsWith('u')) {
    const providerID = token.slice(1)
    return isValidUgcProviderID(providerID) ? `ugc:ugc-showing-${providerID}` : null
  }
  if (token.startsWith('k')) {
    const providerID = token.slice(1)
    return KINEPOLIS_PROVIDER_ID_PATTERN.test(providerID) ? `kinepolis:kinepolis-showing-${providerID}` : null
  }
  if (token.startsWith('p')) {
    const providerID = token.slice(1)
    return providerID.length <= 115 && PATHE_PROVIDER_ID_PATTERN.test(providerID) ? `pathe:pathe-showing-${providerID}` : null
  }

  const cgrMatch = CGR_TOKEN_PATTERN.exec(token)
  if (!cgrMatch?.[1] || !cgrMatch[2]) return null
  const hash = base64UrlToHex(cgrMatch[2])
  if (!hash || hash.length !== 64 || hexToBase64Url(hash) !== cgrMatch[2]) return null
  return `cgr:cgr-showing-${cgrMatch[1]}-${hash}`
}

export function parseShowtimeSelection(value: string | undefined): string[] {
  if (!value) return []
  const keys = value.split(',').map(decodeShowtimeSelectionToken).filter((key): key is string => key !== null)
  return [...new Set(keys)].sort()
}

export function serializeShowtimeSelection(keys: readonly string[]): string | undefined {
  const tokens = [...new Set(keys)].sort().map(encodeShowtimeSelectionKey).filter((token): token is string => token !== null)
  return tokens.length > 0 ? tokens.join(',') : undefined
}

export function validShowtimeSelectionKeys(results: readonly ShowtimeResultViewModel[], keys: readonly string[]): string[] {
  const availableKeys = new Set(results.map((result) => result.key))
  return [...new Set(keys.filter((key) => availableKeys.has(key)))].sort()
}

function showtimeInterval(result: ShowtimeResultViewModel): readonly [number, number] | null {
  const start = Date.parse(result.effectiveStartTime)
  const end = Date.parse(result.endTime)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return null
  return [start, end]
}

export function areShowtimeResultsCompatible(first: ShowtimeResultViewModel, second: ShowtimeResultViewModel): boolean {
  const firstInterval = showtimeInterval(first)
  const secondInterval = showtimeInterval(second)
  if (!firstInterval || !secondInterval) return true
  return firstInterval[1] <= secondInterval[0] || firstInterval[0] >= secondInterval[1]
}

export function filterCompatibleShowtimeResults(results: readonly ShowtimeResultViewModel[], selectedKeys: readonly string[]): ShowtimeResultViewModel[] {
  const validKeys = validShowtimeSelectionKeys(results, selectedKeys)
  if (validKeys.length === 0) return [...results]

  const selectedKeySet = new Set(validKeys)
  const selectedResults = results.filter((result) => selectedKeySet.has(result.key))
  return results.filter((result) => selectedKeySet.has(result.key)
    || selectedResults.every((selectedResult) => areShowtimeResultsCompatible(result, selectedResult)))
}
