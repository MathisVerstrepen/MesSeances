const YOUTUBE_VIDEO_KEY_PATTERN = /^[A-Za-z0-9_-]{11}$/u

type YouTubeKeyInput = string | null | undefined

export function validatedYouTubeKey(value: YouTubeKeyInput): string | null {
  if (!value) return null
  return YOUTUBE_VIDEO_KEY_PATTERN.test(value) ? value : null
}

export function youtubeNoCookieTrailerEmbedUrl(value: YouTubeKeyInput): string | null {
  const key = validatedYouTubeKey(value)
  return key ? `https://www.youtube-nocookie.com/embed/${key}?autoplay=1` : null
}
