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
import imaxLogo from '~/assets/imgs/imax_logo_large.webp?no-inline'
import {
  guideCinemaTarget,
  imaxDirectoryTarget,
  imaxLandmarks,
  imaxFeaturedCinemas,
  imaxVenueSources,
} from '~/utils/imaxGuide'
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
const featuredCinemas = computed(() =>
  imaxFeaturedCinemas.map((cinema) => ({
    ...cinema,
    name: imaxLandmarks[cinema.key].name,
    to: guideCinemaTarget(theaters.value, imaxLandmarks[cinema.key]),
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
  ...imaxVenueSources,
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
          class="px-4 py-6 sm:px-6 lg:sticky lg:top-28 lg:px-6 lg:py-10"
        >
          <p class="guide-eyebrow">AU PROGRAMME</p>
          <ol
            class="mt-4 grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-4 lg:grid-cols-1"
          >
            <li v-for="(chapter, index) in chapters" :key="chapter.id">
              <a
                :href="`#${chapter.id}`"
                :aria-current="activeChapter === chapter.id ? 'location' : undefined"
                :class="activeChapter === chapter.id ? 'bg-accent-soft text-accent' : undefined"
                class="flex min-h-11 items-center gap-3 py-2 text-xs font-bold hover:text-primary"
                ><span class="font-mono text-[10px] text-accent"
                  >0{{ index + 1 }}</span
                >{{ chapter.label }}</a
              >
            </li>
          </ol>
          <a
            href="#sources"
            :aria-current="activeChapter === 'sources' ? 'location' : undefined"
            :class="activeChapter === 'sources' ? 'text-accent' : undefined"
            class="mt-5 inline-block font-mono text-[11px] underline underline-offset-4 hover:text-primary"
            >Sources &amp; méthode ↗</a
          >
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
          <div class="mt-7"><GuidesImaxFrameComparison /></div>
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
            Le laser fournit la lumière à un projecteur numérique. Le 70 mm
            désigne la pellicule utilisée pour la projection. Voici ce que
            chaque système peut afficher.
          </p>
          <div class="mt-7"><GuidesImaxProjectionExplorer /></div>
          <div class="mt-8">
            <h3 class="font-black">Le 15/70, sans jargon</h3>
            <p class="mt-3 text-sm leading-relaxed">
              Le « 70 » indique la largeur de la pellicule en millimètres. Le «
              15 » correspond au nombre de perforations par image. Le film
              défile horizontalement dans le projecteur. Sur cette bande de
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
                différente à chaque œil. Les lunettes séparent les deux images.
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
          id="choisir"
          class="guide-section"
          aria-labelledby="choisir-title"
        >
          <p class="guide-eyebrow">05 / AVANT DE RÉSERVER</p>
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
                Le supplément dépend du cinéma, de la séance et de votre tarif
                ou abonnement. Avant de payer, vérifiez le montant final avec
                les éventuelles lunettes et options.
              </p>
            </li>
            <li class="border-2 border-ink bg-surface p-5">
              <span class="font-mono text-sm font-bold text-accent"
                >04 — LA PLACE</span
              >
              <h3 class="mt-3 font-black">Immersion ou recul ?</h3>
              <p class="mt-2 text-sm leading-relaxed">
                Pour une première séance, choisissez une place au centre, vers
                le milieu de la salle ou légèrement derrière. Plus vous êtes
                près de l’écran, plus l’image remplit votre champ de vision. En
                reculant, vous voyez plus facilement l’ensemble de l’image.
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
          id="salles"
          class="guide-section"
          aria-labelledby="salles-title"
        >
          <p class="guide-eyebrow">06 / TROUVER SA SALLE</p>
          <h2 id="salles-title" class="guide-title">
            Où voir de l’IMAX en France ?
          </h2>
          <p class="guide-intro">
            Voici quelques salles pour découvrir les différentes expériences
            IMAX en France. Retrouvez l’ensemble des cinémas dans l’annuaire.
          </p>
          <div class="mt-7 border-2 border-ink bg-surface">
            <div class="border-b-2 border-ink bg-highlight p-5 sm:p-7">
              <p class="guide-eyebrow">PATHÉ ODYSSEUM · MONTPELLIER</p>
              <h3
                class="mt-3 text-2xl font-black leading-tight tracking-tight sm:text-3xl"
              >
                Le seul cinéma de France à proposer l’IMAX 70 mm en 1,43:1.
              </h3>
            </div>
            <div class="p-5 sm:p-7">
              <dl
                class="grid gap-5 border-b border-line pb-5 sm:grid-cols-2"
                aria-label="Équipement du Pathé Odysseum"
              >
                <div>
                  <dt class="font-mono text-[10px] uppercase text-muted">
                    Format d’image
                  </dt>
                  <dd class="mt-2 text-4xl font-black tracking-tight">
                    1,43:1
                  </dd>
                </div>
                <div>
                  <dt class="font-mono text-[10px] uppercase text-muted">
                    Projection sur pellicule
                  </dt>
                  <dd class="mt-2 text-4xl font-black tracking-tight">
                    IMAX 70 mm
                  </dd>
                </div>
              </dl>
              <p class="mt-5 leading-relaxed">
                Au
                <NuxtLink :to="landmarkTarget('odysseum')" class="guide-link"
                  >Pathé Odysseum</NuxtLink
                >, les films compatibles profitent de toute la hauteur du cadre
                IMAX 1,43. La salle dispose aussi d’une projection IMAX Laser.
              </p>
              <p class="mt-3 text-sm leading-relaxed">
                Pour découvrir la pellicule, choisissez une séance portant la
                mention « IMAX 70 mm ».
              </p>
              <NuxtLink
                :to="landmarkTarget('odysseum')"
                class="editorial-button mt-5"
              >
                Voir les séances au Pathé Odysseum
                <ArrowRight :size="16" aria-hidden="true" />
              </NuxtLink>
            </div>
          </div>
          <ul
            class="mt-6 grid gap-4 sm:grid-cols-2"
            aria-label="Sélection de cinémas IMAX"
          >
            <li
              v-for="cinema in featuredCinemas"
              :key="cinema.key"
              class="flex flex-col border-2 border-ink bg-surface p-5"
            >
              <p
                class="font-mono text-[10px] uppercase tracking-wide text-muted"
              >
                {{ cinema.city }}
              </p>
              <h3 class="mt-2 font-black">
                <NuxtLink :to="cinema.to" class="guide-link">{{
                  cinema.name
                }}</NuxtLink>
              </h3>
              <dl class="mt-4 grid gap-3 text-sm">
                <div>
                  <dt class="font-mono text-[10px] uppercase text-muted">
                    Projection
                  </dt>
                  <dd class="mt-1 font-bold">{{ cinema.projection }}</dd>
                </div>
                <div v-if="cinema.ratio">
                  <dt class="font-mono text-[10px] uppercase text-muted">
                    {{ cinema.key === 'geode' ? 'Écran' : 'Cadre maximal' }}
                  </dt>
                  <dd class="mt-1 font-bold">{{ cinema.ratio }}</dd>
                </div>
              </dl>
            </li>
          </ul>
          <div class="mt-6 flex flex-wrap gap-3">
            <NuxtLink :to="imaxDirectoryTarget" class="editorial-button"
              >Voir tous les cinémas IMAX
              <ArrowRight :size="16" aria-hidden="true" /></NuxtLink
            >
            <NuxtLink
              :to="`${imaxDirectoryTarget}&view=map`"
              class="editorial-button-outline"
              >Voir la carte <MapPin :size="16" aria-hidden="true" /></NuxtLink
            >
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
            Visuels : scans de photogrammes de <em>Dune : Deuxième Partie</em>,
            <em>Project Hail Mary</em>
            et <em>L’Odyssée</em>, fournis pour ce guide. Affiches :
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
