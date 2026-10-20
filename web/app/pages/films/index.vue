<script setup lang="ts">
import { isBroadTheaterSelection } from '~/utils/cinemaSelection'
import {
  AlertTriangle,
  CalendarDays,
  Film,
  LoaderCircle,
  RefreshCw,
  Search,
  SlidersHorizontal,
  X,
} from '@lucide/vue'
import { fr } from 'date-fns/locale/fr'
import type { MoviesQuery, MoviesResponse, MovieSort } from '~/types/api'
import {
  addCalendarDays,
  calendarDateFromDate,
  dateFromCalendarDate,
  formatShortCalendarDate,
  todayInParis,
} from '~/utils/date'
import {
  hasMovieCatalogFilters,
  movieCatalogDraftError,
  movieCatalogFilterDraft,
  movieCatalogFiltersFromDraft,
  movieCatalogFiltersKey,
  normalizeMovieGenres,
  parseMovieCatalogFilters,
  serializeMovieCatalogFilters,
} from '~/utils/movieCatalogFilters'
import type {
  MovieCatalogFilterDraft,
  MovieCatalogFilters,
  MovieDateMode,
} from '~/utils/movieCatalogFilters'
import {
  movieCatalogSortOptions,
  movieCatalogSortValues,
} from '~/utils/movieCatalogPresentation'
import {
  enumQueryValue,
  mergeOwnedQuery,
  positiveSafeInteger,
  queriesEqual,
  singularQueryValue,
} from '~/utils/routeQuery'
import { serializeJsonLd } from '~/utils/jsonLd'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const PAGE_SIZE = 24
const DEFAULT_SORT: MovieSort = 'showtimes_desc'
const OWNED_QUERY_KEYS = [
  'q',
  'sort',
  'page',
  'genres',
  'duration',
  'date',
  'date_to',
  'all_theaters',
] as const
const EMPTY_FILTERS: MovieCatalogFilters = { genres: [] }
const DURATION_OPTIONS = [
  { value: 'short', label: 'Moins de 1h30', shortLabel: '< 1h30' },
  { value: 'medium', label: 'De 1h30 à 2h', shortLabel: '1h30–2h' },
  { value: 'long', label: 'Plus de 2h', shortLabel: '> 2h' },
] as const
const DATE_OPTIONS = [
  { value: 'none', label: 'Toutes les dates' },
  { value: 'today', label: 'Aujourd’hui' },
  { value: 'tomorrow', label: 'Demain' },
  { value: 'weekend', label: 'Ce week-end' },
  { value: 'custom', label: 'Date précise' },
  { value: 'range', label: 'Période' },
] as const satisfies readonly { value: MovieDateMode; label: string }[]

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const preferences = useCinemaPreferences()
const discovery = useWatchedCatalog('films')
const appliedSearch = ref('')
const sort = ref<MovieSort>(DEFAULT_SORT)
const draftSort = ref<MovieSort>(DEFAULT_SORT)
const page = ref(1)
const todayDate = ref(todayInParis())
const appliedFilters = ref<MovieCatalogFilters>({ genres: [] })
const draftFilters = ref<MovieCatalogFilterDraft>(
  movieCatalogFilterDraft(EMPTY_FILTERS, todayDate.value),
)
const isAdvancedFiltersOpen = ref(false)
const advancedFiltersTrigger = ref<HTMLButtonElement | null>(null)
const advancedFiltersDialog = ref<HTMLDialogElement | null>(null)
const advancedFiltersCloseButton = ref<HTMLButtonElement | null>(null)
const genreSearch = ref('')
const genreChips = ref<HTMLElement | null>(null)
const genreExpandButton = ref<HTMLButtonElement | null>(null)
const areGenresExpanded = ref(false)
const collapsedGenreCount = ref(Number.POSITIVE_INFINITY)
const genreActionNeedsRow = ref(false)
const isCalendarOpen = ref(false)
const catalogDatePicker = ref<{ closeMenu: () => void } | null>(null)
let previousScrollStyles: { overflow: string; scrollbarGutter: string } | null =
  null
const catalog = ref<MoviesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
let requestId = 0
let isMounted = false
let isInitializing = false
let scrollAfterLoad = false
let lastLoadKey = ''

const totalPages = computed(() =>
  Math.max(1, Math.ceil((catalog.value?.total ?? 0) / PAGE_SIZE)),
)
const draftError = computed(() =>
  movieCatalogDraftError(draftFilters.value, todayDate.value),
)
const hasAppliedAdvancedFilters = computed(() =>
  hasMovieCatalogFilters(appliedFilters.value),
)
const availableGenres = computed(() =>
  normalizeMovieGenres([
    ...(catalog.value?.available_genres ?? []),
    ...draftFilters.value.genres,
    ...appliedFilters.value.genres,
  ]),
)
const filteredGenres = computed(() => {
  const normalize = (value: string) =>
    value
      .normalize('NFD')
      .replace(/\p{Diacritic}/gu, '')
      .toLocaleLowerCase('fr-FR')
  const search = normalize(genreSearch.value.trim())
  return availableGenres.value.filter((genre) =>
    normalize(genre).includes(search),
  )
})
const isGenreSearchActive = computed(() => Boolean(genreSearch.value.trim()))
const hasMoreGenres = computed(
  () => collapsedGenreCount.value < filteredGenres.value.length,
)
const visibleGenreCount = computed(() =>
  areGenresExpanded.value || isGenreSearchActive.value
    ? filteredGenres.value.length
    : collapsedGenreCount.value,
)

