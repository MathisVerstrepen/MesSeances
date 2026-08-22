<script setup lang="ts">
import { AlertTriangle, ArrowRight, Building2, CalendarRange, Film, RefreshCw, Search } from '@lucide/vue'
import type { CatalogMovie } from '~/types/api'
import { safePosterUrl } from '~/utils/safeImageUrl'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const api = useMesSeancesApi()
const movies = ref<CatalogMovie[]>([])
const pending = ref(true)
const errorMessage = ref('')
const failedPosters = ref<string[]>([])
let requestId = 0

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

function posterUrl(movie: CatalogMovie): string | null {
  return safePosterUrl(movie.poster_url)
}

function posterAvailable(movie: CatalogMovie): boolean {
  return Boolean(posterUrl(movie)) && !failedPosters.value.includes(movie.slug)
}

function markPosterUnavailable(slug: string) {
  if (!failedPosters.value.includes(slug)) failedPosters.value = [...failedPosters.value, slug]
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

useSeoMeta({
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
          <h1 id="home-title" class="hero-title text-[clamp(4.6rem,15vw,13rem)] font-black uppercase leading-[0.72] tracking-[-0.085em] text-ink">
            Mes<span class="text-primary">.</span><br />Seances
          </h1>
        </div>

        <div class="max-w-md lg:justify-self-end lg:pb-2">
          <p class="text-balance text-2xl font-black leading-[0.95] tracking-[-0.04em] text-ink sm:text-3xl">
            Le bon film.<br />Le bon cinéma.<br /><span class="inline-block bg-highlight px-1">Au bon moment.</span>
          </p>
          <div class="mt-7 flex flex-col gap-3 sm:flex-row lg:flex-col xl:flex-row">
            <NuxtLink to="/recherche" class="brutal-button bg-ink text-white hover:bg-primary">
              <Search :size="18" aria-hidden="true" /> Trouver une séance
            </NuxtLink>
            <NuxtLink to="/planning" class="brutal-button border-2 border-ink bg-surface text-ink hover:bg-[#ffcf3f]">
              Planning <ArrowRight :size="18" aria-hidden="true" />
            </NuxtLink>
          </div>
        </div>
      </div>
    </section>

    <section class="poster-canvas relative border-b-2 border-ink" aria-labelledby="now-showing-title" :aria-busy="pending">
      <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 lg:px-10 lg:py-10">
        <div class="relative z-10 flex items-center justify-between gap-4">
          <h2 id="now-showing-title" class="font-mono text-xs font-bold uppercase tracking-[0.18em] text-ink">À l’affiche maintenant</h2>
          <NuxtLink to="/films" class="editorial-link shrink-0 text-xs font-bold uppercase tracking-[0.12em]">
            Tous les films <ArrowRight :size="15" aria-hidden="true" />
          </NuxtLink>
        </div>

        <ul v-if="pending" class="poster-collage" aria-hidden="true">
          <li v-for="index in 6" :key="index" :class="`poster-card poster-card--${index}`">
            <div class="poster-skeleton">
              <div class="poster-frame poster-frame--skeleton" />
              <div class="poster-label poster-label--skeleton" />
            </div>
          </li>
        </ul>

        <div v-else-if="errorMessage" class="canvas-state" role="alert">
          <AlertTriangle :size="32" class="text-primary" aria-hidden="true" />
          <p class="max-w-lg">{{ errorMessage }}</p>
          <button type="button" class="brutal-button bg-ink text-white hover:bg-primary" @click="loadMovies">
            <RefreshCw :size="17" aria-hidden="true" /> Réessayer
          </button>
        </div>

        <div v-else-if="movies.length === 0" class="canvas-state">
          <Film :size="34" aria-hidden="true" />
          <p>Aucun film à l’affiche actuellement.</p>
        </div>

        <ul v-else class="poster-collage" aria-label="Films à l’affiche">
          <li v-for="(movie, index) in movies" :key="movie.slug" :class="`poster-card poster-card--${index + 1}`">
            <NuxtLink :to="`/film/${movie.slug}`" class="poster-link group" :aria-label="`${movie.title}, ${movie.runtime_minutes} minutes`">
              <div class="poster-frame">
                <img
                  v-if="posterAvailable(movie)"
                  :src="posterUrl(movie)!"
                  :alt="`Affiche de ${movie.title}`"
                  class="h-full w-full object-cover grayscale-[15%] transition duration-300 group-hover:grayscale-0"
                  :loading="index < 3 ? 'eager' : 'lazy'"
                  @error="markPosterUnavailable(movie.slug)"
                />
                <div v-else class="flex h-full flex-col items-center justify-center gap-2 bg-[#e8e6de] px-3 text-center text-muted">
                  <Film :size="30" aria-hidden="true" />
                  <span class="text-xs font-bold">Affiche indisponible</span>
                </div>
              </div>
              <span class="poster-label">
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
.hero-title {
  text-wrap: balance;
}

.brutal-button {
  display: inline-flex;
  min-height: 3rem;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
  border-radius: 0.35rem;
  padding: 0.75rem 1rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  transition: background-color 160ms ease, color 160ms ease, transform 160ms ease;
}

.brutal-button:hover {
  transform: translateY(-2px);
}

.poster-canvas {
  min-height: 43rem;
  background-color: #f8f7f2;
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.09) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.09) 1px, transparent 1px);
  background-size: 24px 24px;
}

