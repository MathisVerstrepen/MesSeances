export type MovieExternalLinkDestination =
  | 'tmdb'
  | 'letterboxd'
  | 'imdb'
  | 'boxofficemojo'
  | 'metacritic'

export interface MovieExternalLink {
  destination: MovieExternalLinkDestination
  label: string
  url: string
}

const CANONICAL_IMDB_ID = /^tt[0-9]{7,30}$/u
const CANONICAL_METACRITIC_ID = /^movie\/[a-z0-9!+_()-]{1,249}(?![\s\S])/u

export function buildMovieExternalLinks(
  tmdbId: number | null | undefined,
  imdbId: string | null | undefined,
  metacriticId?: string | null,
): readonly MovieExternalLink[] {
  const links: MovieExternalLink[] = []
  const hasTmdbIdentity = Number.isSafeInteger(tmdbId) && (tmdbId ?? 0) > 0

  if (hasTmdbIdentity) {
    links.push(
      {
        destination: 'tmdb',
        label: 'TMDB',
        url: `https://www.themoviedb.org/movie/${tmdbId}`,
      },
      {
        destination: 'letterboxd',
        label: 'Letterboxd',
        url: `https://letterboxd.com/tmdb/${tmdbId}`,
      },
    )
  }

  if (imdbId && CANONICAL_IMDB_ID.test(imdbId)) {
    links.push(
      {
        destination: 'imdb',
        label: 'IMDb',
        url: `https://www.imdb.com/title/${imdbId}/`,
      },
      {
        destination: 'boxofficemojo',
        label: 'Box Office',
        url: `https://www.boxofficemojo.com/title/${imdbId}/?ref_=bo_rl_rl`,
      },
    )
  }

  if (
    hasTmdbIdentity &&
    metacriticId &&
    CANONICAL_METACRITIC_ID.test(metacriticId)
  ) {
    links.push({
      destination: 'metacritic',
      label: 'Metacritic',
      url: `https://www.metacritic.com/${metacriticId}/`,
    })
  }

  return links
}
