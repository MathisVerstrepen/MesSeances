<script setup lang="ts">
import { AlertTriangle, ArrowDownUp, CalendarDays, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import tmdbLogo from '~/assets/imgs/logo_tmdb.svg?no-inline'
import type { MovieShowtimesResponse, MovieShowtimesTheater, Showtime, ShowtimeFormat } from '~/types/api'
import { formatDateLabel, formatLongDate, formatParisTime, todayInParis } from '~/utils/date'
import { formatLabel, isShowtimeFormat } from '~/utils/formats'
import { calendarDate, enumQueryValue, mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'

type LanguageFilter = 'ALL' | Showtime['language']
type TechnologyFilter = 'ALL' | ShowtimeFormat
type ShowtimeTimingState = 'upcoming' | 'warning' | 'past'

const SHOWTIME_WARNING_DURATION_MS = 20 * 60 * 1000
const OWNED_QUERY_KEYS = ['date', 'language', 'format', 'sort'] as const
const SHOWTIME_LANGUAGES: readonly Showtime['language'][] = ['VOSTFR', 'VF', 'VO', 'VF_SME']

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const preferences = useCinemaPreferences()
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
const currentTime = ref<number | null>(null)
let requestId = 0
let currentTimeTimer: number | undefined
let isReady = false
let lastScheduleKey = ''

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})

const availableDates = computed(() => [...new Set(preferences.favoriteTheaters.value.flatMap((theater) => theater.available_dates))].sort())
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

  const favoriteOrder = new Map(preferences.favoriteTheaterIds.value.map((id, index) => [id, index]))
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
  const today = todayInParis()
  return availableDates.value.includes(today) ? today : availableDates.value[0] ?? today
}

