function hasSafePath(url: string, origin: string) {
  const pathEnd = url.search(/[?#]/)
  let decodedPath = url.slice(origin.length, pathEnd === -1 ? undefined : pathEnd)

  try {
    for (let depth = 0; depth < 3; depth += 1) {
      const decoded = decodeURIComponent(decodedPath)
      if (decoded === decodedPath) break
      decodedPath = decoded
    }
  } catch {
    return false
  }

  return !decodedPath.includes('%')
    && !decodedPath.includes('\\')
    && !decodedPath.slice(1).split('/').some((segment) => segment === '')
    && !decodedPath.split('/').some((segment) => segment === '.' || segment === '..')
}

export function safePosterUrl(url: string | null | undefined) {
  if (!url || url.includes('\\')) return null

  try {
    const parsed = new URL(url)
    const hostname = parsed.hostname.toLowerCase()
    const isTmdbPoster = hostname === 'image.tmdb.org'
      && parsed.pathname.startsWith('/t/p/w500/')
      && parsed.pathname !== '/t/p/w500/'
    const isUgcPoster = (hostname === 'ugc.fr' || hostname.endsWith('.ugc.fr')) && parsed.pathname !== '/'
    const isKinepolisPoster = hostname === 'cdn.kinepolis.fr'
      && parsed.pathname.startsWith('/images/')
      && parsed.pathname !== '/images/'
    const isPathePoster = (hostname === 'pathe.fr' || hostname.endsWith('.pathe.fr'))
      && parsed.pathname !== '/'
      && !parsed.pathname.includes('%')
    const isCgrPoster = (hostname === 'acsta.net' || hostname.endsWith('.acsta.net'))
      && parsed.pathname !== '/'
      && !parsed.pathname.includes('%')

    if (
      parsed.protocol !== 'https:'
      || parsed.port
      || parsed.username
      || parsed.password
      || parsed.search
      || parsed.hash
      || (!isTmdbPoster && !isUgcPoster && !isKinepolisPoster && !isPathePoster && !isCgrPoster)
      || !hasSafePath(url, parsed.origin)
    ) return null

    return parsed.href
  } catch {
    return null
  }
}

export function safeBackdropUrl(url: string | null | undefined) {
  const prefix = 'https://image.tmdb.org/t/p/w780/'
  if (!url?.startsWith(prefix) || url.includes('\\')) return null

  try {
    const parsed = new URL(url)
    if (
      parsed.protocol !== 'https:'
      || parsed.hostname !== 'image.tmdb.org'
      || parsed.port
      || parsed.username
      || parsed.password
      || parsed.search
      || parsed.hash
      || !parsed.pathname.startsWith('/t/p/w780/')
      || parsed.pathname === '/t/p/w780/'
      || !hasSafePath(url, parsed.origin)
    ) return null

    return parsed.href
  } catch {
    return null
  }
}
