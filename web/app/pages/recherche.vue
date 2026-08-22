<script setup lang="ts">
import { AlertTriangle, CalendarDays, CalendarSearch, LoaderCircle, Search, SlidersHorizontal, X } from '@lucide/vue'
import { VueDatePicker } from '@vuepic/vue-datepicker'
import { fr } from 'date-fns/locale/fr'
import '@vuepic/vue-datepicker/dist/main.css'
import MovieSlotResultGroup from '~/components/MovieSlotResultGroup.vue'
import TimeRangeSlider from '~/components/TimeRangeSlider.vue'
import type { Language, QueryFormat, SlotResult } from '~/types/api'
import { createServiceTimeOptions, formatLongDate, todayInParis } from '~/utils/date'
import { formatOptions } from '~/utils/formats'
import { calendarDate, enumQueryValue, mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import type { LocationQuery } from 'vue-router'

const OWNED_QUERY_KEYS = ['theaters', 'date', 'start_after', 'finish_before', 'language', 'format', 'include_ads', 'buffer_ads'] as const
const DISPLAY_QUERY_KEYS = ['grouping', 'layout', 'view'] as const
const REQUIRED_QUERY_KEYS = ['theaters', 'date', 'start_after', 'finish_before'] as const
const LANGUAGES: readonly Language[] = ['ALL', 'VOSTFR', 'VF']
const PARIS_TIMEZONE = 'Europe/Paris'
const DEFAULT_RANGE_STEPS = 12
type ResultGrouping = 'movie' | 'chronological'
type ResultLayout = 'lines' | 'boxes'

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const { favoriteTheaterIds, favoriteTheaters, isInitialized, isLoading, error: preferencesError, initialize } = useCinemaPreferences()

interface SearchForm {
  date: string
  startAfter: string
  finishBefore: string
  language: Language
  format: QueryFormat
  includeAds: boolean
}

const form = reactive<SearchForm>({
  date: '',
  startAfter: '12:00',
  finishBefore: '15:00',
  language: 'ALL',
  format: 'ALL',
  includeAds: true
})
const timeOptions = createServiceTimeOptions()
const validTimes = new Set(timeOptions.map((option) => option.value))
const results = ref<SlotResult[] | null>(null)
const pending = ref(false)
const errorMessage = ref('')
const searchedDate = ref('')
const theaterValidationMessage = ref('')
const appliedSearch = ref<AppliedSearch | null>(null)
const isFilterSheetOpen = ref(false)
const filterForm = ref<HTMLFormElement | null>(null)
const sheetCloseButton = ref<HTMLButtonElement | null>(null)
const modifierButton = ref<HTMLButtonElement | null>(null)
const calendarTrigger = ref<HTMLButtonElement | null>(null)
const calendarMenu = ref<HTMLElement | null>(null)
const resultsRegion = ref<HTMLElement | null>(null)
const isCalendarOpen = ref(false)
const isCompactCalendarViewport = ref(false)
const isCenteredCalendar = computed(() => isFilterSheetOpen.value && isCompactCalendarViewport.value)
const todayDate = ref(todayInParis())
const resultGrouping = computed<ResultGrouping>(() => singularQueryValue(route.query.grouping) === 'chronological' ? 'chronological' : 'movie')
const resultLayout = computed<ResultLayout>(() => singularQueryValue(route.query.layout) === 'boxes' ? 'boxes' : 'lines')
const groupingOptions: [{ value: ResultGrouping; label: string }, { value: ResultGrouping; label: string }] = [
  { value: 'movie', label: 'Par film' },
  { value: 'chronological', label: 'Chronologique' }
]
const layoutOptions: [{ value: ResultLayout; label: string }, { value: ResultLayout; label: string }] = [
  { value: 'lines', label: 'Lignes' },
  { value: 'boxes', label: 'Boîtes' }
]
const chronologicalResults = computed(() => [...(results.value ?? [])].sort((first, second) => {
  const timeDifference = Date.parse(first.showtime.start_time) - Date.parse(second.showtime.start_time)
  return timeDifference || first.showtime.id.localeCompare(second.showtime.id)
}))
const movieGroups = computed(() => {
  const groups = new Map<string, SlotResult[]>()
  for (const result of chronologicalResults.value) {
    const key = `${result.showtime.provider}:${result.showtime.movie.slug}`
    const group = groups.get(key)
    if (group) group.push(result)
    else groups.set(key, [result])
  }
  return [...groups.entries()].map(([key, slots]) => ({ key, slots }))
})
const activeFilterSummary = computed(() => {
  const search = appliedSearch.value
  if (!search) return []
  const items = [
    formatLongDate(search.date),
    `${search.startAfter.replace(':', 'h')}–${search.finishBefore.replace(':', 'h')}`,
    `${search.theaterIds.length} cinéma${search.theaterIds.length > 1 ? 's' : ''}`,
    search.includeAds ? 'Publicités incluses' : 'Publicités exclues · arrivée +20 min'
  ]
  if (search.language !== 'ALL') items.push(search.language)
  if (search.format !== 'ALL') items.push(search.format.replace('_', ' '))
  return items
})
const compactFilterSummary = computed(() => {
  const search = appliedSearch.value
  if (!search) return ''
  return `${formatCompactDate(search.date)} · ${formatCompactTime(search.startAfter)}–${formatCompactTime(search.finishBefore)} · ${search.theaterIds.length} cinéma${search.theaterIds.length > 1 ? 's' : ''}`
})
function addCalendarDays(date: string, offset: number) {
  const [year, month, day] = date.split('-').map(Number)
  if (!year || !month || !day) return date
  const value = new Date(Date.UTC(year, month - 1, day + offset, 12))
  return [value.getUTCFullYear(), String(value.getUTCMonth() + 1).padStart(2, '0'), String(value.getUTCDate()).padStart(2, '0')].join('-')
}

const availableDateOptions = computed(() => {
  const available = new Set(favoriteTheaters.value.flatMap((theater) => theater.available_dates ?? []))
  return [...available].sort()
})
const hasAvailableDates = computed(() => availableDateOptions.value.length > 0)
const tomorrowDate = computed(() => addCalendarDays(todayDate.value, 1))
const allowedDateValues = computed(() => availableDateOptions.value.map(dateFromCalendarDate).filter((date): date is Date => date !== null))
const datePickerDate = computed<Date | null>({
  get: () => dateFromCalendarDate(form.date),
  set: (value) => {
    if (!value) return
    const date = calendarDateFromDate(value)
    if (availableDateOptions.value.includes(date)) form.date = date
  }
})
const favoriteSummary = computed(() => {
  const count = favoriteTheaterIds.value.length
  return `${count} cinéma${count > 1 ? 's' : ''} favori${count > 1 ? 's' : ''} inclus`
})
const calendarAriaLabels = {
  menu: 'Calendrier des dates disponibles',
  input: 'Choisir une autre date',
  calendarIcon: 'Ouvrir le calendrier',
  prevMonth: 'Mois précédent',
  nextMonth: 'Mois suivant',
  prevYear: 'Année précédente',
  nextYear: 'Année suivante',
  openMonthsOverlay: 'Choisir un mois',
  openYearsOverlay: 'Choisir une année',
  monthPicker: (overlay: boolean) => overlay ? 'Fermer le choix du mois' : 'Ouvrir le choix du mois',
  yearPicker: (overlay: boolean) => overlay ? 'Fermer le choix de l’année' : 'Ouvrir le choix de l’année',
  day: ({ value }: { value: Date }) => `Choisir ${formatLongDate(calendarDateFromDate(value))}`
}
let isReady = false
let lastSearchKey = ''
let requestId = 0
let sheetTrigger: HTMLElement | null = null
let bodyOverflowBeforeLock: string | null = null
let mobileMediaQuery: MediaQueryList | null = null
let compactCalendarMediaQuery: MediaQueryList | null = null
let resultScrollIntent = false

interface AppliedSearch {
  theaterIds: string[]
  date: string
  startAfter: string
  finishBefore: string
  language: Language
  format: QueryFormat
  includeAds: boolean
}

function dateFromCalendarDate(value: string): Date | null {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  return new Date(year, month - 1, day, 12)
}

function calendarDateFromDate(value: Date): string {
  return [value.getFullYear(), String(value.getMonth() + 1).padStart(2, '0'), String(value.getDate()).padStart(2, '0')].join('-')
}

function isDateAvailable(date: string) {
  return availableDateOptions.value.includes(date)
}

function selectQuickDate(date: string) {
  if (isDateAvailable(date)) form.date = date
}

function parisWallMinutes(now: Date): number {
  const parts = new Intl.DateTimeFormat('en-GB', {
    timeZone: PARIS_TIMEZONE,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23'
  }).formatToParts(now)
  const hour = Number(parts.find((part) => part.type === 'hour')?.value)
  const minute = Number(parts.find((part) => part.type === 'minute')?.value)
  const second = Number(parts.find((part) => part.type === 'second')?.value)
  return (Number.isFinite(hour) ? hour : 0) * 60
    + (Number.isFinite(minute) ? minute : 0)
    + (Number.isFinite(second) ? second : 0) / 60
    + now.getMilliseconds() / 60_000
}

function serviceMinutes(value: string): number {
  const [hour, minute] = value.split(':').map(Number)
  const wallMinutes = (hour ?? 0) * 60 + (minute ?? 0)
  return wallMinutes < 8 * 60 ? wallMinutes + 24 * 60 : wallMinutes
}

function currentDefaultTimeRange(now: Date) {
  const lastIndex = timeOptions.length - 1
  const firstServiceMinute = serviceMinutes(timeOptions[0]?.value ?? '08:00')
  const wallMinutes = parisWallMinutes(now)
  const currentMinutes = wallMinutes < firstServiceMinute ? wallMinutes + 24 * 60 : wallMinutes
  let startIndex = timeOptions.findIndex((option) => serviceMinutes(option.value) >= currentMinutes)
  if (startIndex < 0) startIndex = lastIndex
  startIndex = Math.min(startIndex, Math.max(lastIndex - 1, 0))
  const endIndex = Math.min(startIndex + DEFAULT_RANGE_STEPS, lastIndex)
  return {
    start: timeOptions[startIndex]?.value ?? '12:00',
    end: timeOptions[endIndex]?.value ?? '15:00'
  }
}

function formatCompactDate(date: string) {
  const [year, month, day] = date.split('-').map(Number)
  if (!year || !month || !day) return date
  return new Intl.DateTimeFormat('fr-FR', { day: 'numeric', month: 'long', timeZone: 'Europe/Paris' }).format(new Date(Date.UTC(year, month - 1, day, 12)))
}

function formatCompactTime(time: string) {
  const [hour, minute] = time.split(':')
  return minute === '00' ? `${Number(hour)}h` : `${Number(hour)}h${minute}`
}

function canonicalDisplayValues(query: LocationQuery) {
  return {
    grouping: singularQueryValue(query.grouping) === 'chronological' ? 'chronological' : undefined,
    layout: singularQueryValue(query.layout) === 'boxes' ? 'boxes' : undefined
  }
}

function withCanonicalDisplayQuery(query: LocationQuery) {
  return mergeOwnedQuery(query, DISPLAY_QUERY_KEYS, canonicalDisplayValues(query))
}

function bareQuery() {
  const query = mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {})
  return withCanonicalDisplayQuery(query)
}

