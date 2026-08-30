<script setup lang="ts">
import { AlertTriangle, ArrowRight, Building2, CalendarRange, Film, RefreshCw, Search } from '@lucide/vue'
import type { CatalogMovie } from '~/types/api'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const api = useMesSeancesApi()
const route = useRoute()
const movies = ref<CatalogMovie[]>([])
const pending = ref(true)
const errorMessage = ref('')
let requestId = 0

const homepagePosterSizes = ['10.5rem', '8.5rem', '13rem', '8rem', '10rem', '7rem']
  .map((desktopWidth) => `(max-width: 639px) calc((100vw - 3.25rem) / 2), (max-width: 767px) calc((100vw - 4.25rem) / 2), ${desktopWidth}`)

const homepagePosterClasses = [
  'absolute left-[1%] top-[11%] w-[10.5rem] -rotate-3 max-md:static max-md:w-auto max-md:rotate-0',
  'absolute left-[23%] top-[30%] w-[8.5rem] rotate-2 max-md:static max-md:mt-10 max-md:w-auto max-md:rotate-0',
  'absolute left-[42%] top-[5%] w-52 -rotate-1 max-md:static max-md:w-auto max-md:rotate-0',
  'absolute right-[28%] top-[44%] w-32 rotate-3 max-md:static max-md:mt-10 max-md:w-auto max-md:rotate-0',
  'absolute right-[11%] top-[8%] w-40 rotate-2 max-md:static max-md:w-auto max-md:rotate-0',
  'absolute right-0 top-[52%] w-28 -rotate-3 max-md:static max-md:mt-10 max-md:w-auto max-md:rotate-0'
] as const

const shortcuts = [
  {
    to: '/recherche',
    eyebrow: '01 / Recherche',
    title: 'Trouver une séance',
    description: 'Partez de votre disponibilité, pas d’une liste interminable.',
    icon: Search
  },
  {
    to: '/planning',
    eyebrow: '02 / Planning',
    title: 'Composer ma sortie',
    description: 'Comparez horaires, films et cinémas sur une seule frise.',
    icon: CalendarRange
  },
  {
    to: '/cinemas',
    eyebrow: '03 / Cinémas',
    title: 'Choisir mes salles',
    description: 'Gardez vos cinémas favoris au centre de chaque recherche.',
    icon: Building2
  }
]

