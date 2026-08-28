export type MovieExternalLinkDestination = 'tmdb' | 'letterboxd' | 'imdb'

export interface MovieExternalLink {
  destination: MovieExternalLinkDestination
  label: string
  url: string
}

const CANONICAL_IMDB_ID = /^tt[0-9]{7,30}$/u

export function buildMovieExternalLinks(
  tmdbId: number | null | undefined,
  imdbId: string | null | undefined
): readonly MovieExternalLink[] {
  const links: MovieExternalLink[] = []

  if (Number.isSafeInteger(tmdbId) && (tmdbId ?? 0) > 0) {
    links.push(
      {
        destination: 'tmdb',
        label: 'TMDB',
        url: `https://www.themoviedb.org/movie/${tmdbId}`
      },
      {
        destination: 'letterboxd',
        label: 'Letterboxd',
        url: `https://letterboxd.com/tmdb/${tmdbId}`
      }
    )
  }

  if (imdbId && CANONICAL_IMDB_ID.test(imdbId)) {
    links.push({
      destination: 'imdb',
      label: 'IMDb',
      url: `https://www.imdb.com/title/${imdbId}/`
    })
  }

  return links
}