.editorial-link {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  border-bottom: 2px solid currentColor;
  padding-bottom: 0.2rem;
}

.canvas-state {
  position: relative;
  z-index: 1;
  margin: 6rem auto;
  display: flex;
  min-height: 17rem;
  max-width: 36rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  border: 2px solid #27272a;
  border-radius: 0.4rem;
  background: #fff;
  padding: 2rem;
  text-align: center;
  font-weight: 700;
  box-shadow: 8px 8px 0 #27272a;
}

.poster-collage {
  position: relative;
  min-height: 35rem;
}

.poster-card {
  position: absolute;
}

.poster-card--1 { left: 1%; top: 11%; width: 10.5rem; transform: rotate(-3deg); }
.poster-card--2 { left: 23%; top: 30%; width: 8.5rem; transform: rotate(2deg); }
.poster-card--3 { left: 42%; top: 5%; width: 13rem; transform: rotate(-1deg); }
.poster-card--4 { right: 28%; top: 44%; width: 8rem; transform: rotate(3deg); }
.poster-card--5 { right: 11%; top: 8%; width: 10rem; transform: rotate(2deg); }
.poster-card--6 { right: 0; top: 52%; width: 7rem; transform: rotate(-3deg); }

.poster-link {
  display: block;
  border-radius: 0.4rem;
  color: #27272a;
  transition: transform 180ms ease;
}

.poster-skeleton {
  display: block;
  border-radius: 0.4rem;
}

.poster-link:hover {
  transform: translateY(-4px) rotate(1deg);
}

.poster-frame {
  aspect-ratio: 2 / 3;
  overflow: hidden;
  border: 2px solid #27272a;
  border-radius: 0.4rem;
  background: #e8e6de;
  box-shadow: 7px 7px 0 rgba(39, 39, 42, 0.95);
}

.poster-frame--skeleton,
.poster-label--skeleton {
  animation: skeleton-pulse 1.8s ease-in-out infinite;
  background: #d8d5cc;
}

.poster-label {
  margin: 0.65rem 0 0 0.35rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  border: 2px solid #27272a;
  border-radius: 999px;
  background: var(--color-highlight);
  padding: 0.35rem 0.55rem;
  font-size: 0.68rem;
  font-weight: 900;
  line-height: 1;
}

.poster-label--skeleton {
  height: 1.75rem;
  border-radius: 999px;
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

@media (max-width: 767px) {
  .poster-canvas {
    min-height: auto;
  }

  .poster-collage {
    display: grid;
    min-height: 0;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 2rem 1.25rem;
    padding: 3rem 0 1.5rem;
  }

  .poster-card,
  .poster-card--1,
  .poster-card--2,
  .poster-card--3,
  .poster-card--4,
  .poster-card--5,
  .poster-card--6 {
    position: static;
    width: auto;
    transform: none;
  }

  .poster-card:nth-child(even) {
    margin-top: 2.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .poster-frame--skeleton,
  .poster-label--skeleton {
    animation: none;
  }

  .brutal-button:hover,
  .poster-link:hover {
    transform: none;
  }
}
</style>
