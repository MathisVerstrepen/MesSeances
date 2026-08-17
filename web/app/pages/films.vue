<script setup lang="ts">
import { AlertTriangle, Film, LoaderCircle, RefreshCw, Search } from '@lucide/vue'
import type { CatalogMovie, MoviesResponse } from '~/types/api'
import { safePosterUrl } from '~/utils/safeImageUrl'

const PAGE_SIZE = 24

const api = useMovieFlowApi()
const searchInput = ref('')
const appliedSearch = ref('')
const page = ref(1)
const catalog = ref<MoviesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const failedPosters = ref<string[]>([])
let requestId = 0

const totalPages = computed(() => Math.max(1, Math.ceil((catalog.value?.total ?? 0) / PAGE_SIZE)))

async function loadMovies() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''

  try {
    const response = await api.movies({
      currently_screened: true,
      search: appliedSearch.value || undefined,
      page: page.value,
      page_size: PAGE_SIZE
    })
    if (currentRequest === requestId) catalog.value = response
  } catch (error) {
    if (currentRequest === requestId) {
      catalog.value = null
      errorMessage.value = getFrenchApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

function submitSearch() {
  const nextSearch = searchInput.value.trim()
  const searchChanged = nextSearch !== appliedSearch.value
  appliedSearch.value = nextSearch

  if (page.value !== 1) page.value = 1
  else if (searchChanged || errorMessage.value) loadMovies()
}

function changePage(nextPage: number) {
  if (pending.value || nextPage < 1 || nextPage > totalPages.value || nextPage === page.value) return
  page.value = nextPage
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function posterAvailable(movie: CatalogMovie): boolean {
  return Boolean(posterUrl(movie)) && !failedPosters.value.includes(movie.slug)
}

function posterUrl(movie: CatalogMovie): string | null {
  return safePosterUrl(movie.poster_url)
}

function markPosterUnavailable(slug: string) {
  if (!failedPosters.value.includes(slug)) failedPosters.value = [...failedPosters.value, slug]
}

watch(page, loadMovies)
onMounted(loadMovies)

useHead({ title: 'Films à l’affiche — MovieFlow' })
</script>

<template>
  <main class="mx-auto max-w-[1280px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <div class="flex flex-col gap-5 border-b border-line pb-6 sm:flex-row sm:items-end sm:justify-between">
      <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Films à l’affiche</h1>

      <form class="flex w-full gap-2 sm:max-w-md" role="search" @submit.prevent="submitSearch">
        <label class="sr-only" for="film-search">Rechercher un film par titre</label>
        <input
          id="film-search"
          v-model="searchInput"
          type="search"
          class="field min-w-0"
          autocomplete="off"
          placeholder="Rechercher un film"
        />
        <button type="submit" class="button-primary shrink-0" :disabled="pending">
          <Search :size="18" aria-hidden="true" />
          <span class="hidden sm:inline">Rechercher</span>
          <span class="sr-only sm:hidden">Rechercher</span>
        </button>
      </form>
    </div>

    <div v-if="catalog && !pending" class="mb-5 mt-6 flex items-baseline justify-between gap-4">
      <h2 class="text-base font-semibold text-ink">
        {{ appliedSearch ? `Résultats pour « ${appliedSearch} »` : 'Tous les films' }}
      </h2>
      <p class="text-sm text-muted">{{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}</p>
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

    <div v-else-if="!catalog?.items.length" class="state-panel mt-6">
      <Film :size="30" class="text-muted" aria-hidden="true" />
      <p>{{ appliedSearch ? 'Aucun film ne correspond à cette recherche.' : 'Aucun film à l’affiche actuellement.' }}</p>
    </div>

    <template v-else>
      <ul class="grid grid-cols-2 gap-x-4 gap-y-7 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6" aria-label="Films à l’affiche">
        <li v-for="movie in catalog.items" :key="movie.slug" class="min-w-0">
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
            <h2 class="mt-3 line-clamp-2 text-sm font-semibold leading-snug text-ink group-hover:text-accent">{{ movie.title }}</h2>
            <p class="mt-1 text-xs text-muted">{{ movie.runtime_minutes }} min</p>
          </NuxtLink>
        </li>
      </ul>

      <nav v-if="totalPages > 1" class="mt-10 flex items-center justify-center gap-4 border-t border-line pt-6" aria-label="Pagination des films">
        <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink hover:border-stone-400 disabled:cursor-not-allowed disabled:opacity-50" :disabled="page <= 1 || pending" @click="changePage(page - 1)">
          Précédent
        </button>
        <span class="text-sm text-muted" aria-live="polite">Page {{ page }} sur {{ totalPages }}</span>
        <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink hover:border-stone-400 disabled:cursor-not-allowed disabled:opacity-50" :disabled="page >= totalPages || pending" @click="changePage(page + 1)">
          Suivant
        </button>
      </nav>
    </template>
  </main>
</template>