function submittedQuery(search: AppliedSearch) {
  const query = mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    theaters: search.theaterIds.join(','),
    date: search.date,
    start_after: search.startAfter,
    finish_before: search.finishBefore,
    language: search.language === 'ALL' ? undefined : search.language,
    format: search.format === 'ALL' ? undefined : search.format,
    include_ads: search.includeAds ? undefined : '0'
  })
  return withCanonicalDisplayQuery(query)
}

async function setResultGrouping(grouping: string) {
  if (grouping !== 'movie' && grouping !== 'chronological') return
  if (grouping === resultGrouping.value) return
  await router.push({
    query: mergeOwnedQuery(route.query, DISPLAY_QUERY_KEYS, {
      grouping: grouping === 'chronological' ? grouping : undefined,
      layout: resultLayout.value === 'boxes' ? 'boxes' : undefined
    })
  })
}

async function setResultLayout(layout: string) {
  if (layout !== 'lines' && layout !== 'boxes') return
  if (layout === resultLayout.value) return
  await router.push({
    query: mergeOwnedQuery(route.query, DISPLAY_QUERY_KEYS, {
      grouping: resultGrouping.value === 'chronological' ? 'chronological' : undefined,
      layout: layout === 'boxes' ? layout : undefined
    })
  })
}

function lockBodyScroll() {
  if (bodyOverflowBeforeLock !== null) return
  bodyOverflowBeforeLock = document.body.style.overflow
  document.body.style.overflow = 'hidden'
}