async function loadMovies() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''

  try {
    const response = await api.movies({
      currently_screened: true,
      sort: 'showtimes_desc',
      page: 1,
      page_size: 6
    })
    if (currentRequest === requestId) movies.value = response.items.slice(0, 6)
  } catch (error) {
    if (currentRequest === requestId) {
      movies.value = []
      errorMessage.value = getFrenchApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

const initialResult = await useAsyncData('home-current-movies', async () => {
  try {
    const response = await api.movies({
      currently_screened: true,
      sort: 'showtimes_desc',
      page: 1,
      page_size: 6
    })
    return { movies: response.items.slice(0, 6), errorMessage: '' }
  } catch (error) {
    const emptyMovies: CatalogMovie[] = []
    return { movies: emptyMovies, errorMessage: getFrenchApiError(error) }
  }
})

movies.value = initialResult.data.value?.movies ?? []
errorMessage.value = initialResult.data.value?.errorMessage ?? ''
pending.value = false
if (import.meta.server && errorMessage.value) {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/')
const socialImageUrl = absoluteSiteUrl(config.public.siteUrl, '/pwa-512x512.png')
const pageTitle = 'MesSeances - Vos séances, au bon moment'
const pageDescription = 'Découvrez les films actuellement à l’affiche et trouvez rapidement la séance de cinéma qui correspond à votre emploi du temps.'
const robots = computed(() => Object.keys(route.query).length === 0 && !errorMessage.value ? 'index,follow' : 'noindex,follow')

useSeoMeta({
  robots,
  title: pageTitle,
  description: pageDescription,
  ogTitle: pageTitle,
  ogDescription: pageDescription,
  ogUrl: canonicalUrl,
  ogType: 'website',
  ogImage: socialImageUrl,
  ogSiteName: 'MesSeances',
  ogLocale: 'fr_FR',
  twitterCard: 'summary_large_image',
  twitterTitle: pageTitle,
  twitterDescription: pageDescription,
  twitterImage: socialImageUrl
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="home-page overflow-hidden bg-[#f8f7f2]">
    <section class="hero-intro border-b-2 border-ink bg-surface" aria-labelledby="home-title">
      <div class="mx-auto grid max-w-[1440px] gap-10 px-4 pb-10 pt-12 sm:px-6 sm:pb-14 sm:pt-16 lg:grid-cols-[minmax(0,1.45fr)_minmax(300px,.55fr)] lg:items-end lg:px-10 lg:pb-16 lg:pt-20">
        <div class="min-w-0">
          <h1 id="home-title" class="text-balance text-[clamp(4.6rem,15vw,13rem)] font-black uppercase leading-[0.72] tracking-[-0.085em] text-ink">
            Mes<span class="text-primary">.</span><br />Seances
          </h1>
        </div>

        <div class="max-w-md lg:justify-self-end lg:pb-2">
          <p class="text-balance text-2xl font-black leading-[0.95] tracking-[-0.04em] text-ink sm:text-3xl">
            Le bon film.<br />Le bon cinéma.<br /><span class="inline-block bg-highlight px-1">Au bon moment.</span>
          </p>
          <div class="mt-7 flex flex-col gap-3 sm:flex-row lg:flex-col xl:flex-row">
            <NuxtLink to="/recherche" class="inline-flex min-h-12 items-center justify-center gap-[0.6rem] rounded-[0.35rem] bg-ink px-4 py-3 font-mono text-[0.72rem] font-extrabold uppercase tracking-[0.08em] text-white [transition:background-color_160ms_ease,color_160ms_ease,transform_160ms_ease] hover:-translate-y-0.5 hover:bg-primary motion-reduce:hover:translate-y-0">
              <Search :size="18" aria-hidden="true" /> Trouver une séance
            </NuxtLink>
            <NuxtLink to="/planning" class="inline-flex min-h-12 items-center justify-center gap-[0.6rem] rounded-[0.35rem] border-2 border-ink bg-surface px-4 py-3 font-mono text-[0.72rem] font-extrabold uppercase tracking-[0.08em] text-ink [transition:background-color_160ms_ease,color_160ms_ease,transform_160ms_ease] hover:-translate-y-0.5 hover:bg-[#ffcf3f] motion-reduce:hover:translate-y-0">
              Planning <ArrowRight :size="18" aria-hidden="true" />
            </NuxtLink>
          </div>
        </div>
      </div>
    </section>

    <section class="relative min-h-[43rem] border-b-2 border-ink bg-[#f8f7f2] bg-[linear-gradient(rgba(39,39,42,0.09)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.09)_1px,transparent_1px)] bg-[size:24px_24px] max-md:min-h-0" aria-labelledby="now-showing-title" :aria-busy="pending">
      <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 lg:px-10 lg:py-10">
        <div class="relative z-10 flex items-center justify-between gap-4">
          <h2 id="now-showing-title" class="font-mono text-xs font-bold uppercase tracking-[0.18em] text-ink">À l’affiche maintenant</h2>
          <NuxtLink to="/films" class="inline-flex shrink-0 items-center gap-[0.4rem] border-b-2 border-current pb-[0.2rem] text-xs font-bold uppercase tracking-[0.12em]">
            Tous les films <ArrowRight :size="15" aria-hidden="true" />
          </NuxtLink>
        </div>

        <ul v-if="pending" class="relative min-h-[35rem] max-md:grid max-md:min-h-0 max-md:grid-cols-2 max-md:gap-x-5 max-md:gap-y-8 max-md:px-0 max-md:pt-12 max-md:pb-6" aria-hidden="true">
          <li v-for="index in 6" :key="index" :class="homepagePosterClasses[index - 1]">
            <div class="block rounded-[0.4rem]">
              <div class="poster-frame--skeleton aspect-[2/3] overflow-hidden rounded-[0.4rem] border-2 border-ink bg-[#d8d5cc] shadow-[7px_7px_0_rgba(39,39,42,0.95)]" />
              <div class="poster-label--skeleton mt-[0.65rem] ml-[0.35rem] h-7 rounded-full bg-[#d8d5cc]" />
            </div>
          </li>
        </ul>

        <EditorialStatePanel v-else-if="errorMessage" semantic="alert" size="compact" shadow="large" class="canvas-state relative z-[1] my-24 mx-auto max-w-xl rounded-[0.4rem] font-bold">
          <template #icon><AlertTriangle :size="32" class="text-primary" aria-hidden="true" /></template>
          <p class="max-w-lg">{{ errorMessage }}</p>
          <template #actions><button type="button" class="inline-flex min-h-12 items-center justify-center gap-[0.6rem] rounded-[0.35rem] bg-ink px-4 py-3 font-mono text-[0.72rem] font-extrabold uppercase tracking-[0.08em] text-white [transition:background-color_160ms_ease,color_160ms_ease,transform_160ms_ease] hover:-translate-y-0.5 hover:bg-primary motion-reduce:hover:translate-y-0" @click="loadMovies"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="movies.length === 0" size="compact" shadow="large" class="canvas-state relative z-[1] my-24 mx-auto max-w-xl rounded-[0.4rem] font-bold">
          <template #icon><Film :size="34" aria-hidden="true" /></template>
          <p>Aucun film à l’affiche actuellement.</p>
        </EditorialStatePanel>

        <ul v-else class="relative min-h-[35rem] max-md:grid max-md:min-h-0 max-md:grid-cols-2 max-md:gap-x-5 max-md:gap-y-8 max-md:px-0 max-md:pt-12 max-md:pb-6" aria-label="Films à l’affiche">
          <li v-for="(movie, index) in movies" :key="movie.slug" :class="homepagePosterClasses[index]">
            <NuxtLink :to="`/film/${movie.slug}`" class="group block rounded-[0.4rem] text-ink [transition:transform_180ms_ease] hover:-translate-y-1 hover:rotate-1 motion-reduce:hover:translate-y-0 motion-reduce:hover:rotate-0" :aria-label="`${movie.title}, ${movie.runtime_minutes} minutes`">
              <div class="aspect-[2/3] overflow-hidden rounded-[0.4rem] border-2 border-ink bg-[#e8e6de] shadow-[7px_7px_0_rgba(39,39,42,0.95)]">
                <PosterImage
                  :src="movie.poster_url"
                  :alt="`Affiche de ${movie.title}`"
                  :sizes="homepagePosterSizes[index]!"
                  class="h-full w-full"
                  image-class="h-full w-full object-cover grayscale-[15%] transition duration-300 group-hover:grayscale-0"
                  fallback-class="gap-2 bg-[#e8e6de] px-3 text-center text-xs font-bold text-muted"
                />
              </div>
              <span class="mt-[0.65rem] ml-[0.35rem] flex items-center justify-between gap-2 rounded-full border-2 border-ink bg-highlight px-[0.55rem] py-[0.35rem] text-[0.68rem] leading-none font-black">
                <span class="line-clamp-1">{{ movie.title }}</span>
                <span aria-hidden="true">↗</span>
              </span>
            </NuxtLink>
          </li>
        </ul>
      </div>
    </section>

    <section class="border-b-2 border-ink bg-[#ffcf3f]" aria-labelledby="shortcuts-title">
      <h2 id="shortcuts-title" class="sr-only">Accès rapides</h2>
      <ul class="mx-auto grid max-w-[1440px] md:grid-cols-3">
        <li v-for="shortcut in shortcuts" :key="shortcut.to" class="border-ink md:border-r-2 md:last:border-r-0">
          <NuxtLink :to="shortcut.to" class="shortcut group flex h-full min-h-64 flex-col justify-between border-b-2 border-ink p-6 transition-colors hover:bg-highlight md:border-b-0 lg:p-10">
            <div class="flex items-start justify-between gap-4">
              <span class="font-mono text-[11px] font-bold uppercase tracking-[0.16em]">{{ shortcut.eyebrow }}</span>
              <component :is="shortcut.icon" :size="26" stroke-width="2.5" aria-hidden="true" />
            </div>
            <div class="mt-12">
              <h3 class="text-3xl font-black leading-none tracking-[-0.04em] sm:text-4xl">{{ shortcut.title }}</h3>
              <p class="mt-4 max-w-sm text-sm font-medium leading-6">{{ shortcut.description }}</p>
              <span class="mt-6 inline-flex items-center gap-2 text-xs font-black uppercase tracking-[0.12em]">
                Explorer <ArrowRight :size="16" class="transition-transform group-hover:translate-x-1" aria-hidden="true" />
              </span>
            </div>
          </NuxtLink>
        </li>
      </ul>
    </section>
  </main>
</template>

<style scoped>
.poster-frame--skeleton,
.poster-label--skeleton {
  animation: skeleton-pulse 1.8s ease-in-out infinite;
}

@keyframes skeleton-pulse {
  0%,
  100% {
    opacity: 0.55;
  }

  50% {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .poster-frame--skeleton,
  .poster-label--skeleton {
    animation: none;
  }
}
</style>
