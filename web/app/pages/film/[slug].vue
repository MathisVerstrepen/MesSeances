<script setup lang="ts">
import { AlertTriangle, ArrowDownUp, CalendarDays, Film, LoaderCircle, MapPin, RefreshCw, SlidersHorizontal } from '@lucide/vue'
import { VueDatePicker } from '@vuepic/vue-datepicker'
import { fr } from 'date-fns/locale/fr'
import '@vuepic/vue-datepicker/dist/main.css'
import tmdbLogo from '~/assets/imgs/logo_tmdb.svg?no-inline'
import type { MovieShowtimesResponse, MovieShowtimesTheater, Showtime, ShowtimeFormat } from '~/types/api'
import { formatDateLabel, formatLongDate, formatParisTime, todayInParis } from '~/utils/date'
import { formatLabel, isShowtimeFormat } from '~/utils/formats'
import { calendarDate, enumQueryValue, mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'
import { absoluteSiteUrl } from '~/utils/siteUrl'

type LanguageFilter = 'ALL' | Showtime['language']
type TechnologyFilter = 'ALL' | ShowtimeFormat
type ShowtimeTimingState = 'upcoming' | 'warning' | 'past'
type MobileControlPanel = 'date' | 'filters'

const SHOWTIME_WARNING_DURATION_MS = 20 * 60 * 1000
const OWNED_QUERY_KEYS = ['date', 'language', 'format', 'sort'] as const
const SHOWTIME_LANGUAGES: readonly Showtime['language'][] = ['VOSTFR', 'VF', 'VO', 'VF_SME']

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const preferences = usePageCinemaSelection()
const today = ref(todayInParis())
const schedule = ref<MovieShowtimesResponse | null>(null)
const selectedDate = ref('')
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const posterFailed = ref(false)
const backdropFailed = ref(false)
const activeLanguage = ref<LanguageFilter>('ALL')
const activeTechnology = ref<TechnologyFilter>('ALL')
const sortByNextShowtime = ref(false)
const openMobilePanel = ref<MobileControlPanel | null>(null)
const mobileDateTrigger = ref<HTMLButtonElement | null>(null)
const mobileFilterTrigger = ref<HTMLButtonElement | null>(null)
const currentTime = ref<number | null>(null)
const isPersonalizedSchedule = ref(false)
const isEndedFilm = ref(false)
const isMobileCalendarOpen = ref(false)
const isDesktopCalendarOpen = ref(false)
let requestId = 0
let currentTimeTimer: number | undefined
let dayCheckTimer: ReturnType<typeof setTimeout> | undefined
let isReady = false
let lastScheduleKey = ''

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})

function nonPastAvailableDates(response: MovieShowtimesResponse): string[] {
  return [...new Set(response.available_dates)]
    .filter((date) => calendarDate(date) === date && date >= today.value)
    .sort()
}

function resolvedAvailableDate(dates: string[], requestedDate: string): string {
  if (dates.includes(requestedDate)) return requestedDate
  return dates.includes(today.value) ? today.value : dates[0] ?? today.value
}

const availableDates = computed(() => schedule.value ? nonPastAvailableDates(schedule.value) : [])
const hasAvailableDates = computed(() => availableDates.value.length > 0)
const posterUrl = computed(() => safePosterUrl(schedule.value?.movie.poster_url))
const posterAvailable = computed(() => Boolean(posterUrl.value) && !posterFailed.value)
const backdropUrl = computed(() => safeBackdropUrl(schedule.value?.backdrop_url))
const backdropAvailable = computed(() => Boolean(backdropUrl.value) && !backdropFailed.value)
const releaseDateLabel = computed(() => {
  const value = schedule.value?.movie.release_date
  if (!value) return ''

  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return ''

  const date = new Date(Date.UTC(year, month - 1, day, 12))
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return ''

  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: 'Europe/Paris',
    day: 'numeric',
    month: 'long',
    year: 'numeric'
  }).format(date)
})
const tmdbUrl = computed(() => {
  const id = schedule.value?.movie.tmdb_id
  return id !== null && id !== undefined && Number.isFinite(id) && id > 0 ? `https://www.themoviedb.org/movie/${id}` : ''
})
const languages = computed<Array<Showtime['language']>>(() => {
  const values = schedule.value?.theaters.flatMap((theater) => theater.showtimes.map((showtime) => showtime.language)) ?? []
  return [...new Set(values)]
})
const languageOptions = computed<Array<{ value: LanguageFilter; label: string }>>(() => [
  { value: 'ALL', label: 'Tous' },
  ...languages.value.map((language) => ({ value: language, label: language }))
])
const technologyFormats = computed<ShowtimeFormat[]>(() => {
  const formats = schedule.value?.theaters.flatMap((theater) => theater.showtimes.map((showtime) => showtime.format)) ?? []
  return [...new Set(formats)]
})
const technologyOptions = computed<Array<{ value: TechnologyFilter; label: string }>>(() => [
  { value: 'ALL', label: 'Tous' },
  ...technologyFormats.value.map((format) => ({ value: format, label: formatLabel(format) }))
])
const activeFilterSummary = computed(() => {
  const values: string[] = []
  if (activeLanguage.value !== 'ALL') values.push(activeLanguage.value)
  if (activeTechnology.value !== 'ALL') values.push(formatLabel(activeTechnology.value))
  return values.length ? values.join(' · ') : 'Tous'
})

function toggleMobilePanel(panel: MobileControlPanel) {
  openMobilePanel.value = openMobilePanel.value === panel ? null : panel
}

function closeMobilePanel(event?: KeyboardEvent) {
  const panel = openMobilePanel.value
  openMobilePanel.value = null
  if (!event || !panel) return
  nextTick(() => (panel === 'date' ? mobileDateTrigger.value : mobileFilterTrigger.value)?.focus())
}

function selectMobileDate(date: string) {
  updateFilmQuery({ date: date === fallbackDate() ? undefined : date })
  openMobilePanel.value = null
  nextTick(() => mobileDateTrigger.value?.focus())
}

function matchesFilter(showtime: Showtime): boolean {
  const matchesLanguage = activeLanguage.value === 'ALL' || showtime.language === activeLanguage.value
  const matchesTechnology = activeTechnology.value === 'ALL' || showtime.format === activeTechnology.value
  return matchesLanguage && matchesTechnology
}