function unlockBodyScroll() {
  if (bodyOverflowBeforeLock === null) return
  document.body.style.overflow = bodyOverflowBeforeLock
  bodyOverflowBeforeLock = null
}

async function openFilterSheet(event: MouseEvent) {
  sheetTrigger = event.currentTarget instanceof HTMLElement ? event.currentTarget : modifierButton.value
  isFilterSheetOpen.value = true
  lockBodyScroll()
  await nextTick()
  sheetCloseButton.value?.focus()
}

function closeFilterSheet({ restoreFocus = true } = {}) {
  if (!isFilterSheetOpen.value) return
  isFilterSheetOpen.value = false
  unlockBodyScroll()
  if (restoreFocus && sheetTrigger?.isConnected) nextTick(() => sheetTrigger?.focus())
}

const FOCUSABLE_SELECTOR = 'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])'

function focusableElementsWithin(container: HTMLElement) {
  return [...container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)]
    .filter((element) => !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true')
}

function sheetFocusableElements() {
  if (!filterForm.value) return []
  const formElements = focusableElementsWithin(filterForm.value)
  if (!isCalendarOpen.value || !calendarMenu.value) return formElements

  const menuElements = focusableElementsWithin(calendarMenu.value)
  const triggerIndex = calendarTrigger.value ? formElements.indexOf(calendarTrigger.value) : -1
  if (triggerIndex < 0) return [...formElements, ...menuElements]
  return [...formElements.slice(0, triggerIndex + 1), ...menuElements, ...formElements.slice(triggerIndex + 1)]
}

function originatedInCalendarMenu(event: KeyboardEvent) {
  return event.composedPath().some((target) => target instanceof Element && target.classList.contains('editorial-calendar-menu'))
}

