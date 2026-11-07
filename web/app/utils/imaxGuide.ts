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
    description:
      'Un cadre encore plus haut. Il faut une version du film, un projecteur et un écran compatibles.',
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
