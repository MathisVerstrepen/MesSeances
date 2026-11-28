export interface FrenchRegion {
  slug: string
  name: string
}

export interface FrenchDepartment {
  code: string
  name: string
  region: string
}

export const FRENCH_REGIONS: readonly FrenchRegion[] = [
  { slug: 'auvergne-rhone-alpes', name: 'Auvergne-Rhône-Alpes' },
  { slug: 'bourgogne-franche-comte', name: 'Bourgogne-Franche-Comté' },
  { slug: 'bretagne', name: 'Bretagne' },
  { slug: 'centre-val-de-loire', name: 'Centre-Val de Loire' },
  { slug: 'corse', name: 'Corse' },
  { slug: 'grand-est', name: 'Grand Est' },
  { slug: 'hauts-de-france', name: 'Hauts-de-France' },
  { slug: 'ile-de-france', name: 'Île-de-France' },
  { slug: 'normandie', name: 'Normandie' },
  { slug: 'nouvelle-aquitaine', name: 'Nouvelle-Aquitaine' },
  { slug: 'occitanie', name: 'Occitanie' },
  { slug: 'pays-de-la-loire', name: 'Pays de la Loire' },
  {
    slug: 'provence-alpes-cote-d-azur',
    name: 'Provence-Alpes-Côte d’Azur',
  },
  { slug: 'outre-mer', name: 'Outre-mer' },
]

// Postal-code prefixes: Corsica keeps the shared "20" prefix and overseas
// departments use their three-digit prefix.
const DEPARTMENT_ROWS: ReadonlyArray<readonly [string, string, string]> = [
  ['01', 'Ain', 'auvergne-rhone-alpes'],
  ['02', 'Aisne', 'hauts-de-france'],
  ['03', 'Allier', 'auvergne-rhone-alpes'],
  ['04', 'Alpes-de-Haute-Provence', 'provence-alpes-cote-d-azur'],
  ['05', 'Hautes-Alpes', 'provence-alpes-cote-d-azur'],
  ['06', 'Alpes-Maritimes', 'provence-alpes-cote-d-azur'],
  ['07', 'Ardèche', 'auvergne-rhone-alpes'],
  ['08', 'Ardennes', 'grand-est'],
  ['09', 'Ariège', 'occitanie'],
  ['10', 'Aube', 'grand-est'],
  ['11', 'Aude', 'occitanie'],
  ['12', 'Aveyron', 'occitanie'],
  ['13', 'Bouches-du-Rhône', 'provence-alpes-cote-d-azur'],
  ['14', 'Calvados', 'normandie'],
  ['15', 'Cantal', 'auvergne-rhone-alpes'],
  ['16', 'Charente', 'nouvelle-aquitaine'],
  ['17', 'Charente-Maritime', 'nouvelle-aquitaine'],
  ['18', 'Cher', 'centre-val-de-loire'],
  ['19', 'Corrèze', 'nouvelle-aquitaine'],
  ['20', 'Corse', 'corse'],
  ['21', 'Côte-d’Or', 'bourgogne-franche-comte'],
  ['22', 'Côtes-d’Armor', 'bretagne'],
  ['23', 'Creuse', 'nouvelle-aquitaine'],
  ['24', 'Dordogne', 'nouvelle-aquitaine'],
  ['25', 'Doubs', 'bourgogne-franche-comte'],
  ['26', 'Drôme', 'auvergne-rhone-alpes'],
  ['27', 'Eure', 'normandie'],
  ['28', 'Eure-et-Loir', 'centre-val-de-loire'],
  ['29', 'Finistère', 'bretagne'],
  ['30', 'Gard', 'occitanie'],
  ['31', 'Haute-Garonne', 'occitanie'],
  ['32', 'Gers', 'occitanie'],
  ['33', 'Gironde', 'nouvelle-aquitaine'],
  ['34', 'Hérault', 'occitanie'],
  ['35', 'Ille-et-Vilaine', 'bretagne'],
  ['36', 'Indre', 'centre-val-de-loire'],
  ['37', 'Indre-et-Loire', 'centre-val-de-loire'],
  ['38', 'Isère', 'auvergne-rhone-alpes'],
  ['39', 'Jura', 'bourgogne-franche-comte'],
  ['40', 'Landes', 'nouvelle-aquitaine'],
  ['41', 'Loir-et-Cher', 'centre-val-de-loire'],
  ['42', 'Loire', 'auvergne-rhone-alpes'],
  ['43', 'Haute-Loire', 'auvergne-rhone-alpes'],
  ['44', 'Loire-Atlantique', 'pays-de-la-loire'],
  ['45', 'Loiret', 'centre-val-de-loire'],
  ['46', 'Lot', 'occitanie'],
  ['47', 'Lot-et-Garonne', 'nouvelle-aquitaine'],
  ['48', 'Lozère', 'occitanie'],
  ['49', 'Maine-et-Loire', 'pays-de-la-loire'],
  ['50', 'Manche', 'normandie'],
  ['51', 'Marne', 'grand-est'],
  ['52', 'Haute-Marne', 'grand-est'],
  ['53', 'Mayenne', 'pays-de-la-loire'],
  ['54', 'Meurthe-et-Moselle', 'grand-est'],
  ['55', 'Meuse', 'grand-est'],
  ['56', 'Morbihan', 'bretagne'],
  ['57', 'Moselle', 'grand-est'],
  ['58', 'Nièvre', 'bourgogne-franche-comte'],
  ['59', 'Nord', 'hauts-de-france'],
  ['60', 'Oise', 'hauts-de-france'],
  ['61', 'Orne', 'normandie'],
  ['62', 'Pas-de-Calais', 'hauts-de-france'],
  ['63', 'Puy-de-Dôme', 'auvergne-rhone-alpes'],
  ['64', 'Pyrénées-Atlantiques', 'nouvelle-aquitaine'],
  ['65', 'Hautes-Pyrénées', 'occitanie'],
  ['66', 'Pyrénées-Orientales', 'occitanie'],
  ['67', 'Bas-Rhin', 'grand-est'],
  ['68', 'Haut-Rhin', 'grand-est'],
  ['69', 'Rhône', 'auvergne-rhone-alpes'],
  ['70', 'Haute-Saône', 'bourgogne-franche-comte'],
  ['71', 'Saône-et-Loire', 'bourgogne-franche-comte'],
  ['72', 'Sarthe', 'pays-de-la-loire'],
  ['73', 'Savoie', 'auvergne-rhone-alpes'],
  ['74', 'Haute-Savoie', 'auvergne-rhone-alpes'],
  ['75', 'Paris', 'ile-de-france'],
  ['76', 'Seine-Maritime', 'normandie'],
  ['77', 'Seine-et-Marne', 'ile-de-france'],
  ['78', 'Yvelines', 'ile-de-france'],
  ['79', 'Deux-Sèvres', 'nouvelle-aquitaine'],
  ['80', 'Somme', 'hauts-de-france'],
  ['81', 'Tarn', 'occitanie'],
  ['82', 'Tarn-et-Garonne', 'occitanie'],
  ['83', 'Var', 'provence-alpes-cote-d-azur'],
  ['84', 'Vaucluse', 'provence-alpes-cote-d-azur'],
  ['85', 'Vendée', 'pays-de-la-loire'],
  ['86', 'Vienne', 'nouvelle-aquitaine'],
  ['87', 'Haute-Vienne', 'nouvelle-aquitaine'],
  ['88', 'Vosges', 'grand-est'],
  ['89', 'Yonne', 'bourgogne-franche-comte'],
  ['90', 'Territoire de Belfort', 'bourgogne-franche-comte'],
  ['91', 'Essonne', 'ile-de-france'],
  ['92', 'Hauts-de-Seine', 'ile-de-france'],
  ['93', 'Seine-Saint-Denis', 'ile-de-france'],
  ['94', 'Val-de-Marne', 'ile-de-france'],
  ['95', 'Val-d’Oise', 'ile-de-france'],
  ['971', 'Guadeloupe', 'outre-mer'],
  ['972', 'Martinique', 'outre-mer'],
  ['973', 'Guyane', 'outre-mer'],
  ['974', 'La Réunion', 'outre-mer'],
  ['976', 'Mayotte', 'outre-mer'],
]

