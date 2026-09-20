import type { SlotResult, TheaterShowtimesResponse } from '../types/api'
import type {
  ResultGrouping,
  ResultLayout,
  ShowtimeMovieResultGroup,
  ShowtimeResultViewModel,
} from '../types/showtimeResults'
import { resolveShowtimeEnd } from './showtimeEnd.ts'
import { isValidMk2ShowingId } from './mk2.ts'

export const resultGroupingOptions: [
  { value: ResultGrouping; label: string },
  { value: ResultGrouping; label: string },
] = [
  { value: 'movie', label: 'Par film' },
  { value: 'chronological', label: 'Chronologique' },
]

export const resultLayoutOptions: [
  { value: ResultLayout; label: string },
  { value: ResultLayout; label: string },
] = [
  { value: 'lines', label: 'Lignes' },
  { value: 'boxes', label: 'Boîtes' },
]

export function toSlotShowtimeResults(
  results: readonly SlotResult[],
): ShowtimeResultViewModel[] {
  return results.map((result) => ({
    key: `${result.showtime.provider}:${result.showtime.id}`,
    showtimeId: result.showtime.id,
    provider: result.showtime.provider,
    movieKey: `${result.showtime.provider}:${result.showtime.movie.slug}`,
    movieSlug: result.showtime.movie.slug,
    movieTitle: result.showtime.movie.title,
    movieRuntimeMinutes: result.showtime.movie.runtime_minutes,
    theaterName: result.theater.name,
    theaterId: result.theater.id,
    advertisedStartTime: result.showtime.start_time,
    effectiveStartTime: result.effective_start_time,
    end: resolveShowtimeEnd(result.showtime),
    language: result.showtime.language,
    format: result.showtime.format,
    room: result.showtime.room,
    bookingUrl: result.showtime.booking_url,
    posterUrl: result.poster_url,
    backdropUrl: result.backdrop_url,
  }))
}

export function toTheaterShowtimeResults(
  response: TheaterShowtimesResponse,
): ShowtimeResultViewModel[] {
  return response.showtimes.map((showtime) => ({
    key: `${showtime.provider}:${showtime.id}`,
    showtimeId: showtime.id,
    provider: showtime.provider,
    movieKey: `${showtime.provider}:${showtime.movie.slug}`,
    movieSlug: showtime.movie.slug,
    movieTitle: showtime.movie.title,
    movieRuntimeMinutes: showtime.movie.runtime_minutes,
    theaterName: response.theater.name,
    theaterId: response.theater.id,
    advertisedStartTime: showtime.start_time,
    effectiveStartTime: showtime.start_time,
    end: resolveShowtimeEnd(showtime),
    language: showtime.language,
    format: showtime.format,
    room: showtime.room,
    bookingUrl: showtime.booking_url,
    posterUrl: showtime.poster_url,
    backdropUrl: showtime.backdrop_url,
  }))
}

export function sortShowtimeResults(
  results: readonly ShowtimeResultViewModel[],
): ShowtimeResultViewModel[] {
  return [...results].sort((first, second) => {
    const timeDifference =
      Date.parse(first.advertisedStartTime) -
      Date.parse(second.advertisedStartTime)
    return timeDifference || first.showtimeId.localeCompare(second.showtimeId)
  })
}

export function groupShowtimeResults(
  results: readonly ShowtimeResultViewModel[],
): ShowtimeMovieResultGroup[] {
  const groups = new Map<string, ShowtimeResultViewModel[]>()
  for (const result of results) {
    const group = groups.get(result.movieKey)
    if (group) group.push(result)
    else groups.set(result.movieKey, [result])
  }
  return [...groups.entries()].map(([key, groupedResults]) => ({
    key,
    results: groupedResults,
  }))
}

const BASE64URL_ALPHABET =
  'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