function handleSheetKeydown(event: KeyboardEvent) {
  if (!isFilterSheetOpen.value) return
  if (event.key === 'Escape') {
    if (isCalendarOpen.value || originatedInCalendarMenu(event)) return
    event.preventDefault()
    closeFilterSheet()
    return
  }
  if (event.key !== 'Tab') return

  const focusable = sheetFocusableElements()
  if (focusable.length === 0) {
    event.preventDefault()
    return
  }
  const first = focusable[0]!
  const last = focusable.at(-1)!
  if (isCalendarOpen.value) {
    event.preventDefault()
    const activeIndex = document.activeElement instanceof HTMLElement ? focusable.indexOf(document.activeElement) : -1
    const nextIndex = event.shiftKey
      ? (activeIndex <= 0 ? focusable.length - 1 : activeIndex - 1)
      : (activeIndex < 0 || activeIndex === focusable.length - 1 ? 0 : activeIndex + 1)
    focusable[nextIndex]?.focus()
    return
  }
  if (event.shiftKey && (document.activeElement === first || !filterForm.value?.contains(document.activeElement))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (document.activeElement === last || !filterForm.value?.contains(document.activeElement))) {
    event.preventDefault()
    first.focus()
  }
}

function isMobileViewport() {
  return mobileMediaQuery?.matches ?? false
}

async function consumeResultScrollIntent() {
  if (!resultScrollIntent) return
  resultScrollIntent = false
  await nextTick()
  await nextTick()
  if (!appliedSearch.value || !isMobileViewport() || !resultsRegion.value) return
  resultsRegion.value.focus({ preventScroll: true })
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  resultsRegion.value.scrollIntoView({ behavior: reducedMotion ? 'auto' : 'smooth', block: 'start' })
}

function handleViewportChange(event: MediaQueryListEvent) {
  if (!event.matches) closeFilterSheet({ restoreFocus: false })
}

function handleCompactCalendarViewportChange(event: MediaQueryListEvent) {
  isCompactCalendarViewport.value = event.matches
}

function handleCalendarMounted(menu: HTMLElement) {
  calendarMenu.value = menu
}

function handleCalendarUnmounted(menu: HTMLElement) {
  if (calendarMenu.value !== menu) return
  calendarMenu.value = null
  isCalendarOpen.value = false
}

function resetBareState() {
  closeFilterSheet({ restoreFocus: false })
  resultScrollIntent = false
  requestId++
  todayDate.value = todayInParis()
  form.date = availableDateOptions.value.includes(todayDate.value) ? todayDate.value : availableDateOptions.value[0] ?? ''
  const defaultRange = currentDefaultTimeRange(new Date())
  form.startAfter = defaultRange.start
  form.finishBefore = defaultRange.end
  form.language = 'ALL'
  form.format = 'ALL'
  form.includeAds = true
  results.value = null
  appliedSearch.value = null
  searchedDate.value = ''
  errorMessage.value = ''
  pending.value = false
  lastSearchKey = ''
}

function parseAppliedSearch(): AppliedSearch | null | 'bare' {
  const hasOwnedKey = OWNED_QUERY_KEYS.some((key) => key in route.query)
  if (!hasOwnedKey) return 'bare'
  if (REQUIRED_QUERY_KEYS.some((key) => !(key in route.query))) return null

  const theaterValue = singularQueryValue(route.query.theaters)
  const date = calendarDate(singularQueryValue(route.query.date))
  const startAfter = singularQueryValue(route.query.start_after)
  const finishBefore = singularQueryValue(route.query.finish_before)
  if (!theaterValue || !date || !availableDateOptions.value.includes(date) || !startAfter || !finishBefore || !validTimes.has(startAfter) || !validTimes.has(finishBefore)) return null

  if (favoriteTheaterIds.value.length === 0) return null
  const theaterIds = [...favoriteTheaterIds.value]

  const languageValue = singularQueryValue(route.query.language)
  const formatValue = singularQueryValue(route.query.format)
  const includeAdsValue = singularQueryValue(route.query.include_ads)
  return {
    theaterIds,
    date,
    startAfter,
    finishBefore,
    language: enumQueryValue(languageValue, LANGUAGES) ?? 'ALL',
    format: enumQueryValue(formatValue, formatOptions.map((option) => option.value)) ?? 'ALL',
    includeAds: includeAdsValue !== '0'
  }
}

function hydrateAppliedSearch(search: AppliedSearch) {
  form.date = search.date
  form.startAfter = search.startAfter
  form.finishBefore = search.finishBefore
  form.language = search.language
  form.format = search.format
  form.includeAds = search.includeAds
}

