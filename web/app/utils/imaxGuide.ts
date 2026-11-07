import type { Theater } from '../types/api'

export const imaxDirectoryTarget = '/cinemas?technologies=IMAX'

export const imaxRatios = [
  {
    id: 'scope',
    ratio: 2.39,
    label: 'Cinéma Scope',
    value: '2,39:1',
    description:
      'Un cadre panoramique, très courant au cinéma. Le point de départ de cette comparaison.',
  },
  {
    id: 'digital',
    ratio: 1.9,
    label: 'IMAX 1,90',
    value: '1,90:1',
    description:
      'Le cadre IMAX le plus courant dans les multiplexes : davantage de hauteur quand le film le prévoit.',
  },
  {
    id: 'full',
    ratio: 1.43,
    label: 'IMAX 1,43',
    value: '1,43:1',
    description: 'Le cadre IMAX le plus haut de cette comparaison.',
  },
] as const

// At equal width, area is proportional to 1 / aspect ratio.
export function imaxExtraImage(ratio: number): number {
  return Math.round((2.39 / ratio - 1) * 100)
}

export interface GuideCinema {
  id?: string
  name: string
  search: string
  aliases?: readonly string[]
}

export const imaxLandmarks = {
  odysseum: {
    id: 'pathe-cinema-pathe-odysseum',
    name: 'Pathé Odysseum',
    search: 'Odysseum',
  },
  lomme: { id: 'kinepolis-KLOM', name: 'Kinepolis Lomme', search: 'Lomme' },
  villette: {
    id: 'pathe-cinema-pathe-la-villette',
    name: 'Pathé La Villette',
    search: 'Villette',
  },
  cannes: { name: 'Cineum Cannes', search: 'Cannes' },
  dole: {
    id: 'majestic-EMS1333',
    name: 'Majestic Rive Gauche',
    search: 'Dole',
  },
  geode: { name: 'La Géode', search: 'Géode', aliases: ['Pathé La Géode'] },
} satisfies Record<string, GuideCinema>

export interface ImaxFeaturedCinema {
  key: keyof typeof imaxLandmarks
  city: string
  projection: string
  ratio?: string
  sourceId: string
}

// Only publish capabilities explicitly stated by the linked primary source.
// No flat-screen ratio is inferred from an IMAX or Laser label.
export const imaxFeaturedCinemas: readonly ImaxFeaturedCinema[] = [
  {
    key: 'lomme',
    city: 'Lille · Lomme',
    projection: 'IMAX Laser',
    sourceId: 'lomme',
  },
  {
    key: 'cannes',
    city: 'Cannes',
    projection: 'IMAX Laser 4K',
    sourceId: 'cineum',
  },
  { key: 'dole', city: 'Dole', projection: 'IMAX Laser', sourceId: 'dole' },
  {
    key: 'geode',
    city: 'Paris · La Villette',
    projection: 'IMAX Laser 4K',
    ratio: 'Dôme hémisphérique',
    sourceId: 'geode',
  },
]

export const imaxVenueSources = [
  {
    id: 'lomme',
    name: 'IMAX · Kinepolis Lomme',
    detail: 'La fiche officielle confirme la projection IMAX with Laser.',
    url: 'https://www.imax.com/theatre/kinepolis-lomme-and-imax',
  },
  {
    id: 'cineum',
    name: 'Cineum Cannes · Nos salles',
    detail:
      'L’exploitant annonce une projection IMAX Laser 4K. Le ratio maximal n’est pas précisé.',
    url: 'https://www.cineum.fr/nos-salles/',
  },
  {
    id: 'dole',
    name: 'IMAX · Majestic Dole Rive Gauche',
    detail: 'La fiche officielle confirme la projection IMAX with Laser.',
    url: 'https://www.imax.com/theatre/majestic-dole-rive-gauche-and-imax',
  },
  {
    id: 'geode',
    name: 'Apsys · Réouverture de La Géode',
    detail:
      '18 décembre 2024 · Écran hémisphérique et projection IMAX Laser 4K.',
    url: 'https://www.apsysgroup.com/la-geode-pathe-rouvre-ses-portes-aujourdhui-au-parc-de-la-villette/',
  },
] as const

export function guideCinemaTarget(
  theaters: readonly Theater[],
  cinema: GuideCinema,
): string {
  const normalizeName = (name: string) =>
    name
      .normalize('NFD')
      .replace(/\p{M}/gu, '')
      .toLocaleLowerCase('fr-FR')
      .replace(/[^\p{L}\p{N}]/gu, '')
  const expectedNames = [cinema.name, ...(cinema.aliases ?? [])].map(
    normalizeName,
  )
  const namedMatches = cinema.id
    ? []
    : theaters.filter((theater) =>
        [theater.name, `${theater.name} ${theater.city}`].some((name) =>
          expectedNames.includes(normalizeName(name)),
        ),
      )
  const registered = cinema.id
    ? theaters.find((theater) => theater.id === cinema.id)
    : namedMatches.length === 1
      ? namedMatches[0]
      : undefined
  return registered?.slug
    ? `/cinema/${encodeURIComponent(registered.slug)}`
    : `${imaxDirectoryTarget}&q=${encodeURIComponent(cinema.search)}`
}