function resetFilters() {
  updateFilmQuery({ language: undefined, format: undefined })
}

function showtimeTimingState(showtime: Showtime): ShowtimeTimingState {
  if (currentTime.value === null) return 'upcoming'

  const startTime = Date.parse(showtime.start_time)
  if (!Number.isFinite(startTime)) return 'upcoming'
  if (currentTime.value > startTime + SHOWTIME_WARNING_DURATION_MS) return 'past'
  if (currentTime.value >= startTime) return 'warning'
  return 'upcoming'
}

const visibleTheaters = computed<Array<MovieShowtimesTheater & { showtimes: Array<Showtime & { timingState: ShowtimeTimingState }> }>>(() => {
  if (!schedule.value) return []

  const favoriteOrder = new Map(preferences.activeTheaterIds.value.map((id, index) => [id, index]))
  const sourceOrder = new Map(schedule.value.theaters.map((theater, index) => [theater.id, index]))
  const theaters = schedule.value.theaters
    .map((theater) => ({
      ...theater,
      showtimes: theater.showtimes
        .filter(matchesFilter)
        .map((showtime) => ({ ...showtime, timingState: showtimeTimingState(showtime) }))
    }))
    .filter((theater) => theater.showtimes.length > 0)

  return theaters.sort((left, right) => {
    if (sortByNextShowtime.value) {
      const earliestTime = (theater: MovieShowtimesTheater) => Math.min(...theater.showtimes.map((showtime) => Date.parse(showtime.start_time)))
      const timeDifference = earliestTime(left) - earliestTime(right)
      if (timeDifference !== 0) return timeDifference
    }

    return (favoriteOrder.get(left.id) ?? Number.MAX_SAFE_INTEGER) - (favoriteOrder.get(right.id) ?? Number.MAX_SAFE_INTEGER)
      || (sourceOrder.get(left.id) ?? 0) - (sourceOrder.get(right.id) ?? 0)
  })
})
const visibleShowtimeCount = computed(() => visibleTheaters.value.reduce((total, theater) => total + theater.showtimes.length, 0))

function fallbackDate(): string {
  return availableDates.value.includes(today.value) ? today.value : availableDates.value[0] ?? today.value
}

function isAvailableDate(value: string | undefined): value is string {
  if (!value || value < today.value) return false
  if (!schedule.value) return true
  return availableDates.value.includes(value)
}

function dateFromCalendarDate(value: string): Date | null {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  return new Date(year, month - 1, day, 12)
}

function calendarDateFromDate(value: Date): string {
  return [value.getFullYear(), String(value.getMonth() + 1).padStart(2, '0'), String(value.getDate()).padStart(2, '0')].join('-')
}

const allowedDateValues = computed(() => availableDates.value.map(dateFromCalendarDate).filter((value): value is Date => value !== null))
const datePickerDate = computed<Date | null>({
  get: () => hasAvailableDates.value ? dateFromCalendarDate(selectedDate.value) : null,
  set: (value) => {
    if (!value) return
    const date = calendarDateFromDate(value)
    if (availableDates.value.includes(date)) updateFilmQuery({ date: date === fallbackDate() ? undefined : date })
  }
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

function filmQuery() {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    date: selectedDate.value === fallbackDate() ? undefined : selectedDate.value,
    language: activeLanguage.value === 'ALL' ? undefined : activeLanguage.value,
    format: activeTechnology.value === 'ALL' ? undefined : activeTechnology.value,
    sort: sortByNextShowtime.value ? 'next' : undefined
  })
}

function updateFilmQuery(values: Partial<Record<'date' | 'language' | 'format' | 'sort', string | undefined>>) {
  const query = mergeOwnedQuery(route.query, Object.keys(values), values)
  if (!queriesEqual(route.query, query)) router.push({ query })
}

function hydrateRoute() {
  const requestedDate = calendarDate(singularQueryValue(route.query.date))
  selectedDate.value = isAvailableDate(requestedDate) ? requestedDate : fallbackDate()

  const requestedLanguage = singularQueryValue(route.query.language)
  activeLanguage.value = requestedLanguage === 'ALL'
    ? 'ALL'
    : enumQueryValue(requestedLanguage, SHOWTIME_LANGUAGES) ?? 'ALL'

  const requestedFormat = singularQueryValue(route.query.format)
  activeTechnology.value = requestedFormat === 'ALL'
    ? 'ALL'
    : requestedFormat && isShowtimeFormat(requestedFormat) ? requestedFormat : 'ALL'
  sortByNextShowtime.value = singularQueryValue(route.query.sort) === 'next'
  return filmQuery()
}

async function normalizeDynamicFilters() {
  const values: Record<string, string | undefined> = {}
  if (activeLanguage.value !== 'ALL' && !languages.value.includes(activeLanguage.value)) values.language = undefined
  if (activeTechnology.value !== 'ALL' && !technologyFormats.value.includes(activeTechnology.value)) values.format = undefined
  if (Object.keys(values).length === 0) return
  const query = mergeOwnedQuery(route.query, Object.keys(values), values)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
}

function selectAdjacentDate(event: KeyboardEvent, index: number) {
  let nextIndex: number | undefined
  if (event.key === 'ArrowRight') nextIndex = (index + 1) % availableDates.value.length
  else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + availableDates.value.length) % availableDates.value.length
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = availableDates.value.length - 1
  if (nextIndex === undefined) return

  event.preventDefault()
  const nextDate = availableDates.value[nextIndex]
  if (!nextDate) return
  updateFilmQuery({ date: nextDate === fallbackDate() ? undefined : nextDate })
  const currentTarget = event.currentTarget
  if (!(currentTarget instanceof HTMLElement)) return
  const tabs = currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
  nextTick(() => tabs?.[nextIndex]?.focus())
}

function bookingLabel(showtime: Showtime, theater: MovieShowtimesTheater, timingState: ShowtimeTimingState): string {
  const timingLabel = timingState === 'warning' ? ', séance commencée' : timingState === 'past' ? ', séance passée' : ''
  return `Séance de ${formatParisTime(showtime.start_time)} à ${theater.name}${timingLabel}, réserver`
}

function formatRoom(room: string): string {
  const roomName = room.trim().replace(/^salle\b\s*/i, '')
  return roomName ? `Salle ${roomName}` : 'Salle'
}