export const FRENCH_DEPARTMENTS: readonly FrenchDepartment[] =
  DEPARTMENT_ROWS.map(([code, name, region]) => ({ code, name, region }))

const departmentsByCode = new Map(
  FRENCH_DEPARTMENTS.map((department) => [department.code, department]),
)
const regionsBySlug = new Map(
  FRENCH_REGIONS.map((region) => [region.slug, region]),
)

export function frenchDepartment(code: string): FrenchDepartment | undefined {
  return departmentsByCode.get(code)
}

export function frenchRegion(slug: string): FrenchRegion | undefined {
  return regionsBySlug.get(slug)
}

export function departmentCodeFromPostalCode(
  postalCode: string,
): string | undefined {
  const value = postalCode.trim()
  if (!/^\d{5}$/.test(value)) return undefined
  const code = value.startsWith('97') ? value.slice(0, 3) : value.slice(0, 2)
  return departmentsByCode.has(code) ? code : undefined
}

/** Paris arrondissement number (1–20) from a 750xx or 75116 postal code. */
export function parisArrondissement(postalCode: string): number | undefined {
  const value = postalCode.trim()
  if (value === '75116') return 16
  if (!/^750\d{2}$/.test(value)) return undefined
  const number = Number(value.slice(3))
  return number >= 1 && number <= 20 ? number : undefined
}

export function parisArrondissementLabel(number: number): string {
  return number === 1 ? 'Paris 1er' : `Paris ${number}e`
}