async function runSearch(search: AppliedSearch) {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  theaterValidationMessage.value = ''
  results.value = null
  try {
    const response = await api.searchSlot({
      theaters: search.theaterIds.join(','),
      date: search.date,
      start_after: search.startAfter,
      finish_before: search.finishBefore,
      buffer_ads: 20,
      include_ads: search.includeAds,
      language: search.language,
      format: search.format
    })
    if (currentRequest === requestId) {
      results.value = response
    }
  } catch (error) {
    if (currentRequest === requestId) errorMessage.value = getFrenchApiError(error)
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

async function applyRoute() {
  const parsed = parseAppliedSearch()
  if (parsed === 'bare' || parsed === null) {
    resetBareState()
    const query = bareQuery()
    if (!queriesEqual(route.query, query)) await router.replace({ query })
    return
  }

  hydrateAppliedSearch(parsed)
  appliedSearch.value = { ...parsed, theaterIds: [...parsed.theaterIds] }
  searchedDate.value = parsed.date
  const query = submittedQuery(parsed)
  if (!queriesEqual(route.query, query)) {
    await router.replace({ query })
    return
  }

  const key = [parsed.theaterIds.join(','), parsed.date, parsed.startAfter, parsed.finishBefore, parsed.language, parsed.format, parsed.includeAds ? '1' : '0'].join('|')
  if (key === lastSearchKey) return
  lastSearchKey = key
  await runSearch(parsed)
}

watch(
  favoriteTheaterIds,
  (favoriteIds) => {
    if (favoriteIds.length > 0) theaterValidationMessage.value = ''
    if (isReady && OWNED_QUERY_KEYS.some((key) => key in route.query)) applyRoute()
  },
  { immediate: true }
)

watch(isCenteredCalendar, () => {
  calendarMenu.value = null
  isCalendarOpen.value = false
})

watch(() => route.query, () => {
  if (!isReady) return
  if (isFilterSheetOpen.value) {
    const parsed = parseAppliedSearch()
    closeFilterSheet({ restoreFocus: parsed !== 'bare' && parsed !== null })
  }
  applyRoute()
})

async function initializePreferences() {
  await initialize()
  if (!isInitialized.value) return
  isReady = true
  await applyRoute()
}

onMounted(() => {
  mobileMediaQuery = window.matchMedia('(max-width: 1023px)')
  compactCalendarMediaQuery = window.matchMedia('(max-width: 1023px) and (max-height: 600px)')
  isCompactCalendarViewport.value = compactCalendarMediaQuery.matches
  mobileMediaQuery.addEventListener('change', handleViewportChange)
  compactCalendarMediaQuery.addEventListener('change', handleCompactCalendarViewportChange)
  document.addEventListener('keydown', handleSheetKeydown)
  initializePreferences()
})

onBeforeUnmount(() => {
  mobileMediaQuery?.removeEventListener('change', handleViewportChange)
  compactCalendarMediaQuery?.removeEventListener('change', handleCompactCalendarViewportChange)
  document.removeEventListener('keydown', handleSheetKeydown)
  unlockBodyScroll()
})

async function submitSearch() {
  const theaterIds = [...favoriteTheaterIds.value]
  if (theaterIds.length === 0) {
    theaterValidationMessage.value = 'Ajoutez au moins un cinéma favori pour lancer la recherche.'
    return
  }

  if (!calendarDate(form.date) || !validTimes.has(form.startAfter) || !validTimes.has(form.finishBefore)) return

  const search: AppliedSearch = {
    theaterIds,
    date: form.date,
    startAfter: form.startAfter,
    finishBefore: form.finishBefore,
    language: form.language,
    format: form.format,
    includeAds: form.includeAds
  }
  if (isMobileViewport()) {
    resultScrollIntent = true
    closeFilterSheet({ restoreFocus: false })
  }
  const query = submittedQuery(search)
  if (queriesEqual(route.query, query)) {
    const searchRequest = errorMessage.value ? runSearch(search) : null
    await consumeResultScrollIntent()
    await searchRequest
    return
  }
  await router.push({ query })
  await consumeResultScrollIntent()
}

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/recherche')
const pageTitle = 'Trouver une séance - MesSeances'
const pageDescription = 'Trouvez les séances qui tiennent entièrement dans votre créneau horaire.'

useSeoMeta({
  title: pageTitle,
  description: pageDescription,
  robots: computed(() => Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="search-page mx-auto max-w-[1440px] px-4 py-4 sm:px-6 sm:py-6 lg:px-10 lg:py-8">
    <h1 class="sr-only">Trouver une séance</h1>

    <div class="grid gap-8 lg:grid-cols-[360px_minmax(0,1fr)] lg:items-start lg:gap-12">
      <div v-if="isFilterSheetOpen" class="fixed inset-0 z-40 bg-black/60 lg:hidden" aria-hidden="true" @click.self="closeFilterSheet()" />

      <form
        id="search-filters"
        ref="filterForm"
        class="filter-form min-w-0 scroll-mt-28 lg:sticky lg:top-24 lg:block lg:max-h-none lg:overflow-visible lg:overscroll-auto lg:border-2 lg:border-ink lg:bg-[#f1efe8] lg:p-6 lg:shadow-[7px_7px_0_#27272a]"
        :class="[
          appliedSearch && !isFilterSheetOpen ? 'hidden' : '',
          isFilterSheetOpen ? 'fixed inset-x-0 bottom-0 z-50 max-h-[calc(100dvh-1rem)] overflow-y-auto overscroll-contain border-2 border-b-0 border-ink bg-[#f8f7f2] px-4 pt-4 pb-[max(1.5rem,env(safe-area-inset-bottom))] shadow-[0_-8px_0_#27272a] sm:px-6' : ''
        ]"
        :role="isFilterSheetOpen ? 'dialog' : undefined"
        :aria-modal="isFilterSheetOpen ? 'true' : undefined"
        :aria-labelledby="isFilterSheetOpen ? 'search-filter-sheet-title' : undefined"
        @submit.prevent="submitSearch"
      >
        <div class="mb-6 flex items-center gap-2.5 border-b-2 border-ink pb-4">
          <SlidersHorizontal :size="18" aria-hidden="true" />
          <h2 id="search-filter-sheet-title" class="text-xl font-black tracking-[-0.035em] text-ink">{{ isFilterSheetOpen ? 'Modifier la recherche' : 'Votre disponibilité' }}</h2>
          <button
            v-if="isFilterSheetOpen"
            ref="sheetCloseButton"
            type="button"
            class="ml-auto inline-flex size-10 items-center justify-center border-2 border-ink bg-surface text-ink hover:bg-[#e8e6de] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2 lg:hidden"
            aria-label="Fermer les filtres"
            @click="closeFilterSheet()"
          >
            <X :size="20" aria-hidden="true" />
          </button>
        </div>

        <div class="space-y-5">
          <fieldset :aria-invalid="theaterValidationMessage || preferencesError ? 'true' : undefined" :aria-describedby="theaterValidationMessage || preferencesError ? 'theater-selection-message' : undefined">
            <legend class="utility-label float-left mb-2">Cinémas</legend>
            <NuxtLink to="/cinemas" class="float-right mb-2 border-b-2 border-ink font-mono text-[10px] font-bold uppercase tracking-[0.08em] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">Gérer mes favoris</NuxtLink>
            <div v-if="preferencesError && !isInitialized" id="theater-selection-message" class="clear-both rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">
              <p>{{ preferencesError }}</p>
              <button type="button" class="mt-3 font-semibold underline underline-offset-4" @click="initializePreferences">Réessayer</button>
            </div>
            <div v-else-if="isLoading || !isInitialized" class="clear-both flex min-h-11 items-center gap-2 border-2 border-ink bg-surface px-3 text-sm text-muted">
              <LoaderCircle :size="16" class="animate-spin" aria-hidden="true" /> Chargement des cinémas…
            </div>
            <p v-else-if="favoriteTheaterIds.length" class="clear-both border-2 border-ink bg-surface px-3 py-3 text-sm font-bold text-ink">{{ favoriteSummary }}</p>
            <p v-else class="clear-both border-2 border-ink bg-surface px-3 py-3 text-sm text-primary">Aucun cinéma favori. Ajoutez-en pour lancer une recherche.</p>
            <p v-if="theaterValidationMessage" id="theater-selection-message" class="mt-1.5 text-sm text-red-700" role="alert">{{ theaterValidationMessage }}</p>
          </fieldset>

          <fieldset class="min-w-0">
            <legend class="utility-label mb-2">Date de la séance</legend>
            <div class="grid grid-cols-[1fr_1fr_3rem] gap-2" role="group" aria-label="Choisir la date de la séance">
              <button
                type="button"
                class="date-choice h-12 border-2 border-ink bg-surface px-2 font-mono text-[10px] font-bold uppercase focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
                :class="form.date === todayDate ? 'date-choice--active' : 'hover:bg-[#e8e6de]'"
                :disabled="!isDateAvailable(todayDate)"
                :aria-pressed="form.date === todayDate"
                @click="selectQuickDate(todayDate)"
              >
                Aujourd’hui
              </button>
              <button
                type="button"
                class="date-choice h-12 border-2 border-ink bg-surface px-2 font-mono text-[10px] font-bold uppercase focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
                :class="form.date === tomorrowDate ? 'date-choice--active' : 'hover:bg-[#e8e6de]'"
                :disabled="!isDateAvailable(tomorrowDate)"
                :aria-pressed="form.date === tomorrowDate"
                @click="selectQuickDate(tomorrowDate)"
              >
                Demain
              </button>
              <VueDatePicker
                :key="isCenteredCalendar ? 'centered' : 'anchored'"
                v-model="datePickerDate"
                class="editorial-datepicker"
                :allowed-dates="allowedDateValues"
                :aria-labels="calendarAriaLabels"
                :disabled="!hasAvailableDates"
                :locale="fr"
                :time-config="{ enableTimePicker: false }"
                :transitions="false"
                :floating="{ arrow: false, offset: 6 }"
                :ui="{ menu: 'editorial-calendar-menu' }"
                :centered="isCenteredCalendar"
                teleport="body"
                auto-apply
                arrow-navigation
                prevent-min-max-navigation
                @open="isCalendarOpen = true"
                @closed="isCalendarOpen = false"
                @menu-mounted="handleCalendarMounted"
                @menu-unmounted="handleCalendarUnmounted"
              >
                <template #trigger>
                  <button
                    ref="calendarTrigger"
                    type="button"
                    class="date-choice flex size-12 items-center justify-center border-2 border-ink bg-surface focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
                    :class="form.date && form.date !== todayDate && form.date !== tomorrowDate ? 'date-choice--active' : 'hover:bg-[#e8e6de]'"
                    :disabled="!hasAvailableDates"
                    :aria-label="form.date ? `Choisir une autre date. Date actuelle : ${formatLongDate(form.date)}` : 'Choisir une autre date. Aucune date disponible.'"
                    :aria-expanded="isCalendarOpen"
                  >
                    <CalendarDays :size="19" aria-hidden="true" />
                  </button>
                </template>
              </VueDatePicker>
            </div>
            <p v-if="isInitialized && !hasAvailableDates" class="mt-2 text-sm font-semibold text-ink" role="status">Aucune date de séance disponible pour vos cinémas favoris.</p>
          </fieldset>

          <label class="block">
            <span class="utility-label mb-2 block">Technologie</span>
            <select v-model="form.format" class="editorial-field">
              <option v-for="option in formatOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>

          <TimeRangeSlider v-model:start="form.startAfter" v-model:end="form.finishBefore" :options="timeOptions" />

          <label class="block">
            <span class="utility-label mb-2 block">Langue</span>
            <select v-model="form.language" class="editorial-field">
              <option value="ALL">Toutes</option>
              <option value="VOSTFR">VOSTFR</option>
              <option value="VF">VF</option>
            </select>
          </label>

          <label class="flex cursor-pointer items-start gap-3 border-2 border-ink bg-surface p-3 text-sm font-medium text-ink hover:bg-[#e8e6de]">
            <input v-model="form.includeAds" type="checkbox" class="mt-0.5 size-4 accent-primary" />
            <span>Inclure les publicités (+20 min)</span>
          </label>

          <button type="submit" class="search-submit w-full" :disabled="pending || isLoading || !isInitialized || favoriteTheaterIds.length === 0 || !hasAvailableDates">
            <LoaderCircle v-if="pending" :size="18" class="animate-spin" aria-hidden="true" />
            <Search v-else :size="18" aria-hidden="true" />
            {{ pending ? 'Recherche…' : 'Trouver une séance' }}
          </button>
        </div>
      </form>

      <section ref="resultsRegion" class="min-w-0 scroll-mt-28 outline-none" aria-live="polite" aria-label="Résultats de recherche" tabindex="-1">
        <div class="mb-5 flex items-end justify-between gap-4 border-b-2 border-ink pb-5">
          <div>
            <p class="utility-label">Résultats</p>
            <h2 class="mt-2 text-3xl font-black capitalize tracking-[-0.045em] text-ink sm:text-4xl">{{ searchedDate ? formatLongDate(searchedDate) : 'Lancez votre recherche' }}</h2>
          </div>
        </div>

        <div v-if="appliedSearch" class="sticky top-0 z-20 mb-6 border-2 border-ink bg-[#f1efe8]/95 shadow-[5px_5px_0_#27272a] backdrop-blur lg:top-[4.5rem] lg:p-3" :class="results ? '' : 'lg:hidden'">
          <div class="grid grid-cols-[auto_minmax(0,1fr)_minmax(3.5rem,auto)_minmax(3.5rem,auto)] divide-x-2 divide-ink lg:hidden">
            <p class="flex min-h-12 min-w-14 flex-col items-center justify-center px-2 font-mono font-black leading-none text-ink">
              <span class="text-base">{{ results?.length ?? '-' }}</span>
              <span class="mt-1 text-[9px] uppercase">séance{{ results?.length === 1 ? '' : 's' }}</span>
            </p>
            <button
              ref="modifierButton"
              type="button"
              class="flex min-h-12 min-w-0 items-center gap-2 bg-surface px-3 text-left hover:bg-[#e8e6de] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ink"
              aria-controls="search-filters"
              :aria-expanded="isFilterSheetOpen"
              :aria-label="`Modifier les filtres. Filtres appliqués : ${compactFilterSummary}`"
              @click="openFilterSheet"
            >
              <SlidersHorizontal :size="17" class="shrink-0" aria-hidden="true" />
              <span class="flex min-w-0 flex-col font-mono font-black uppercase leading-none">
                <span class="text-[10px]">Filtres</span>
                <span class="mt-1 truncate text-[9px] text-muted">{{ compactFilterSummary }}</span>
              </span>
            </button>
            <ResultSettingMenu id="mobile-result-grouping" label="Groupe" :current-value="resultGrouping" :options="groupingOptions" @select="setResultGrouping" />
            <ResultSettingMenu id="mobile-result-layout" label="Vue" :current-value="resultLayout" :options="layoutOptions" @select="setResultLayout" />
          </div>

          <div v-if="results" class="hidden lg:flex lg:items-center lg:justify-between lg:gap-3">
            <div class="min-w-0">
              <p class="shrink-0 font-semibold text-ink">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</p>
              <ul class="mt-1 hidden flex-wrap gap-x-2 gap-y-1 text-sm text-ink lg:flex" aria-label="Filtres appliqués">
                <li v-for="(item, index) in activeFilterSummary" :key="item" class="flex items-center gap-2 capitalize">
                  <span v-if="index > 0" aria-hidden="true">·</span>
                  <span>{{ item }}</span>
                </li>
              </ul>
            </div>
            <div class="flex shrink-0 items-stretch border-2 border-ink bg-surface divide-x-2 divide-ink" role="group" aria-label="Réglages des résultats">
              <ResultSettingMenu id="desktop-result-grouping" class="w-40" label="Groupement" :current-value="resultGrouping" :options="groupingOptions" @select="setResultGrouping" />
              <ResultSettingMenu id="desktop-result-layout" class="w-32" label="Vue" :current-value="resultLayout" :options="layoutOptions" @select="setResultLayout" />
            </div>
          </div>
        </div>

        <div v-if="pending" class="search-state" role="status">
          <LoaderCircle :size="32" class="animate-spin" aria-hidden="true" />
          <p>Recherche des séances compatibles…</p>
        </div>
        <div v-else-if="errorMessage" class="search-state" role="alert">
          <AlertTriangle :size="32" class="text-primary" aria-hidden="true" />
          <p class="max-w-lg">{{ errorMessage }}</p>
        </div>
        <div v-else-if="results?.length === 0" class="search-state">
          <CalendarSearch :size="30" class="text-muted" aria-hidden="true" />
          <p>Aucune séance ne tient entièrement dans ce créneau.</p>
        </div>
        <div v-else-if="results && resultGrouping === 'movie'" class="space-y-4">
          <MovieSlotResultGroup v-for="group in movieGroups" :key="group.key" :results="group.slots" :layout="resultLayout" />
        </div>
        <div v-else-if="results && resultLayout === 'lines'" class="divide-y-2 divide-ink border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
          <SlotResultCard v-for="result in chronologicalResults" :key="result.showtime.id" :result="result" />
        </div>
        <ul v-else-if="results" class="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4" aria-label="Séances compatibles par ordre chronologique">
          <li v-for="result in chronologicalResults" :key="result.showtime.id" class="min-w-0">
            <SlotResultBox :result="result" show-movie />
          </li>
        </ul>
        <div v-else class="search-state">
          <CalendarSearch :size="32" aria-hidden="true" />
          <p>Définissez votre créneau pour voir les séances compatibles.</p>
        </div>
      </section>
    </div>

  </main>
</template>

<style scoped>
.search-page {
  background-color: #f8f7f2;
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.07) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.07) 1px, transparent 1px);
  background-size: 28px 28px;
}

.utility-label {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.62rem;
  font-weight: 900;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.editorial-field {
  height: 3rem;
  width: 100%;
  border: 2px solid #27272a;
  border-radius: 0;
  background: #fff;
  padding: 0 0.75rem;
  color: #27272a;
  font-size: 0.85rem;
  font-weight: 700;
}

.editorial-field:focus-visible,
.search-submit:focus-visible {
  outline: 3px solid #27272a;
  outline-offset: 3px;
}

.date-choice--active {
  background: #27272a;
  color: #fff;
  box-shadow: inset 0 -4px 0 var(--color-highlight);
}

.date-choice:disabled {
  cursor: not-allowed;
  color: #71717a;
  opacity: 0.55;
}

.editorial-datepicker {
  min-width: 0;
}

:global(.editorial-calendar-menu) {
  --dp-font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  --dp-border-radius: 0;
  --dp-cell-border-radius: 0;
  --dp-background-color: #f8f7f2;
  --dp-text-color: #27272a;
  --dp-hover-color: #e8e6de;
  --dp-hover-text-color: #27272a;
  --dp-hover-icon-color: #27272a;
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

:global(.editorial-calendar-menu .dp__calendar_header_item),
:global(.editorial-calendar-menu .dp__month_year_select) {
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

:global(.editorial-calendar-menu .dp__active_date) {
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

:global(.editorial-calendar-menu .dp__today) {
  border: 2px solid #991b1b;
}

.search-submit {
  display: inline-flex;
  min-height: 3.25rem;
  align-items: center;
  justify-content: center;
  gap: 0.55rem;
  border: 2px solid #27272a;
  background: #27272a;
  color: #fff;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 900;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.search-submit:hover:not(:disabled) {
  background: #991b1b;
}

.search-submit:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.search-state {
  display: flex;
  min-height: 24rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 2rem;
  text-align: center;
  font-weight: 800;
  box-shadow: 7px 7px 0 #27272a;
}

@media (max-width: 639px) {
  .search-state {
    min-height: 19rem;
  }
}
</style>
