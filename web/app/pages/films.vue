<script setup lang="ts">
import { AlertTriangle, CalendarDays, Film, LoaderCircle, RefreshCw, Search, SlidersHorizontal } from '@lucide/vue'
import { VueDatePicker } from '@vuepic/vue-datepicker'
import { fr } from 'date-fns/locale/fr'
import '@vuepic/vue-datepicker/dist/main.css'
import type { CatalogMovie, MoviesResponse, MovieSort } from '~/types/api'
import { todayInParis } from '~/utils/date'
import {
  addMovieCatalogDays,
  calendarDateFromDate,
  dateFromCalendarDate,
  formatShortCalendarDate,
  hasMovieCatalogFilters,
  movieCatalogDraftError,
  movieCatalogFilterDraft,
  movieCatalogFiltersFromDraft,
  movieCatalogFiltersKey,
  normalizeMovieGenres,
  parseMovieCatalogFilters,
  serializeMovieCatalogFilters
} from '~/utils/movieCatalogFilters'
import type { MovieCatalogFilterDraft, MovieCatalogFilters, MovieDateMode } from '~/utils/movieCatalogFilters'
import { enumQueryValue, mergeOwnedQuery, positiveSafeInteger, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { serializeJsonLd } from '~/utils/jsonLd'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const PAGE_SIZE = 24
const DEFAULT_SORT: MovieSort = 'showtimes_desc'
const LG_MEDIA_QUERY = '(min-width: 1024px)'
const SORT_OPTIONS = [
  { value: 'title_asc', label: 'Titre A–Z' },
  { value: 'title_desc', label: 'Titre Z–A' },
  { value: 'release_date_desc', label: 'Sorties récentes' },
  { value: 'runtime_asc', label: 'Durée croissante' },
  { value: 'runtime_desc', label: 'Durée décroissante' },
  { value: 'showtimes_desc', label: 'Plus de séances' }
] as const satisfies readonly { value: MovieSort, label: string }[]
const SORT_VALUES = SORT_OPTIONS.map((option) => option.value)
const OWNED_QUERY_KEYS = ['q', 'sort', 'page', 'genres', 'duration', 'date', 'date_to', 'all_theaters'] as const
const EMPTY_FILTERS: MovieCatalogFilters = { genres: [] }
const DURATION_OPTIONS = [
  { value: 'short', label: 'Moins de 1h30' },
  { value: 'medium', label: 'De 1h30 à 2h' },
  { value: 'long', label: 'Plus de 2h' }
] as const
const DATE_OPTIONS = [
  { value: 'none', label: 'Toutes les dates' },
  { value: 'today', label: 'Aujourd’hui' },
  { value: 'tomorrow', label: 'Demain' },
  { value: 'weekend', label: 'Ce week-end' },
  { value: 'custom', label: 'Date précise' },
  { value: 'range', label: 'Période' }
] as const satisfies readonly { value: MovieDateMode, label: string }[]

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const preferences = useCinemaPreferences()
const searchInput = ref('')
const appliedSearch = ref('')
const sort = ref<MovieSort>(DEFAULT_SORT)
const page = ref(1)
const todayDate = ref(todayInParis())
const appliedFilters = ref<MovieCatalogFilters>({ genres: [] })
const draftFilters = ref<MovieCatalogFilterDraft>(movieCatalogFilterDraft(EMPTY_FILTERS, todayDate.value))
const isAdvancedFiltersOpen = ref(false)
const advancedFiltersTrigger = ref<HTMLButtonElement | null>(null)
const resultsSection = ref<HTMLElement | null>(null)
const catalog = ref<MoviesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
let requestId = 0
let isMounted = false
let isInitializing = false
let advancedApplyNavigation: 'none' | 'collapse' | 'scroll' = 'none'
let scrollAfterLoad = false
let lastLoadKey = ''

const totalPages = computed(() => Math.max(1, Math.ceil((catalog.value?.total ?? 0) / PAGE_SIZE)))
const draftError = computed(() => movieCatalogDraftError(draftFilters.value, todayDate.value))
const hasAppliedAdvancedFilters = computed(() => hasMovieCatalogFilters(appliedFilters.value))
const availableGenres = computed(() => normalizeMovieGenres([
  ...(catalog.value?.available_genres ?? []),
  ...draftFilters.value.genres,
  ...appliedFilters.value.genres
]))
const advancedFilterCount = computed(() => appliedFilters.value.genres.length
  + Number(Boolean(appliedFilters.value.allTheaters))
  + Number(Boolean(appliedFilters.value.duration))
  + Number(Boolean(appliedFilters.value.date)))
const advancedFiltersButtonLabel = computed(() => {
  const action = isAdvancedFiltersOpen.value ? 'Masquer' : 'Afficher'
  const count = advancedFilterCount.value
  return `${action} les filtres avancés${count ? ` (${count} actif${count > 1 ? 's' : ''})` : ''}`
})
const minPickerDate = computed(() => dateFromCalendarDate(todayDate.value) ?? new Date())
const singlePickerDate = computed<Date | null>({
  get: () => dateFromCalendarDate(draftFilters.value.customDate),
  set: (value) => { draftFilters.value.customDate = value ? calendarDateFromDate(value) : '' }
})
const rangePickerDates = computed<Date[] | null>({
  get: () => {
    const start = dateFromCalendarDate(draftFilters.value.rangeStart)
    const end = dateFromCalendarDate(draftFilters.value.rangeEnd)
    return start && end ? [start, end] : null
  },
  set: (value) => {
    draftFilters.value.rangeStart = value?.[0] ? calendarDateFromDate(value[0]) : ''
    draftFilters.value.rangeEnd = value?.[1] ? calendarDateFromDate(value[1]) : ''
  }
})
const datePickerFormats = { input: 'dd-MM-yy' }
const datePickerTextInput = { format: 'dd-MM-yy', rangeSeparator: ' au ', enterSubmit: true, tabSubmit: true, applyOnBlur: true }
const calendarAriaLabels = {
  menu: 'Calendrier des séances',
  input: 'Saisir une date au format jour-mois-année',
  calendarIcon: 'Ouvrir le calendrier',
  clearInput: 'Effacer la date',
  prevMonth: 'Mois précédent',
  nextMonth: 'Mois suivant',
  prevYear: 'Année précédente',
  nextYear: 'Année suivante',
  openMonthsOverlay: 'Choisir un mois',
  openYearsOverlay: 'Choisir une année',
  day: ({ value }: { value: Date }) => `Choisir le ${formatShortCalendarDate(calendarDateFromDate(value))}`
}
const appliedFilterSummary = computed(() => {
  const filters = appliedFilters.value
  const items: string[] = []
  if (filters.allTheaters) items.push('cinémas : tous les cinémas')
  if (filters.genres.length) items.push(`genres : ${filters.genres.join(', ')}`)
  const duration = DURATION_OPTIONS.find((option) => option.value === filters.duration)
  if (duration) items.push(`durée : ${duration.label.toLocaleLowerCase('fr-FR')}`)
  if (filters.date === 'today') items.push('séances : aujourd’hui')
  else if (filters.date === 'tomorrow') items.push('séances : demain')
  else if (filters.date === 'weekend') items.push('séances : ce week-end')
  else if (filters.date) items.push(filters.dateTo
    ? `séances : du ${formatShortCalendarDate(filters.date)} au ${formatShortCalendarDate(filters.dateTo)}`
    : `séances : le ${formatShortCalendarDate(filters.date)}`)
  return items
})

function prepareAdvancedApplyNavigation() {
  if (!isMounted) return
  if (window.matchMedia(LG_MEDIA_QUERY).matches) {
    advancedApplyNavigation = 'scroll'
    return
  }
  advancedApplyNavigation = 'collapse'
  isAdvancedFiltersOpen.value = false
}

async function finishAdvancedApplyNavigation() {
  if (!isMounted || advancedApplyNavigation === 'none') return
  if (advancedApplyNavigation === 'collapse') {
    advancedApplyNavigation = 'none'
    isAdvancedFiltersOpen.value = false
    return
  }
  await nextTick()
  const target = resultsSection.value
  if (!target) return
  advancedApplyNavigation = 'none'
  target.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

async function loadMovies() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''

  if (preferences.error.value) {
    catalog.value = null
    errorMessage.value = preferences.error.value
    pending.value = false
    await finishAdvancedApplyNavigation()
    return
  }
  if (!preferences.isInitialized.value) return
  if (!appliedFilters.value.allTheaters && preferences.favoriteTheaterIds.value.length === 0) {
    catalog.value = null
    pending.value = false
    await finishAdvancedApplyNavigation()
    return
  }

  const theaterIds = appliedFilters.value.allTheaters
    ? undefined
    : preferences.favoriteTheaterIds.value.join(',')
  let shouldFinishAdvancedApplyNavigation = false

  try {
    const filterQuery = serializeMovieCatalogFilters(appliedFilters.value)
    const response = await api.movies({
      currently_screened: true,
      theaters: theaterIds,
      search: appliedSearch.value || undefined,
      genres: filterQuery.genres,
      duration: appliedFilters.value.duration,
      date: filterQuery.date,
      date_to: filterQuery.date_to,
      sort: sort.value,
      page: page.value,
      page_size: PAGE_SIZE
    })
    if (currentRequest === requestId) {
      const lastPage = Math.max(1, Math.ceil(response.total / PAGE_SIZE))
      if (page.value > lastPage) {
        const query = filmQuery({ search: appliedSearch.value, page: lastPage, sort: sort.value, filters: appliedFilters.value })
        if (!queriesEqual(route.query, query)) await router.replace({ query })
        return
      }
      catalog.value = response
      shouldFinishAdvancedApplyNavigation = true
      if (scrollAfterLoad) {
        scrollAfterLoad = false
        window.scrollTo({ top: 0, behavior: 'smooth' })
      }
    }
  } catch (error) {
    if (currentRequest === requestId) {
      catalog.value = null
      errorMessage.value = getFrenchApiError(error)
      shouldFinishAdvancedApplyNavigation = true
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
  if (shouldFinishAdvancedApplyNavigation) await finishAdvancedApplyNavigation()
}

async function retryMovies() {
  pending.value = true
  errorMessage.value = ''
  await preferences.initialize()
  if (!preferences.isInitialized.value) {
    await loadMovies()
    return
  }
  lastLoadKey = ''
  await loadMovies()
}

interface FilmRouteState {
  search: string
  page: number
  sort: MovieSort
  filters: MovieCatalogFilters
}

function filmQuery(state: FilmRouteState) {
  const filters = serializeMovieCatalogFilters(state.filters)
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    q: state.search || undefined,
    sort: state.sort === DEFAULT_SORT ? undefined : state.sort,
    page: state.page === 1 ? undefined : String(state.page),
    ...filters
  })
}

function hydrateRoute() {
  todayDate.value = todayInParis()
  const rawSearch = singularQueryValue(route.query.q)
  const nextSearch = rawSearch?.trim() ?? ''
  const nextSort = enumQueryValue(singularQueryValue(route.query.sort), SORT_VALUES) ?? DEFAULT_SORT
  const nextPage = positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1
  const nextFilters = parseMovieCatalogFilters(route.query, todayDate.value)
  searchInput.value = nextSearch
  appliedSearch.value = nextSearch
  sort.value = nextSort
  page.value = nextPage
  appliedFilters.value = nextFilters
  draftFilters.value = movieCatalogFilterDraft(nextFilters, todayDate.value)
  if (hasMovieCatalogFilters(nextFilters) && advancedApplyNavigation !== 'collapse') isAdvancedFiltersOpen.value = true
  return filmQuery({ search: nextSearch, page: nextPage, sort: nextSort, filters: nextFilters })
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  const key = `${appliedSearch.value}|${sort.value}|${page.value}|${movieCatalogFiltersKey(appliedFilters.value)}|${preferences.favoriteTheaterIds.value.join(',')}`
  if (key === lastLoadKey) return
  lastLoadKey = key
  await loadMovies()
}

function submitSearch() {
  const nextSearch = searchInput.value.trim()
  const query = filmQuery({ search: nextSearch, page: 1, sort: sort.value, filters: appliedFilters.value })
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
  router.push({ query: filmQuery({ search: appliedSearch.value, page: 1, sort: nextSort, filters: appliedFilters.value }) })
}

function toggleAdvancedFilters() {
  isAdvancedFiltersOpen.value = !isAdvancedFiltersOpen.value
}

function closeAdvancedFilters() {
  if (!isAdvancedFiltersOpen.value) return
  isAdvancedFiltersOpen.value = false
  nextTick(() => advancedFiltersTrigger.value?.focus())
}

function selectDateMode(mode: MovieDateMode) {
  draftFilters.value.dateMode = mode
  if (mode === 'custom' && (!draftFilters.value.customDate || draftFilters.value.customDate < todayDate.value)) {
    draftFilters.value.customDate = todayDate.value
  }
  if (mode === 'range') {
    if (!draftFilters.value.rangeStart || draftFilters.value.rangeStart < todayDate.value) draftFilters.value.rangeStart = todayDate.value
    if (!draftFilters.value.rangeEnd || draftFilters.value.rangeEnd < draftFilters.value.rangeStart) {
      draftFilters.value.rangeEnd = addMovieCatalogDays(draftFilters.value.rangeStart, 1)
    }
  }
}

function handleSingleDateTextInput(_event: Event | string, parsedDate: Date | Array<Date | null> | null) {
  draftFilters.value.customDate = parsedDate instanceof Date ? calendarDateFromDate(parsedDate) : ''
}

function handleRangeDateTextInput(_event: Event | string, parsedDate: Date | Array<Date | null> | null) {
  const dates = Array.isArray(parsedDate) ? parsedDate : []
  draftFilters.value.rangeStart = dates[0] instanceof Date ? calendarDateFromDate(dates[0]) : ''
  draftFilters.value.rangeEnd = dates[1] instanceof Date ? calendarDateFromDate(dates[1]) : ''
}

async function applyAdvancedFilters() {
  const filters = movieCatalogFiltersFromDraft(draftFilters.value, todayDate.value)
  if (!filters) return
  const query = filmQuery({ search: appliedSearch.value, page: 1, sort: sort.value, filters })
  prepareAdvancedApplyNavigation()
  if (queriesEqual(route.query, query)) {
    if (errorMessage.value) await loadMovies()
    else if (!pending.value) await finishAdvancedApplyNavigation()
    return
  }
  await router.push({ query })
}

function clearAdvancedFilters() {
  const query = filmQuery({ search: appliedSearch.value, page: 1, sort: sort.value, filters: EMPTY_FILTERS })
  if (queriesEqual(route.query, query)) {
    appliedFilters.value = { genres: [] }
    draftFilters.value = movieCatalogFilterDraft(EMPTY_FILTERS, todayDate.value)
    return
  }
  router.push({ query })
}

function followPageLink(event: MouseEvent, nextPage: number) {
  if (pending.value) {
    event.preventDefault()
    return
  }
  if (nextPage < 1 || nextPage > totalPages.value || nextPage === page.value) return
  scrollAfterLoad = true
}

function formatRuntime(runtimeMinutes: number): string {
  if (!Number.isInteger(runtimeMinutes) || runtimeMinutes <= 0) return 'Durée non renseignée'
  const hours = Math.floor(runtimeMinutes / 60)
  const minutes = runtimeMinutes % 60
  return [hours ? `${hours}h` : '', minutes ? `${minutes}min` : ''].filter(Boolean).join(' ')
}

function formatShowtimeCount(showtimeCount: number): string {
  return `${showtimeCount} séance${showtimeCount === 1 ? '' : 's'}`
}

hydrateRoute()
const initialCatalogKey = `films-catalog:${encodeURIComponent(appliedSearch.value)}:${sort.value}:${page.value}:${encodeURIComponent(movieCatalogFiltersKey(appliedFilters.value))}`
const initialResult = await useAsyncData(initialCatalogKey, async () => {
  try {
    const filterQuery = serializeMovieCatalogFilters(appliedFilters.value)
    const response = await api.movies({
      currently_screened: true,
      search: appliedSearch.value || undefined,
      genres: filterQuery.genres,
      duration: appliedFilters.value.duration,
      date: filterQuery.date,
      date_to: filterQuery.date_to,
      sort: sort.value,
      page: page.value,
      page_size: PAGE_SIZE
    })
    return { kind: 'success' as const, catalog: response, errorMessage: '' }
  } catch (error) {
    return { kind: 'upstream-error' as const, catalog: null, errorMessage: getFrenchApiError(error) }
  }
})

const initialState = initialResult.data.value
catalog.value = initialState?.catalog ?? null
errorMessage.value = initialState?.errorMessage ?? ''
pending.value = false
if (import.meta.server && initialState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

watch(() => route.query, () => {
  if (isMounted && !isInitializing) applyRoute()
})
watch(preferences.favoriteTheaterIds, () => {
  if (preferences.isInitialized.value && !isInitializing) applyRoute()
})
onMounted(async () => {
  isMounted = true
  isInitializing = true
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) await router.replace({ query: canonicalQuery })
  await preferences.initialize()
  isInitializing = false
  if (preferences.isInitialized.value) await applyRoute()
  else await loadMovies()
})

const config = useRuntimeConfig()
const socialImageUrl = absoluteSiteUrl(config.public.siteUrl, '/pwa-512x512.png')
const pageDescription = 'Parcourez les films actuellement au cinéma, recherchez un titre et consultez toutes les séances disponibles.'
const rawQueryKeys = computed(() => Object.keys(route.query))
const isPageOnlyQuery = computed(() => rawQueryKeys.value.length === 1 && rawQueryKeys.value[0] === 'page')
const rawPage = computed(() => singularQueryValue(route.query.page))
const normalizedCanonicalPage = computed(() => {
  if (!isPageOnlyQuery.value) return 1
  const parsed = positiveSafeInteger(rawPage.value) ?? 1
  return Math.min(parsed, totalPages.value)
})
const canonicalUrl = computed(() => absoluteSiteUrl(
  config.public.siteUrl,
  normalizedCanonicalPage.value >= 2 ? `/films?page=${normalizedCanonicalPage.value}` : '/films'
))
const isCanonicalPageQuery = computed(() => {
  const value = rawPage.value
  return isPageOnlyQuery.value
    && value !== undefined
    && /^(?:[2-9]|[1-9]\d+)$/.test(value)
    && positiveSafeInteger(value) === normalizedCanonicalPage.value
})
const isIndexable = computed(() => Boolean(catalog.value) && !errorMessage.value && (
  rawQueryKeys.value.length === 0 || isCanonicalPageQuery.value
))
const pageTitle = computed(() => normalizedCanonicalPage.value >= 2
  ? `Films à l’affiche - Page ${normalizedCanonicalPage.value} - MesSeances`
  : 'Films à l’affiche - MesSeances')
const filmsJsonLd = computed(() => {
  if (pending.value || !isIndexable.value || !catalog.value?.items.length) return null
  return serializeJsonLd({
    '@context': 'https://schema.org',
    '@graph': [{
      '@type': 'ItemList',
      '@id': `${canonicalUrl.value}#film-list`,
      itemListElement: catalog.value.items.map((movie, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(config.public.siteUrl, `/film/${encodeURIComponent(movie.slug)}`)
      }))
    }]
  })
})

