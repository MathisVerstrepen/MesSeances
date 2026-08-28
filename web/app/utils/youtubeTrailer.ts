const YOUTUBE_VIDEO_KEY_PATTERN = /^[A-Za-z0-9_-]{11}$/u

type YouTubeKeyInput = string | null | undefined

export type TrailerVariant = 'VF' | 'VO'

export interface TrailerSelection {
  variant: TrailerVariant
  youtubeKey: string
}

export function validatedYouTubeKey(value: YouTubeKeyInput): string | null {
  if (!value) return null
  return YOUTUBE_VIDEO_KEY_PATTERN.test(value) ? value : null
}

export function youtubeNoCookieTrailerEmbedUrl(value: YouTubeKeyInput): string | null {
  const key = validatedYouTubeKey(value)
  return key ? `https://www.youtube-nocookie.com/embed/${key}?autoplay=1` : null
}

export function availableTrailerSelections(vfValue: YouTubeKeyInput, voValue: YouTubeKeyInput): TrailerSelection[] {
  const vfKey = validatedYouTubeKey(vfValue)
  const voKey = validatedYouTubeKey(voValue)
  const selections: TrailerSelection[] = []

  if (vfKey) selections.push({ variant: 'VF', youtubeKey: vfKey })
  if (voKey && voKey !== vfKey) selections.push({ variant: 'VO', youtubeKey: voKey })

  return selections
}
