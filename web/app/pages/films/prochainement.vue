<script setup lang="ts">
import { AlertTriangle, CalendarDays, LoaderCircle, RefreshCw } from '@lucide/vue'
import type { UpcomingMoviesResponse } from '~/types/api'
import { normalizeMovieGenres } from '~/utils/movieCatalogFilters'
import { queriesEqual } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { formatFrenchReleaseDate, formatReleaseMonth, groupUpcomingMovies, parseUpcomingFilters, upcomingApiQuery, upcomingRouteQuery } from '~/utils/upcomingMovies'
import type { UpcomingFilters } from '~/utils/upcomingMovies'

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const filters = ref(parseUpcomingFilters(route.query))
const catalog = ref<UpcomingMoviesResponse | null>(null)
const pending = ref(false)
const errorMessage = ref('')
let requestId = 0
let mounted = false
const groups = computed(() => groupUpcomingMovies(catalog.value?.items ?? []))
const totalPages = computed(() => Math.max(1, Math.ceil((catalog.value?.total ?? 0) / 24)))
const genres = computed(() => normalizeMovieGenres([...(catalog.value?.available_genres ?? []), ...filters.value.genres]))
const months = computed(() => [...new Set([...(catalog.value?.available_months ?? []), ...(filters.value.month ? [filters.value.month] : [])])].sort())
const hasFilters = computed(() => Boolean(filters.value.month || filters.value.genres.length))

async function fetchCatalog(state: UpcomingFilters) {
  let response = await api.upcomingMovies(upcomingApiQuery(state))
  const lastPage = Math.max(1, Math.ceil(response.total / 24))
  if (state.page > lastPage) response = await api.upcomingMovies(upcomingApiQuery({ ...state, page: lastPage }))
  // Validate verified dates before committing a response. Never substitute the general release date.
  groupUpcomingMovies(response.items)
  return response
}

