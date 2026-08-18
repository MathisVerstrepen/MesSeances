<script setup lang="ts">
import { AlertTriangle, ArrowRight, Building2, CalendarRange, Film, LoaderCircle, RefreshCw, Search } from '@lucide/vue'
import type { CatalogMovie } from '~/types/api'
import { safePosterUrl } from '~/utils/safeImageUrl'

const api = useMesSeancesApi()
const movies = ref<CatalogMovie[]>([])
const pending = ref(true)
const errorMessage = ref('')
const failedPosters = ref<string[]>([])
let requestId = 0

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

onMounted(loadMovies)

useHead({ title: 'MesSeances — Vos séances, au bon moment' })
</script>

<template>
  <main>
    <section class="mx-auto max-w-[1280px] px-4 pb-12 pt-12 sm:px-6 sm:pb-16 sm:pt-16 lg:px-10 lg:pb-20 lg:pt-24">
      <div class="max-w-3xl">
        <h1 class="text-4xl font-semibold tracking-tight text-ink sm:text-5xl lg:text-6xl">Vos séances, au bon moment.</h1>
        <p class="mt-6 max-w-2xl text-lg leading-8 text-muted">
          Consultez les séances de vos cinémas, visualisez votre journée et trouvez les films qui tiennent dans votre créneau.
        </p>
        <div class="mt-8 flex flex-col gap-3 sm:flex-row">
          <NuxtLink to="/recherche" class="button-primary h-11 w-full sm:w-auto">
            <Search :size="18" aria-hidden="true" /> Trouver une séance
          </NuxtLink>
          <NuxtLink to="/planning" class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md border border-line bg-surface px-5 text-sm font-semibold text-ink transition hover:border-line-hover hover:bg-subtle sm:w-auto">
            <CalendarRange :size="18" aria-hidden="true" /> Voir le planning
          </NuxtLink>
        </div>
      </div>
    </section>

    <section class="border-y border-line bg-surface">
      <div class="mx-auto max-w-[1280px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10">
        <h2 class="sr-only">Accès rapides</h2>
        <ul class="grid gap-6 md:grid-cols-3 md:gap-0 md:divide-x md:divide-line">
          <li>
            <NuxtLink to="/recherche" class="group block rounded-md md:pr-8">
              <Search :size="22" class="text-accent" aria-hidden="true" />
              <h3 class="mt-4 flex items-center gap-2 text-base font-semibold text-ink group-hover:text-accent">
                Trouver une séance <ArrowRight :size="16" aria-hidden="true" />
              </h3>
              <p class="mt-2 text-sm leading-6 text-muted">Indiquez votre disponibilité et affichez les séances qui tiennent dans ce créneau.</p>
            </NuxtLink>
          </li>
          <li>
            <NuxtLink to="/planning" class="group block rounded-md md:px-8">
              <CalendarRange :size="22" class="text-accent" aria-hidden="true" />
              <h3 class="mt-4 flex items-center gap-2 text-base font-semibold text-ink group-hover:text-accent">
                Explorer le planning <ArrowRight :size="16" aria-hidden="true" />
              </h3>
              <p class="mt-2 text-sm leading-6 text-muted">Comparez les horaires par cinéma ou par film sur la frise.</p>
            </NuxtLink>
          </li>
          <li>
            <NuxtLink to="/cinemas" class="group block rounded-md md:pl-8">
              <Building2 :size="22" class="text-accent" aria-hidden="true" />
              <h3 class="mt-4 flex items-center gap-2 text-base font-semibold text-ink group-hover:text-accent">
                Choisir mes cinémas <ArrowRight :size="16" aria-hidden="true" />
              </h3>
              <p class="mt-2 text-sm leading-6 text-muted">Sélectionnez les cinémas utilisés dans le planning et la recherche.</p>
            </NuxtLink>
          </li>
        </ul>
      </div>
    </section>

    <section class="mx-auto max-w-[1280px] px-4 py-12 sm:px-6 sm:py-16 lg:px-10 lg:py-20">
      <div class="flex items-center justify-between gap-4">
        <h2 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Films à l’affiche</h2>
        <NuxtLink to="/films" class="shrink-0 text-sm font-semibold text-accent transition hover:text-accent-hover">
          Voir tous les films
        </NuxtLink>
      </div>

      <div v-if="pending" class="state-panel mt-6" role="status" aria-live="polite">
        <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
        <p>Chargement des films…</p>
      </div>

      <div v-else-if="errorMessage" class="state-panel mt-6" role="alert">
        <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
        <p class="max-w-lg">{{ errorMessage }}</p>
        <button type="button" class="button-primary" @click="loadMovies">
          <RefreshCw :size="17" aria-hidden="true" /> Réessayer
        </button>
      </div>

      <div v-else-if="movies.length === 0" class="state-panel mt-6">
        <Film :size="30" class="text-muted" aria-hidden="true" />
        <p>Aucun film à l’affiche actuellement.</p>
      </div>

      <ul v-else class="mt-6 grid grid-cols-2 gap-x-4 gap-y-7 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-6" aria-label="Films à l’affiche">
        <li v-for="movie in movies" :key="movie.slug" class="min-w-0">
          <NuxtLink :to="`/film/${movie.slug}`" class="group block rounded-md focus-visible:ring-offset-4">
            <div class="aspect-[2/3] overflow-hidden rounded-md border border-line bg-subtle shadow-sm">
              <img
                v-if="posterAvailable(movie)"
                :src="posterUrl(movie)!"
                :alt="`Affiche de ${movie.title}`"
                class="h-full w-full object-cover transition duration-200 group-hover:scale-[1.02]"
                loading="lazy"
                @error="markPosterUnavailable(movie.slug)"
              />
              <div v-else class="flex h-full flex-col items-center justify-center gap-2 px-3 text-center text-muted">
                <Film :size="32" aria-hidden="true" />
                <span class="text-xs font-medium">Affiche indisponible</span>
              </div>
            </div>
            <h3 class="mt-3 line-clamp-2 text-sm font-semibold leading-snug text-ink group-hover:text-accent">{{ movie.title }}</h3>
            <p class="mt-1 text-xs text-muted">{{ movie.runtime_minutes }} min</p>
          </NuxtLink>
        </li>
      </ul>
    </section>
  </main>
</template>