function measureGenreRows() {
  const container = genreChips.value
  const button = genreExpandButton.value
  if (!container || !button || !container.clientWidth) return
  const widths = [
    ...container.querySelectorAll<HTMLElement>('[data-genre-chip]'),
  ].map((chip) => chip.getBoundingClientRect().width)
  const gap = Number.parseFloat(getComputedStyle(container).columnGap) || 0
  const availableWidth = container.clientWidth
  let row = 1
  let usedWidth = 0
  let count = 0
  const firstRowCount = () => {
    let used = 0
    let items = 0
    for (const width of widths) {
      const next = used ? used + gap + width : width
      if (next > availableWidth + 0.5) break
      used = next
      items++
    }
    return items
  }
  const firstCount = firstRowCount()
  for (const width of widths) {
    const next = usedWidth ? usedWidth + gap + width : width
    if (next > availableWidth + 0.5) {
      row++
      usedWidth = width
    } else usedWidth = next
    if (row > 2) break
    count++
  }
  if (count === widths.length) {
    collapsedGenreCount.value = count
    genreActionNeedsRow.value = false
    return
  }
  const buttonWidth = Number.parseFloat(getComputedStyle(button).minWidth)
  // Leave room for the action at the end of the second row.
  usedWidth = widths
    .slice(firstCount, count)
    .reduce((total, width, index) => total + width + (index ? gap : 0), 0)
  while (
    count > firstCount &&
    usedWidth + gap + buttonWidth > availableWidth + 0.5
  ) {
    count--
    usedWidth -= widths[count]! + (count > firstCount ? gap : 0)
  }
  collapsedGenreCount.value = count
  genreActionNeedsRow.value = count === firstCount
}

watch(filteredGenres, () => measureGenreRows(), { flush: 'post' })
watch(
  genreChips,
  (container, _previous, onCleanup) => {
    if (!container) return
    let measuredWidth = 0
    let resizeFrame = 0
    const observer = new ResizeObserver(([entry]) => {
      if (!entry || entry.contentRect.width === measuredWidth) return
      measuredWidth = entry.contentRect.width
      cancelAnimationFrame(resizeFrame)
      resizeFrame = requestAnimationFrame(measureGenreRows)
    })
    observer.observe(container)
    let active = true
    void document.fonts.ready.then(() => {
      if (active) measureGenreRows()
    })
    onCleanup(() => {
      active = false
      cancelAnimationFrame(resizeFrame)
      observer.disconnect()
    })
  },
  { flush: 'post' },
)

const advancedFilterCount = computed(
  () =>
    appliedFilters.value.genres.length +
    Number(Boolean(appliedFilters.value.allTheaters)) +
    Number(Boolean(appliedFilters.value.duration)) +
    Number(Boolean(appliedFilters.value.date)),
)
const advancedFiltersButtonLabel = computed(() => {
  const action = isAdvancedFiltersOpen.value ? 'Masquer' : 'Afficher'
  const count = advancedFilterCount.value
  return `${action} les filtres avancés${count ? ` (${count} actif${count > 1 ? 's' : ''})` : ''}`
})
const minPickerDate = computed(
  () => dateFromCalendarDate(todayDate.value) ?? new Date(),
)
const singlePickerDate = computed<Date | null>({
  get: () => dateFromCalendarDate(draftFilters.value.customDate),
  set: (value) => {
    draftFilters.value.customDate = value ? calendarDateFromDate(value) : ''
  },
})
const rangePickerDates = computed<Date[] | null>({
  get: () => {
    const start = dateFromCalendarDate(draftFilters.value.rangeStart)
    const end = dateFromCalendarDate(draftFilters.value.rangeEnd)
    return start && end ? [start, end] : null
  },
  set: (value) => {
    draftFilters.value.rangeStart = value?.[0]
      ? calendarDateFromDate(value[0])
      : ''
    draftFilters.value.rangeEnd = value?.[1]
      ? calendarDateFromDate(value[1])
      : ''
  },
})
const datePickerFormats = { input: 'dd-MM-yy' }
const datePickerTextInput = {
  format: 'dd-MM-yy',
  rangeSeparator: ' au ',
  enterSubmit: true,
  tabSubmit: true,
  applyOnBlur: true,
}
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
  day: ({ value }: { value: Date }) =>
    `Choisir le ${formatShortCalendarDate(calendarDateFromDate(value))}`,
}
const appliedFilterSummary = computed(() => {
  const filters = appliedFilters.value
  const items: string[] = []
  if (filters.allTheaters) items.push('cinémas : tous les cinémas')
  if (filters.genres.length) items.push(`genres : ${filters.genres.join(', ')}`)
  const duration = DURATION_OPTIONS.find(
    (option) => option.value === filters.duration,
  )
  if (duration)
    items.push(`durée : ${duration.label.toLocaleLowerCase('fr-FR')}`)
  if (filters.date === 'today') items.push('séances : aujourd’hui')
  else if (filters.date === 'tomorrow') items.push('séances : demain')
  else if (filters.date === 'weekend') items.push('séances : ce week-end')
  else if (filters.date)
    items.push(
      filters.dateTo
        ? `séances : du ${formatShortCalendarDate(filters.date)} au ${formatShortCalendarDate(filters.dateTo)}`
        : `séances : le ${formatShortCalendarDate(filters.date)}`,
    )
  return items
})

function usesCinemaPreferences() {
  return !appliedFilters.value.allTheaters
}

function buildMoviesQuery(theaters?: string): MoviesQuery {
  const filterQuery = serializeMovieCatalogFilters(appliedFilters.value)
  const includeEnded = Boolean(appliedSearch.value)
  return {
    ...(includeEnded ? { include_ended: true } : { currently_screened: true }),
    theaters,
    search: appliedSearch.value || undefined,
    genres: filterQuery.genres,
    duration: appliedFilters.value.duration,
    date: filterQuery.date,
    date_to: filterQuery.date_to,
    sort: sort.value,
    page: page.value,
    page_size: PAGE_SIZE,
  }
}