useSeoMeta({
  robots: computed(() => isIndexable.value ? 'index,follow' : 'noindex,follow'),
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
useHead(() => ({
  link: [{ rel: 'canonical', href: canonicalUrl.value }],
  script: filmsJsonLd.value ? [{ key: 'films-jsonld', type: 'application/ld+json', innerHTML: filmsJsonLd.value }] : []
}))
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
        <div class="search-workspace border-2 border-ink bg-[#ffcf3f] p-4 shadow-[7px_7px_0_#27272a] sm:p-6">
          <div class="grid grid-cols-[minmax(0,1fr)_3.25rem] items-end gap-4 lg:grid-cols-[minmax(0,1fr)_14rem_3.25rem] lg:gap-5">
            <form class="col-span-2 min-w-0 lg:col-span-1" role="search" @submit.prevent="submitSearch">
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

            <label class="block min-w-0">
              <span class="control-label">Trier par</span>
              <select :value="sort" class="catalog-field mt-2 w-full" :disabled="pending" @change="changeSort">
                <option v-for="option in SORT_OPTIONS" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </label>

            <button
              ref="advancedFiltersTrigger"
              type="button"
              class="advanced-trigger"
              :class="{ 'advanced-trigger--active': advancedFilterCount > 0 }"
              :aria-label="advancedFiltersButtonLabel"
              aria-controls="advanced-film-filters"
              :aria-expanded="isAdvancedFiltersOpen"
              @click="toggleAdvancedFilters"
              @keydown.esc.stop.prevent="closeAdvancedFilters"
            >
              <SlidersHorizontal :size="20" stroke-width="2.5" aria-hidden="true" />
              <span v-if="advancedFilterCount" class="advanced-trigger-count" aria-hidden="true">{{ advancedFilterCount }}</span>
            </button>
          </div>

          <form
            v-show="isAdvancedFiltersOpen"
            id="advanced-film-filters"
            class="advanced-panel"
            aria-label="Filtres avancés des films"
            @submit.prevent="applyAdvancedFilters"
            @keydown.esc.stop.prevent="closeAdvancedFilters"
          >
              <div class="grid gap-7 lg:grid-cols-3">
                <fieldset class="min-w-0">
                  <legend class="control-label mb-3">Genres</legend>
                  <div v-if="availableGenres.length" class="grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
                    <label v-for="genre in availableGenres" :key="genre" class="filter-choice">
                      <input v-model="draftFilters.genres" type="checkbox" :value="genre" class="size-4 shrink-0 accent-primary" />
                      <span>{{ genre }}</span>
                    </label>
                  </div>
                  <p v-else class="border-2 border-ink bg-surface px-3 py-3 text-sm font-semibold">Aucun genre disponible.</p>
                </fieldset>

                <div>
                  <fieldset>
                    <legend class="control-label mb-3">Durée</legend>
                    <div class="grid gap-2">
                      <label class="filter-choice">
                        <input v-model="draftFilters.duration" type="radio" name="film-duration" value="" class="size-4 shrink-0 accent-primary" />
                        <span>Toutes les durées</span>
                      </label>
                      <label v-for="option in DURATION_OPTIONS" :key="option.value" class="filter-choice">
                        <input v-model="draftFilters.duration" type="radio" name="film-duration" :value="option.value" class="size-4 shrink-0 accent-primary" />
                        <span>{{ option.label }}</span>
                      </label>
                    </div>
                  </fieldset>
                  <fieldset class="mt-6">
                    <legend class="control-label mb-3">Cinémas</legend>
                    <label class="filter-choice">
                      <input v-model="draftFilters.allTheaters" type="checkbox" class="size-4 shrink-0 accent-primary" />
                      <span>Tous les cinémas</span>
                    </label>
                  </fieldset>
                </div>

                <fieldset class="min-w-0" :aria-invalid="draftError ? 'true' : undefined" :aria-describedby="draftError ? 'film-date-error' : undefined">
                  <legend class="control-label mb-3">Date de séance</legend>
                  <div class="grid grid-cols-2 gap-2">
                    <label v-for="option in DATE_OPTIONS" :key="option.value" class="filter-choice">
                      <input
                        type="radio"
                        name="film-date-mode"
                        :value="option.value"
                        :checked="draftFilters.dateMode === option.value"
                        class="size-4 shrink-0 accent-primary"
                        @change="selectDateMode(option.value)"
                      />
                      <span>{{ option.label }}</span>
                    </label>
                  </div>

                  <div v-if="draftFilters.dateMode === 'custom'" class="mt-3">
                    <label for="film-custom-date" class="control-label mb-2">Date (dd-MM-yy)</label>
                    <VueDatePicker
                      v-model="singlePickerDate"
                      class="catalog-datepicker"
                      :aria-labels="calendarAriaLabels"
                      :formats="datePickerFormats"
                      :input-attrs="{ id: 'film-custom-date', autocomplete: 'off', clearable: true }"
                      :locale="fr"
                      :min-date="minPickerDate"
                      :text-input="datePickerTextInput"
                      :time-config="{ enableTimePicker: false }"
                      :transitions="false"
                      :floating="{ arrow: false, offset: 6 }"
                      :ui="{ menu: 'catalog-calendar-menu' }"
                      teleport="body"
                      auto-apply
                      arrow-navigation
                      prevent-min-max-navigation
                      @text-input="handleSingleDateTextInput"
                      @cleared="draftFilters.customDate = ''"
                    >
                      <template #input-icon><CalendarDays :size="18" aria-hidden="true" /></template>
                    </VueDatePicker>
                  </div>

                  <div v-else-if="draftFilters.dateMode === 'range'" class="mt-3">
                    <label for="film-custom-range" class="control-label mb-2">Période (dd-MM-yy)</label>
                    <VueDatePicker
                      v-model="rangePickerDates"
                      class="catalog-datepicker"
                      :aria-labels="calendarAriaLabels"
                      :formats="datePickerFormats"
                      :input-attrs="{ id: 'film-custom-range', autocomplete: 'off', clearable: true }"
                      :locale="fr"
                      :min-date="minPickerDate"
                      :range="{ partialRange: false, autoSwitchStartEnd: false }"
                      :text-input="datePickerTextInput"
                      :time-config="{ enableTimePicker: false }"
                      :transitions="false"
                      :floating="{ arrow: false, offset: 6 }"
                      :ui="{ menu: 'catalog-calendar-menu' }"
                      teleport="body"
                      auto-apply
                      arrow-navigation
                      prevent-min-max-navigation
                      @text-input="handleRangeDateTextInput"
                      @cleared="draftFilters.rangeStart = ''; draftFilters.rangeEnd = ''"
                    >
                      <template #input-icon><CalendarDays :size="18" aria-hidden="true" /></template>
                    </VueDatePicker>
                  </div>
                  <p v-if="draftError" id="film-date-error" class="mt-2 text-sm font-bold text-red-800" role="alert">{{ draftError }}</p>
                </fieldset>
              </div>

              <div class="mt-7 flex flex-col-reverse gap-3 border-t-2 border-ink pt-5 sm:flex-row sm:justify-end">
                <button type="button" class="filter-reset-button" @click="clearAdvancedFilters">Effacer les filtres</button>
                <button type="submit" class="filter-apply-button" :disabled="Boolean(draftError)">Appliquer</button>
              </div>
          </form>
        </div>

        <div ref="resultsSection" class="scroll-mt-4" aria-hidden="true"></div>

        <div v-if="catalog && !pending" class="results-bar mt-10 flex flex-col gap-2 border-y-2 border-ink py-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
          <h2 class="text-xl font-black tracking-[-0.035em] sm:text-2xl">
            {{ appliedSearch ? `Résultats pour « ${appliedSearch} »` : 'Tous les films' }}
          </h2>
          <div class="flex flex-wrap items-center gap-3 sm:justify-end">
            <p class="font-mono text-[11px] font-bold uppercase tracking-[0.14em]">{{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}</p>
            <ShareButton />
          </div>
        </div>

        <EditorialStatePanel v-if="pending" semantic="status" live="polite" size="tall" shadow="large" class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /></template>
          <p>Chargement des films…</p>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="errorMessage" semantic="alert" size="tall" shadow="large" class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template>
          <p class="max-w-lg">{{ errorMessage }}</p>
          <template #actions><button type="button" class="state-button" @click="retryMovies"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="!appliedFilters.allTheaters && preferences.isInitialized.value && preferences.favoriteTheaterIds.value.length === 0" size="tall" shadow="large" class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><Film :size="36" aria-hidden="true" /></template>
          <p>Sélectionnez au moins un cinéma pour voir les films disponibles.</p>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="!catalog?.items.length" size="tall" shadow="large" class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><Film :size="36" aria-hidden="true" /></template>
          <p v-if="hasAppliedAdvancedFilters">Aucun film ne correspond à ces filtres : {{ appliedFilterSummary.join(' · ') }}.</p>
          <p v-else>{{ appliedSearch ? 'Aucun film ne correspond à cette recherche.' : 'Aucun film à l’affiche actuellement.' }}</p>
          <template v-if="hasAppliedAdvancedFilters" #actions><button type="button" class="state-button" @click="clearAdvancedFilters">Effacer les filtres</button></template>
        </EditorialStatePanel>

        <template v-else>
          <ul class="catalog-grid mt-8 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6" aria-label="Films à l’affiche">
            <li v-for="movie in catalog.items" :key="movie.slug" class="min-w-0">
              <NuxtLink :to="`/film/${movie.slug}`" class="catalog-card group block focus-visible:ring-offset-4">
                <div class="poster-frame">
                  <PosterImage
                    :src="movie.poster_url"
                    :alt="`Affiche de ${movie.title}`"
                    sizes="(min-width: 1280px) calc((min(100vw, 1440px) - 12.5rem) / 6), (min-width: 1024px) calc((100vw - 9.5rem) / 4), (min-width: 640px) calc((100vw - 6rem) / 3), calc((100vw - 3rem) / 2)"
                    class="h-full w-full"
                    image-class="h-full w-full object-cover transition duration-200 group-hover:scale-[1.025]"
                    fallback-class="gap-2 bg-[#e8e6de] px-3 text-center text-xs font-bold text-muted"
                    :fallback-icon-size="32"
                  />
                </div>
                <div class="border-x-2 border-b-2 border-ink bg-surface px-3 py-3">
                  <h3 class="line-clamp-2 min-h-[2.5rem] text-sm font-black leading-snug tracking-[-0.02em] group-hover:text-primary">{{ movie.title }}</h3>
                  <span class="inline-block font-mono text-[9px] font-bold uppercase tracking-[0.14em]">{{ formatRuntime(movie.runtime_minutes) }} · {{ formatShowtimeCount(movie.showtime_count ?? 0) }}</span>
                </div>
              </NuxtLink>
            </li>
          </ul>

          <nav v-if="totalPages > 1" class="pagination mt-14 flex flex-col items-stretch justify-between gap-4 border-2 border-ink bg-surface p-3 shadow-[6px_6px_0_#27272a] sm:flex-row sm:items-center" aria-label="Pagination des films">
            <span v-if="page <= 1" class="page-button page-button--disabled" aria-disabled="true">
              ← Précédent
            </span>
            <NuxtLink v-else :to="{ query: filmQuery({ search: appliedSearch, page: page - 1, sort, filters: appliedFilters }) }" class="page-button" :aria-disabled="pending || undefined" @click="followPageLink($event, page - 1)">
              ← Précédent
            </NuxtLink>
            <span class="order-first text-center font-mono text-[11px] font-bold uppercase tracking-[0.14em] sm:order-none" aria-live="polite">Page {{ page }} / {{ totalPages }}</span>
            <span v-if="page >= totalPages" class="page-button page-button--disabled" aria-disabled="true">
              Suivant →
            </span>
            <NuxtLink v-else :to="{ query: filmQuery({ search: appliedSearch, page: page + 1, sort, filters: appliedFilters }) }" class="page-button" :aria-disabled="pending || undefined" @click="followPageLink($event, page + 1)">
              Suivant →
            </NuxtLink>
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
.filter-apply-button:disabled,
.page-button--disabled,
.page-button[aria-disabled="true"] {
  cursor: not-allowed;
  opacity: 0.55;
}

.advanced-trigger {
  position: relative;
  display: inline-flex;
  width: 3.25rem;
  height: 3.25rem;
  align-items: center;
  justify-content: center;
  border: 2px solid #27272a;
  background: #fff;
  color: #27272a;
  transition: background-color 150ms ease, color 150ms ease, box-shadow 150ms ease;
}

.advanced-trigger:hover,
.advanced-trigger[aria-expanded="true"] {
  background: #27272a;
  color: #fff;
}

.advanced-trigger--active:not([aria-expanded="true"]) {
  box-shadow: inset 0 -4px 0 var(--color-highlight);
}

.advanced-trigger-count {
  position: absolute;
  top: -0.6rem;
  right: -0.6rem;
  display: inline-flex;
  min-width: 1.4rem;
  height: 1.4rem;
  align-items: center;
  justify-content: center;
  border: 2px solid #27272a;
  background: var(--color-highlight);
  padding: 0 0.2rem;
  color: #27272a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  line-height: 1;
}

.advanced-trigger:focus-visible,
.filter-choice:focus-within,
.filter-reset-button:focus-visible,
.filter-apply-button:focus-visible {
  outline: 2px solid #27272a;
  outline-offset: 3px;
}

.advanced-panel {
  margin-top: 1.5rem;
  border-top: 2px solid #27272a;
  padding-top: 1.5rem;
}

.filter-choice {
  display: flex;
  min-height: 2.75rem;
  cursor: pointer;
  align-items: center;
  gap: 0.65rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.55rem 0.7rem;
  font-size: 0.82rem;
  font-weight: 750;
}

.filter-choice:hover {
  background: #f8f7f2;
}

.filter-choice:has(input:checked) {
  background: var(--color-highlight);
  box-shadow: inset 0 -3px 0 #27272a;
}

.filter-reset-button,
.filter-apply-button {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  border: 2px solid #27272a;
  padding: 0.65rem 1rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.filter-reset-button {
  background: #fff;
}

.filter-apply-button {
  background: #27272a;
  color: #fff;
}

.filter-reset-button:hover,
.filter-apply-button:hover:not(:disabled) {
  background: #991b1b;
  color: #fff;
}

.catalog-datepicker {
  --dp-font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  --dp-border-radius: 0;
  --dp-cell-border-radius: 0;
  --dp-background-color: #fff;
  --dp-text-color: #27272a;
  --dp-border-color: #27272a;
  --dp-border-color-hover: #27272a;
  --dp-border-color-focus: #27272a;
  --dp-primary-color: #27272a;
  --dp-primary-text-color: #fff;
  --dp-icon-color: #27272a;
  --dp-font-size: 0.82rem;
}

.catalog-datepicker :deep(.dp__input) {
  min-height: 3.25rem;
  border-width: 2px;
  border-radius: 0;
  font-weight: 800;
}

.catalog-datepicker :deep(.dp__input_focus) {
  box-shadow: inset 0 0 0 2px var(--color-highlight);
}

:global(.catalog-calendar-menu) {
  --dp-font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  --dp-border-radius: 0;
  --dp-cell-border-radius: 0;
  --dp-background-color: #f8f7f2;
  --dp-text-color: #27272a;
  --dp-hover-color: #e8e6de;
  --dp-hover-text-color: #27272a;
  --dp-primary-color: #27272a;
  --dp-primary-text-color: #fff;
  --dp-secondary-color: #71717a;
  --dp-border-color: #27272a;
  --dp-menu-border-color: #27272a;
  --dp-border-color-hover: #27272a;
  --dp-border-color-focus: #27272a;
  --dp-disabled-color: #e8e6de;
  --dp-disabled-color-text: #71717a;
  --dp-icon-color: #27272a;
  --dp-menu-min-width: 19rem;
  --dp-font-size: 0.78rem;
  --dp-common-transition: none;
  --dp-animation-duration: 0s;
  border-width: 2px;
  box-shadow: 6px 6px 0 #27272a;
}

:global(.catalog-calendar-menu .dp__calendar_header_item),
:global(.catalog-calendar-menu .dp__month_year_select) {
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

:global(.catalog-calendar-menu .dp__active_date),
:global(.catalog-calendar-menu .dp__range_between),
:global(.catalog-calendar-menu .dp__range_start),
:global(.catalog-calendar-menu .dp__range_end) {
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

:global(.catalog-calendar-menu .dp__today) {
  border: 2px solid #991b1b;
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

.page-button {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
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

.page-button:hover:not(.page-button--disabled, [aria-disabled="true"]) {
  background: #27272a;
  color: #fff;
}

@media (max-width: 639px) {
  .title-accent {
    right: 1.25rem;
    bottom: 1.5rem;
  }

}

@media (prefers-reduced-motion: reduce) {
  .catalog-card,
  .catalog-card :deep(img),
  .search-button,
  .state-button,
  .advanced-trigger,
  .advanced-trigger svg,
  .filter-reset-button,
  .filter-apply-button,
  .page-button {
    transition: none;
  }

  .catalog-card:hover {
    transform: none;
  }
}
</style>