const MAX_UINT64_DECIMAL = '18446744073709551615'
const UGC_SELECTION_KEY_PATTERN = /^ugc:ugc-showing-([0-9]{1,128})$/
const KINEPOLIS_SELECTION_KEY_PATTERN =
  /^kinepolis:kinepolis-showing-([A-Za-z0-9][A-Za-z0-9_-]{0,127})$/
const PATHE_SELECTION_KEY_PATTERN =
  /^pathe:pathe-showing-(V[1-9][0-9]*S[1-9][0-9]*)$/
const CGR_SELECTION_KEY_PATTERN =
  /^cgr:cgr-showing-([A-Z][0-9]{4})-([a-f0-9]{64})$/
const GRAND_ECRAN_SELECTION_KEY_PATTERN =
  /^grandecran:grandecran-showing-([A-Z0-9]{5})-([a-f0-9]{64})$/
const GRAND_ECRAN_TOKEN_PATTERN = /^g([A-Z0-9]{5})-([A-Za-z0-9_-]{43})$/
const NOE_CINEMAS_SELECTION_KEY_PATTERN =
  /^noecinemas:noecinemas-showing-([A-Z0-9]{5})-([a-f0-9]{64})$/
const NOE_CINEMAS_TOKEN_PATTERN = /^n([A-Z0-9]{5})-([A-Za-z0-9_-]{43})$/
const KINEPOLIS_PROVIDER_ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/
const PATHE_PROVIDER_ID_PATTERN = /^V[1-9][0-9]*S[1-9][0-9]*$/
const CGR_TOKEN_PATTERN = /^c([A-Z][0-9]{4})-([A-Za-z0-9_-]{43})$/
const MEGARAMA_SELECTION_KEY_PATTERN =
  /^megarama:megarama-showing-([A-Za-z0-9][A-Za-z0-9_-]{0,110})$/
const MEGARAMA_TOKEN_PATTERN = /^m([A-Za-z0-9][A-Za-z0-9_-]{0,110})$/
const CINEVILLE_SELECTION_KEY_PATTERN =
  /^cineville:cineville-showing-([1-9][0-9]{0,18}-[1-9][0-9]{0,18})$/
const CINEVILLE_TOKEN_PATTERN = /^v([1-9][0-9]{0,18}-[1-9][0-9]{0,18})$/
const CINEWEST_SELECTION_KEY_PATTERN =
  /^cinewest:cinewest-showing-(cineoffice|ticketingcine|webediamovies)-([a-f0-9]{64})$/
const CINEWEST_TOKEN_PATTERN =
  /^w(cineoffice|ticketingcine|webediamovies)-([A-Za-z0-9_-]{43})$/

function isValidCinevilleShowingID(value: string): boolean {
  return value.split('-').every((id) => BigInt(id) <= 9223372036854775807n)
}