function isNotFoundError(cause: unknown): boolean {
  return getApiErrorStatus(cause) === 404 || getApiErrorCode(cause) === 'not_found'
}

async function loadSchedule() {
  if (!preferences.isInitialized.value) {
    pending.value = false
    schedule.value = null
    errorMessage.value = preferences.error.value || 'Impossible de charger vos cinémas favoris.'
    return
  }
  if (!slug.value || !selectedDate.value) return
  if (preferences.activeTheaterIds.value.length === 0) {
    pending.value = false
    schedule.value = null
    errorMessage.value = preferences.error.value || 'Sélectionnez au moins un cinéma pour consulter les séances.'
    return
  }

  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  notFound.value = false

  try {
    let response = await api.movieShowtimes(slug.value, {
      date: selectedDate.value,
      theaters: preferences.activeTheaterIds.value.join(',')
    })
    if (response.movie.slug !== slug.value) {
      await navigateTo({ path: `/film/${encodeURIComponent(response.movie.slug)}`, query: route.query }, { redirectCode: 308, replace: true })
      return
    }
    if (currentRequest === requestId) {
      schedule.value = response
      const responseDates = nonPastAvailableDates(response)
      const resolvedDate = resolvedAvailableDate(responseDates, selectedDate.value)

      if (resolvedDate !== selectedDate.value && responseDates.length > 0) {
        selectedDate.value = resolvedDate
        lastScheduleKey = `${slug.value}|${selectedDate.value}|${preferences.activeTheaterIds.value.join(',')}`
        const query = filmQuery()
        if (!queriesEqual(route.query, query)) await router.replace({ query })
        response = await api.movieShowtimes(slug.value, {
          date: resolvedDate,
          theaters: preferences.activeTheaterIds.value.join(',')
        })
      } else if (responseDates.length === 0) {
        selectedDate.value = today.value
        lastScheduleKey = `${slug.value}|${selectedDate.value}|${preferences.activeTheaterIds.value.join(',')}`
      }
      if (currentRequest !== requestId) return
      schedule.value = response
      isEndedFilm.value = !response.currently_screened
      isPersonalizedSchedule.value = true
      const canonicalQuery = filmQuery()
      if (!queriesEqual(route.query, canonicalQuery)) await router.replace({ query: canonicalQuery })
      await nextTick()
      await normalizeDynamicFilters()
    }
  } catch (error) {
    if (currentRequest === requestId) {
      notFound.value = isNotFoundError(error)
      if (notFound.value) {
        schedule.value = null
        isPersonalizedSchedule.value = false
      } else {
        errorMessage.value = getFrenchApiError(error)
      }
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }

  const key = `${slug.value}|${selectedDate.value}|${preferences.activeTheaterIds.value.join(',')}`
  if (key === lastScheduleKey) return
  lastScheduleKey = key
  await loadSchedule()
}

async function initializePreferencesAndLoad() {
  pending.value = true
  errorMessage.value = ''
  notFound.value = false

  await preferences.initialize()
  if (!preferences.isInitialized.value) {
    pending.value = false
    errorMessage.value = preferences.error.value || 'Impossible de charger vos cinémas favoris.'
    return
  }

  isReady = true
  await applyRoute()
}

async function refreshFilmDay() {
  const currentDay = todayInParis()
  if (currentDay === today.value) return
  today.value = currentDay
  if (isReady) await applyRoute()
}

function scheduleDayCheck() {
  if (dayCheckTimer) window.clearTimeout(dayCheckTimer)
  const delay = 60_000 - Date.now() % 60_000
  dayCheckTimer = window.setTimeout(() => {
    dayCheckTimer = undefined
    void refreshFilmDay()
    scheduleDayCheck()
  }, delay)
}

function handleVisibilityChange() {
  if (document.visibilityState !== 'visible') return
  void refreshFilmDay()
  scheduleDayCheck()
}

async function retryLoad() {
  if (!preferences.isInitialized.value) await initializePreferencesAndLoad()
  else await loadSchedule()
}

hydrateRoute()
const initialScheduleKey = `${slug.value}|${selectedDate.value}`
const initialResult = await useAsyncData(`film-schedule:${initialScheduleKey}`, async () => {
  try {
    let resolvedDate = selectedDate.value
    let response = await api.movieShowtimes(slug.value, { date: resolvedDate })
    const responseDates = nonPastAvailableDates(response)
    const fallback = resolvedAvailableDate(responseDates, resolvedDate)
    if (!responseDates.includes(resolvedDate) && responseDates.length > 0) {
      resolvedDate = fallback
      response = await api.movieShowtimes(slug.value, { date: resolvedDate })
    } else if (responseDates.length === 0) {
      resolvedDate = today.value
    }
    return { kind: 'success' as const, schedule: response, selectedDate: resolvedDate, errorMessage: '' }
  } catch (error) {
    if (isNotFoundError(error)) {
      return { kind: 'not-found' as const, schedule: null, selectedDate: selectedDate.value, errorMessage: '' }
    }
    return { kind: 'upstream-error' as const, schedule: null, selectedDate: selectedDate.value, errorMessage: getFrenchApiError(error) }
  }
})

const initialState = initialResult.data.value
const responseSlug = initialState?.schedule?.movie.slug
if (initialState?.kind === 'success' && responseSlug && responseSlug !== slug.value) {
  await navigateTo({ path: `/film/${encodeURIComponent(responseSlug)}`, query: route.query }, { redirectCode: 308, replace: true })
}
schedule.value = initialState?.schedule ?? null
selectedDate.value = initialState?.selectedDate ?? selectedDate.value
isEndedFilm.value = initialState?.kind === 'success' && !initialState.schedule.currently_screened
notFound.value = initialState?.kind === 'not-found'
errorMessage.value = initialState?.errorMessage ?? ''
pending.value = false
if (import.meta.server && initialState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, initialState?.kind === 'not-found' ? 404 : 502)
}

watch(
  () => preferences.activeTheaterIds.value.join(','),
  () => {
    if (isReady) applyRoute()
  }
)
watch(() => route.query, () => {
  if (isReady) applyRoute()
})
watch(slug, () => {
  schedule.value = null
  isEndedFilm.value = false
  isPersonalizedSchedule.value = false
  posterFailed.value = false
  backdropFailed.value = false
  lastScheduleKey = ''
  if (isReady) applyRoute()
})

