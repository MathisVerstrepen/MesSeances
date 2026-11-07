<script setup lang="ts">
import {
  ArrowDown,
  ArrowRight,
  Check,
  Film,
  MapPin,
  Maximize,
  Volume2,
} from '@lucide/vue'
import type { Theater } from '~/types/api'
import { filterCinemaDirectory } from '~/utils/cinemaDirectoryChains'
import {
  guideCinemaTarget,
  imaxDirectoryTarget,
  imaxLandmarks,
} from '~/utils/imaxGuide'
import { theaterDisplayName } from '~/utils/theaterDisplayName'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { serializeJsonLd } from '~/utils/jsonLd'

const api = useMesSeancesApi()
const { data: directory } = await useAsyncData(
  'imax-guide-theaters',
  async () => {
    try {
      return { theaters: await api.theaters(), unavailable: false }
    } catch {
      const theaters: Theater[] = []
      return { theaters, unavailable: true }
    }
  },
)
const theaters = computed(() => directory.value?.theaters ?? [])
const theaterSearch = ref('')
const imaxTheaters = computed(() =>
  filterCinemaDirectory(theaters.value, '', [], ['IMAX']),
)
const visibleTheaters = computed(() =>
  filterCinemaDirectory(imaxTheaters.value, theaterSearch.value, []).sort(
    (left, right) =>
      left.city.localeCompare(right.city, 'fr-FR') ||
      left.name.localeCompare(right.name, 'fr-FR'),
  ),
)
const landmarks = computed(() =>
  Object.entries(imaxLandmarks).map(([key, cinema]) => ({
    key,
    ...cinema,
    to: guideCinemaTarget(theaters.value, cinema),
  })),
)
function landmarkTarget(key: keyof typeof imaxLandmarks) {
  return guideCinemaTarget(theaters.value, imaxLandmarks[key])
}
const chapters = [
  { id: 'essentiel', label: 'L’essentiel' },
  { id: 'formats', label: 'Comparer l’image' },
  { id: 'projection', label: 'Laser ou pellicule ?' },
  { id: 'experience', label: 'Son & 3D' },
  { id: 'salles', label: 'Les salles en France' },
  { id: 'choisir', label: 'Choisir sa séance' },
  { id: 'questions', label: 'Questions fréquentes' },
]
const faqs = [
  {
    question: 'Toutes les séances IMAX montrent-elles plus d’image ?',
    answer:
      'Non. Le film doit disposer d’une version à cadre étendu, parfois pour quelques séquences seulement. Un film présenté en 2,39:1 peut garder ce format pendant toute la séance. Les bandes noires respectent alors le cadrage prévu.',
  },
  {
    question: '« Filmé pour IMAX » veut-il dire tourné sur pellicule ?',
    answer:
      'Non. Un film peut utiliser des caméras numériques certifiées IMAX. Le tournage, le support de projection et le ratio de l’image sont trois informations différentes. Une sortie en IMAX 70 mm doit être annoncée explicitement.',
  },
  {
    question: 'Peut-on voir de l’IMAX 70 mm en France ?',
    answer:
      'Oui. Le Pathé Odysseum à Montpellier propose des projections IMAX 70 mm depuis juillet 2026, avec L’Odyssée comme premier film. Pour voir un film sur pellicule, choisissez une séance portant la mention « IMAX 70 mm ».',
  },
  {
    question: 'Le 70 mm classique est-il de l’IMAX ?',
    answer:
      'Non. Le 70 mm classique utilise généralement 5 perforations par image, avec un défilement vertical et un cadre 2,20:1. L’IMAX 15/70 défile horizontalement et utilise 15 perforations, pour un cadre pouvant atteindre 1,43:1.',
  },
  {
    question: 'Pourquoi parle-t-on parfois de « faux IMAX » ou de « LieMAX » ?',
    answer:
      'C’est un surnom critique, pas une catégorie officielle. Il désigne souvent des salles de multiplexe plus petites ou limitées au 1,90. Il est plus utile de comparer l’équipement, le cadre disponible, la taille de l’écran et votre distance à celui-ci.',
  },
  {
    question: 'Peut-on convertir la pellicule IMAX en un nombre de K ?',
    answer:
      'Pas avec une équivalence universelle. Le résultat dépend du négatif, des optiques, du tirage, du scan et de la projection. Les chiffres comme « 18K » sont des estimations : ils ne permettent pas de comparer directement une séance sur pellicule avec un projecteur numérique 4K.',
  },
  {
    question: 'Faut-il des lunettes pour l’IMAX ?',
    answer:
      'Seulement si la séance est annoncée en 3D. L’IMAX existe aussi en 2D. En 3D, utilisez les lunettes indiquées par le cinéma : elles ne sont pas nécessairement interchangeables entre les systèmes de projection.',
  },
  {
    question: 'L’IMAX Enhanced à la maison remplace-t-il une salle IMAX ?',
    answer:
      'IMAX Enhanced est une offre pour certains contenus et équipements domestiques. Certains films y proposent un cadre étendu, mais le label ne reproduit ni la taille d’un écran de cinéma, ni la géométrie, ni l’installation sonore d’une salle IMAX.',
  },
]
const sources = [
  {
    id: 'cnc',
    name: 'CNC · IMAX, l’odyssée française d’un format devenu une référence',
    detail:
      '10 juillet 2026 · Implantation française, Montpellier et périmètre des 37 salles citées.',
    url: 'https://www.cnc.fr/cinema/actualites/imax-lodyssee-francaise-dun-format-devenu-une-reference_2624138',
  },
  {
    id: 'imax',
    name: 'IMAX · The IMAX Experience',
    detail: 'Conception des salles, caméras, son et remasterisation DMR.',
    url: 'https://www.imax.com/the-imax-experience',
  },
  {
    id: 'laser',
    name: 'IMAX · IMAX with Laser',
    detail: 'Projection laser 4K et système sonore à 12 canaux.',
    url: 'https://www.imax.com/news/imax-laser-here',
  },
  {
    id: '143',
    name: 'IMAX · Salles Laser 1,43 pour Joker : Folie à Deux',
    detail:
      '19 septembre 2024 · Présence de Montpellier dans la liste ; le 1,43 peut aussi venir d’un tournage numérique.',
    url: 'https://www.imax.com/news/experience-joker-folie-a-deux-imax-with-laser-theatres-1-43',
  },
  {
    id: 'odyssey',
    name: 'L’Odyssée · Explore Premium Formats',
    detail:
      'Comparateur officiel de cadrages, source d’inspiration de cette présentation.',
    url: 'https://www.odysseymovie.com/explore-formats/',
  },
  {
    id: 'dolby',
    name: 'Dolby · Dolby Vision et Dolby Atmos au cinéma',
    detail: 'Les technologies d’image et de son qui équipent Dolby Cinema.',
    url: 'https://news.dolby.com/en-WW/236229-dolby-expanding-the-best-way-to-experience-movies-by-making-dolby-vision-and-dolby-atmos-available-to-all-premium-theater-exhibitors/',
  },
]
const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/guides/imax')
const title = 'IMAX en France : formats, salles et guide pratique - MesSeances'
const description =
  'Comprendre l’IMAX sans jargon : comparez les formats 1,90 et 1,43, le laser et le 70 mm, puis trouvez votre cinéma IMAX en France.'
