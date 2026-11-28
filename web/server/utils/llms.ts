import type { SitemapDataResponse } from '../../app/types/api.ts'
import { absoluteSiteUrl } from '../../app/utils/siteUrl.ts'
import { API_SITEMAP_CACHE_POLICY, validateSitemapData } from './sitemap.ts'

export const LLMS_CONTENT_TYPE = 'text/plain; charset=utf-8'
export const LLMS_FULL_TOP_MOVIES = 200

export const LLMS_FULL_CACHE_POLICY = Object.freeze({
  ...API_SITEMAP_CACHE_POLICY,
  getKey: (_event: { path: string }) => '/llms-full.txt',
})

const LANGUAGES =
  '`ALL`, `ORIGINAL` (VOF et VOSTFR), `VOF`, `VOSTFR`, `VF`, `VO`, `VF_SME`, `VFSTF`'
const FORMATS =
  '`ALL`, `2D`, `3D`, `IMAX`, `DOLBY`, `SCREENX`, `LASER_ULTRA`, `4DX`, `ICE`, `INFINITY_VISION`'

// Markdown link text and URL templates must not break the list syntax.
function markdownText(value: string): string {
  return value
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/([\\`*_[\]<>|])/g, '\\$1')
}

export function renderLlmsTxt(siteUrl: string): string {
  // Keeps URL templates such as {slug} readable instead of percent-encoded.
  const url = (path: string) =>
    absoluteSiteUrl(siteUrl, '/').replace(/\/$/, '') + path

  return `# MesSeances

> MesSeances regroupe les horaires des séances de cinéma en France (UGC, Pathé, CGR, Kinepolis, Mégarama et de nombreux cinémas indépendants) et aide à trouver une séance qui tient dans un créneau horaire. Le site et les données sont en français ; la consultation est publique et sans compte.

Repères pour les agents :

- Préférez l'API JSON publique (section ci-dessous) pour extraire des horaires : elle est plus compacte et plus stable que le HTML. Donnez à l'utilisateur les liens vers les pages du site.
- Fuseau horaire : les dates (\`AAAA-MM-JJ\`) et les heures de créneau (\`HH:MM\`) s'entendent à l'heure de Paris (Europe/Paris). Les horodatages renvoyés par l'API sont en UTC (ISO 8601, suffixe \`Z\`).
- Identifiants : un film a un slug \`film-{n}\` (ex. \`film-8806\`), un cinéma un slug \`{chaîne}-{identifiant}\` (ex. \`cgr-P0867\`), une ville un slug en minuscules (ex. \`agen\`). Les paramètres \`theaters\` attendent des \`id\` de cinéma séparés par des virgules ; l'\`id\` est égal au slug sauf mention contraire dans llms-full.txt. Trouvez les slugs avec \`/api/v1/cities\`, \`/api/v1/movies?search=\` ou dans [llms-full.txt](${url('/llms-full.txt')}).
- Valeurs de \`language\` : ${LANGUAGES}. Valeurs de \`format\` : ${FORMATS}.
- Réservation : chaque séance renvoyée par l'API porte un \`booking_url\` vers le site de l'exploitant. MesSeances ne vend pas de billets.
- Limite de débit : les lectures coûteuses de l'API sont limitées à environ 1 requête par seconde par client, avec une rafale de 20. Au-delà, l'API répond \`429\` avec un en-tête \`Retry-After\` : respectez-le et réutilisez les réponses déjà obtenues.
- Erreurs : corps \`{"error": {"code": "...", "message": "..."}}\`. \`400 invalid_query\` signale un paramètre invalide, \`404\` un slug inconnu, \`503 schedule_unavailable\` des horaires momentanément indisponibles.
- Les pages \`/compte\` et \`/admin\` ainsi que les routes \`/api/v1/account\` et \`/api/v1/auth\` demandent une authentification : ne les utilisez pas.

## Pages du site

- [Accueil](${url('/')}): présentation et films du moment.
- [Films à l'affiche](${url('/films')}): catalogue paginé ; paramètres \`q\` (recherche), \`sort\`, \`page\`.
- [Prochaines sorties](${url('/films/prochainement')}): films qui sortent bientôt en salle.
- [Fiche film](${url('/film/{slug}')}): séances d'un film par cinéma ; paramètres \`date\`, \`language\`, \`format\`, \`sort=next\` (prochaines séances d'abord).
- [Annuaire des cinémas](${url('/cinemas')}): tous les cinémas couverts, par ville et par chaîne.
- [Cinémas d'une ville](${url('/ville/{slug}/cinemas')}): paramètres \`q\` (recherche), \`sort\`.
- [Fiche cinéma](${url('/cinema/{slug}')}): séances du jour d'un cinéma ; paramètres \`date\`, \`view=films\`.
- [Recherche par créneau](${url('/recherche?theaters=&date=AAAA-MM-JJ&start_after=HH:MM&finish_before=HH:MM')}): séances qui commencent après \`start_after\` et finissent avant \`finish_before\` (pas de 15 minutes, de 08:00 à 02:00 le lendemain). \`theaters\` vide pour tous les cinémas ; options \`language\`, \`format\`, \`include_ads=0\`, \`buffer_ads\`.
- [Articles](${url('/articles')}): articles éditoriaux (par exemple le guide IMAX).

## API JSON publique

Lecture seule, requêtes \`GET\` sans authentification, réponses \`application/json\`.

- [Villes et cinémas](${url('/api/v1/cities')}): inventaire complet des villes et de leurs cinémas (\`slug\`, \`id\`, \`provider\`, \`name\`).
- [Détail d'une ville](${url('/api/v1/cities/{slug}')}): cinémas de la ville et films qui y sont programmés.
- [Cinémas](${url('/api/v1/theaters?city={slug}')}): adresse, code postal, technologies et \`available_dates\` de chaque cinéma ; filtres \`city\` (slug de ville) et \`chain\` (ex. \`ugc\`, \`pathe\`, \`cgr\`).
- [Séances d'un cinéma](${url('/api/v1/theaters/{slug}/showtimes?date=AAAA-MM-JJ')}): séances d'un cinéma pour une date (aujourd'hui si \`date\` est absent).
- [Catalogue des films](${url('/api/v1/movies?search={titre}&page_size=20')}): recherche et liste des films ; paramètres \`search\`, \`currently_screened=true|false\`, \`sort\` (\`title_asc\`, \`title_desc\`, \`release_date_desc\`, \`runtime_asc\`, \`runtime_desc\`, \`showtimes_desc\`), \`genres\`, \`duration\` (\`short\`, \`medium\`, \`long\`), \`date\`, \`date_to\`, \`theaters\`, \`page\`, \`page_size\` (1 à 100).
- [Séances d'un film](${url('/api/v1/movies/{slug}/showtimes?date=AAAA-MM-JJ&city={slug}')}): \`date\` obligatoire ; \`city\` ou \`theaters\` (exclusifs ; aucun des deux pour toute la France) ; \`language\`, \`format\`, \`sort=catalog|next\`, \`page\`. La réponse donne aussi \`available_dates\`.
- [Recherche par créneau](${url('/api/v1/search/slot?date=AAAA-MM-JJ&start_after=18:00&finish_before=23:00&city={slug}')}): séances qui tiennent dans le créneau ; \`city\` ou \`theaters\` (exclusifs) ; \`language\`, \`format\`, \`buffer_ads\` (minutes de publicité estimées, 0 à 120, 15 par défaut), \`include_ads=true|false\`.
- [Prochaines sorties](${url('/api/v1/movies/upcoming')}): films à venir, paginés avec \`page\` ; \`view=history\` avec \`year\` et \`month\` pour les sorties passées.

## Optional

- [Guide complet](${url('/llms-full.txt')}): ce guide, la liste de toutes les villes et cinémas avec leurs slugs, et les films les plus programmés.
- [Plan du site](${url('/sitemap.xml')}): index des sitemaps films, cinémas, villes et articles.
- [Mentions légales](${url('/mentions-legales')})
- [Politique de confidentialité](${url('/confidentialite')})
`
}

export function renderLlmsFullTxt(
  siteUrl: string,
  data: SitemapDataResponse,
): string {
  validateSitemapData(data)
  if (!data.cities) throw new Error('City inventory unavailable')

  const cities = [...data.cities.items].sort((left, right) =>
    left.name.localeCompare(right.name, 'fr'),
  )
  const theaterCount = cities.reduce(
    (total, city) => total + city.theaters.length,
    0,
  )
  const cityLines = cities.flatMap((city) => [
    '',
    `### [${markdownText(city.name)}](${absoluteSiteUrl(siteUrl, `/ville/${encodeURIComponent(city.slug)}/cinemas`)})`,
    '',
    `Slug de ville : \`${city.slug}\``,
    '',
    ...[...city.theaters]
      .sort((left, right) => left.name.localeCompare(right.name, 'fr'))
      .map(
        (theater) =>
          `- [${markdownText(theater.name)}](${absoluteSiteUrl(siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`)}): slug \`${theater.slug}\`${theater.id === theater.slug ? '' : `, id \`${theater.id}\``}, chaîne \`${theater.provider}\``,
      ),
  ])

  const movies = data.movies
    .filter(
      (movie) => movie.title.trim() !== '' && (movie.showtime_count ?? 0) > 0,
    )
    .sort(
      (left, right) =>
        (right.showtime_count ?? 0) - (left.showtime_count ?? 0) ||
        left.title.localeCompare(right.title, 'fr'),
    )
    .slice(0, LLMS_FULL_TOP_MOVIES)
  const movieLines = movies.map((movie) => {
    const year = movie.release_date?.slice(0, 4)
    const details = [
      `slug \`${movie.slug}\``,
      ...(year && /^\d{4}$/.test(year) ? [`sortie ${year}`] : []),
      ...(movie.genres.length > 0
        ? [movie.genres.map(markdownText).join(', ')]
        : []),
    ]
    return `- [${markdownText(movie.title)}](${absoluteSiteUrl(siteUrl, `/film/${encodeURIComponent(movie.slug)}`)}): ${details.join(' ; ')}`
  })

  return `${renderLlmsTxt(siteUrl)}
## Villes et cinémas

Inventaire au ${data.cities.generated_at} : ${cities.length} villes, ${theaterCount} cinémas.
${cityLines.join('\n')}

## Films les plus programmés

Les ${movies.length} films qui comptent le plus de séances à venir, au ${data.as_of}. Pour les autres, utilisez \`/api/v1/movies?search=\`.

${movieLines.join('\n')}
`
}