async function loadMovies() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  if (discovery.blocked.value) {
    catalog.value = null
    return
  }

  if (usesCinemaPreferences() && preferences.error.value) {
    catalog.value = null
    errorMessage.value = preferences.error.value
    pending.value = false
    return
  }
  if (usesCinemaPreferences() && !preferences.isInitialized.value) {
    catalog.value = null
    return
  }
  const theaterIds =
    !usesCinemaPreferences() ||
    isBroadTheaterSelection(
      preferences.favoriteTheaterIds.value,
      preferences.theaters.value,
    )
      ? undefined
      : preferences.favoriteTheaterIds.value.join(',')

  try {
    const response = await discovery.movies(buildMoviesQuery(theaterIds))
    if (currentRequest === requestId) {
      const lastPage = Math.max(1, Math.ceil(response.total / PAGE_SIZE))
      if (page.value > lastPage) {
        const query = filmQuery({
          search: appliedSearch.value,
          page: lastPage,
          sort: sort.value,
          filters: appliedFilters.value,
        })
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

async function retryMovies() {
  pending.value = true
  errorMessage.value = ''
  if (usesCinemaPreferences()) await preferences.retrySynchronization()
  if (usesCinemaPreferences() && !preferences.isInitialized.value) {
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
    ...filters,
  })
}

function hydrateRoute() {
  todayDate.value = todayInParis()
  const rawSearch = singularQueryValue(route.query.q)
  const nextSearch = rawSearch?.trim() ?? ''
  const nextSort =
    enumQueryValue(
      singularQueryValue(route.query.sort),
      movieCatalogSortValues,
    ) ?? DEFAULT_SORT
  const nextPage =
    positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1
  const nextFilters = parseMovieCatalogFilters(route.query, todayDate.value)
  appliedSearch.value = nextSearch
  sort.value = nextSort
  draftSort.value = nextSort
  page.value = nextPage
  appliedFilters.value = nextFilters
  draftFilters.value = movieCatalogFilterDraft(nextFilters, todayDate.value)
  return filmQuery({
    search: nextSearch,
    page: nextPage,
    sort: nextSort,
    filters: nextFilters,
  })
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  const theaterKey = usesCinemaPreferences()
    ? preferences.favoriteTheaterIds.value.join(',')
    : ''
  const key = `${appliedSearch.value}|${sort.value}|${page.value}|${movieCatalogFiltersKey(appliedFilters.value)}|${theaterKey}`
  if (key === lastLoadKey) return
  lastLoadKey = key
  await loadMovies()
}

function submitSearch(nextSearch: string) {
  const query = filmQuery({
    search: nextSearch,
    page: 1,
    sort: sort.value,
    filters: appliedFilters.value,
  })
  if (queriesEqual(route.query, query)) {
    if (errorMessage.value) loadMovies()
    return
  }
  router.replace({ query })
}

async function openAdvancedFilters() {
  if (isAdvancedFiltersOpen.value) return
  genreSearch.value = ''
  areGenresExpanded.value = false
  isAdvancedFiltersOpen.value = true
  await nextTick()
  if (!isAdvancedFiltersOpen.value || !advancedFiltersDialog.value) return
  const root = document.documentElement
  previousScrollStyles = {
    overflow: root.style.overflow,
    scrollbarGutter: root.style.scrollbarGutter,
  }
  // Reserve the existing scrollbar space so the catalogue stays in place.
  if (window.innerWidth > root.clientWidth)
    root.style.scrollbarGutter = 'stable'
  root.style.overflow = 'hidden'
  advancedFiltersDialog.value.showModal()
  measureGenreRows()
  advancedFiltersCloseButton.value?.focus({ preventScroll: true })
}

function closeAdvancedFilters({ restoreFocus = true } = {}) {
  if (!isAdvancedFiltersOpen.value) return
  advancedFiltersDialog.value?.close()
  isAdvancedFiltersOpen.value = false
  isCalendarOpen.value = false
  if (previousScrollStyles) {
    Object.assign(document.documentElement.style, previousScrollStyles)
    previousScrollStyles = null
  }
  if (restoreFocus)
    nextTick(() => advancedFiltersTrigger.value?.focus({ preventScroll: true }))
}

function trapAdvancedFiltersFocus(event: KeyboardEvent) {
  const elements = [
    ...(advancedFiltersDialog.value?.querySelectorAll<HTMLElement>(
      'button, input, select, textarea, a[href], [tabindex="0"]',
    ) ?? []),
  ].filter(
    (element) =>
      !element.hasAttribute('disabled') && element.getClientRects().length > 0,
  )
  const first = elements[0]
  const last = elements.at(-1)
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last?.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first?.focus()
  }
}

function handleCalendarEscape(event: KeyboardEvent) {
  if (!isCalendarOpen.value) return
  event.preventDefault()
  event.stopPropagation()
  catalogDatePicker.value?.closeMenu()
}

function cancelAdvancedFilters() {
  if (isCalendarOpen.value) {
    catalogDatePicker.value?.closeMenu()
    return
  }
  closeAdvancedFilters()
}

function selectDateMode(mode: MovieDateMode) {
  draftFilters.value.dateMode = mode
  if (
    mode === 'custom' &&
    (!draftFilters.value.customDate ||
      draftFilters.value.customDate < todayDate.value)
  ) {
    draftFilters.value.customDate = todayDate.value
  }
  if (mode === 'range') {
    if (
      !draftFilters.value.rangeStart ||
      draftFilters.value.rangeStart < todayDate.value
    )
      draftFilters.value.rangeStart = todayDate.value
    if (
      !draftFilters.value.rangeEnd ||
      draftFilters.value.rangeEnd < draftFilters.value.rangeStart
    ) {
      draftFilters.value.rangeEnd = addCalendarDays(
        draftFilters.value.rangeStart,
        1,
      )
    }
  }
}

function handleSingleDateTextInput(
  _event: Event | string,
  parsedDate: Date | Array<Date | null> | null,
) {
  draftFilters.value.customDate =
    parsedDate instanceof Date ? calendarDateFromDate(parsedDate) : ''
}

function handleRangeDateTextInput(
  _event: Event | string,
  parsedDate: Date | Array<Date | null> | null,
) {
  const dates = Array.isArray(parsedDate) ? parsedDate : []
  draftFilters.value.rangeStart =
    dates[0] instanceof Date ? calendarDateFromDate(dates[0]) : ''
  draftFilters.value.rangeEnd =
    dates[1] instanceof Date ? calendarDateFromDate(dates[1]) : ''
}

async function applyAdvancedFilters() {
  const filters = movieCatalogFiltersFromDraft(
    draftFilters.value,
    todayDate.value,
  )
  if (!filters) return
  const query = filmQuery({
    search: appliedSearch.value,
    page: 1,
    sort: draftSort.value,
    filters,
  })
  closeAdvancedFilters()
  if (queriesEqual(route.query, query)) {
    if (errorMessage.value) await loadMovies()
    return
  }
  await router.replace({ query })
}

function clearAdvancedFilters() {
  const query = filmQuery({
    search: appliedSearch.value,
    page: 1,
    sort: sort.value,
    filters: EMPTY_FILTERS,
  })
  if (queriesEqual(route.query, query)) {
    appliedFilters.value = { genres: [] }
    draftSort.value = sort.value
    draftFilters.value = movieCatalogFilterDraft(EMPTY_FILTERS, todayDate.value)
    return
  }
  router.replace({ query })
}

function followPageLink(event: MouseEvent, nextPage: number) {
  if (pending.value) {
    event.preventDefault()
    return
  }
  if (nextPage < 1 || nextPage > totalPages.value || nextPage === page.value)
    return
  scrollAfterLoad = true
}

hydrateRoute()
const initialCatalogKey = `films-catalog:${encodeURIComponent(appliedSearch.value)}:${sort.value}:${page.value}:${encodeURIComponent(movieCatalogFiltersKey(appliedFilters.value))}`
const initialResult = await useAsyncData(
  initialCatalogKey,
  async () => {
    try {
      const response = await api.movies(buildMoviesQuery())
      return { kind: 'success' as const, catalog: response, errorMessage: '' }
    } catch (error) {
      return {
        kind: 'upstream-error' as const,
        catalog: null,
        errorMessage: getFrenchApiError(error),
      }
    }
  },
  // Hydrate SSR data, but leave client navigation to the personalized loader.
  { immediate: import.meta.server },
)

const initialState = initialResult.data.value
catalog.value = initialState?.catalog ?? null
errorMessage.value = initialState?.errorMessage ?? ''
pending.value = !initialState
if (import.meta.server && initialState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

watch(
  () => route.query,
  () => {
    if (isMounted && !isInitializing) applyRoute()
  },
)
watch(
  [
    preferences.favoriteTheaterIds,
    preferences.selectionScopeKey,
    preferences.isInitialized,
    preferences.error,
  ],
  () => {
    if (!usesCinemaPreferences()) return
    requestId++
    catalog.value = null
    lastLoadKey = ''
    if (isMounted && !isInitializing) void loadMovies()
  },
  { flush: 'sync' },
)
watch(discovery.key, () => {
  if (isMounted && !isInitializing) {
    lastLoadKey = ''
    void loadMovies()
  }
})
onMounted(async () => {
  isMounted = true
  isInitializing = true
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery))
    await router.replace({ query: canonicalQuery })
  const preferenceInitialization = preferences.initialize()
  isInitializing = false
  await applyRoute()
  await preferenceInitialization
})
onBeforeUnmount(() => {
  closeAdvancedFilters({ restoreFocus: false })
  isMounted = false
  requestId++
})