onMounted(() => {
  currentTime.value = Date.now()
  currentTimeTimer = window.setInterval(() => {
    currentTime.value = Date.now()
  }, 30_000)
  scheduleDayCheck()
  document.addEventListener('visibilitychange', handleVisibilityChange)
  initializePreferencesAndLoad()
})
onBeforeUnmount(() => {
  if (currentTimeTimer !== undefined) window.clearInterval(currentTimeTimer)
  if (dayCheckTimer) window.clearTimeout(dayCheckTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

const config = useRuntimeConfig()
const canonicalSlug = computed(() => schedule.value?.movie.slug ?? slug.value)
const canonicalUrl = computed(() => absoluteSiteUrl(config.public.siteUrl, `/film/${encodeURIComponent(canonicalSlug.value)}`))
const fallbackImageUrl = absoluteSiteUrl(config.public.siteUrl, '/pwa-512x512.png')
const seoTitle = computed(() => schedule.value?.movie.title ? `${schedule.value.movie.title} : horaires et séances au cinéma - MesSeances` : 'Séances du film - MesSeances')
const seoDescription = computed(() => {
  const movie = schedule.value?.movie
  if (!movie) return 'Consultez les séances, horaires et cinémas disponibles pour ce film sur MesSeances.'
  return movie.overview?.trim() || `Retrouvez toutes les séances de ${movie.title} et choisissez votre cinéma sur MesSeances.`
})
const seoImageUrl = computed(() => safeBackdropUrl(schedule.value?.backdrop_url) ?? safePosterUrl(schedule.value?.movie.poster_url) ?? fallbackImageUrl)
const robots = computed(() => schedule.value && schedule.value.movie.slug === slug.value && Object.keys(route.query).length === 0 && !errorMessage.value && !notFound.value
  ? 'index,follow'
  : 'noindex,follow')
const filmJsonLd = computed(() => {
  const currentSchedule = schedule.value
  if (!currentSchedule || pending.value || errorMessage.value || notFound.value) return null

  const movieUrl = canonicalUrl.value
  const movieId = `${movieUrl}#movie`
  const images = [safePosterUrl(currentSchedule.movie.poster_url), safeBackdropUrl(currentSchedule.backdrop_url)]
    .filter((value): value is string => Boolean(value))
  const movie: JsonLdNode = {
    '@type': 'Movie',
    '@id': movieId,
    name: currentSchedule.movie.title,
    url: movieUrl
  }
  if (currentSchedule.movie.runtime_minutes > 0) movie.duration = `PT${currentSchedule.movie.runtime_minutes}M`
  if (currentSchedule.movie.overview?.trim()) movie.description = currentSchedule.movie.overview.trim()
  if (releaseDateLabel.value && currentSchedule.movie.release_date) movie.datePublished = currentSchedule.movie.release_date
  if (currentSchedule.movie.genres.length) movie.genre = currentSchedule.movie.genres
  if (images.length === 1) movie.image = images[0]
  else if (images.length > 1) movie.image = images
  if (tmdbUrl.value) movie.sameAs = tmdbUrl.value

  const graph: JsonLdNode[] = [
    movie,
    {
      '@type': 'BreadcrumbList',
      '@id': `${movieUrl}#breadcrumb`,
      itemListElement: [
        { '@type': 'ListItem', position: 1, name: 'Accueil', item: absoluteSiteUrl(config.public.siteUrl, '/') },
        { '@type': 'ListItem', position: 2, name: 'Films', item: absoluteSiteUrl(config.public.siteUrl, '/films') },
        { '@type': 'ListItem', position: 3, name: currentSchedule.movie.title, item: movieUrl }
      ]
    }
  ]
  const seenShowtimes = new Set<string>()
  for (const theater of visibleTheaters.value) {
    const theaterUrl = absoluteSiteUrl(config.public.siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`)
    const theaterId = `${theaterUrl}#cinema`
    graph.push({ '@type': 'MovieTheater', '@id': theaterId, name: theater.name, url: theaterUrl })
    for (const showtime of theater.showtimes) {
      const showtimeId = showtime.id.trim()
      const start = Date.parse(showtime.start_time)
      const end = Date.parse(showtime.end_time)
      if (!showtimeId || seenShowtimes.has(showtimeId) || !Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue
      seenShowtimes.add(showtimeId)
      graph.push({
        '@type': 'ScreeningEvent',
        '@id': `${theaterUrl}#screening-${encodeURIComponent(showtimeId)}`,
        name: `${currentSchedule.movie.title} à ${theater.name}`,
        startDate: showtime.start_time,
        endDate: showtime.end_time,
        location: { '@id': theaterId },
        workPresented: { '@id': movieId }
      })
    }
  }
  return serializeJsonLd({ '@context': 'https://schema.org', '@graph': graph })
})

useSeoMeta({
  robots,
  title: seoTitle,
  description: seoDescription,
  ogTitle: seoTitle,
  ogDescription: seoDescription,
  ogUrl: canonicalUrl,
  ogType: 'video.movie',
  ogImage: seoImageUrl,
  ogSiteName: 'MesSeances',
  ogLocale: 'fr_FR',
  twitterCard: 'summary_large_image',
  twitterTitle: seoTitle,
  twitterDescription: seoDescription,
  twitterImage: seoImageUrl
})
useHead(() => ({
  link: [{ rel: 'canonical', href: canonicalUrl.value }],
  script: filmJsonLd.value ? [{ key: 'film-jsonld', type: 'application/ld+json', innerHTML: filmJsonLd.value }] : []
}))
</script>

<template>
  <main class="film-page mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-14">
    <div v-if="pending && !schedule" class="film-state" role="status" aria-live="polite">
      <LoaderCircle :size="34" class="animate-spin" aria-hidden="true" />
      <p>Chargement des séances…</p>
    </div>

    <div v-else-if="notFound" class="film-state" role="alert">
      <Film :size="36" aria-hidden="true" />
      <div>
        <p class="text-2xl font-black tracking-[-0.04em] text-ink">Film introuvable</p>
        <p class="mt-1 text-sm">Ce film n’est pas disponible dans le catalogue actuel.</p>
      </div>
      <NuxtLink to="/films" class="brutal-action">Voir les films</NuxtLink>
    </div>

    <div v-else-if="errorMessage && !schedule" class="film-state" role="alert">
      <AlertTriangle :size="34" class="text-primary" aria-hidden="true" />
      <p class="max-w-lg">{{ errorMessage }}</p>
      <div class="flex flex-wrap justify-center gap-3">
        <button v-if="!preferences.isInitialized.value || preferences.activeTheaterIds.value.length" type="button" class="brutal-action" @click="retryLoad">
          <RefreshCw :size="17" aria-hidden="true" /> Réessayer
        </button>
        <NuxtLink to="/cinemas" class="brutal-action brutal-action--light">
          Mes cinémas
        </NuxtLink>
      </div>
    </div>

    <template v-else-if="schedule">
      <nav class="mb-6 font-mono text-xs font-bold uppercase tracking-[0.08em] text-muted" aria-label="Fil d’Ariane">
        <ol class="flex flex-wrap items-center gap-2">
          <li><NuxtLink to="/" class="hover:text-primary">Accueil</NuxtLink></li>
          <li aria-hidden="true">/</li>
          <li><NuxtLink to="/films" class="hover:text-primary">Films</NuxtLink></li>
          <li aria-hidden="true">/</li>
          <li class="text-ink" aria-current="page">{{ schedule.movie.title }}</li>
        </ol>
      </nav>
      <header
        class="movie-hero relative grid gap-7 overflow-hidden border-2 border-ink p-5 shadow-[8px_8px_0_#27272a] sm:grid-cols-[180px_minmax(0,1fr)] sm:items-end sm:p-7 lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-10 lg:p-10"
        :class="backdropAvailable ? 'isolate text-white' : 'bg-surface'"
      >
        <img
          v-if="backdropAvailable"
          :src="backdropUrl ?? undefined"
          alt=""
          aria-hidden="true"
          class="absolute inset-0 -z-20 size-full object-cover"
          @error="backdropFailed = true"
        />
        <div v-if="backdropAvailable" class="absolute inset-0 -z-10 bg-black/80" aria-hidden="true" />
        <a
          v-if="tmdbUrl"
          :href="tmdbUrl"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="Voir ce film sur TMDB (nouvel onglet)"
          class="absolute right-4 top-4 z-20 inline-flex min-h-11 min-w-11 items-center justify-center p-1 hover:opacity-75 focus-visible:ring-2 focus-visible:ring-highlight focus-visible:ring-offset-4 sm:right-6 sm:top-6 lg:right-8 lg:top-8"
        >
          <img :src="tmdbLogo" alt="" class="h-auto w-20" />
        </a>
        <div
          class="relative z-10 mx-auto aspect-[2/3] w-40 overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[8px_8px_0_#27272a] sm:mx-0 sm:w-[180px] lg:w-[220px]"
        >
          <img
            v-if="posterAvailable"
            :src="posterUrl!"
            :alt="`Affiche de ${schedule.movie.title}`"
            class="h-full w-full object-cover"
            @error="posterFailed = true"
          />
          <div
            v-else
            class="flex h-full flex-col items-center justify-center gap-2 px-3 text-center text-muted"
          >
            <Film :size="32" aria-hidden="true" />
            <span class="text-xs font-bold">Affiche indisponible</span>
          </div>
        </div>
        <div class="min-w-0" :class="[backdropAvailable ? 'relative z-10' : undefined, tmdbUrl ? 'sm:pr-28' : undefined]">
          <h1 class="movie-title text-[clamp(3rem,7vw,7rem)] font-black uppercase leading-[0.82] tracking-[-0.075em]" :class="backdropAvailable ? 'text-white' : 'text-ink'">{{ schedule.movie.title }}</h1>
          <div class="mt-6 flex flex-wrap items-center gap-2 font-mono text-[10px] font-bold uppercase tracking-[0.1em]" :class="backdropAvailable ? 'text-white' : 'text-ink'">
            <span class="meta-chip">{{ schedule.movie.runtime_minutes }} min</span>
            <template v-if="releaseDateLabel">
              <time :datetime="schedule.movie.release_date!" class="meta-chip">{{ releaseDateLabel }}</time>
            </template>
          </div>
          <ul v-if="schedule.movie.genres.length" class="mt-3 flex flex-wrap gap-2" aria-label="Genres">
            <li
              v-for="genre in schedule.movie.genres"
              :key="genre"
              class="genre-chip"
            >
              {{ genre }}
            </li>
          </ul>
          <div v-if="schedule.movie.overview?.trim()" class="mt-7 max-w-3xl border-l-2 pl-4" :class="backdropAvailable ? 'border-white' : 'border-ink'">
            <h2 class="font-mono text-[10px] font-bold uppercase tracking-[0.18em]" :class="backdropAvailable ? 'text-white/70' : 'text-muted'">Synopsis</h2>
            <p class="mt-2 text-sm font-medium leading-6 sm:text-base" :class="backdropAvailable ? 'text-white/90' : 'text-ink'">{{ schedule.movie.overview }}</p>
          </div>
        </div>
      </header>

      <section class="schedule-section mt-12 border-t-2 border-ink pt-8 sm:mt-16 sm:pt-10" aria-labelledby="schedule-heading">
        <div class="flex flex-col gap-3 border-b-2 border-ink pb-5 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
          <div>
            <p class="font-mono text-[10px] font-bold uppercase tracking-[0.18em] text-muted">Programmation</p>
            <h2 id="schedule-heading" class="mt-2 flex flex-wrap items-baseline gap-x-3 text-4xl font-black tracking-[-0.05em] text-ink sm:text-5xl">
              <span>Séances</span>
              <span class="font-mono text-xs font-bold uppercase tracking-[0.1em] text-muted">{{ visibleShowtimeCount }} horaire{{ visibleShowtimeCount === 1 ? '' : 's' }}</span>
            </h2>
            <p class="mt-2 font-mono text-[11px] font-bold uppercase tracking-[0.1em] capitalize text-muted">{{ formatLongDate(selectedDate) }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-3 self-start sm:justify-end sm:self-end">
            <NuxtLink to="/cinemas" class="editorial-link shrink-0">Modifier mes cinémas</NuxtLink>
            <ShareButton />
          </div>
        </div>

        <SharedTheaterNotice v-if="preferences.isInitialized.value && preferences.isSharedSelectionDifferent.value" class="mt-5" />

        <div class="filter-dock sticky top-0 z-20 -mx-4 mt-5 border-y-2 border-ink bg-[#f1efe8]/95 shadow-[0_6px_0_#27272a] backdrop-blur sm:-mx-6 lg:hidden">
          <div class="grid grid-cols-3 divide-x-2 divide-ink">
            <button
              id="mobile-date-trigger"
              ref="mobileDateTrigger"
              type="button"
              class="compact-control"
              aria-controls="mobile-date-panel"
              :aria-expanded="openMobilePanel === 'date'"
              :aria-label="`Choisir une date, sélection actuelle : ${formatDateLabel(selectedDate)}`"
              @click="toggleMobilePanel('date')"
            >
              <CalendarDays :size="17" aria-hidden="true" />
              <span class="compact-control__text">
                <span>Date</span>
                <span class="truncate">{{ formatDateLabel(selectedDate) }}</span>
              </span>
            </button>
            <button
              id="mobile-filter-trigger"
              ref="mobileFilterTrigger"
              type="button"
              class="compact-control"
              aria-controls="mobile-filter-panel"
              :aria-expanded="openMobilePanel === 'filters'"
              :aria-label="`Filtres, sélection actuelle : ${activeFilterSummary}`"
              @click="toggleMobilePanel('filters')"
            >
              <SlidersHorizontal :size="17" aria-hidden="true" />
              <span class="compact-control__text">
                <span>Filtres</span>
                <span class="truncate">{{ activeFilterSummary }}</span>
              </span>
            </button>
            <button
              type="button"
              class="compact-control"
              :class="sortByNextShowtime ? 'compact-control--active' : undefined"
              :aria-pressed="sortByNextShowtime"
              aria-label="Trier les cinémas par prochain horaire"
              @click="updateFilmQuery({ sort: sortByNextShowtime ? undefined : 'next' })"
            >
              <ArrowDownUp :size="17" aria-hidden="true" />
              <span class="min-w-0 truncate">Horaire</span>
            </button>
          </div>

          <div
            v-show="openMobilePanel === 'date'"
            id="mobile-date-panel"
            class="border-t-2 border-ink px-4 py-3 sm:px-6"
            @keydown.esc.stop="closeMobilePanel($event)"
          >
            <div class="flex items-start gap-2">
              <VueDatePicker
                v-model="datePickerDate"
                class="editorial-datepicker shrink-0"
                :allowed-dates="allowedDateValues"
                :aria-labels="calendarAriaLabels"
                :disabled="!hasAvailableDates"
                :locale="fr"
                :time-config="{ enableTimePicker: false }"
                :transitions="false"
                :floating="{ arrow: false, offset: 6 }"
                :ui="{ menu: 'editorial-calendar-menu' }"
                teleport="body"
                auto-apply
                arrow-navigation
                prevent-min-max-navigation
                @open="isMobileCalendarOpen = true"
                @closed="isMobileCalendarOpen = false"
              >
                <template #trigger>
                  <button
                    type="button"
                    class="calendar-trigger grid size-11 shrink-0 place-items-center border-2 border-ink bg-[#ffcf3f] hover:bg-highlight disabled:cursor-not-allowed disabled:bg-[#e8e6de] disabled:text-muted"
                    :disabled="!hasAvailableDates"
                    :aria-label="hasAvailableDates ? `Choisir une autre date. Date actuelle : ${formatLongDate(selectedDate)}` : 'Choisir une autre date. Aucune date disponible.'"
                    :aria-expanded="isMobileCalendarOpen"
                  >
                    <CalendarDays :size="18" aria-hidden="true" />
                  </button>
                </template>
              </VueDatePicker>
              <div class="flex min-w-0 gap-2 overflow-x-auto pb-1" role="tablist" aria-label="Choisir une date">
                <button
                  v-for="(date, index) in availableDates"
                  :id="`mobile-date-tab-${date}`"
                  :key="date"
                  type="button"
                  role="tab"
                  :aria-selected="selectedDate === date"
                  aria-controls="showtime-panel"
                  :tabindex="selectedDate === date ? 0 : -1"
                  class="date-tab"
                  :class="selectedDate === date ? 'date-tab--active' : undefined"
                  @click="selectMobileDate(date)"
                  @keydown="selectAdjacentDate($event, index)"
                >
                  {{ formatDateLabel(date) }}
                </button>
                <span v-if="!hasAvailableDates" class="inline-flex h-11 items-center font-mono text-xs font-bold uppercase">Aucune date disponible</span>
              </div>
            </div>
          </div>

          <div
            v-show="openMobilePanel === 'filters'"
            id="mobile-filter-panel"
            class="space-y-3 border-t-2 border-ink px-4 py-3 sm:px-6"
            @keydown.esc.stop="closeMobilePanel($event)"
          >
            <div v-if="languages.length > 1" class="flex flex-wrap items-center gap-2">
              <span id="mobile-language-filter-label" class="filter-label">Langue</span>
              <div class="flex max-w-full gap-1 overflow-x-auto" role="group" aria-labelledby="mobile-language-filter-label">
                <button
                  v-for="option in languageOptions"
                  :key="option.value"
                  type="button"
                  class="filter-button"
                  :class="activeLanguage === option.value ? 'filter-button--active' : undefined"
                  :aria-pressed="activeLanguage === option.value"
                  @click="updateFilmQuery({ language: option.value === 'ALL' ? undefined : option.value })"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <div v-if="technologyFormats.length > 1" class="flex flex-wrap items-center gap-2">
              <span id="mobile-technology-filter-label" class="filter-label">Technologie</span>
              <div class="flex max-w-full gap-1 overflow-x-auto" role="group" aria-labelledby="mobile-technology-filter-label">
                <button
                  v-for="option in technologyOptions"
                  :key="option.value"
                  type="button"
                  class="filter-button"
                  :class="activeTechnology === option.value ? 'filter-button--active' : undefined"
                  :aria-pressed="activeTechnology === option.value"
                  @click="updateFilmQuery({ format: option.value === 'ALL' ? undefined : option.value })"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <p v-if="languages.length <= 1 && technologyFormats.length <= 1" class="font-mono text-xs font-bold uppercase">Aucun filtre disponible</p>
          </div>
        </div>

        <div class="filter-dock sticky top-[4.5rem] z-20 -mx-10 mt-5 hidden border-y-2 border-ink bg-[#f1efe8]/95 px-10 py-4 shadow-[0_6px_0_#27272a] backdrop-blur lg:block">
          <div class="flex items-start gap-2">
            <VueDatePicker
              v-model="datePickerDate"
              class="editorial-datepicker shrink-0"
              :allowed-dates="allowedDateValues"
              :aria-labels="calendarAriaLabels"
              :disabled="!hasAvailableDates"
              :locale="fr"
              :time-config="{ enableTimePicker: false }"
              :transitions="false"
              :floating="{ arrow: false, offset: 6 }"
              :ui="{ menu: 'editorial-calendar-menu' }"
              teleport="body"
              auto-apply
              arrow-navigation
              prevent-min-max-navigation
              @open="isDesktopCalendarOpen = true"
              @closed="isDesktopCalendarOpen = false"
            >
              <template #trigger>
                <button
                  type="button"
                  class="calendar-trigger grid size-11 shrink-0 place-items-center border-2 border-ink bg-[#ffcf3f] hover:bg-highlight disabled:cursor-not-allowed disabled:bg-[#e8e6de] disabled:text-muted"
                  :disabled="!hasAvailableDates"
                  :aria-label="hasAvailableDates ? `Choisir une autre date. Date actuelle : ${formatLongDate(selectedDate)}` : 'Choisir une autre date. Aucune date disponible.'"
                  :aria-expanded="isDesktopCalendarOpen"
                >
                  <CalendarDays :size="18" aria-hidden="true" />
                </button>
              </template>
            </VueDatePicker>
            <div class="flex min-w-0 gap-2 overflow-x-auto pb-1" role="tablist" aria-label="Choisir une date">
              <button
                v-for="(date, index) in availableDates"
                :id="`desktop-date-tab-${date}`"
                :key="date"
                type="button"
                role="tab"
                :aria-selected="selectedDate === date"
                aria-controls="showtime-panel"
                :tabindex="selectedDate === date ? 0 : -1"
                class="date-tab"
                :class="selectedDate === date ? 'date-tab--active' : undefined"
                @click="updateFilmQuery({ date: date === fallbackDate() ? undefined : date })"
                @keydown="selectAdjacentDate($event, index)"
              >
                {{ formatDateLabel(date) }}
              </button>
              <span v-if="!hasAvailableDates" class="inline-flex h-11 items-center font-mono text-xs font-bold uppercase">Aucune date disponible</span>
            </div>
          </div>

          <div class="mt-3 flex flex-col gap-2 border-t-2 border-ink/30 pt-3">
            <div v-if="languages.length > 1" class="flex flex-wrap items-center gap-2">
              <span id="language-filter-label" class="filter-label">Langue</span>
              <div class="flex max-w-full gap-1 overflow-x-auto" role="group" aria-labelledby="language-filter-label">
                <button
                  v-for="option in languageOptions"
                  :key="option.value"
                  type="button"
                  class="filter-button"
                  :class="activeLanguage === option.value ? 'filter-button--active' : undefined"
                  :aria-pressed="activeLanguage === option.value"
                  @click="updateFilmQuery({ language: option.value === 'ALL' ? undefined : option.value })"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <div class="flex min-w-0 items-center gap-2">
              <template v-if="technologyFormats.length > 1">
                <span id="technology-filter-label" class="filter-label">Technologie</span>
                <div class="flex min-w-0 gap-1 overflow-x-auto" role="group" aria-labelledby="technology-filter-label">
                  <button
                    v-for="option in technologyOptions"
                    :key="option.value"
                    type="button"
                    class="filter-button"
                    :class="activeTechnology === option.value ? 'filter-button--active' : undefined"
                    :aria-pressed="activeTechnology === option.value"
                    @click="updateFilmQuery({ format: option.value === 'ALL' ? undefined : option.value })"
                  >
                    {{ option.label }}
                  </button>
                </div>
              </template>
              <button
                type="button"
                class="sort-button ml-auto"
                :class="sortByNextShowtime ? 'filter-button--active' : undefined"
                :aria-pressed="sortByNextShowtime"
                aria-label="Trier les cinémas par prochain horaire"
                @click="updateFilmQuery({ sort: sortByNextShowtime ? undefined : 'next' })"
              >
                <ArrowDownUp :size="16" aria-hidden="true" />
                <span class="whitespace-nowrap">Prochain horaire</span>
              </button>
            </div>
          </div>
        </div>

        <div id="showtime-panel" role="tabpanel" :aria-label="`Séances du ${formatLongDate(selectedDate)}`" :aria-busy="pending">
          <div v-if="pending" class="film-state mt-10" role="status" aria-live="polite">
            <LoaderCircle :size="34" class="animate-spin" aria-hidden="true" />
            <p>Chargement des séances…</p>
          </div>

          <div v-else-if="errorMessage" class="film-state mt-10" role="alert">
            <AlertTriangle :size="34" class="text-primary" aria-hidden="true" />
            <p class="max-w-lg">{{ errorMessage }}</p>
            <button type="button" class="brutal-action" @click="loadSchedule">
              <RefreshCw :size="17" aria-hidden="true" /> Réessayer
            </button>
          </div>

          <div v-else-if="isEndedFilm" class="film-state mt-10">
            <CalendarDays :size="36" aria-hidden="true" />
            <p>Aucune séance programmée pour le moment.</p>
          </div>

          <div v-else-if="!hasAvailableDates" class="film-state mt-10">
            <CalendarDays :size="36" aria-hidden="true" />
            <p>Aucune date de séance disponible pour ce film dans ces cinémas.</p>
          </div>

          <div v-else-if="schedule.theaters.length === 0" class="film-state mt-10">
            <CalendarDays :size="36" aria-hidden="true" />
            <p>{{ isPersonalizedSchedule ? 'Aucune séance dans ces cinémas à cette date.' : 'Aucune séance à cette date.' }}</p>
          </div>

          <div v-else-if="visibleTheaters.length === 0" class="film-state mt-10">
            <CalendarDays :size="36" aria-hidden="true" />
            <p>Aucune séance ne correspond à ces filtres.</p>
            <button type="button" class="brutal-action" @click="resetFilters">Voir toutes les séances</button>
          </div>

          <div v-else class="mt-10 space-y-10">
            <section v-for="theater in visibleTheaters" :key="theater.id" class="theater-section border-2 border-ink bg-surface shadow-[7px_7px_0_#27272a]" :aria-labelledby="`theater-${theater.id}`">
              <div class="flex flex-wrap items-center justify-between gap-2 border-b-2 border-ink bg-[#f1efe8] px-4 py-4 sm:px-6">
                <h3 :id="`theater-${theater.id}`" class="text-xl font-black tracking-[-0.035em] text-ink sm:text-2xl">
                  <NuxtLink :to="`/cinema/${encodeURIComponent(theater.slug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary">
                    <BrandedText :text="theater.name" />
                  </NuxtLink>
                </h3>
                <NuxtLink :to="`/ville/${encodeURIComponent(theater.city_slug)}/cinemas`" class="flex min-h-11 items-center gap-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-ink underline decoration-2 underline-offset-4 hover:text-primary"><MapPin :size="15" aria-hidden="true" /> {{ theater.city }}</NuxtLink>
              </div>

              <ul class="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 p-4 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4 sm:p-6">
                <li v-for="showtime in theater.showtimes" :key="showtime.id" class="min-w-0">
                  <BookingLink
                    v-slot="{ available }"
                    :url="showtime.booking_url"
                    :provider="showtime.provider"
                    :aria-label="bookingLabel(showtime, theater, showtime.timingState)"
                    unstyled
                    class="showtime-card group relative flex h-full min-h-32 w-full scroll-mt-[19rem] flex-col items-start justify-between overflow-hidden border-2 p-3 text-left lg:scroll-mt-52"
                    :class="showtime.timingState === 'past' ? 'opacity-60' : showtime.timingState === 'warning' ? 'showtime-card--warning' : undefined"
                    :available-class="showtime.timingState === 'past' ? 'border-ink bg-surface text-ink' : 'border-ink bg-surface text-ink shadow-[4px_4px_0_#27272a] hover:bg-[#f1efe8]'"
                    unavailable-class="cursor-not-allowed border-dashed border-muted bg-[#e8e6de] text-muted shadow-none"
                  >
                    <div class="flex w-full items-baseline justify-between gap-2">
                      <span class="text-2xl font-black tracking-[-0.045em]">{{ formatParisTime(showtime.start_time) }}</span>
                      <span class="font-mono text-[9px] font-bold uppercase text-muted">fin {{ formatParisTime(showtime.end_time) }}</span>
                    </div>
                    <div class="mt-5 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
                      <span>{{ showtime.language }}</span>
                      <span aria-hidden="true">·</span>
                      <ShowtimeFormat :format="showtime.format" />
                      <template v-if="showtime.room">
                        <span aria-hidden="true">·</span>
                        <span>{{ formatRoom(showtime.room) }}</span>
                      </template>
                    </div>
                    <span v-if="showtime.timingState === 'warning'" class="mt-2 inline-flex items-center gap-1 text-xs font-black text-amber-800">
                      <AlertTriangle :size="14" aria-hidden="true" /> Séance commencée
                    </span>
                    <span v-else-if="showtime.timingState === 'past'" class="sr-only">Séance passée</span>
                    <span v-if="!available" class="mt-2 text-xs font-black">Réservation indisponible</span>
                    <svg
                      v-if="showtime.timingState === 'past'"
                      viewBox="0 0 100 100"
                      preserveAspectRatio="none"
                      aria-hidden="true"
                      focusable="false"
                      class="pointer-events-none absolute inset-0 size-full text-muted"
                    >
                      <line x1="0" y1="100" x2="100" y2="0" stroke="currentColor" stroke-width="1.5" vector-effect="non-scaling-stroke" />
                    </svg>
                  </BookingLink>
                </li>
              </ul>
            </section>
          </div>
        </div>
      </section>
    </template>
  </main>
</template>

<style scoped>
.film-page {
  background-color: #f8f7f2;
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.07) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.07) 1px, transparent 1px);
  background-size: 28px 28px;
}

.meta-chip,
.genre-chip {
  border: 2px solid #27272a;
  background: #ffcf3f;
  padding: 0.35rem 0.55rem;
  color: #27272a;
  line-height: 1;
}

.genre-chip {
  background: #fff;
  font-size: 0.7rem;
  font-weight: 800;
}

.editorial-link {
  display: inline-block;
  border-bottom: 2px solid currentColor;
  padding-bottom: 0.2rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.compact-control {
  display: flex;
  min-width: 0;
  min-height: 3rem;
  align-items: center;
  justify-content: center;
  gap: 0.4rem;
  padding: 0.5rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  text-transform: uppercase;
}

.compact-control:hover {
  background: #e8e6de;
}

.compact-control--active {
  background: #27272a;
  color: #fff;
}

.compact-control__text {
  display: flex;
  min-width: 0;
  flex-direction: column;
  line-height: 1.1;
}

.compact-control__text > :last-child {
  color: var(--color-muted);
  font-size: 0.58rem;
}

.compact-control--active .compact-control__text > :last-child {
  color: inherit;
}

.date-tab {
  min-height: 2.75rem;
  flex-shrink: 0;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.65rem 0.8rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 800;
  text-transform: uppercase;
}

.date-tab:hover {
  background: #e8e6de;
}

.date-tab--active {
  background: #27272a;
  color: #fff;
  box-shadow: inset 0 -4px 0 var(--color-highlight);
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

.filter-label {
  flex-shrink: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.58rem;
  font-weight: 900;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.filter-button,
.sort-button {
  display: inline-flex;
  min-height: 2rem;
  flex-shrink: 0;
  align-items: center;
  gap: 0.3rem;
  border: 1.5px solid #27272a;
  background: transparent;
  padding: 0.35rem 0.55rem;
  font-size: 0.72rem;
  font-weight: 800;
}

.filter-button:hover,
.sort-button:hover {
  background: #e8e6de;
}

.filter-button--active {
  background: #27272a;
  color: #fff;
}

.showtime-card--warning {
  outline: 3px solid #f59e0b;
  outline-offset: 2px;
}

.film-state {
  display: flex;
  min-height: 22rem;
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

.brutal-action {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border: 2px solid #27272a;
  background: #27272a;
  padding: 0.65rem 1rem;
  color: #fff;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.brutal-action:hover {
  background: #991b1b;
}

.brutal-action--light {
  background: #fff;
  color: #27272a;
}

.brutal-action--light:hover {
  background: var(--color-highlight);
}

@media (max-width: 639px) {
  .movie-title {
    overflow-wrap: anywhere;
  }

  .film-state {
    min-height: 19rem;
  }
}

</style>