const initial = await useAsyncData(`upcoming:${JSON.stringify(upcomingRouteQuery(filters.value))}`, async () => {
  try {
    return { catalog: await fetchCatalog(filters.value), errorMessage: '' }
  } catch (error) {
    return { catalog: null, errorMessage: getFrenchApiError(error) }
  }
})
catalog.value = initial.data.value?.catalog ?? null
errorMessage.value = initial.data.value?.errorMessage ?? ''
if (catalog.value) filters.value.page = catalog.value.page
if (import.meta.server && errorMessage.value) {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

async function loadCatalog() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await fetchCatalog(filters.value)
    if (currentRequest !== requestId) return
    catalog.value = response
    filters.value.page = response.page
    const query = upcomingRouteQuery(filters.value)
    if (!queriesEqual(route.query, query)) await router.replace({ query })
  } catch (error) {
    if (currentRequest !== requestId) return
    catalog.value = null
    errorMessage.value = getFrenchApiError(error)
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

function changeFilters(values: Partial<UpcomingFilters>) {
  void router.push({ query: upcomingRouteQuery({ ...filters.value, ...values, page: 1 }) })
}

function changeMonth(event: Event) {
  if (event.target instanceof HTMLSelectElement) changeFilters({ month: event.target.value })
}

function toggleGenre(genre: string) {
  changeFilters({ genres: filters.value.genres.includes(genre) ? filters.value.genres.filter(value => value !== genre) : normalizeMovieGenres([...filters.value.genres, genre]) })
}

watch(() => route.query, async () => {
  if (!mounted) return
  const next = parseUpcomingFilters(route.query)
  const changed = !queriesEqual(upcomingRouteQuery(filters.value), upcomingRouteQuery(next))
  filters.value = next
  const query = upcomingRouteQuery(next)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
  if (changed) await loadCatalog()
})
onMounted(async () => {
  const query = upcomingRouteQuery(filters.value)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
  mounted = true
})
onBeforeUnmount(() => { requestId++ })

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/films/prochainement')
const title = 'Films prochainement au cinéma - MesSeances'
const description = 'Les prochaines sorties françaises au cinéma, mois par mois, pour l’année à venir.'
useSeoMeta({
  title, description, ogTitle: title, ogDescription: description, ogUrl: canonicalUrl,
  ogType: 'website', ogLocale: 'fr_FR',
  robots: computed(() => catalog.value && !errorMessage.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="min-h-screen bg-[#f8f7f2] text-ink">
    <FilmCatalogTabs active="upcoming" />
    <header class="border-b-2 border-ink bg-surface">
      <div class="mx-auto max-w-[1440px] px-4 py-12 sm:px-6 sm:py-16 lg:px-10">
        <h1 class="text-[clamp(2.4rem,8vw,7.5rem)] font-black uppercase leading-[0.9] tracking-[-0.065em]">Prochainement<span class="text-primary">.</span></h1>
        <p v-if="catalog" class="mt-6 font-mono text-xs font-bold uppercase tracking-wide">Du <time :datetime="catalog.window.from">{{ formatFrenchReleaseDate(catalog.window.from) }}</time> au <time :datetime="catalog.window.through">{{ formatFrenchReleaseDate(catalog.window.through) }}</time></p>
      </div>
    </header>
    <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 lg:px-10">
      <form class="border-b-2 border-ink pb-8" aria-label="Filtres des prochaines sorties" @submit.prevent>
        <div class="max-w-sm">
          <label for="release-month" class="mb-2 block font-mono text-xs font-black uppercase tracking-wide">Mois de sortie</label>
          <select id="release-month" :value="filters.month" class="min-h-12 w-full border-2 border-ink bg-highlight px-3 font-bold focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink" @change="changeMonth">
            <option value="">Tous les mois</option>
            <option v-for="month in months" :key="month" :value="month">{{ formatReleaseMonth(month) }}</option>
          </select>
        </div>
        <fieldset v-if="genres.length" class="mt-6">
          <legend class="mb-3 font-mono text-xs font-black uppercase tracking-wide">Genres</legend>
          <div class="flex flex-wrap gap-2">
            <label v-for="genre in genres" :key="genre" class="flex min-h-11 cursor-pointer items-center gap-2 border-2 border-ink bg-surface px-3 text-sm font-bold hover:bg-highlight focus-within:outline-2 focus-within:outline-offset-3 focus-within:outline-ink has-[input:checked]:bg-highlight">
              <input type="checkbox" :value="genre" :checked="filters.genres.includes(genre)" class="size-4 accent-primary" @change="toggleGenre(genre)" />{{ genre }}
            </label>
          </div>
        </fieldset>
        <button v-if="hasFilters" type="button" class="mt-4 min-h-11 font-mono text-xs font-bold underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="changeFilters({ month: '', genres: [] })">Effacer les filtres</button>
      </form>

      <div aria-live="polite" :aria-busy="pending">
        <EditorialStatePanel v-if="pending" semantic="status" size="tall" class="mt-8 font-bold">
          <template #icon><LoaderCircle :size="32" class="animate-spin motion-reduce:animate-none" aria-hidden="true" /></template>
          <p>Chargement des sorties…</p>
        </EditorialStatePanel>
        <EditorialStatePanel v-else-if="errorMessage" semantic="alert" size="tall" class="mt-8 font-bold">
          <template #icon><AlertTriangle :size="32" aria-hidden="true" /></template>
          <p>{{ errorMessage }}</p>
          <template #actions><button type="button" class="inline-flex min-h-11 items-center gap-2 border-2 border-ink bg-ink px-4 font-mono text-xs font-black uppercase text-white hover:bg-primary focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="loadCatalog"><RefreshCw :size="16" aria-hidden="true" />Réessayer</button></template>
        </EditorialStatePanel>
        <EditorialStatePanel v-else-if="!catalog?.items.length" size="tall" class="mt-8 font-bold">
          <template #icon><CalendarDays :size="32" aria-hidden="true" /></template>
          <p>{{ catalog?.available_months.length && hasFilters ? 'Aucun film ne correspond aux filtres' : 'Aucune sortie annoncée' }}</p>
        </EditorialStatePanel>
        <template v-else>
          <p class="mt-6 font-mono text-xs font-bold uppercase">{{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}</p>
          <section v-for="group in groups" :key="group.month" class="mt-8" :aria-labelledby="`month-${group.month}`">
            <h2 :id="`month-${group.month}`" class="border-b-2 border-ink pb-3 text-3xl font-black capitalize tracking-tight">{{ formatReleaseMonth(group.month) }}</h2>
            <ul class="mt-6 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6">
              <li v-for="movie in group.movies" :key="movie.slug" class="min-w-0">
                <MovieCatalogCard :movie="movie" :to="`/film/${encodeURIComponent(movie.slug)}`">
                  <template #release><p class="mt-2 text-xs font-bold leading-relaxed">Sortie le <time :datetime="movie.french_release_date">{{ formatFrenchReleaseDate(movie.french_release_date) }}</time></p></template>
                </MovieCatalogCard>
              </li>
            </ul>
          </section>
          <MovieCatalogPagination :page="filters.page" :total-pages="totalPages" :previous-to="filters.page > 1 ? { query: upcomingRouteQuery({ ...filters, page: filters.page - 1 }) } : null" :next-to="filters.page < totalPages ? { query: upcomingRouteQuery({ ...filters, page: filters.page + 1 }) } : null" :pending="pending" />
        </template>
      </div>
    </div>
  </main>
</template>