const config = useRuntimeConfig()
const socialImageUrl = absoluteSiteUrl(
  config.public.siteUrl,
  '/pwa-512x512.png',
)
const pageDescription =
  'Parcourez les films actuellement au cinéma, recherchez un titre et consultez toutes les séances disponibles.'
const rawQueryKeys = computed(() => Object.keys(route.query))
const isPageOnlyQuery = computed(
  () => rawQueryKeys.value.length === 1 && rawQueryKeys.value[0] === 'page',
)
const rawPage = computed(() => singularQueryValue(route.query.page))
const normalizedCanonicalPage = computed(() => {
  if (!isPageOnlyQuery.value) return 1
  const parsed = positiveSafeInteger(rawPage.value) ?? 1
  return Math.min(parsed, totalPages.value)
})
const canonicalUrl = computed(() =>
  absoluteSiteUrl(
    config.public.siteUrl,
    normalizedCanonicalPage.value >= 2
      ? `/films?page=${normalizedCanonicalPage.value}`
      : '/films',
  ),
)
const isCanonicalPageQuery = computed(() => {
  const value = rawPage.value
  return (
    isPageOnlyQuery.value &&
    value !== undefined &&
    /^(?:[2-9]|[1-9]\d+)$/.test(value) &&
    positiveSafeInteger(value) === normalizedCanonicalPage.value
  )
})
const isIndexable = computed(
  () =>
    Boolean(catalog.value) &&
    !errorMessage.value &&
    (rawQueryKeys.value.length === 0 || isCanonicalPageQuery.value),
)
const pageTitle = computed(() =>
  normalizedCanonicalPage.value >= 2
    ? `Films à l’affiche - Page ${normalizedCanonicalPage.value} - MesSeances`
    : 'Films à l’affiche - MesSeances',
)
const filmsJsonLd = computed(() => {
  if (pending.value || !isIndexable.value || !catalog.value?.items.length)
    return null
  return serializeJsonLd({
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'ItemList',
        '@id': `${canonicalUrl.value}#film-list`,
        itemListElement: catalog.value.items.map((movie, index) => ({
          '@type': 'ListItem',
          position: index + 1,
          url: absoluteSiteUrl(
            config.public.siteUrl,
            `/film/${encodeURIComponent(movie.slug)}`,
          ),
        })),
      },
    ],
  })
})