useSeoMeta({
  title,
  description,
  robots: 'index,follow',
  ogTitle: title,
  ogDescription: description,
  ogUrl: canonicalUrl,
  ogType: 'article',
})
useHead({
  link: [{ rel: 'canonical', href: canonicalUrl }],
  script: [
    {
      key: 'imax-guide-jsonld',
      type: 'application/ld+json',
      innerHTML: serializeJsonLd({
        '@context': 'https://schema.org',
        '@graph': [
          {
            '@type': 'Article',
            headline: 'L’IMAX en France, tout simplement.',
            description,
            inLanguage: 'fr-FR',
            datePublished: '2026-11-07',
            dateModified: '2026-11-07',
            mainEntityOfPage: canonicalUrl,
            author: {
              '@id': `${absoluteSiteUrl(config.public.siteUrl, '/')}#organization`,
            },
            publisher: {
              '@id': `${absoluteSiteUrl(config.public.siteUrl, '/')}#organization`,
            },
          },
          {
            '@type': 'BreadcrumbList',
            itemListElement: [
              {
                '@type': 'ListItem',
                position: 1,
                name: 'Accueil',
                item: absoluteSiteUrl(config.public.siteUrl, '/'),
              },
              {
                '@type': 'ListItem',
                position: 2,
                name: 'Guide IMAX',
                item: canonicalUrl,
              },
            ],
          },
        ],
      }),
    },
  ],
})
</script>

