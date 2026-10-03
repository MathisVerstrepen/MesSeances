import type { PublicTheaterImage } from '../types/api.ts'

const publicImagePath =
  /^\/api\/v1\/theaters\/(?:ugc|kinepolis|pathe|cgr|megarama|cineville|mk2|cinewest|grandecran|noecinemas)\/[A-Za-z0-9_-]{1,128}\/image\/([1-9][0-9]*)$/

export function publicCinemaImageUrl(
  image: PublicTheaterImage | null,
  publicApiBase: string,
): string {
  if (
    !image ||
    !Number.isInteger(image.width) ||
    !Number.isInteger(image.height) ||
    image.width < 1 ||
    image.width > 1600 ||
    image.height < 1 ||
    image.height > 1600
  )
    return ''

  const match = publicImagePath.exec(image.url)
  if (
    !match ||
    match[0] !== image.url ||
    !Number.isSafeInteger(Number(match[1]))
  )
    return ''

  return `${publicApiBase.replace(/\/$/, '')}${image.url}`
}