useSeoMeta({
  robots: computed(() =>
    isIndexable.value ? 'index,follow' : 'noindex,follow',
  ),
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
  twitterImage: socialImageUrl,
})
useHead(() => ({
  link: [{ rel: 'canonical', href: canonicalUrl.value }],
  script: filmsJsonLd.value
    ? [
        {
          key: 'films-jsonld',
          type: 'application/ld+json',
          innerHTML: filmsJsonLd.value,
        },
      ]
    : [],
}))
</script>

<template>
  <main
    id="films-catalog"
    :aria-busy="pending"
    class="catalog-page bg-[#f8f7f2] text-ink"
  >
    <FilmCatalogTabs active="showing" />
    <section
      class="border-b-2 border-ink bg-surface"
      aria-labelledby="catalog-title"
    >
      <div
        class="relative mx-auto max-w-[1440px] overflow-hidden px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12"
      >
        <p
          class="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-muted"
        >
          Catalogue · En salle
        </p>
        <h1
          id="catalog-title"
          class="mt-4 max-w-6xl [font-family:'Noto_Sans_Variable',sans-serif] text-[clamp(4rem,11.7vw,10.5rem)] font-black uppercase leading-[0.85] tracking-[-0.085em]"
        >
          Films<span class="text-primary">.</span>
        </h1>
        <span
          class="absolute right-[8%] bottom-[20%] aspect-square w-[clamp(2.25rem,5vw,4.5rem)] rotate-[8deg] border-2 border-ink bg-highlight shadow-[5px_5px_0_#27272a] max-sm:right-5 max-sm:bottom-6"
          aria-hidden="true"
        ></span>
      </div>
    </section>

    <section
      class="border-b-2 border-ink bg-[#f8f7f2] bg-[linear-gradient(rgba(39,39,42,0.075)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.075)_1px,transparent_1px)] bg-[size:28px_28px]"
    >
      <div
        class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12"
      >
        <div
          class="search-workspace border-2 border-ink bg-[#ffcf3f] p-4 shadow-[7px_7px_0_#27272a] sm:p-6"
        >
          <MovieCatalogControls
            :search="appliedSearch"
            :sort="sort"
            :show-sort="false"
            :pending="pending"
            input-id="film-search"
            @search="submitSearch"
          >
            <template #actions>
              <button
                ref="advancedFiltersTrigger"
                type="button"
                class="relative inline-flex size-[3.25rem] items-center justify-center border-2 border-ink bg-surface text-ink transition-[background-color,color,box-shadow] duration-150 hover:bg-ink hover:text-white focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink aria-expanded:bg-ink aria-expanded:text-white motion-reduce:transition-none [&>svg]:motion-reduce:transition-none"
                :class="{ 'not-aria-expanded:shadow-[inset_0_-4px_0_var(--color-highlight)]': advancedFilterCount > 0 }"
                :aria-label="advancedFiltersButtonLabel"
                aria-controls="advanced-film-filters"
                aria-haspopup="dialog"
                :aria-expanded="isAdvancedFiltersOpen"
                @click="openAdvancedFilters"
                @keydown.esc.stop.prevent="closeAdvancedFilters()"
              >
                <SlidersHorizontal
                  :size="20"
                  stroke-width="2.5"
                  aria-hidden="true"
                />
                <span
                  v-if="advancedFilterCount"
                  class="absolute -top-[0.6rem] -right-[0.6rem] inline-flex h-[1.4rem] min-w-[1.4rem] items-center justify-center border-2 border-ink bg-highlight px-[0.2rem] font-mono text-[0.65rem] leading-none font-black text-ink"
                  aria-hidden="true"
                  >{{
                    advancedFilterCount
                  }}</span
                >
              </button>
            </template>
          </MovieCatalogControls>

          <dialog
            v-if="isAdvancedFiltersOpen"
            id="advanced-film-filters"
            ref="advancedFiltersDialog"
            class="fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none overflow-hidden border-0 bg-transparent p-0 text-ink backdrop:bg-black/60 open:flex open:items-end open:justify-end lg:open:items-stretch"
            aria-labelledby="advanced-film-filters-title"
            @cancel.prevent="cancelAdvancedFilters"
            @keydown.esc.capture="handleCalendarEscape"
            @keydown.tab="trapAdvancedFiltersFocus"
            @click.self="closeAdvancedFilters()"
          >
            <form
              class="flex h-[calc(100dvh-1rem)] min-h-0 w-full flex-col overflow-hidden border-2 border-b-0 border-ink bg-[#f8f7f2] shadow-[0_-8px_0_#27272a] lg:h-full lg:max-h-none lg:w-[32rem] lg:border-y-0 lg:border-r-0 lg:shadow-[-8px_0_0_#27272a]"
              @submit.prevent="applyAdvancedFilters"
            >
              <header
                class="flex shrink-0 items-center gap-3 border-b-2 border-ink bg-[#ffcf3f] px-4 py-4 sm:px-6"
              >
                <SlidersHorizontal :size="20" aria-hidden="true" />
                <h2
                  id="advanced-film-filters-title"
                  class="text-xl font-black tracking-[-0.035em]"
                >
                  Filtres avancés
                </h2>
                <button
                  ref="advancedFiltersCloseButton"
                  type="button"
                  class="ml-auto grid size-11 shrink-0 place-items-center border-2 border-ink bg-surface hover:bg-highlight focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                  aria-label="Fermer les filtres"
                  @click="closeAdvancedFilters()"
                >
                  <X :size="20" aria-hidden="true" />
                </button>
              </header>
              <div
                class="min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain px-4 py-6 sm:px-6"
              >
                <label class="block min-w-0">
                  <span
                    class="mb-3 block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                    >Trier par</span
                  >
                  <select
                    v-model="draftSort"
                    class="h-[3.25rem] w-full rounded-none border-2 border-ink bg-surface px-3 text-sm font-bold text-ink focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                  >
                    <option
                      v-for="option in movieCatalogSortOptions"
                      :key="option.value"
                      :value="option.value"
                    >
                      {{ option.label }}
                    </option>
                  </select>
                </label>
                <fieldset class="min-w-0" aria-labelledby="film-genres-title">
                  <legend class="mb-3 w-full">
                    <span
                      class="flex min-h-8 items-center justify-between gap-3"
                    >
                      <span
                        id="film-genres-title"
                        class="font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                        >Genres</span
                      >
                      <button
                        v-if="draftFilters.genres.length"
                        type="button"
                        class="min-h-8 text-xs font-bold underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                        @click="draftFilters.genres = []"
                      >
                        Tout désélectionner
                      </button>
                    </span>
                  </legend>
                  <div v-if="availableGenres.length" class="space-y-3">
                    <div class="relative">
                      <Search
                        :size="17"
                        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted"
                        aria-hidden="true"
                      />
                      <input
                        v-model="genreSearch"
                        type="search"
                        aria-label="Rechercher un genre"
                        placeholder="Rechercher un genre"
                        class="min-h-11 w-full border-2 border-ink bg-surface py-2 pl-10 pr-3 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                      >
                    </div>
                    <div
                      id="film-genre-options"
                      ref="genreChips"
                      class="relative flex flex-wrap gap-2"
                    >
                      <label
                        v-for="(genre, index) in filteredGenres"
                        :key="genre"
                        data-genre-chip
                        class="w-max max-w-full cursor-pointer"
                        :class="index < visibleGenreCount ? 'relative' : 'invisible absolute top-0 left-0 pointer-events-none'"
                        :inert="index >= visibleGenreCount"
                        :aria-hidden="index >= visibleGenreCount ? 'true' : undefined"
                      >
                        <input
                          v-model="draftFilters.genres"
                          type="checkbox"
                          :value="genre"
                          class="peer sr-only"
                        >
                        <span
                          class="inline-flex min-h-11 items-center border-2 border-ink bg-surface px-3 py-2 text-[0.82rem] font-bold transition-colors hover:bg-[#e8e6de] peer-checked:bg-ink peer-checked:text-white peer-checked:shadow-[inset_0_-4px_0_var(--color-highlight)] peer-focus-visible:outline-2 peer-focus-visible:outline-offset-3 peer-focus-visible:outline-ink motion-reduce:transition-none"
                        >
                          {{ genre }}
                        </span>
                      </label>
                      <button
                        ref="genreExpandButton"
                        type="button"
                        class="min-h-11 min-w-20 px-1 text-sm font-bold underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                        :class="[
                          hasMoreGenres && !isGenreSearchActive ? 'relative' : 'invisible absolute top-0 left-0 pointer-events-none',
                          !areGenresExpanded && genreActionNeedsRow ? 'basis-full text-right' : '',
                        ]"
                        :inert="!hasMoreGenres || isGenreSearchActive"
                        :aria-hidden="!hasMoreGenres || isGenreSearchActive ? 'true' : undefined"
                        :aria-expanded="areGenresExpanded"
                        aria-controls="film-genre-options"
                        @click="areGenresExpanded = !areGenresExpanded"
                      >
                        {{ areGenresExpanded ? 'Voir moins' : 'Voir plus' }}
                      </button>
                    </div>
                    <p
                      v-if="!filteredGenres.length"
                      class="py-2 text-sm text-muted"
                      role="status"
                    >
                      Aucun genre ne correspond à votre recherche.
                    </p>
                  </div>
                  <p v-else class="text-sm text-muted">
                    Aucun genre disponible.
                  </p>
                </fieldset>

                <div>
                  <fieldset>
                    <legend
                      class="mb-3 block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                    >
                      Durée
                    </legend>
                    <div
                      class="grid grid-cols-4 border-2 border-ink bg-surface"
                    >
                      <label
                        v-for="option in [{ value: '', label: 'Toutes les durées', shortLabel: 'Toutes' }, ...DURATION_OPTIONS]"
                        :key="option.value"
                        class="relative min-w-0 cursor-pointer border-r-2 border-ink last:border-r-0"
                      >
                        <input
                          v-model="draftFilters.duration"
                          type="radio"
                          name="film-duration"
                          :value="option.value"
                          :aria-label="option.label"
                          class="peer sr-only"
                        >
                        <span
                          class="flex min-h-12 items-center justify-center whitespace-nowrap px-1 py-3 font-mono text-[0.65rem] font-black uppercase tracking-[0.02em] hover:bg-[#e8e6de] peer-checked:bg-ink peer-checked:text-white peer-checked:shadow-[inset_0_-4px_0_var(--color-highlight)] peer-focus-visible:outline-2 peer-focus-visible:-outline-offset-4 peer-focus-visible:outline-highlight sm:text-xs"
                          >{{
                            option.shortLabel
                          }}</span
                        >
                      </label>
                    </div>
                  </fieldset>
                  <fieldset class="mt-6">
                    <legend
                      class="mb-3 block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                    >
                      Cinémas
                    </legend>
                    <div
                      class="grid grid-cols-2 border-2 border-ink bg-surface"
                    >
                      <label
                        v-for="option in [{ value: false, label: 'Cinémas sélectionnés' }, { value: true, label: 'Tous les cinémas' }]"
                        :key="String(option.value)"
                        class="relative min-w-0 cursor-pointer border-r-2 border-ink last:border-r-0"
                      >
                        <input
                          v-model="draftFilters.allTheaters"
                          type="radio"
                          name="film-theaters"
                          :value="option.value"
                          class="peer sr-only"
                        >
                        <span
                          class="flex min-h-12 items-center justify-center px-2 py-3 text-center font-mono text-[0.65rem] font-black uppercase tracking-[0.02em] hover:bg-[#e8e6de] peer-checked:bg-ink peer-checked:text-white peer-checked:shadow-[inset_0_-4px_0_var(--color-highlight)] peer-focus-visible:outline-2 peer-focus-visible:-outline-offset-4 peer-focus-visible:outline-highlight sm:text-xs"
                          >{{
                            option.label
                          }}</span
                        >
                      </label>
                    </div>
                  </fieldset>
                </div>

                <fieldset
                  class="min-w-0"
                  :aria-invalid="draftError ? 'true' : undefined"
                  :aria-describedby="draftError ? 'film-date-error' : undefined"
                >
                  <legend
                    class="mb-3 block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                  >
                    Date de séance
                  </legend>
                  <div
                    class="grid grid-cols-2 gap-0.5 border-2 border-ink bg-ink sm:grid-cols-3"
                  >
                    <label
                      v-for="option in DATE_OPTIONS"
                      :key="option.value"
                      class="relative min-w-0 cursor-pointer bg-surface"
                    >
                      <input
                        type="radio"
                        name="film-date-mode"
                        :value="option.value"
                        :checked="draftFilters.dateMode === option.value"
                        class="peer sr-only"
                        @change="selectDateMode(option.value)"
                      >
                      <span
                        class="flex min-h-12 items-center justify-center px-2 py-3 text-center font-mono text-[0.65rem] font-black uppercase tracking-[0.02em] hover:bg-[#e8e6de] peer-checked:bg-ink peer-checked:text-white peer-checked:shadow-[inset_0_-4px_0_var(--color-highlight)] peer-focus-visible:outline-2 peer-focus-visible:-outline-offset-4 peer-focus-visible:outline-highlight sm:text-xs"
                        >{{
                          option.label
                        }}</span
                      >
                    </label>
                  </div>

                  <div v-if="draftFilters.dateMode === 'custom'" class="mt-3">
                    <label
                      for="film-custom-date"
                      class="mb-2 block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                      >Date précise</label
                    >
                    <DeferredVueDatePicker
                      v-model="singlePickerDate"
                      ref="catalogDatePicker"
                      active
                      class="catalog-datepicker"
                      :aria-labels="calendarAriaLabels"
                      :formats="datePickerFormats"
                      placeholder="JJ-MM-AA"
                      :input-attrs="{ id: 'film-custom-date', autocomplete: 'off', clearable: true }"
                      :locale="fr"
                      :min-date="minPickerDate"
                      :text-input="datePickerTextInput"
                      :time-config="{ enableTimePicker: false }"
                      :transitions="false"
                      :floating="{ arrow: false, offset: 6 }"
                      :ui="{ menu: 'catalog-calendar-menu' }"
                      teleport="#advanced-film-filters"
                      @open="isCalendarOpen = true"
                      @closed="isCalendarOpen = false"
                      auto-apply
                      arrow-navigation
                      prevent-min-max-navigation
                      @text-input="handleSingleDateTextInput"
                      @cleared="draftFilters.customDate = ''"
                    >
                      <template #input-icon
                        ><CalendarDays
                          :size="18"
                          aria-hidden="true"
                        /></template
                      >
                    </DeferredVueDatePicker>
                  </div>

                  <div
                    v-else-if="draftFilters.dateMode === 'range'"
                    class="mt-3"
                  >
                    <label
                      for="film-custom-range"
                      class="mb-2 block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]"
                      >Période</label
                    >
                    <DeferredVueDatePicker
                      v-model="rangePickerDates"
                      ref="catalogDatePicker"
                      active
                      class="catalog-datepicker"
                      :aria-labels="calendarAriaLabels"
                      :formats="datePickerFormats"
                      placeholder="JJ-MM-AA au JJ-MM-AA"
                      :input-attrs="{ id: 'film-custom-range', autocomplete: 'off', clearable: true }"
                      :locale="fr"
                      :min-date="minPickerDate"
                      :range="{ partialRange: false, autoSwitchStartEnd: false }"
                      :text-input="datePickerTextInput"
                      :time-config="{ enableTimePicker: false }"
                      :transitions="false"
                      :floating="{ arrow: false, offset: 6 }"
                      :ui="{ menu: 'catalog-calendar-menu' }"
                      teleport="#advanced-film-filters"
                      @open="isCalendarOpen = true"
                      @closed="isCalendarOpen = false"
                      auto-apply
                      arrow-navigation
                      prevent-min-max-navigation
                      @text-input="handleRangeDateTextInput"
                      @cleared="draftFilters.rangeStart = ''; draftFilters.rangeEnd = ''"
                    >
                      <template #input-icon
                        ><CalendarDays
                          :size="18"
                          aria-hidden="true"
                        /></template
                      >
                    </DeferredVueDatePicker>
                  </div>
                  <p
                    v-if="draftError"
                    id="film-date-error"
                    class="mt-2 text-sm font-bold text-red-800"
                    role="alert"
                  >
                    {{ draftError }}
                  </p>
                </fieldset>
              </div>

              <footer
                class="sticky bottom-0 z-10 mt-auto flex shrink-0 flex-col-reverse gap-3 border-t-2 border-ink bg-[#f8f7f2] px-4 pt-4 pb-[calc(1rem+env(safe-area-inset-bottom))] sm:flex-row sm:px-6 [&>button]:flex-1"
              >
                <button
                  type="button"
                  class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-surface px-4 py-[0.65rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] hover:bg-primary hover:text-white focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink motion-reduce:transition-none"
                  @click="clearAdvancedFilters"
                >
                  Effacer les filtres
                </button>
                <button
                  type="submit"
                  class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-ink px-4 py-[0.65rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] text-white enabled:hover:bg-primary focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-55 motion-reduce:transition-none"
                  :disabled="Boolean(draftError)"
                >
                  Appliquer
                </button>
              </footer>
            </form>
          </dialog>
        </div>

        <div id="tous-les-films" class="scroll-mt-4" aria-hidden="true"></div>

        <div
          v-if="catalog && !pending && !discovery.blocked.value"
          class="results-bar mt-6 flex items-center justify-between gap-3 border-b-2 border-ink py-4 sm:items-end sm:gap-6"
        >
          <h2
            class="min-w-0 flex-1 text-xl font-black tracking-[-0.035em] max-sm:truncate sm:text-2xl"
          >
            {{
              appliedSearch ? `Résultats pour « ${appliedSearch} »` : 'Tous les films'
            }}
          </h2>
          <div class="flex shrink-0 items-center gap-3 sm:justify-end">
            <p
              class="font-mono text-[11px] font-bold uppercase tracking-[0.14em]"
            >
              {{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}
            </p>
            <ShareButton class="shrink-0" />
          </div>
        </div>

        <WatchedReadiness v-if="discovery.blocked.value" />
        <EditorialStatePanel
          v-else-if="pending"
          semantic="status"
          live="polite"
          size="tall"
          shadow="large"
          class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon
            ><LoaderCircle
              :size="34"
              class="animate-spin"
              aria-hidden="true"
            /></template
          >
          <p>Chargement des films…</p>
        </EditorialStatePanel>

        <EditorialStatePanel
          v-else-if="errorMessage"
          semantic="alert"
          size="tall"
          shadow="large"
          class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon
            ><AlertTriangle
              :size="34"
              class="text-primary"
              aria-hidden="true"
            /></template
          >
          <p class="max-w-lg">{{ errorMessage }}</p>
          <template #actions
            ><button
              type="button"
              class="inline-flex min-h-[3.25rem] items-center justify-center gap-[0.55rem] border-2 border-ink bg-ink px-4 font-mono text-[0.68rem] font-extrabold uppercase tracking-[0.08em] text-white transition-[background-color] duration-150 hover:bg-primary motion-reduce:transition-none"
              @click="retryMovies"
            >
              <RefreshCw :size="17" aria-hidden="true" />
              Réessayer
            </button></template
          >
        </EditorialStatePanel>

        <EditorialStatePanel
          v-else-if="!catalog?.items.length"
          size="tall"
          shadow="large"
          class="catalog-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon><Film :size="36" aria-hidden="true" /></template>
          <p v-if="hasAppliedAdvancedFilters">
            Aucun film ne correspond à ces filtres :
            {{ appliedFilterSummary.join(' · ') }}.
          </p>
          <p v-else>
            {{
              appliedSearch ? 'Aucun film ne correspond à cette recherche.' : 'Aucun film à l’affiche actuellement.'
            }}
          </p>
          <template v-if="hasAppliedAdvancedFilters" #actions
            ><button
              type="button"
              class="inline-flex min-h-[3.25rem] items-center justify-center gap-[0.55rem] border-2 border-ink bg-ink px-4 font-mono text-[0.68rem] font-extrabold uppercase tracking-[0.08em] text-white transition-[background-color] duration-150 hover:bg-primary motion-reduce:transition-none"
              @click="clearAdvancedFilters"
            >
              Effacer les filtres
            </button></template
          >
        </EditorialStatePanel>

        <template v-else>
          <ul
            class="catalog-grid mt-8 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6"
            aria-label="Films à l’affiche"
          >
            <li
              v-for="movie in catalog.items"
              :key="movie.slug"
              class="min-w-0"
            >
              <MovieCatalogCard
                :movie="movie"
                :to="`/film/${encodeURIComponent(movie.slug)}`"
              />
            </li>
          </ul>

          <MovieCatalogPagination
            :page="page"
            :total-pages="totalPages"
            :previous-to="page > 1 ? { query: filmQuery({ search: appliedSearch, page: page - 1, sort, filters: appliedFilters }) } : null"
            :next-to="page < totalPages ? { query: filmQuery({ search: appliedSearch, page: page + 1, sort, filters: appliedFilters }) } : null"
            :pending="pending"
            @navigate="followPageLink"
          />
        </template>
      </div>
    </section>
  </main>
</template>

<style scoped>
:global(.catalog-datepicker.dp--main) {
  --dp-background-color: #fff;
  --dp-border-color: #27272a;
  --dp-border-color-hover: #27272a;
  --dp-border-color-focus: #27272a;
  --dp-border-radius: 0;
  --dp-cell-border-radius: 0;
  --dp-font-family:
    ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  --dp-font-size: 0.82rem;
  --dp-icon-color: #27272a;
  --dp-primary-color: #27272a;
  --dp-primary-text-color: #fff;
  --dp-text-color: #27272a;
}

:global(.catalog-datepicker .dp--input) {
  min-height: 3.25rem;
  border: 2px solid var(--color-ink);
  border-radius: 0;
  padding: 0.75rem 2.75rem;
  font-weight: 800;
}

:global(.catalog-datepicker .dp--input-focus) {
  box-shadow: inset 0 0 0 2px var(--color-highlight);
}

:global(.catalog-datepicker .dp--input-icon),
:global(.catalog-datepicker .dp--clear-btn) {
  display: grid;
  width: 2.75rem;
  height: 2.75rem;
  place-items: center;
}

:global(.catalog-datepicker .dp--clear-btn .dp--input-icons) {
  width: 1.125rem;
  height: 1.125rem;
  padding: 0;
}

:global(.dp--menu.catalog-calendar-menu) {
  --dp-font-family:
    ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
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

:global(.catalog-calendar-menu .dp--calendar-header-item),
:global(.catalog-calendar-menu .dp--month-year-select) {
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

:global(.catalog-calendar-menu .dp--active),
:global(.catalog-calendar-menu .dp--range-between),
:global(.catalog-calendar-menu .dp--range-border-start),
:global(.catalog-calendar-menu .dp--range-border-end) {
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

:global(.catalog-calendar-menu .dp--today) {
  border: 2px solid #991b1b;
}
</style>
