<script setup lang="ts">
import { AlertTriangle, Film, LoaderCircle, RefreshCw, Search } from '@lucide/vue'
import type { CatalogMovie, MoviesResponse, MovieSort } from '~/types/api'
import { enumQueryValue, mergeOwnedQuery, positiveSafeInteger, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { safePosterUrl } from '~/utils/safeImageUrl'

const PAGE_SIZE = 24
const DEFAULT_SORT: MovieSort = 'showtimes_desc'
const SORT_OPTIONS = [
  { value: 'title_asc', label: 'Titre A–Z' },
  { value: 'title_desc', label: 'Titre Z–A' },
  { value: 'release_date_desc', label: 'Sorties récentes' },
  { value: 'runtime_asc', label: 'Durée croissante' },
  { value: 'runtime_desc', label: 'Durée décroissante' },
  { value: 'showtimes_desc', label: 'Plus de séances' }
] as const satisfies readonly { value: MovieSort, label: string }[]
const SORT_VALUES = SORT_OPTIONS.map((option) => option.value)
const OWNED_QUERY_KEYS = ['q', 'sort', 'page'] as const

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const searchInput = ref('')
const appliedSearch = ref('')
const sort = ref<MovieSort>(DEFAULT_SORT)
const page = ref(1)
const catalog = ref<MoviesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const failedPosters = ref<string[]>([])
let requestId = 0
let isMounted = false
let scrollAfterLoad = false
let lastLoadKey = ''

const totalPages = computed(() => Math.max(1, Math.ceil((catalog.value?.total ?? 0) / PAGE_SIZE)))

async function loadMovies() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''

  try {
    const response = await api.movies({
      currently_screened: true,
      search: appliedSearch.value || undefined,
      sort: sort.value,
      page: page.value,
      page_size: PAGE_SIZE
    })
    if (currentRequest === requestId) {
      const lastPage = Math.max(1, Math.ceil(response.total / PAGE_SIZE))
      if (page.value > lastPage) {
        const query = filmQuery(appliedSearch.value, lastPage, sort.value)
        if (!queriesEqual(route.query, query)) await router.replace({ query })
        return
      }
      catalog.value = response
      if (scrollAfterLoad) {
        scrollAfterLoad = false
        window.scrollTo({ top: 0, behavior: 'smooth' })
      }
    }
  } catch (error) {
    if (currentRequest === requestId) {
      catalog.value = null
      errorMessage.value = getFrenchApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

function filmQuery(search: string, nextPage: number, nextSort: MovieSort) {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    q: search || undefined,
    sort: nextSort === DEFAULT_SORT ? undefined : nextSort,
    page: nextPage === 1 ? undefined : String(nextPage)
  })
}

function hydrateRoute() {
  const rawSearch = singularQueryValue(route.query.q)
  const nextSearch = rawSearch?.trim() ?? ''
  const nextSort = enumQueryValue(singularQueryValue(route.query.sort), SORT_VALUES) ?? DEFAULT_SORT
  const nextPage = positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1
  searchInput.value = nextSearch
  appliedSearch.value = nextSearch
  sort.value = nextSort
  page.value = nextPage
  return filmQuery(nextSearch, nextPage, nextSort)
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  const key = `${appliedSearch.value}|${sort.value}|${page.value}`
  if (key === lastLoadKey) return
  lastLoadKey = key
  await loadMovies()
}

function submitSearch() {
  const nextSearch = searchInput.value.trim()
  const query = filmQuery(nextSearch, 1, sort.value)
  if (queriesEqual(route.query, query)) {
    if (errorMessage.value) loadMovies()
    return
  }
  router.push({ query })
}

function changeSort(event: Event) {
  if (!(event.currentTarget instanceof HTMLSelectElement)) return
  const nextSort = enumQueryValue(event.currentTarget.value, SORT_VALUES)
  if (!nextSort || nextSort === sort.value) return
  router.push({ query: filmQuery(appliedSearch.value, 1, nextSort) })
}

function changePage(nextPage: number) {
  if (pending.value || nextPage < 1 || nextPage > totalPages.value || nextPage === page.value) return
  scrollAfterLoad = true
  router.push({ query: filmQuery(appliedSearch.value, nextPage, sort.value) })
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

watch(() => route.query, () => {
  if (isMounted) applyRoute()
})
onMounted(() => {
  isMounted = true
  applyRoute()
})

useHead({ title: 'Films à l’affiche - MesSeances' })
</script>

<template>
  <main class="catalog-page bg-[#f8f7f2] text-ink">
    <section class="border-b-2 border-ink bg-surface" aria-labelledby="catalog-title">
      <div class="relative mx-auto max-w-[1440px] overflow-hidden px-4 pb-10 pt-12 sm:px-6 sm:pb-14 sm:pt-16 lg:px-10 lg:pb-16 lg:pt-20">
        <p class="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-muted">Catalogue · En salle</p>
        <h1 id="catalog-title" class="catalog-title mt-5 max-w-6xl text-[clamp(4rem,11.7vw,10.5rem)] font-black uppercase leading-[0.75] tracking-[-0.085em]">
          Films<br /><span>à l’affiche</span><span class="text-primary">.</span>
        </h1>
        <span class="title-accent" aria-hidden="true"></span>
      </div>
    </section>

    <section class="catalog-canvas border-b-2 border-ink">
      <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12">
        <div class="search-workspace grid gap-5 border-2 border-ink bg-[#ffcf3f] p-4 shadow-[7px_7px_0_#27272a] sm:p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end lg:gap-8">
          <form role="search" @submit.prevent="submitSearch">
            <label class="control-label" for="film-search">Rechercher un film</label>
            <div class="mt-2 flex min-w-0">
              <input
                id="film-search"
                v-model="searchInput"
                type="search"
                class="catalog-field min-w-0 flex-1 border-r-0"
                autocomplete="off"
                placeholder="Titre du film"
              />
              <button type="submit" class="search-button shrink-0" :disabled="pending">
                <Search :size="19" stroke-width="2.5" aria-hidden="true" />
                <span class="hidden sm:inline">Rechercher</span>
                <span class="sr-only sm:hidden">Rechercher</span>
              </button>
            </div>
          </form>

          <label class="block lg:min-w-56">
            <span class="control-label">Trier par</span>
            <select :value="sort" class="catalog-field mt-2 w-full" :disabled="pending" @change="changeSort">
              <option v-for="option in SORT_OPTIONS" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
        </div>

        <div v-if="catalog && !pending" class="results-bar mt-10 flex flex-col gap-2 border-y-2 border-ink py-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
          <h2 class="text-xl font-black tracking-[-0.035em] sm:text-2xl">
            {{ appliedSearch ? `Résultats pour « ${appliedSearch} »` : 'Tous les films' }}
          </h2>
          <p class="font-mono text-[11px] font-bold uppercase tracking-[0.14em]">{{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}</p>
        </div>

        <div v-if="pending" class="catalog-state" role="status" aria-live="polite">
          <LoaderCircle :size="34" class="animate-spin" aria-hidden="true" />
          <p>Chargement des films…</p>
        </div>

        <div v-else-if="errorMessage" class="catalog-state" role="alert">
          <AlertTriangle :size="34" class="text-primary" aria-hidden="true" />
          <p class="max-w-lg">{{ errorMessage }}</p>
          <button type="button" class="state-button" @click="loadMovies">
            <RefreshCw :size="17" aria-hidden="true" /> Réessayer
          </button>
        </div>

        <div v-else-if="!catalog?.items.length" class="catalog-state">
          <Film :size="36" aria-hidden="true" />
          <p>{{ appliedSearch ? 'Aucun film ne correspond à cette recherche.' : 'Aucun film à l’affiche actuellement.' }}</p>
        </div>

        <template v-else>
          <ul class="catalog-grid mt-8 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6" aria-label="Films à l’affiche">
            <li v-for="movie in catalog.items" :key="movie.slug" class="min-w-0">
              <NuxtLink :to="`/film/${movie.slug}`" class="catalog-card group block focus-visible:ring-offset-4">
                <div class="poster-frame">
                  <img
                    v-if="posterAvailable(movie)"
                    :src="posterUrl(movie)!"
                    :alt="`Affiche de ${movie.title}`"
                    class="h-full w-full object-cover transition duration-200 group-hover:scale-[1.025]"
                    loading="lazy"
                    @error="markPosterUnavailable(movie.slug)"
                  />
                  <div v-else class="flex h-full flex-col items-center justify-center gap-2 bg-[#e8e6de] px-3 text-center text-muted">
                    <Film :size="32" aria-hidden="true" />
                    <span class="text-xs font-bold">Affiche indisponible</span>
                  </div>
                  <span class="runtime-badge">{{ movie.runtime_minutes }} min</span>
                </div>
                <div class="border-x-2 border-b-2 border-ink bg-surface px-3 py-3">
                  <h3 class="line-clamp-2 min-h-[2.5rem] text-sm font-black leading-snug tracking-[-0.02em] group-hover:text-primary">{{ movie.title }}</h3>
                  <span class="mt-3 inline-block border-b-2 border-ink font-mono text-[9px] font-bold uppercase tracking-[0.14em]">Voir le film</span>
                </div>
              </NuxtLink>
            </li>
          </ul>

          <nav v-if="totalPages > 1" class="pagination mt-14 flex flex-col items-stretch justify-between gap-4 border-2 border-ink bg-surface p-3 shadow-[6px_6px_0_#27272a] sm:flex-row sm:items-center" aria-label="Pagination des films">
            <button type="button" class="page-button" :disabled="page <= 1 || pending" @click="changePage(page - 1)">
              ← Précédent
            </button>
            <span class="order-first text-center font-mono text-[11px] font-bold uppercase tracking-[0.14em] sm:order-none" aria-live="polite">Page {{ page }} / {{ totalPages }}</span>
            <button type="button" class="page-button" :disabled="page >= totalPages || pending" @click="changePage(page + 1)">
              Suivant →
            </button>
          </nav>
        </template>
      </div>
    </section>
  </main>
</template>

<style scoped>
.catalog-title {
  font-family: "Noto Sans Variable", sans-serif;
}

.catalog-title span:first-of-type {
  -webkit-text-stroke: 2px #27272a;
  color: transparent;
}

.title-accent {
  position: absolute;
  right: 8%;
  bottom: 20%;
  width: clamp(2.25rem, 5vw, 4.5rem);
  aspect-ratio: 1;
  transform: rotate(8deg);
  border: 2px solid #27272a;
  background: var(--color-highlight);
  box-shadow: 5px 5px 0 #27272a;
}

.catalog-canvas {
  background-color: #f8f7f2;
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.075) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.075) 1px, transparent 1px);
  background-size: 28px 28px;
}