function isAvailableDate(value: string | undefined): value is string {
  if (!value) return false
  return availableDates.value.length > 0 ? availableDates.value.includes(value) : value === todayInParis()
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
  if (preferences.favoriteTheaterIds.value.length === 0) {
    pending.value = false
    schedule.value = null
    errorMessage.value = preferences.error.value || 'Sélectionnez au moins un cinéma favori pour consulter les séances.'
    return
  }

  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  notFound.value = false

  try {
    const response = await api.movieShowtimes(slug.value, {
      date: selectedDate.value,
      theaters: preferences.favoriteTheaterIds.value.join(',')
    })
    if (currentRequest === requestId) {
      schedule.value = response
      await nextTick()
      await normalizeDynamicFilters()
    }
  } catch (error) {
    if (currentRequest === requestId) {
      schedule.value = null
      notFound.value = isNotFoundError(error)
      if (!notFound.value) errorMessage.value = getFrenchApiError(error)
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

  const key = `${slug.value}|${selectedDate.value}|${preferences.favoriteTheaterIds.value.join(',')}`
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
    schedule.value = null
    errorMessage.value = preferences.error.value || 'Impossible de charger vos cinémas favoris.'
    return
  }

  isReady = true
  await applyRoute()
}

async function retryLoad() {
  if (!preferences.isInitialized.value) await initializePreferencesAndLoad()
  else await loadSchedule()
}

watch(
  () => preferences.favoriteTheaterIds.value.join(','),
  () => {
    if (isReady) applyRoute()
  }
)
watch(() => route.query, () => {
  if (isReady) applyRoute()
})
watch(slug, () => {
  schedule.value = null
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
  initializePreferencesAndLoad()
})
onBeforeUnmount(() => {
  if (currentTimeTimer !== undefined) window.clearInterval(currentTimeTimer)
})

useHead(() => ({
  title: schedule.value?.movie.title ? `${schedule.value.movie.title} — MesSeances` : 'Séances du film — MesSeances'
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
        <button v-if="!preferences.isInitialized.value || preferences.favoriteTheaterIds.value.length" type="button" class="brutal-action" @click="retryLoad">
          <RefreshCw :size="17" aria-hidden="true" /> Réessayer
        </button>
        <NuxtLink to="/cinemas" class="brutal-action brutal-action--light">
          Mes cinémas
        </NuxtLink>
      </div>
    </div>

    <template v-else-if="schedule">
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
          class="absolute right-4 top-4 z-20 inline-flex min-h-11 min-w-11 items-center justify-center p-1 hover:opacity-75 focus-visible:ring-2 focus-visible:ring-[#d7ff38] focus-visible:ring-offset-4 sm:right-6 sm:top-6 lg:right-8 lg:top-8"
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
          <NuxtLink to="/cinemas" class="editorial-link shrink-0 self-start sm:self-end">Modifier mes cinémas</NuxtLink>
        </div>

        <div class="filter-dock sticky top-[7.5rem] z-20 -mx-4 mt-5 border-y-2 border-ink bg-[#ffcf3f]/95 px-4 py-4 shadow-[0_6px_0_#27272a] backdrop-blur sm:-mx-6 sm:px-6 lg:top-[4.5rem] lg:-mx-10 lg:px-10">
          <div
            class="flex gap-2 overflow-x-auto pb-1"
            role="tablist"
            aria-label="Choisir une date"
          >
            <button
              v-for="(date, index) in availableDates"
              :id="`date-tab-${date}`"
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
            <span v-if="availableDates.length === 0" class="inline-flex h-10 items-center font-mono text-xs font-bold uppercase">{{ formatDateLabel(selectedDate) }}</span>
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

        <div id="showtime-panel" role="tabpanel" :aria-labelledby="availableDates.length ? `date-tab-${selectedDate}` : undefined" :aria-busy="pending">
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

          <div v-else-if="schedule.theaters.length === 0" class="film-state mt-10">
            <CalendarDays :size="36" aria-hidden="true" />
            <p>Aucune séance dans vos cinémas favoris à cette date.</p>
          </div>

          <div v-else-if="visibleTheaters.length === 0" class="film-state mt-10">
            <CalendarDays :size="36" aria-hidden="true" />
            <p>Aucune séance ne correspond à ces filtres.</p>
            <button type="button" class="brutal-action" @click="resetFilters">Voir toutes les séances</button>
          </div>

          <div v-else class="mt-10 space-y-10">
            <section v-for="theater in visibleTheaters" :key="theater.id" class="theater-section border-2 border-ink bg-surface shadow-[7px_7px_0_#27272a]" :aria-labelledby="`theater-${theater.id}`">
              <div class="flex flex-wrap items-center justify-between gap-2 border-b-2 border-ink bg-[#d7ff38] px-4 py-4 sm:px-6">
                <h3 :id="`theater-${theater.id}`" class="text-xl font-black tracking-[-0.035em] text-ink sm:text-2xl"><BrandedText :text="theater.name" /></h3>
                <span class="flex items-center gap-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-ink"><MapPin :size="15" aria-hidden="true" /> {{ theater.city }}</span>
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
                    :available-class="showtime.timingState === 'past' ? 'border-ink bg-surface text-ink' : 'border-ink bg-surface text-ink shadow-[4px_4px_0_#27272a] hover:bg-[#ffcf3f]'"
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

.date-tab:hover,
.date-tab--active {
  background: #27272a;
  color: #fff;
}

.date-tab--active {
  box-shadow: inset 0 -4px 0 #d7ff38;
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
.sort-button:hover,
.filter-button--active {
  background: #27272a;
  color: #fff;
}

.showtime-card {
  transition: background-color 150ms ease, transform 150ms ease, box-shadow 150ms ease;
}

.showtime-card[href]:hover {
  transform: translateY(-2px);
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
  background: #d7ff38;
}

@media (max-width: 639px) {
  .movie-title {
    overflow-wrap: anywhere;
  }

  .film-state {
    min-height: 19rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .showtime-card {
    transition: none;
  }

  .showtime-card[href]:hover {
    transform: none;
  }
}
</style>
