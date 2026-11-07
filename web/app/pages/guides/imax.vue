<script setup lang="ts">
import {
  ArrowDown,
  ArrowRight,
  Armchair,
  Film,
  FileText,
  Glasses,
  MapPin,
  Maximize,
  Volume2,
} from '@lucide/vue'
import type { Theater } from '~/types/api'
import imaxLogo from '~/assets/imgs/imax_logo_large.webp?no-inline'
import {
  guideCinemaTarget,
  guideMovieTarget,
  imaxGuideMovies,
  imaxDirectoryTarget,
  imaxLandmarks,
} from '~/utils/imaxGuide'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { serializeJsonLd } from '~/utils/jsonLd'

const api = useMesSeancesApi()
const isInitialHydration = useNuxtApp().isHydrating
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
const { data: movieTargets } = await useAsyncData(
  'imax-guide-movies',
  async () => {
    const entries = await Promise.all(
      imaxGuideMovies.map(async (reference) => {
        try {
          const response = await api.movies({
            search: reference.search,
            include_ended: true,
            page_size: 100,
          })
          return [
            reference.id,
            guideMovieTarget(response.items, reference),
          ] as const
        } catch {
          return [reference.id, guideMovieTarget([], reference)] as const
        }
      }),
    )
    return Object.fromEntries(entries)
  },
)
function movieTarget(id: (typeof imaxGuideMovies)[number]['id']) {
  return (
    movieTargets.value?.[id] ??
    guideMovieTarget([], imaxGuideMovies.find((movie) => movie.id === id)!)
  )
}
function guideTextParts(text: string) {
  return text.split(/(L’Odyssée|Montpellier)/u).map((part) => ({
    text: part,
    to:
      part === 'Montpellier'
        ? '/ville/montpellier/cinemas'
        : part === 'L’Odyssée'
          ? movieTarget('odyssee-voilier')
          : undefined,
  }))
}
const theaters = computed(() => directory.value?.theaters ?? [])
function landmarkTarget(key: keyof typeof imaxLandmarks) {
  return guideCinemaTarget(theaters.value, imaxLandmarks[key])
}
const chapters = [
  { id: 'essentiel', label: 'L’essentiel' },
  { id: 'formats', label: 'Comparer l’image' },
  { id: 'projection', label: 'Laser ou pellicule ?' },
  { id: 'experience', label: 'Son & 3D' },
  { id: 'choisir', label: 'Choisir sa séance' },
  { id: 'salles', label: 'Les salles en France' },
  { id: 'questions', label: 'Questions fréquentes' },
]
const navigationChapters = [
  ...chapters,
  { id: 'sources', label: 'Sources & méthode' },
]
const activeChapter = ref('essentiel')
const activeChapterLabel = computed(
  () =>
    navigationChapters.find((chapter) => chapter.id === activeChapter.value)
      ?.label ?? chapters[0]!.label,
)
const mobileContents = ref<HTMLDetailsElement | null>(null)
const headerHeight = ref(124)
let headerObserver: ResizeObserver | undefined
let scrollFrame: number | undefined