.control-label {
  display: block;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 800;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.catalog-field {
  height: 3.25rem;
  border: 2px solid #27272a;
  border-radius: 0;
  background: #fff;
  padding: 0 0.9rem;
  color: #27272a;
  font-size: 0.9rem;
  font-weight: 700;
  outline: none;
}

.catalog-field:focus {
  box-shadow: inset 0 0 0 2px var(--color-highlight);
}

.catalog-field:disabled,
.search-button:disabled,
.page-button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.search-button,
.state-button {
  display: inline-flex;
  min-height: 3.25rem;
  align-items: center;
  justify-content: center;
  gap: 0.55rem;
  border: 2px solid #27272a;
  background: #27272a;
  padding: 0 1rem;
  color: #fff;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  transition: background-color 150ms ease;
}

.search-button:hover:not(:disabled),
.state-button:hover {
  background: #991b1b;
}

.catalog-state {
  margin: 4rem auto 1rem;
  display: flex;
  min-height: 24rem;
  max-width: 48rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 2rem;
  text-align: center;
  font-weight: 800;
  box-shadow: 8px 8px 0 #27272a;
}

.catalog-card {
  color: #27272a;
  transition: transform 170ms ease;
}

.catalog-card:hover {
  transform: translateY(-4px);
}

.poster-frame {
  position: relative;
  aspect-ratio: 2 / 3;
  overflow: hidden;
  border: 2px solid #27272a;
  background: #e8e6de;
  box-shadow: 5px 5px 0 #27272a;
}

.runtime-badge {
  position: absolute;
  right: 0.4rem;
  bottom: 0.4rem;
  border: 2px solid #27272a;
  border-radius: 999px;
  background: var(--color-highlight);
  padding: 0.25rem 0.45rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.58rem;
  font-weight: 900;
  line-height: 1;
}

.page-button {
  min-height: 2.75rem;
  border: 2px solid #27272a;
  background: #ffcf3f;
  padding: 0.65rem 1rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  transition: background-color 150ms ease, color 150ms ease;
}

.page-button:hover:not(:disabled) {
  background: #27272a;
  color: #fff;
}

@media (max-width: 639px) {
  .title-accent {
    right: 1.25rem;
    bottom: 1.5rem;
  }

  .catalog-state {
    min-height: 19rem;
    margin-top: 2.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .catalog-card,
  .catalog-card img,
  .search-button,
  .state-button,
  .page-button {
    transition: none;
  }

  .catalog-card:hover {
    transform: none;
  }
}
</style>