function isValidUgcProviderID(value: string): boolean {
  if (!/^[0-9]{1,128}$/.test(value)) return false
  const normalized = value.replace(/^0+/, '')
  if (!normalized) return false
  return (
    normalized.length < MAX_UINT64_DECIMAL.length ||
    (normalized.length === MAX_UINT64_DECIMAL.length &&
      normalized <= MAX_UINT64_DECIMAL)
  )
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
  if (bitCount > 0)
    output += BASE64URL_ALPHABET[(buffer << (6 - bitCount)) & 0x3f]
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
  const noeCinemas = NOE_CINEMAS_SELECTION_KEY_PATTERN.exec(key)
  if (noeCinemas?.[1] && noeCinemas[2] && noeCinemas[0] === key)
    return `n${noeCinemas[1]}-${hexToBase64Url(noeCinemas[2])}`
  const cinewest = CINEWEST_SELECTION_KEY_PATTERN.exec(key)
  if (cinewest?.[0] === key)
    return `w${cinewest[1]}-${hexToBase64Url(cinewest[2]!)}`
  const mk2Prefix = 'mk2:mk2-showing-'
  if (
    key.startsWith(mk2Prefix) &&
    isValidMk2ShowingId(key.slice(mk2Prefix.length))
  )
    return `x${key.slice(mk2Prefix.length)}`
  const ugcMatch = UGC_SELECTION_KEY_PATTERN.exec(key)
  if (ugcMatch?.[1] && isValidUgcProviderID(ugcMatch[1]))
    return `u${ugcMatch[1]}`

  const kinepolisMatch = KINEPOLIS_SELECTION_KEY_PATTERN.exec(key)
  if (kinepolisMatch?.[1]) return `k${kinepolisMatch[1]}`

  const patheMatch = PATHE_SELECTION_KEY_PATTERN.exec(key)
  if (patheMatch?.[1] && patheMatch[1].length <= 115) return `p${patheMatch[1]}`

  const cgrMatch = CGR_SELECTION_KEY_PATTERN.exec(key)
  if (cgrMatch?.[1] && cgrMatch[2])
    return `c${cgrMatch[1]}-${hexToBase64Url(cgrMatch[2])}`
  const grandEcranMatch = GRAND_ECRAN_SELECTION_KEY_PATTERN.exec(key)
  if (grandEcranMatch?.[1] && grandEcranMatch[2] && grandEcranMatch[0] === key)
    return `g${grandEcranMatch[1]}-${hexToBase64Url(grandEcranMatch[2])}`
  const megaramaMatch = MEGARAMA_SELECTION_KEY_PATTERN.exec(key)
  if (megaramaMatch?.[1] && megaramaMatch[0] === key)
    return `m${megaramaMatch[1]}`
  const cinevilleMatch = CINEVILLE_SELECTION_KEY_PATTERN.exec(key)
  if (
    cinevilleMatch?.[1] &&
    cinevilleMatch[0] === key &&
    isValidCinevilleShowingID(cinevilleMatch[1])
  )
    return `v${cinevilleMatch[1]}`
  return null
}

function decodeShowtimeSelectionToken(token: string): string | null {
  const noeCinemas = NOE_CINEMAS_TOKEN_PATTERN.exec(token)
  if (noeCinemas?.[1] && noeCinemas[2] && noeCinemas[0] === token) {
    const hash = base64UrlToHex(noeCinemas[2])
    if (!hash || hash.length !== 64 || hexToBase64Url(hash) !== noeCinemas[2])
      return null
    return `noecinemas:noecinemas-showing-${noeCinemas[1]}-${hash}`
  }
  const cinewest = CINEWEST_TOKEN_PATTERN.exec(token)
  if (cinewest?.[0] === token) {
    const hash = base64UrlToHex(cinewest[2]!)
    if (!hash || hash.length !== 64 || hexToBase64Url(hash) !== cinewest[2])
      return null
    return `cinewest:cinewest-showing-${cinewest[1]}-${hash}`
  }
  if (token.startsWith('x'))
    return isValidMk2ShowingId(token.slice(1))
      ? `mk2:mk2-showing-${token.slice(1)}`
      : null
  const cinevilleMatch = CINEVILLE_TOKEN_PATTERN.exec(token)
  if (
    cinevilleMatch?.[1] &&
    cinevilleMatch[0] === token &&
    isValidCinevilleShowingID(cinevilleMatch[1])
  )
    return `cineville:cineville-showing-${cinevilleMatch[1]}`
  const megaramaMatch = MEGARAMA_TOKEN_PATTERN.exec(token)
  if (megaramaMatch?.[1] && megaramaMatch[0] === token)
    return `megarama:megarama-showing-${megaramaMatch[1]}`
  if (token.startsWith('u')) {
    const providerID = token.slice(1)
    return isValidUgcProviderID(providerID)
      ? `ugc:ugc-showing-${providerID}`
      : null
  }
  if (token.startsWith('k')) {
    const providerID = token.slice(1)
    return KINEPOLIS_PROVIDER_ID_PATTERN.test(providerID)
      ? `kinepolis:kinepolis-showing-${providerID}`
      : null
  }
  if (token.startsWith('p')) {
    const providerID = token.slice(1)
    return providerID.length <= 115 &&
      PATHE_PROVIDER_ID_PATTERN.test(providerID)
      ? `pathe:pathe-showing-${providerID}`
      : null
  }

  const grandEcranMatch = GRAND_ECRAN_TOKEN_PATTERN.exec(token)
  if (
    grandEcranMatch?.[1] &&
    grandEcranMatch[2] &&
    grandEcranMatch[0] === token
  ) {
    const hash = base64UrlToHex(grandEcranMatch[2])
    if (
      !hash ||
      hash.length !== 64 ||
      hexToBase64Url(hash) !== grandEcranMatch[2]
    )
      return null
    return `grandecran:grandecran-showing-${grandEcranMatch[1]}-${hash}`
  }
  const cgrMatch = CGR_TOKEN_PATTERN.exec(token)
  if (!cgrMatch?.[1] || !cgrMatch[2]) return null
  const hash = base64UrlToHex(cgrMatch[2])
  if (!hash || hash.length !== 64 || hexToBase64Url(hash) !== cgrMatch[2])
    return null
  return `cgr:cgr-showing-${cgrMatch[1]}-${hash}`
}