<template>
  <main class="imax-guide bg-[#f8f7f2] text-ink">
    <header class="border-b-2 border-ink">
      <div class="mx-auto max-w-[1440px] px-4 pt-6 sm:px-6 lg:px-10">
        <nav
          aria-label="Fil d’Ariane"
          class="flex gap-2 font-mono text-[11px] text-muted"
        >
          <NuxtLink
            to="/"
            class="underline underline-offset-4 hover:text-primary"
            >Accueil</NuxtLink
          ><span aria-hidden="true">/</span
          ><span aria-current="page">Guide IMAX</span>
        </nav>
        <div
          class="grid gap-10 py-10 sm:py-16 lg:grid-cols-[1.15fr_1fr] lg:items-center lg:gap-14"
        >
          <div>
            <h1
              class="text-[clamp(3rem,5.2vw,4.7rem)] font-black leading-[0.98] tracking-[-0.075em]"
            >
              L’IMAX en France,<br>
              <span class="text-accent">tout simplement.</span>
            </h1>
            <p class="mt-7 max-w-xl text-lg leading-relaxed sm:text-xl">
              Un écran qui en met plein les yeux. Mais que changent vraiment le
              laser, le 70 mm et ces drôles de chiffres ? On vous montre.
            </p>
            <div class="mt-8 flex flex-wrap gap-3">
              <a href="#formats" class="editorial-button"
                >Comparer les formats
                <ArrowDown :size="16" aria-hidden="true" /></a
              ><NuxtLink
                :to="imaxDirectoryTarget"
                class="editorial-button-outline"
                >Trouver une salle
                <MapPin :size="16" aria-hidden="true" /></NuxtLink
              >
            </div>
          </div>
          <figure
            class="relative mb-4 border-2 border-ink bg-ink p-3 shadow-[8px_8px_0_var(--color-highlight)] sm:p-4"
          >
            <div class="relative aspect-[1.43] overflow-hidden">
              <img
                src="/images/guides/odyssee-voilier-800.webp"
                srcset="
                  /images/guides/odyssee-voilier-800.webp   800w,
                  /images/guides/odyssee-voilier-1600.webp 1600w
                "
                sizes="(max-width: 1023px) 90vw, 42vw"
                width="800"
                height="576"
                alt="Un voilier à voile rouge en pleine mer, photogramme de L’Odyssée."
                class="size-full object-cover"
                fetchpriority="high"
              >
              <div
                class="absolute inset-x-0 top-1/2 aspect-[1.90] -translate-y-1/2 border-y border-dashed border-white/80"
                aria-hidden="true"
              />
              <div
                class="absolute inset-x-0 top-1/2 aspect-[2.39] -translate-y-1/2 border-y border-white/80"
                aria-hidden="true"
              />
              <span
                class="absolute top-3 left-3 bg-ink px-2 py-1 font-mono text-xs font-bold text-white"
                >1,43</span
              >
              <span
                class="absolute top-[13%] right-3 bg-ink px-2 py-1 font-mono text-xs font-bold text-white"
                >1,90</span
              >
              <span
                class="absolute top-[21%] left-3 bg-ink px-2 py-1 font-mono text-xs font-bold text-white"
                >2,39</span
              >
            </div>
            <figcaption
              class="flex items-center justify-between gap-4 pt-3 font-mono text-[10px] font-bold uppercase tracking-wider text-white"
            >
              <span>Une image. Plusieurs cadres.</span
              ><span aria-hidden="true">↓ Explorez</span>
            </figcaption>
            <span
              class="absolute -right-3 -bottom-5 rotate-[-5deg] border-2 border-ink bg-highlight px-4 py-2 font-mono text-xs font-bold"
              aria-hidden="true"
              >VOIR PLUS GRAND</span
            >
          </figure>
        </div>
      </div>
    </header>

    <div
      class="mx-auto grid max-w-[1440px] lg:grid-cols-[210px_minmax(0,1fr)] xl:grid-cols-[240px_minmax(0,1fr)]"
    >
      <aside class="border-b-2 border-ink lg:border-r-2 lg:border-b-0">
        <nav
          aria-label="Sommaire du guide"
          class="px-4 py-6 sm:px-6 lg:sticky lg:top-28 lg:px-6 lg:py-10"
        >
          <p class="guide-eyebrow">AU PROGRAMME</p>
          <ol
            class="mt-4 grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-4 lg:grid-cols-1"
          >
            <li v-for="(chapter, index) in chapters" :key="chapter.id">
              <a
                :href="`#${chapter.id}`"
                class="flex min-h-11 items-center gap-3 py-2 text-xs font-bold hover:text-primary"
                ><span class="font-mono text-[10px] text-accent"
                  >0{{ index + 1 }}</span
                >{{ chapter.label }}</a
              >
            </li>
          </ol>
          <a
            href="#sources"
            class="mt-5 inline-block font-mono text-[11px] underline underline-offset-4 hover:text-primary"
            >Sources &amp; méthode ↗</a
          >
        </nav>
      </aside>

      <article class="min-w-0">
        <section
          id="essentiel"
          class="guide-section"
          aria-labelledby="essentiel-title"
        >
          <p class="guide-eyebrow">01 / LE POINT DE DÉPART</p>
          <h2 id="essentiel-title" class="guide-title">
            L’IMAX, c’est quoi au juste ?
          </h2>
          <p class="guide-intro">
            Une expérience de cinéma qui associe projection, écran, son et
            aménagement de la salle. Le logo désigne un ensemble ; toutes les
            salles n’ont pas le même équipement.
          </p>
          <div class="mt-7 grid gap-4 sm:grid-cols-3">
            <div class="border-2 border-ink bg-surface p-5">
              <Maximize :size="25" class="text-accent" aria-hidden="true" />
              <h3 class="mt-5 font-black">Un écran immersif</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Un grand champ de vision, lié à l’écran, aux gradins et à votre
                place dans la salle.
              </p>
            </div>
            <div class="border-2 border-ink bg-surface p-5">
              <Film :size="25" class="text-accent" aria-hidden="true" />
              <h3 class="mt-5 font-black">Parfois plus d’image</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Certains films ouvrent leur cadre en hauteur, sur tout le film
                ou sur quelques scènes.
              </p>
            </div>
            <div class="border-2 border-ink bg-surface p-5">
              <Volume2 :size="25" class="text-accent" aria-hidden="true" />
              <h3 class="mt-5 font-black">Un son enveloppant</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Un mixage et des enceintes adaptés à la salle, pour des voix,
                des effets et des basses précis.
              </p>
            </div>
          </div>
          <div class="mt-7 border-l-4 border-primary bg-surface p-5">
            <p class="font-black">
              Le bon réflexe : vérifier le film ET la salle.
            </p>
            <p class="mt-2 text-sm leading-relaxed">
              Un grand écran n’ajoute pas automatiquement de l’image. Pour
              profiter du cadre le plus haut, il faut une version du film, un
              projecteur et un écran qui le permettent.
            </p>
          </div>
        </section>

        <section
          id="formats"
          class="guide-section"
          aria-labelledby="formats-title"
        >
          <p class="guide-eyebrow">02 / VOIR LA DIFFÉRENCE</p>
          <h2 id="formats-title" class="guide-title">
            La même scène, plus de hauteur.
          </h2>
          <p class="guide-intro">
            Le « ratio », c’est simplement la forme de l’image : sa largeur
            divisée par sa hauteur. À largeur égale, un chiffre plus petit donne
            un cadre plus haut. Cliquez sur les formats pour comparer.
          </p>
          <div class="mt-7"><GuidesImaxFrameComparison /></div>
          <div class="mt-8 grid gap-5 sm:grid-cols-2">
            <div>
              <h3 class="font-black">1,90 : le repère courant</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Le format IMAX des multiplexes peut montrer environ 26 % de
                surface en plus qu’un cadre 2,39 à largeur égale, si la version
                du film le prévoit.
              </p>
            </div>
            <div>
              <h3 class="font-black">1,43 : le cadre très haut</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Jusqu’à environ 67 % en plus dans cette même comparaison. En
                France, le
                <NuxtLink :to="landmarkTarget('odysseum')" class="guide-link"
                  >Pathé Odysseum à Montpellier</NuxtLink
                >
                permet de voir les films compatibles en 1,43.
              </p>
            </div>
          </div>
          <p class="mt-5 text-sm leading-relaxed text-muted">
            Le cinéma classique utilise aussi le 1,85:1 et d’autres formats. Le
            2,39 n’est donc pas le format de toutes les séances ordinaires. Une
            image plus haute ne dit rien, à elle seule, de la définition ou de
            la taille physique de l’écran.
          </p>
        </section>

        <section
          id="projection"
          class="guide-section"
          aria-labelledby="projection-title"
        >
          <p class="guide-eyebrow">03 / DERRIÈRE LE MOT IMAX</p>
          <h2 id="projection-title" class="guide-title">
            Laser, double laser, 70 mm…
          </h2>
          <p class="guide-intro">
            Le laser désigne la source de lumière d’un projecteur numérique. Le
            70 mm désigne une pellicule. Le 1,43 ou le 1,90 désigne la forme de
            l’image. Ces mots ne parlent pas de la même chose.
          </p>
          <div class="mt-7"><GuidesImaxProjectionExplorer /></div>
          <div class="mt-8">
            <h3 class="font-black">Le 15/70, sans jargon</h3>
            <p class="mt-3 text-sm leading-relaxed">
              « 70 » correspond à la largeur de la pellicule en millimètres ; «
              15 » au nombre de perforations par image. Le film défile
              horizontalement dans le projecteur. Sur cette bande de
              <em>L’Odyssée</em>, on distingue les photogrammes et les
              perforations le long des deux bords.
            </p>
            <figure class="mt-5 border-2 border-ink bg-[#eee9dd] p-3 sm:p-4">
              <img
                src="/images/guides/odyssee-pellicule-1600.webp"
                srcset="
                  /images/guides/odyssee-pellicule-800.webp   800w,
                  /images/guides/odyssee-pellicule-1600.webp 1600w,
                  /images/guides/odyssee-pellicule-2400.webp 2400w
                "
                sizes="(max-width: 1023px) 90vw, 1000px"
                width="2400"
                height="679"
                alt="Bande de pellicule IMAX 70 mm de L’Odyssée : trois photogrammes et leurs perforations sur les bords supérieur et inférieur."
                class="h-auto w-full"
                loading="lazy"
                decoding="async"
              >
              <figcaption
                class="mt-3 flex flex-wrap justify-between gap-2 font-mono text-[10px] leading-relaxed"
              >
                <span>15 perforations par image</span
                ><span>Défilement horizontal →</span>
              </figcaption>
            </figure>
          </div>
          <details class="guide-details mt-7">
            <summary>Et les caméras, le DMR et les chiffres « 18K » ?</summary>
            <div class="space-y-4">
              <p>
                Un film IMAX peut être tourné sur pellicule ou avec des caméras
                numériques certifiées. D’autres films sont adaptés après le
                tournage pour une sortie IMAX. Le format du tournage et celui de
                la projection sont deux choses différentes.
              </p>
              <p>
                Le DMR est une remasterisation de l’image pour la projection
                IMAX. Il peut ajuster le bruit, le grain et le rendu. Il ne crée
                pas les éléments qui n’ont jamais été filmés et son résultat
                dépend du travail sur chaque œuvre.
              </p>
              <p>
                La pellicule n’a pas une grille fixe de pixels. Dire qu’elle «
                vaut 18K » simplifie beaucoup de choses. Préférez les
                informations concrètes : support, version du film, format
                projeté et qualité de la salle.
              </p>
            </div>
          </details>
        </section>

        <section
          id="experience"
          class="guide-section"
          aria-labelledby="experience-title"
        >
          <p class="guide-eyebrow">04 / AU-DELÀ DE L’ÉCRAN</p>
          <h2 id="experience-title" class="guide-title">
            Le son, la salle et la 3D.
          </h2>
          <div class="mt-7 grid gap-7 sm:grid-cols-2">
            <div>
              <h3 class="font-black">Un son qui vous entoure</h3>
              <p class="mt-3 text-sm leading-relaxed">
                Certaines installations IMAX proposent 12 canaux, avec des
                enceintes latérales et en hauteur. Un « canal » est une piste
                dirigée vers une zone d’enceintes. Le mixage et le réglage de la
                salle comptent autant que leur nombre.
              </p>
              <p class="mt-3 text-sm leading-relaxed">
                Le Dolby Atmos fonctionne autrement : il place les sons dans
                l’espace et les répartit entre les enceintes de la salle. Dans
                les deux cas, la qualité d’écoute dépend du mixage et du réglage
                des équipements.
              </p>
            </div>
            <div>
              <h3 class="font-black">La 3D est une option de séance</h3>
              <p class="mt-3 text-sm leading-relaxed">
                L’IMAX peut être en 2D ou en 3D. La 3D envoie une image
                différente à chaque œil ; les lunettes servent à les séparer.
                Elle ne change pas, à elle seule, le ratio du film.
              </p>
              <p class="mt-3 text-sm leading-relaxed">
                La luminosité, les lunettes et votre sensibilité personnelle
                influencent le confort. Si vous préférez la 2D, vérifiez la
                mention sur la séance avant de réserver.
              </p>
            </div>
          </div>
          <div class="mt-7 flex gap-4 border-2 border-ink bg-highlight/25 p-5">
            <Volume2 :size="24" class="shrink-0" aria-hidden="true" />
            <p class="text-sm leading-relaxed">
              <strong>Pour votre confort :</strong>
              le son et le grand champ de vision peuvent paraître intenses. Une
              place plus reculée permet de voir l’ensemble de l’écran sans
              tourner la tête.
            </p>
          </div>
        </section>

        <section
          id="salles"
          class="guide-section"
          aria-labelledby="salles-title"
        >
          <p class="guide-eyebrow">05 / PASSER DE LA THÉORIE À LA SALLE</p>
          <h2 id="salles-title" class="guide-title">
            Où voir de l’IMAX en France ?
          </h2>
          <p class="guide-intro">
            Le réseau s’étend au-delà de Paris : Pathé, Kinepolis, Megarama et
            des exploitants indépendants proposent des salles IMAX. La liste
            ci-dessous vient de l’annuaire MesSeances.
          </p>
          <div class="mt-7 border-2 border-ink bg-surface">
            <div class="border-b-2 border-ink bg-highlight p-5 sm:p-7">
              <p class="guide-eyebrow">LE CAS À CONNAÎTRE</p>
              <h3 class="mt-3 text-2xl font-black tracking-tight">
                Montpellier : le rendez-vous du 1,43.
              </h3>
            </div>
            <div class="p-5 sm:p-7">
              <p class="leading-relaxed">
                Le
                <NuxtLink :to="landmarkTarget('odysseum')" class="guide-link"
                  >Pathé Odysseum</NuxtLink
                >
                possède un écran au format 1,43 et une projection IMAX Laser
                compatible. Depuis juillet 2026, il propose aussi des
                projections sur pellicule IMAX 70 mm, inaugurées avec
                <em>L’Odyssée</em>.
              </p>
              <p class="mt-4 text-sm leading-relaxed">
                Le format dépend de la séance : numérique ou pellicule, 2D ou
                3D, VF ou VOST. Vérifiez ces mentions et la disponibilité des
                billets avant de faire le déplacement.
              </p>
              <NuxtLink
                :to="landmarkTarget('odysseum')"
                class="editorial-button-outline mt-5"
                >Consulter le cinéma
                <ArrowRight :size="16" aria-hidden="true" /></NuxtLink
              >
            </div>
          </div>
          <div class="mt-7">
            <h3 class="font-black">Quelques repères pour commencer</h3>
            <div class="mt-3 flex flex-wrap gap-2">
              <NuxtLink
                v-for="cinema in landmarks"
                :key="cinema.key"
                :to="cinema.to"
                class="inline-flex min-h-11 items-center gap-2 border border-ink bg-surface px-3 py-2 text-xs font-bold hover:bg-highlight"
                >{{ cinema.name }}
                <ArrowRight :size="13" aria-hidden="true" /></NuxtLink
              >
            </div>
            <p class="mt-3 text-xs leading-relaxed text-muted">
              La Géode et les sites scientifiques ou de loisirs proposent une
              programmation spécifique, souvent consacrée aux documentaires.
            </p>
          </div>
          <div class="mt-8 border-2 border-ink bg-surface p-5 sm:p-7">
            <div class="flex flex-wrap items-end justify-between gap-4">
              <h3 class="editorial-heading">Les cinémas de l’annuaire</h3>
              <NuxtLink :to="imaxDirectoryTarget" class="guide-link text-xs"
                >Ouvrir la liste avec le filtre IMAX ↗</NuxtLink
              >
            </div>
            <p class="mt-3 text-sm leading-relaxed text-muted">
              Retrouvez les cinémas équipés en IMAX et consultez leur
              programmation.
            </p>
            <template v-if="!directory?.unavailable && imaxTheaters.length">
              <label
                for="imax-theater-search"
                class="mt-6 block font-mono text-xs font-bold"
                >Chercher une ville ou un cinéma</label
              ><input
                id="imax-theater-search"
                v-model="theaterSearch"
                type="search"
                class="editorial-field mt-2"
                placeholder="Ex. : Lille, Montpellier, Pathé…"
              >
              <p role="status" class="mt-3 font-mono text-xs text-muted">
                {{ visibleTheaters.length }} cinéma{{
                  visibleTheaters.length > 1 ? 's' : ''
                }} affiché{{ visibleTheaters.length > 1 ? 's' : '' }} sur
                {{ imaxTheaters.length }} référencés avec IMAX
              </p>
              <ul
                v-if="visibleTheaters.length"
                class="mt-4 grid sm:grid-cols-2 sm:gap-x-6"
              >
                <li
                  v-for="theater in visibleTheaters"
                  :key="theater.id"
                  class="border-t border-line"
                >
                  <NuxtLink
                    :to="`/cinema/${encodeURIComponent(theater.slug)}`"
                    class="group flex min-h-20 items-center justify-between gap-3 py-3 hover:text-primary"
                    ><span
                      ><span
                        class="block font-mono text-[10px] uppercase tracking-wide text-muted"
                        >{{
                          theater.city
                        }}</span
                      ><span class="mt-1 block text-sm font-bold">{{
                        theaterDisplayName(theater)
                      }}</span></span
                    ><ArrowRight
                      :size="16"
                      class="shrink-0 group-hover:translate-x-1 motion-reduce:transform-none"
                      aria-hidden="true"
                    /></NuxtLink
                  >
                </li>
              </ul>
              <div v-else class="mt-5 border border-line bg-subtle p-5">
                <p class="text-sm">
                  Aucun cinéma ne correspond à votre recherche.
                </p>
                <button
                  type="button"
                  class="mt-3 text-sm font-bold underline underline-offset-4"
                  @click="theaterSearch = ''"
                >
                  Effacer la recherche
                </button>
              </div>
            </template>
            <p
              v-else
              class="mt-5 border-l-4 border-accent bg-accent-soft p-4 text-sm leading-relaxed"
            >
              {{
                directory?.unavailable ? 'L’annuaire est momentanément indisponible.' : 'Aucun cinéma n’est actuellement référencé avec la technologie IMAX dans cet annuaire.'
              }}
              <NuxtLink :to="imaxDirectoryTarget" class="guide-link"
                >Consulter la liste IMAX</NuxtLink
              >.
            </p>
          </div>
          <details class="guide-details mt-7">
            <summary>Combien de salles existe-t-il en France ?</summary>
            <div>
              <p>
                En juillet 2026, la France comptait 37 salles IMAX, en incluant
                la Cité de l’espace et le Futuroscope. L’annuaire MesSeances
                recense les cinémas suivis sur le site.
              </p>
            </div>
          </details>
          <details class="guide-details mt-3">
            <summary>Comment le réseau français s’est-il développé ?</summary>
            <div>
              <p>
                Après les installations de découverte scientifique, l’IMAX
                arrive dans les multiplexes à Montpellier en 1998. Disney
                Village suit en 2005. Le succès d’Avatar en 2009 accompagne
                l’accélération des ouvertures dans les années 2010, puis la
                diversification des exploitants. L’arrivée de l’IMAX 70 mm à
                Montpellier en 2026 marque une nouvelle étape.
              </p>
            </div>
          </details>
        </section>

        <section
          id="choisir"
          class="guide-section"
          aria-labelledby="choisir-title"
        >
          <p class="guide-eyebrow">06 / AVANT DE RÉSERVER</p>
          <h2 id="choisir-title" class="guide-title">
            Votre séance, en quatre vérifications.
          </h2>
          <ol class="mt-7 grid gap-4 sm:grid-cols-2">
            <li class="border-2 border-ink bg-surface p-5">
              <span class="font-mono text-sm font-bold text-accent"
                >01 — LE FILM</span
              >
              <h3 class="mt-3 font-black">Qu’apporte cette version ?</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Cherchez une annonce de cadre étendu et de séquences IMAX. Le
                film peut aussi garder son cadre classique et bénéficier du son
                et de la projection de la salle.
              </p>
            </li>
            <li class="border-2 border-ink bg-surface p-5">
              <span class="font-mono text-sm font-bold text-accent"
                >02 — LA PROJECTION</span
              >
              <h3 class="mt-3 font-black">Quelle mention sur le billet ?</h3>
              <p class="mt-2 text-sm leading-relaxed">
                IMAX, IMAX Laser ou IMAX 70 mm ? Puis 2D ou 3D, VF ou VOST ? En
                cas de doute sur le 1,43 ou la pellicule, demandez au cinéma.
              </p>
            </li>
            <li class="border-2 border-ink bg-surface p-5">
              <span class="font-mono text-sm font-bold text-accent"
                >03 — LE PRIX</span
              >
              <h3 class="mt-3 font-black">Quel total, supplément compris ?</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Le supplément dépend du cinéma, de la séance, du tarif et de
                votre abonnement. Comparez le montant final, y compris les
                éventuelles lunettes ou options ; il n’existe pas de prix IMAX
                unique en France.
              </p>
            </li>
            <li class="border-2 border-ink bg-surface p-5">
              <span class="font-mono text-sm font-bold text-accent"
                >04 — LA PLACE</span
              >
              <h3 class="mt-3 font-black">Immersion ou recul ?</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Pour une première séance, choisissez une place au centre, vers
                le milieu de la salle ou légèrement derrière. Plus près, l’image
                remplit davantage votre champ de vision ; plus loin, vous voyez
                plus facilement l’ensemble de l’écran.
              </p>
            </li>
          </ol>
          <p class="mt-6 flex items-start gap-3 text-sm leading-relaxed">
            <Check
              :size="20"
              class="mt-0.5 shrink-0 text-accent"
              aria-hidden="true"
            /><span
              ><strong>Est-ce que le supplément vaut le coup ?</strong>
              Il peut se justifier si le film exploite le format et si vous
              appréciez les grands écrans et le son immersif. Le confort, la
              langue, l’horaire et le budget restent de bons critères de
              choix.</span
            >
          </p>
        </section>

        <section
          id="questions"
          class="guide-section"
          aria-labelledby="questions-title"
        >
          <p class="guide-eyebrow">07 / LES DERNIERS DOUTES</p>
          <h2 id="questions-title" class="guide-title">
            Les questions qu’on se pose tous.
          </h2>
          <div class="mt-7 space-y-3">
            <details
              v-for="faq in faqs"
              :key="faq.question"
              class="guide-details"
            >
              <summary>{{ faq.question }}</summary>
              <div><p>{{ faq.answer }}</p></div>
            </details>
          </div>
        </section>

        <section
          id="sources"
          class="guide-section bg-[#eeede6]"
          aria-labelledby="sources-title"
        >
          <p class="guide-eyebrow">POUR ALLER PLUS LOIN</p>
          <h2 id="sources-title" class="guide-title">Sources &amp; méthode.</h2>
          <p class="mt-5 text-sm leading-relaxed">
            Les informations de ce guide s’appuient sur les publications
            suivantes.
          </p>
          <ol class="mt-6 space-y-5">
            <li
              v-for="(source, index) in sources"
              :id="`source-${source.id}`"
              :key="source.id"
              class="scroll-mt-32 border-t border-ink/20 pt-4"
            >
              <a :href="source.url" class="guide-link text-sm"
                >{{ index + 1 }}. {{ source.name }} ↗</a
              >
              <p class="mt-1 text-xs leading-relaxed text-muted">
                {{ source.detail }}
              </p>
            </li>
          </ol>
          <p
            class="mt-7 border-t border-ink/20 pt-5 text-xs leading-relaxed text-muted"
          >
            Visuels : scans de photogrammes de <em>Dune : Deuxième Partie</em>,
            <em>Project Hail Mary</em>
            et <em>L’Odyssée</em>, fournis pour ce guide. Les images restent la
            propriété de leurs ayants droit. Les masques de comparaison sont des
            recadrages pédagogiques centrés ; les cadrages des copies
            distribuées peuvent différer. Le schéma de projection est simplifié
            et sans échelle.
          </p>
          <NuxtLink :to="imaxDirectoryTarget" class="editorial-button mt-8"
            >Prêt à voir plus grand ? Trouver une salle IMAX
            <ArrowRight :size="16" aria-hidden="true" /></NuxtLink
          >
        </section>
      </article>
    </div>
  </main>
</template>

<style scoped>
@reference "../../assets/css/main.css";
.guide-section {
  @apply scroll-mt-28 border-b-2 border-ink px-4 py-10 last:border-b-0 sm:px-8 sm:py-14 xl:px-12;
}
.guide-eyebrow {
  @apply flex items-center gap-2 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-accent;
}
.guide-title {
  @apply mt-3 text-[clamp(1.75rem,3.2vw,2.7rem)] font-black leading-[1.08] tracking-[-0.055em];
}
.guide-intro {
  @apply mt-5 max-w-3xl text-base leading-relaxed sm:text-lg;
}
.guide-link {
  @apply font-bold underline decoration-1 underline-offset-4 hover:text-primary;
}
.guide-details {
  @apply border-2 border-ink bg-surface;
}
.guide-details summary {
  @apply cursor-pointer px-5 py-4 text-sm font-bold leading-relaxed marker:text-accent hover:bg-highlight/20;
}
.guide-details > div {
  @apply border-t border-line px-5 py-5 text-sm leading-relaxed;
}
</style>
