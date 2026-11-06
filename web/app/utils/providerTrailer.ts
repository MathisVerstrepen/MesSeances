import type { ResolvedTrailer } from '../types/api'
import { youtubeNoCookieTrailerEmbedUrl } from './youtubeTrailer.ts'

export function providerTrailerPlayback(
  media: ResolvedTrailer | null | undefined,
): { type: 'youtube' | 'mp4'; url: string } | null {
  if (!media) return null
  if (media.type === 'youtube' && media.source === 'cineville') {
    const match =
      /^https:\/\/www\.youtube\.com\/watch\?v=([A-Za-z0-9_-]{11})$/u.exec(
        media.url,
      )
    const url = youtubeNoCookieTrailerEmbedUrl(match?.[1])
    return url ? { type: 'youtube', url } : null
  }
  if (media.type !== 'mp4' || !['cgr', 'ugc'].includes(media.source))
    return null
  if (
    !/^https:\/\/fr\.vid\.web\.acsta\.net\/(?:nmedia|medias)\/[A-Za-z0-9_./-]+\.mp4$/u.test(
      media.url,
    )
  )
    return null
  const path = media.url.slice('https://fr.vid.web.acsta.net'.length)
  if (
    path.includes('//') ||
    path.split('/').some((part) => part === '.' || part === '..')
  )
    return null
  return { type: 'mp4', url: media.url }
}