export function parseShowtimeSelection(value: string | undefined): string[] {
  if (!value) return []
  const keys = value
    .split(',')
    .map(decodeShowtimeSelectionToken)
    .filter((key): key is string => key !== null)
  return [...new Set(keys)].sort()
}

export function serializeShowtimeSelection(
  keys: readonly string[],
): string | undefined {
  const tokens = [...new Set(keys)]
    .sort()
    .map(encodeShowtimeSelectionKey)
    .filter((token): token is string => token !== null)
  return tokens.length > 0 ? tokens.join(',') : undefined
}

export function showtimeSelectionQueryValues(
  keys: readonly string[],
  selectedOnly: boolean,
) {
  const selected = serializeShowtimeSelection(keys)
  return { selected, selected_only: selected && selectedOnly ? '1' : undefined }
}

export function validShowtimeSelectionKeys(
  results: readonly ShowtimeResultViewModel[],
  keys: readonly string[],
): string[] {
  const availableKeys = new Set(results.map((result) => result.key))
  return [...new Set(keys.filter((key) => availableKeys.has(key)))].sort()
}

function showtimeInterval(
  result: ShowtimeResultViewModel,
): readonly [number, number] | null {
  if (!result.end) return null
  const start = Date.parse(result.effectiveStartTime)
  const end = Date.parse(result.end.time)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start)
    return null
  return [start, end]
}

export function areShowtimeResultsCompatible(
  first: ShowtimeResultViewModel,
  second: ShowtimeResultViewModel,
): boolean {
  const firstInterval = showtimeInterval(first)
  const secondInterval = showtimeInterval(second)
  if (!firstInterval || !secondInterval) return false
  return (
    firstInterval[1] <= secondInterval[0] ||
    firstInterval[0] >= secondInterval[1]
  )
}

export function filterCompatibleShowtimeResults(
  results: readonly ShowtimeResultViewModel[],
  selectedKeys: readonly string[],
): ShowtimeResultViewModel[] {
  const validKeys = validShowtimeSelectionKeys(results, selectedKeys)
  if (validKeys.length === 0) return [...results]

  const selectedKeySet = new Set(validKeys)
  const selectedResults = results.filter((result) =>
    selectedKeySet.has(result.key),
  )
  return results.filter(
    (result) =>
      selectedKeySet.has(result.key) ||
      selectedResults.every((selectedResult) =>
        areShowtimeResultsCompatible(result, selectedResult),
      ),
  )
}

export function filterSelectedShowtimeResults(
  results: readonly ShowtimeResultViewModel[],
  selectedKeys: readonly string[],
): ShowtimeResultViewModel[] {
  const selectedKeySet = new Set(selectedKeys)
  const selectedResults = results.filter((result) =>
    selectedKeySet.has(result.key),
  )
  return selectedResults.length > 0 ? selectedResults : [...results]
}