function closeContents() {
  if (mobileContents.value) mobileContents.value.open = false
}
function closeContentsWithFocus() {
  closeContents()
  mobileContents.value?.querySelector('summary')?.focus()
}
function updateActiveChapter() {
  const cutoff = headerHeight.value + (window.innerWidth < 1024 ? 80 : 24)
  let current = chapters[0]!.id
  for (const chapter of navigationChapters) {
    const section = document.getElementById(chapter.id)
    if (section && section.getBoundingClientRect().top <= cutoff + 2)
      current = chapter.id
  }
  activeChapter.value = current
}
function queueChapterUpdate() {
  if (scrollFrame !== undefined) return
  scrollFrame = window.requestAnimationFrame(() => {
    scrollFrame = undefined
    updateActiveChapter()
  })
}
function handleGuideResize() {
  if (window.innerWidth >= 1024) closeContents()
  queueChapterUpdate()
}
onMounted(() => {
  const appHeader = document.getElementById('app-header')
  if (appHeader) {
    headerHeight.value = appHeader.getBoundingClientRect().height
    headerObserver = new ResizeObserver(() => {
      headerHeight.value = appHeader.getBoundingClientRect().height
      queueChapterUpdate()
    })
    headerObserver.observe(appHeader)
  }
  window.addEventListener('scroll', queueChapterUpdate, { passive: true })
  window.addEventListener('resize', handleGuideResize)
  updateActiveChapter()
  nextTick(() => {
    const initialChapter = navigationChapters.find(
      (chapter) => `#${chapter.id}` === window.location.hash,
    )
    if (isInitialHydration && initialChapter)
      document.getElementById(initialChapter.id)?.scrollIntoView({
        behavior: 'instant',
        block: 'start',
      })
    updateActiveChapter()
  })
})
onBeforeUnmount(() => {
  headerObserver?.disconnect()
  window.removeEventListener('scroll', queueChapterUpdate)
  window.removeEventListener('resize', handleGuideResize)
  if (scrollFrame !== undefined) window.cancelAnimationFrame(scrollFrame)
})

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
      '19 septembre 2024 · Montpellier figure dans cette liste de salles Laser 1,43. Le film a été tourné avec des caméras numériques certifiées IMAX.',
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
  {
    id: 'laser-france',
    name: 'IMAX · Déploiement du laser en France',
    detail:
      '15 mai 2024 · Accord avec Megarama pour de nouvelles installations IMAX Laser 4K dans les multiplexes français.',
    url: 'https://imaxcorporation.gcs-web.com/node/25076/pdf',
  },
  {
    id: 'dome-france',
    name: 'Universcience · IMAX Laser sur dôme',
    detail:
      '21 janvier 2025 · Projection laser 4K sur écran hémisphérique, avec des présentations à 180° et en 1,90:1.',
    url: 'https://www.cite-sciences.fr/fileadmin/fileadmin_CSI/fichiers/vous-etes/professionnel/presse-media/cp/CP_Geode.pdf',
  },
  {
    id: 'prix-roubaix',
    name: 'Ville de Roubaix · Tarifs du Megarama',
    detail:
      '2 août 2025 · Plein tarif 8,50 € et supplément IMAX 4 €, soit 12,50 €. Un exemple utilisé pour le repère de budget.',
    url: 'https://www.roubaixxl.fr/cinema-megarama-roubaix/',
  },
  {
    id: 'prix-cannes',
    name: 'AlloCiné · Tarifs du Cineum Cannes',
    detail:
      'Consulté le 7 novembre 2026 · Plein tarif 13,50 € et supplément IMAX 6 €, soit 19,50 € hors 3D.',
    url: 'https://www.allocine.fr/salle/cinema-S4383/tarifs/',
  },
  {
    id: 'prix-villette',
    name: 'Orange · Tarifs du Pathé La Villette',
    detail:
      'Consulté le 7 novembre 2026 · Tarif normal 17,20 € et supplément IMAX 8 €, soit 25,20 € en 2D. Le budget du guide est un ordre de grandeur, pas un tarif national.',
    url: 'https://divertissements.orange.fr/salles/pathe-la-villette-imax-4dx-CNT0000009iVcg/informations.html',
  },
]
const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/guides/imax')
const title = 'IMAX : le guide complet en France - MesSeances'
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
            headline: 'IMAX, le guide complet',
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
  <main
    class="imax-guide bg-[#f8f7f2] text-ink"
    :style="{ '--guide-header-height': `${headerHeight}px` }"
  >
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
              <img
                :src="imaxLogo"
                alt="IMAX"
                width="2048"
                height="388"
                class="mb-4 block h-auto w-[clamp(12rem,24vw,22rem)] max-w-full"
              >
              <span class="block text-ink">le guide complet</span>
            </h1>
            <p class="mt-7 max-w-xl text-lg leading-relaxed sm:text-xl">
              Comprenez les formats IMAX, comparez l’image et trouvez votre
              salle en France.
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
          <figure class="border-2 border-ink bg-ink p-3 sm:p-4">
            <div class="relative aspect-[1.43]">
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
          </figure>
        </div>
      </div>
    </header>

    <nav
      class="guide-mobile-contents sticky z-20 border-y-2 border-ink bg-[#f8f7f2] lg:hidden"
      aria-label="Sommaire mobile du guide"
    >
      <details
        ref="mobileContents"
        @keydown.esc.prevent="closeContentsWithFocus"
      >
        <summary
          class="cursor-pointer px-4 py-3 text-sm font-bold marker:text-accent sm:px-6"
        >
          <span>Sommaire</span
          ><span class="ml-3 font-normal text-muted">{{
            activeChapterLabel
          }}</span>
        </summary>
        <ol
          class="guide-mobile-chapters grid gap-1 overflow-y-auto border-t border-line px-4 py-3 sm:grid-cols-2 sm:px-6"
        >
          <li v-for="(chapter, index) in navigationChapters" :key="chapter.id">
            <a
              :href="`#${chapter.id}`"
              :aria-current="activeChapter === chapter.id ? 'location' : undefined"
              class="flex min-h-11 items-center gap-3 px-3 py-2 text-sm font-bold hover:bg-highlight/30"
              :class="activeChapter === chapter.id ? 'bg-accent-soft text-accent' : undefined"
              @click="closeContents"
            >
              <span class="font-mono text-[10px] text-accent">{{
                String(index + 1).padStart(2, '0')
              }}</span>{{ chapter.label }}
            </a>
          </li>
        </ol>
      </details>
    </nav>

    <div
      class="mx-auto grid max-w-[1440px] lg:grid-cols-[210px_minmax(0,1fr)] xl:grid-cols-[240px_minmax(0,1fr)]"
    >
      <aside class="hidden border-r-2 border-ink lg:block">
        <nav
          aria-label="Sommaire du guide"
          class="guide-desktop-contents sticky px-4 py-7 xl:px-5"
        >
          <p
            class="px-3 font-mono text-[10px] font-bold uppercase tracking-[0.14em] text-accent"
          >
            AU PROGRAMME
          </p>
          <ol class="mt-4">
            <li
              v-for="(chapter, index) in navigationChapters"
              :key="chapter.id"
              :class="chapter.id === 'sources' ? 'mt-3 border-t border-ink/10 pt-3' : undefined"
            >
              <a
                :href="`#${chapter.id}`"
                :aria-current="activeChapter === chapter.id ? 'location' : undefined"
                class="grid min-h-11 grid-cols-[1.25rem_minmax(0,1fr)] items-center gap-2.5 border-l-2 py-2 pr-2 pl-3 text-xs leading-snug transition-colors hover:bg-ink/5 hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-accent"
                :class="activeChapter === chapter.id ? 'border-accent font-bold text-accent' : 'border-ink/10 font-medium text-ink/70'"
              >
                <span
                  class="grid size-5 place-items-center font-mono text-[10px] font-bold transition-colors"
                  :class="activeChapter === chapter.id ? 'bg-accent text-white' : 'text-muted'"
                  aria-hidden="true"
                >
                  <FileText v-if="chapter.id === 'sources'" :size="13" />
                  <template v-else>{{
                    String(index + 1).padStart(2, '0')
                  }}</template>
                </span>
                <span>{{ chapter.label }}</span>
              </a>
            </li>
          </ol>
        </nav>
      </aside>

      <article class="min-w-0">
        <section
          id="essentiel"
          class="guide-section imax-introduction"
          aria-labelledby="essentiel-title"
        >
          <div
            class="grid gap-5 min-[900px]:grid-cols-[minmax(0,1fr)_clamp(260px,28vw,360px)] min-[900px]:gap-x-8"
          >
            <div class="min-[900px]:col-span-2">
              <p class="guide-eyebrow">01 / LE POINT DE DÉPART</p>
              <h2 id="essentiel-title" class="guide-title">
                L’IMAX, c’est quoi ?
              </h2>
              <p class="mt-4 text-base leading-relaxed sm:text-lg">
                L’IMAX associe un grand écran, une projection dédiée et un son
                réglé pour la salle. L’objectif : une image qui remplit
                davantage votre champ de vision et un son qui vous enveloppe.
              </p>
            </div>
            <img
              src="/images/guides/salle-imax-1200.webp"
              srcset="
                /images/guides/salle-imax-640.webp   640w,
                /images/guides/salle-imax-1200.webp 1200w
              "
              sizes="(max-width: 899px) 90vw, 360px"
              width="1619"
              height="1080"
              alt="Une salle IMAX vue depuis les gradins : un grand écran face à des rangées de fauteuils en pente."
              class="h-auto w-full min-[900px]:col-start-2 min-[900px]:row-start-2 min-[900px]:self-center"
              loading="lazy"
              decoding="async"
            >
            <div
              class="self-center min-[900px]:col-start-1 min-[900px]:row-start-2"
            >
              <div class="grid gap-5">
                <div
                  class="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-x-3 gap-y-2"
                >
                  <Maximize
                    :size="20"
                    class="mt-0.5 text-accent"
                    aria-hidden="true"
                  />
                  <h3 class="font-bold">Un écran qui vous immerge</h3>
                  <p class="col-start-2 text-sm leading-relaxed text-muted">
                    L’écran et les gradins sont pensés pour donner l’impression
                    d’être dans le film.
                  </p>
                </div>
                <div
                  class="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-x-3 gap-y-2"
                >
                  <Volume2
                    :size="20"
                    class="mt-0.5 text-accent"
                    aria-hidden="true"
                  />
                  <h3 class="font-bold">Un son qui vous entoure</h3>
                  <p class="col-start-2 text-sm leading-relaxed text-muted">
                    Des voix claires, des effets précis et des basses que l’on
                    ressent.
                  </p>
                </div>
                <div
                  class="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-x-3 gap-y-2"
                >
                  <Film
                    :size="20"
                    class="mt-0.5 text-accent"
                    aria-hidden="true"
                  />
                  <h3 class="font-bold">Parfois, plus d’image</h3>
                  <p class="col-start-2 text-sm leading-relaxed text-muted">
                    Certains films révèlent davantage d’image en haut et en bas,
                    dans les salles compatibles.
                  </p>
                </div>
              </div>
            </div>
            <p
              class="text-sm leading-relaxed text-muted min-[900px]:col-span-2"
            >
              Toutes les salles IMAX n’ont pas le même équipement. Voici comment
              comprendre les différences.
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
            Certaines salles IMAX peuvent montrer une image plus haute que
            d’autres, lorsque le film le permet. Pour comprendre cette
            différence, on parle de « ratio » : le rapport entre la largeur et
            la hauteur de l’image. À largeur égale, plus ce chiffre est petit,
            plus le cadre est haut.
          </p>
          <div class="mt-7">
            <GuidesImaxFrameComparison :movie-targets="movieTargets ?? {}" />
          </div>
          <div class="mt-8">
            <GuidesImaxRatioDiagram
              :full-format-cinema="landmarkTarget('odysseum')"
            />
          </div>
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
            Pour montrer ces différents cadres, les salles IMAX n’utilisent pas
            toutes le même équipement. Certaines projettent un fichier numérique
            avec une lumière laser ou des lampes au xénon. D’autres font défiler
            une copie du film sur pellicule 70 mm. Le système installé détermine
            notamment la définition de l’image et les formats que la salle peut
            afficher.
          </p>
          <div class="mt-7"><GuidesImaxProjectionExplorer /></div>
          <div class="mt-8">
            <h3 class="font-black">Le 15/70, sans jargon</h3>
            <p class="mt-3 text-sm leading-relaxed">
              La pellicule IMAX contient une succession de grandes images
              photographiques, appelées photogrammes. La bande mesure 70 mm de
              large et chaque image s’étend sur 15 perforations, les petits
              trous qui guident le film dans le projecteur. C’est ce qui donne
              le nom « 15/70 ». Contrairement au 70 mm classique, la bande
              défile horizontalement. Cette pellicule de
              <NuxtLink :to="movieTarget('odyssee-voilier')" class="guide-link"
                ><em>L’Odyssée</em></NuxtLink
              >
              permet de voir les images et les perforations sur les deux bords.
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
          <div class="mt-7 space-y-2">
            <details class="guide-details">
              <summary>
                Quelles caméras utilise-t-on pour filmer en IMAX ?
              </summary>
              <div
                class="grid items-start gap-6 min-[900px]:grid-cols-[1.2fr_0.8fr]"
              >
                <div class="space-y-5">
                  <div>
                    <h4 class="font-bold">Les caméras IMAX sur pellicule</h4>
                    <p class="mt-2">
                      Elles enregistrent de grands photogrammes sur un négatif
                      de 65 mm, avec un défilement horizontal de 15 perforations
                      par image. La copie projetée en salle utilise, elle, une
                      bande de 70 mm. On peut donc parler de tournage IMAX 65 mm
                      et de projection IMAX 70 mm pour un même film.
                    </p>
                  </div>
                  <div>
                    <h4 class="font-bold">Les caméras numériques certifiées</h4>
                    <p class="mt-2">
                      Le programme « Filmed for IMAX » inclut aussi des caméras
                      numériques sélectionnées par IMAX. Il accompagne les
                      équipes du tournage jusqu’à la préparation de la version
                      destinée aux salles IMAX. La mention « filmé pour IMAX »
                      ne signifie donc pas nécessairement que le film a été
                      tourné sur pellicule.
                    </p>
                  </div>
                  <div>
                    <h4 class="font-bold">
                      La caméra ne décide pas de toute la séance
                    </h4>
                    <p class="mt-2">
                      Le réalisateur choisit le cadrage de ses scènes. Un film
                      peut mélanger plusieurs caméras et plusieurs ratios.
                      Ensuite, le format visible dépend de la version distribuée
                      et de la salle : une caméra certifiée ne garantit ni un
                      film entier en 1,43:1, ni une projection sur pellicule.
                    </p>
                  </div>
                  <p class="text-xs text-muted">
                    Pour aller plus loin :
                    <a
                      href="https://www.imax.com/filmed-for-imax"
                      class="guide-link"
                      >IMAX · Filmed for IMAX ↗</a
                    >
                    et
                    <a
                      href="https://www.imax.com/the-imax-experience"
                      class="guide-link"
                      >les caméras IMAX ↗</a
                    >.
                  </p>
                </div>
                <figure>
                  <img
                    src="/images/guides/camera-imax-1200.webp"
                    srcset="
                      /images/guides/camera-imax-640.webp   640w,
                      /images/guides/camera-imax-1200.webp 1200w
                    "
                    sizes="(max-width: 899px) 90vw, 35vw"
                    width="1920"
                    height="1080"
                    alt="Caméra IMAX argentique sur un plateau de tournage, avec son objectif, son viseur et son magasin de pellicule."
                    class="h-auto w-full"
                    loading="lazy"
                    decoding="async"
                  >
                  <figcaption class="mt-2 text-xs leading-relaxed text-muted">
                    Une caméra IMAX sur pellicule.
                  </figcaption>
                </figure>
              </div>
            </details>
            <details class="guide-details">
              <summary>Que fait la remasterisation IMAX DMR ?</summary>
              <div class="space-y-5">
                <div>
                  <h4 class="font-bold">
                    Préparer le film pour un très grand écran
                  </h4>
                  <p class="mt-2">
                    Le DMR, pour Digital Media Remastering, intervient après le
                    tournage. À partir du master du film, les équipes IMAX
                    travaillent avec les cinéastes pour préparer une version
                    adaptée à leur présentation en salle. Un film peut ainsi
                    sortir en IMAX sans avoir été tourné avec des caméras IMAX.
                  </p>
                </div>
                <div>
                  <h4 class="font-bold">Un travail sur le rendu de l’image</h4>
                  <p class="mt-2">
                    Sur un écran géant, le grain, le bruit numérique et les
                    défauts de l’image deviennent plus visibles. Les outils DMR
                    permettent d’en ajuster le rendu et le niveau, en accord
                    avec les choix du film. Des projections de contrôle servent
                    à évaluer le résultat. L’intensité du traitement et le rendu
                    final varient selon chaque œuvre.
                  </p>
                </div>
                <div>
                  <h4 class="font-bold">Le DMR ne garantit pas plus d’image</h4>
                  <p class="mt-2">
                    La mention DMR ne garantit pas une image étendue. Pour
                    révéler davantage de décor en haut et en bas, il faut une
                    source et une version prévues pour ce cadrage. Le DMR ne
                    peut pas restituer fidèlement ce qui n’a pas été capté à cet
                    endroit. Il ne change pas non plus, à lui seul, le support
                    de projection ou l’équipement de la salle.
                  </p>
                </div>
                <p class="text-xs text-muted">
                  Pour aller plus loin :
                  <a
                    href="https://www.imax.com/the-imax-experience"
                    class="guide-link"
                    >IMAX · Le procédé DMR ↗</a
                  >.
                </p>
              </div>
            </details>
            <details class="guide-details">
              <summary>
                Pourquoi parle-t-on de « 18K » pour la pellicule IMAX ?
              </summary>
              <div class="space-y-5">
                <div>
                  <h4 class="font-bold">
                    Un chiffre pour donner une idée du niveau de détail
                  </h4>
                  <p class="mt-2">
                    En numérique, le « K » décrit approximativement le nombre de
                    milliers de pixels sur la largeur de l’image. IMAX emploie
                    l’expression « jusqu’à 18K » pour présenter le potentiel de
                    détail de son grand format sur pellicule. C’est une
                    équivalence annoncée, pas la définition d’une matrice de
                    projection comme le 4K numérique.
                  </p>
                </div>
                <div>
                  <h4 class="font-bold">
                    La pellicule n’a pas une grille de pixels
                  </h4>
                  <p class="mt-2">
                    Son pouvoir de résolution décrit sa capacité à distinguer
                    des détails très proches. Il dépend notamment de l’émulsion,
                    de l’exposition, de l’objectif et du contraste de la scène.
                    Il n’existe donc pas un nombre de pixels unique qui
                    résumerait toutes les images enregistrées sur ce support.
                  </p>
                </div>
                <div>
                  <h4 class="font-bold">
                    Du négatif à l’écran, toute la chaîne compte
                  </h4>
                  <p class="mt-2">
                    Le tirage de la copie, les éventuelles étapes numériques,
                    l’optique du projecteur et sa mise au point influencent les
                    détails finalement visibles. Numériser une pellicule à très
                    haute résolution ne garantit pas que chaque pixel
                    corresponde à un détail distinct. Le chiffre « 18K » ne
                    permet donc pas, à lui seul, de classer une séance sur
                    pellicule face à une séance laser 4K.
                  </p>
                </div>
                <p class="text-xs text-muted">
                  Pour aller plus loin :
                  <a
                    href="https://www.imax.com/pr/regal-cinemas-and-imax-expand-partnership-four-new-marquee"
                    class="guide-link"
                    >IMAX · La revendication « jusqu’à 18K » ↗</a
                  >
                  et
                  <a
                    href="https://www.kodak.com/content/products-brochures/Film/kodak-essential-reference-guide-for-filmmakers.pdf"
                    class="guide-link"
                    >Kodak · Résolution et netteté de la pellicule ↗</a
                  >.
                </p>
              </div>
            </details>
          </div>
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
          <p class="guide-intro">
            L’image ne fait pas tout. Le son, votre place dans la salle et le
            choix entre 2D et 3D changent aussi l’expérience.
          </p>
          <div class="mt-7 grid items-start gap-6 md:grid-cols-3 md:gap-8">
            <div
              class="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-x-3 gap-y-2"
            >
              <Volume2
                :size="20"
                class="mt-0.5 text-accent"
                aria-hidden="true"
              />
              <h3 class="font-bold">Un son pensé pour la salle</h3>
              <p class="col-start-2 text-sm leading-relaxed text-muted">
                Des voix claires, des effets répartis autour de vous et des
                basses que l’on ressent. Le mixage et les réglages comptent
                autant que le nombre d’enceintes.
              </p>
            </div>
            <div
              class="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-x-3 gap-y-2"
            >
              <Armchair
                :size="20"
                class="mt-0.5 text-accent"
                aria-hidden="true"
              />
              <h3 class="font-bold">Votre place change la perception</h3>
              <p class="col-start-2 text-sm leading-relaxed text-muted">
                Plus près, l’image occupe davantage votre champ de vision. Plus
                loin, vous voyez plus facilement l’ensemble de l’écran.
              </p>
            </div>
            <div
              class="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-x-3 gap-y-2"
            >
              <Glasses
                :size="20"
                class="mt-0.5 text-accent"
                aria-hidden="true"
              />
              <h3 class="font-bold">La 3D apporte du relief</h3>
              <p class="col-start-2 text-sm leading-relaxed text-muted">
                L’IMAX existe en 2D et en 3D. En 3D, les lunettes permettent à
                chaque œil de recevoir une image différente. Le choix dépend de
                la séance et de vos préférences.
              </p>
            </div>
          </div>
          <div class="mt-6 space-y-2">
            <details class="guide-details">
              <summary>Comment fonctionne le son IMAX ?</summary>
              <div class="flow-root">
                <figure
                  class="mb-5 min-[900px]:float-right min-[900px]:ml-6 min-[900px]:w-[38%]"
                >
                  <img
                    src="/images/guides/enceinte-imax-1200.webp"
                    srcset="
                      /images/guides/enceinte-imax-640.webp   640w,
                      /images/guides/enceinte-imax-1200.webp 1200w
                    "
                    sizes="(max-width: 899px) 90vw, 35vw"
                    width="1500"
                    height="1000"
                    alt="Une grande enceinte IMAX suspendue, avec ses haut-parleurs et son pavillon visibles."
                    class="h-auto w-full"
                    loading="lazy"
                    decoding="async"
                  >
                  <figcaption class="mt-2 text-xs leading-relaxed text-muted">
                    Une enceinte IMAX.
                  </figcaption>
                </figure>
                <div class="space-y-5">
                  <div>
                    <h4 class="font-bold">Des canaux pour répartir les sons</h4>
                    <p class="mt-2">
                      Un canal est une piste audio envoyée vers une enceinte ou
                      un groupe d’enceintes. Le nombre de canaux dépend de
                      l’installation. Certaines salles IMAX proposent un système
                      à 12 canaux, avec des enceintes supplémentaires sur les
                      côtés et en hauteur pour répartir les effets dans la
                      salle.
                    </p>
                  </div>
                  <div>
                    <h4 class="font-bold">
                      Le mixage et les réglages font partie du résultat
                    </h4>
                    <p class="mt-2">
                      Le mixage détermine la place des voix, de la musique et
                      des effets dans ces canaux. Le positionnement des
                      enceintes et le réglage de la salle permettent ensuite de
                      restituer cet équilibre. Le nombre de canaux, à lui seul,
                      ne décrit donc ni la précision du son ni sa qualité
                      d’écoute.
                    </p>
                  </div>
                  <div>
                    <h4 class="font-bold">
                      Quelle différence avec Dolby Atmos ?
                    </h4>
                    <p class="mt-2">
                      Dolby Atmos associe des canaux à des objets sonores : des
                      sons accompagnés d’informations sur leur position et leur
                      déplacement. Le processeur de la salle les répartit entre
                      les enceintes disponibles, y compris au plafond. L’IMAX et
                      l’Atmos organisent ainsi le son différemment. Dans les
                      deux cas, le mixage du film et l’installation restent
                      essentiels.
                    </p>
                  </div>
                  <p class="text-xs text-muted">
                    Pour aller plus loin :
                    <a
                      href="https://www.imax.com/news/imax-laser-here"
                      class="guide-link"
                      >IMAX · Le son à 12 canaux ↗</a
                    >
                    et
                    <a
                      href="https://professional.dolby.com/en-in/cinema/dolby-atmos-cinema-sound/"
                      class="guide-link"
                      >Dolby · Atmos au cinéma ↗</a
                    >.
                  </p>
                </div>
              </div>
            </details>
            <details class="guide-details">
              <summary>Comment fonctionne la 3D IMAX ?</summary>
              <div class="space-y-5">
                <div>
                  <h4 class="font-bold">Une vue différente pour chaque œil</h4>
                  <p class="mt-2">
                    La version 3D contient deux vues légèrement différentes
                    d’une même scène. Les lunettes permettent à chaque œil de
                    recevoir la vue qui lui est destinée. Le cerveau les combine
                    et perçoit ainsi de la profondeur dans l’image.
                  </p>
                </div>
                <div>
                  <h4 class="font-bold">
                    Des lunettes adaptées à la projection
                  </h4>
                  <p class="mt-2">
                    Les installations IMAX n’utilisent pas toutes le même
                    système de séparation des images. Les lunettes ne sont donc
                    pas nécessairement interchangeables entre salles. Utilisez
                    celles indiquées par le cinéma pour votre séance. Comme
                    elles filtrent une partie de la lumière, la luminosité de la
                    projection compte particulièrement en 3D.
                  </p>
                </div>
                <div>
                  <h4 class="font-bold">
                    Le relief, le cadre et votre confort
                  </h4>
                  <p class="mt-2">
                    La 3D ne change pas, à elle seule, la hauteur de l’image. Le
                    ratio reste lié à la version du film et à la salle. Le
                    confort dépend aussi du rendu de la projection, des lunettes
                    et de votre sensibilité. Si vous préférez la 2D, choisissez
                    une séance annoncée en 2D.
                  </p>
                </div>
                <p class="text-xs text-muted">
                  Pour aller plus loin :
                  <a href="https://imaxvictoria.com/faq/" class="guide-link"
                    >IMAX Victoria · Projection laser et lunettes 3D ↗</a
                  >
                  et
                  <a
                    href="https://www.si.edu/Content/SE/Educator%20Guides/Dinosaurs%203D%20-%20Giants%20of%20the%20Patagonia.pdf"
                    class="guide-link"
                    >Smithsonian · Le principe de la 3D IMAX ↗</a
                  >.
                </p>
              </div>
            </details>
          </div>
        </section>

        <section
          id="choisir"
          class="guide-section"
          aria-labelledby="choisir-title"
        >
          <p class="guide-eyebrow">05 / AVANT DE RÉSERVER</p>
          <h2 id="choisir-title" class="guide-title">
            Bien choisir votre séance IMAX.
          </h2>
          <ol class="mt-7 divide-y divide-ink/15">
            <li
              class="grid grid-cols-[2rem_minmax(0,1fr)] items-start gap-x-3 py-4 first:pt-0 md:grid-cols-[2rem_minmax(0,0.8fr)_minmax(0,1.2fr)] md:gap-x-5"
            >
              <span
                class="font-mono text-sm font-bold text-accent"
                aria-hidden="true"
                >01</span
              >
              <h3 class="font-bold">
                Le film propose-t-il une image étendue&nbsp;?
              </h3>
              <p
                class="col-start-2 mt-1 text-sm leading-relaxed text-muted md:col-start-3 md:row-start-1 md:mt-0"
              >
                Certaines versions IMAX ouvrent le cadre sur tout le film ou
                quelques scènes. D’autres conservent leur format classique.
              </p>
            </li>
            <li
              class="grid grid-cols-[2rem_minmax(0,1fr)] items-start gap-x-3 py-4 md:grid-cols-[2rem_minmax(0,0.8fr)_minmax(0,1.2fr)] md:gap-x-5"
            >
              <span
                class="font-mono text-sm font-bold text-accent"
                aria-hidden="true"
                >02</span
              >
              <h3 class="font-bold">Quelle projection est annoncée&nbsp;?</h3>
              <p
                class="col-start-2 mt-1 text-sm leading-relaxed text-muted md:col-start-3 md:row-start-1 md:mt-0"
              >
                IMAX Laser ou IMAX 70 mm, puis 2D ou 3D et VF ou VOST. Pour le
                1,43:1, vérifiez que la séance et la salle sont compatibles.
              </p>
            </li>
            <li
              class="grid grid-cols-[2rem_minmax(0,1fr)] items-start gap-x-3 py-4 md:grid-cols-[2rem_minmax(0,0.8fr)_minmax(0,1.2fr)] md:gap-x-5"
            >
              <span
                class="font-mono text-sm font-bold text-accent"
                aria-hidden="true"
                >03</span
              >
              <h3 class="font-bold">Quel est le prix final&nbsp;?</h3>
              <p
                class="col-start-2 mt-1 text-sm leading-relaxed text-muted md:col-start-3 md:row-start-1 md:mt-0"
              >
                À titre indicatif, prévoyez
                <strong class="text-ink"
                  >environ 13 à 26 € au plein tarif en IMAX 2D</strong
                >, supplément compris. Les réductions, abonnements, lunettes et
                séances spéciales peuvent changer le total.
              </p>
            </li>
            <li
              class="grid grid-cols-[2rem_minmax(0,1fr)] items-start gap-x-3 py-4 last:pb-0 md:grid-cols-[2rem_minmax(0,0.8fr)_minmax(0,1.2fr)] md:gap-x-5"
            >
              <span
                class="font-mono text-sm font-bold text-accent"
                aria-hidden="true"
                >04</span
              >
              <h3 class="font-bold">Quelle place vous convient&nbsp;?</h3>
              <p
                class="col-start-2 mt-1 text-sm leading-relaxed text-muted md:col-start-3 md:row-start-1 md:mt-0"
              >
                Une place centrale, vers le milieu ou légèrement en retrait,
                offre un bon point de départ pour une première séance.
              </p>
            </li>
          </ol>
        </section>

        <section
          id="salles"
          class="guide-section"
          aria-labelledby="salles-title"
        >
          <p class="guide-eyebrow">06 / TROUVER SA SALLE</p>
          <h2 id="salles-title" class="guide-title">
            Où voir de l’IMAX en France&nbsp;?
          </h2>
          <p class="guide-intro">
            Le Pathé Odysseum, à
            <NuxtLink to="/ville/montpellier/cinemas" class="guide-link"
              >Montpellier</NuxtLink
            >, permet de découvrir le grand cadre IMAX et la projection sur
            pellicule. Retrouvez aussi tous les cinémas équipés dans l’annuaire.
          </p>
          <article class="mt-7" aria-labelledby="odysseum-title">
            <img
              src="/images/guides/pathe-odysseum-1600.webp"
              srcset="
                /images/guides/pathe-odysseum-800.webp   800w,
                /images/guides/pathe-odysseum-1600.webp 1600w,
                /images/guides/pathe-odysseum-2103.webp 2103w
              "
              sizes="(max-width: 1023px) 95vw, 80vw"
              width="2103"
              height="512"
              alt="Façade panoramique du Pathé Odysseum à Montpellier, avec son entrée et ses enseignes IMAX."
              class="h-auto w-full"
              loading="lazy"
              decoding="async"
            >
            <div class="mt-5">
              <p
                class="flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-accent"
              >
                <NuxtLink
                  :to="landmarkTarget('odysseum')"
                  class="underline decoration-accent/40 underline-offset-4 hover:text-ink"
                  >Pathé Odysseum</NuxtLink
                >
                <span aria-hidden="true">·</span>
                <NuxtLink
                  to="/ville/montpellier/cinemas"
                  class="underline decoration-accent/40 underline-offset-4 hover:text-ink"
                  >Montpellier</NuxtLink
                >
              </p>
              <h3
                id="odysseum-title"
                class="mt-3 max-w-4xl text-xl font-black leading-tight tracking-tight sm:text-2xl"
              >
                Le seul cinéma de France à proposer l’IMAX 70 mm en 1,43:1.
              </h3>
              <p class="mt-3 max-w-3xl text-sm leading-relaxed text-muted">
                Les films compatibles y profitent de toute la hauteur du grand
                cadre IMAX, sur pellicule ou en projection laser. Pour la
                pellicule, la séance doit porter la mention
                <span class="whitespace-nowrap">« IMAX 70 mm »</span>.
              </p>
              <NuxtLink
                :to="landmarkTarget('odysseum')"
                class="editorial-button mt-4 shadow-none hover:bg-accent"
              >
                Voir les séances au Pathé Odysseum
                <ArrowRight :size="16" class="shrink-0" aria-hidden="true" />
              </NuxtLink>
            </div>
          </article>
          <section class="mt-9" aria-labelledby="autres-imax-title">
            <h3 id="autres-imax-title" class="text-lg font-black sm:text-xl">
              Les autres expériences IMAX en France
            </h3>
            <div class="mt-5 grid items-start gap-6 md:grid-cols-2 md:gap-8">
              <div>
                <h4 class="font-bold">L’IMAX Laser en multiplexe</h4>
                <p class="mt-2 text-sm leading-relaxed text-muted">
                  Le laser 4K s’est développé dans les multiplexes français.
                  Pour les films compatibles, il permet un cadre étendu en
                  1,90:1. La taille de l’écran, la disposition des gradins et
                  les séances proposées en 2D ou en 3D varient d’une salle à
                  l’autre.
                </p>
              </div>
              <div>
                <h4 class="font-bold">L’IMAX sur un dôme</h4>
                <p class="mt-2 text-sm leading-relaxed text-muted">
                  L’IMAX existe aussi sur écran hémisphérique, avec une
                  projection laser 4K disponible en France. L’image se déploie
                  sur la courbure d’un dôme. Les programmes immersifs adaptés à
                  cette forme d’écran offrent une expérience différente du grand
                  écran rectangulaire des multiplexes.
                </p>
              </div>
            </div>
          </section>
          <div
            class="mt-6 flex flex-wrap items-center gap-x-6 gap-y-3 border-t border-ink/15 pt-5"
          >
            <NuxtLink
              :to="imaxDirectoryTarget"
              class="editorial-button-outline hover:bg-subtle"
            >
              Voir tous les cinémas IMAX
              <ArrowRight :size="16" aria-hidden="true" />
            </NuxtLink>
            <NuxtLink
              :to="`${imaxDirectoryTarget}&view=map`"
              class="inline-flex min-h-11 items-center gap-2 text-sm font-bold underline decoration-ink/30 underline-offset-4 hover:text-accent"
            >
              Voir la carte <MapPin :size="16" aria-hidden="true" />
            </NuxtLink>
          </div>
          <p v-if="directory?.unavailable" class="mt-4 text-sm text-muted">
            L’annuaire est momentanément indisponible. Les liens de recherche
            restent accessibles.
          </p>
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
                arrive dans les multiplexes à
                <NuxtLink to="/ville/montpellier/cinemas" class="guide-link"
                  >Montpellier</NuxtLink
                >
                en 1998. Disney Village suit en 2005. Le succès d’<NuxtLink
                  :to="movieTarget('avatar')"
                  class="guide-link"
                  ><em>Avatar</em></NuxtLink
                >
                en 2009 accompagne l’accélération des ouvertures dans les années
                2010, puis la diversification des exploitants. L’arrivée de
                l’IMAX 70 mm à
                <NuxtLink to="/ville/montpellier/cinemas" class="guide-link"
                  >Montpellier</NuxtLink
                >
                en 2026 marque une nouvelle étape.
              </p>
            </div>
          </details>
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
              <div>
                <p>
                  <template
                    v-for="(part, index) in guideTextParts(faq.answer)"
                    :key="index"
                    ><NuxtLink
                      v-if="part.to"
                      :to="part.to"
                      class="guide-link"
                      >{{
                        part.text
                      }}</NuxtLink
                    ><template v-else>{{ part.text }}</template></template
                  >
                </p>
              </div>
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
          <p class="mt-3 text-xs leading-relaxed text-muted">
            Les informations et les équipements des salles ci-dessus ont été
            vérifiés le 7 novembre 2026. Les ratios absents ne sont pas
            confirmés par les sources.
          </p>
          <ol class="mt-5 grid gap-x-8 gap-y-3 md:grid-cols-2">
            <li
              v-for="(source, index) in sources"
              :id="`source-${source.id}`"
              :key="source.id"
              class="min-w-0 scroll-mt-32"
            >
              <a :href="source.url" class="guide-link text-xs leading-relaxed"
                >{{ index + 1 }}. {{ source.name }} ↗</a
              >
              <p class="mt-0.5 text-xs leading-relaxed text-muted">
                {{ source.detail }}
              </p>
            </li>
          </ol>
          <p
            class="mt-5 border-t border-ink/20 pt-3 text-xs leading-relaxed text-muted"
          >
            Visuels : scans de photogrammes de
            <NuxtLink :to="movieTarget('dune')" class="guide-link"
              ><em>Dune : Deuxième Partie</em></NuxtLink
            >,
            <NuxtLink :to="movieTarget('project-hail-mary')" class="guide-link"
              ><em>Projet Dernière Chance</em></NuxtLink
            >
            et
            <NuxtLink :to="movieTarget('odyssee-voilier')" class="guide-link"
              ><em>L’Odyssée</em></NuxtLink
            >, fournis pour ce guide. Affiches :
            <a href="https://www.themoviedb.org/" class="guide-link"
              >The Movie Database (TMDB)</a
            >. Les images restent la propriété de leurs ayants droit. Les images
            du comparateur sont recadrées pour illustrer les proportions. Les
            versions projetées en salle peuvent avoir un cadrage différent. Le
            schéma de projection est simplifié et sans échelle.
          </p>
          <NuxtLink :to="imaxDirectoryTarget" class="editorial-button mt-5"
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
:global(html:has(.imax-guide)) {
  scroll-behavior: smooth;
}
.guide-section {
  scroll-margin-top: calc(var(--guide-header-height, 124px) + 80px);
  @apply border-b-2 border-ink px-4 py-10 last:border-b-0 sm:px-8 sm:py-14 xl:px-12;
}
.imax-introduction {
  @apply py-8 sm:py-10;
}
.guide-desktop-contents {
  top: calc(var(--guide-header-height, 74px) + 20px);
  max-height: calc(100dvh - var(--guide-header-height, 74px) - 40px);
  overflow-y: auto;
  scrollbar-width: thin;
}
.guide-mobile-contents {
  top: var(--guide-header-height, 124px);
}
.guide-mobile-chapters {
  max-height: calc(100dvh - var(--guide-header-height, 124px) - 64px);
}
@media (min-width: 1024px) {
  .guide-section {
    scroll-margin-top: calc(var(--guide-header-height, 74px) + 24px);
  }
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
.guide-details summary {
  @apply min-h-11 cursor-pointer py-3 text-sm font-bold leading-relaxed marker:text-accent hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent;
}
.guide-details > div {
  @apply pb-3 pl-4 text-sm leading-relaxed;
}
</style>
