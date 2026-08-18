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
  <main class="mx-auto max-w-[1120px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <div v-if="pending && !schedule" class="state-panel" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement des séances…</p>
    </div>

    <div v-else-if="notFound" class="state-panel" role="alert">
      <Film :size="30" class="text-muted" aria-hidden="true" />
      <div>
        <p class="text-lg font-semibold text-ink">Film introuvable</p>
        <p class="mt-1 text-sm">Ce film n’est pas disponible dans le catalogue actuel.</p>
      </div>
      <NuxtLink to="/films" class="button-primary">Voir les films</NuxtLink>
    </div>

    <div v-else-if="errorMessage && !schedule" class="state-panel" role="alert">
      <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
      <p class="max-w-lg">{{ errorMessage }}</p>
      <div class="flex flex-wrap justify-center gap-3">
        <button v-if="!preferences.isInitialized.value || preferences.favoriteTheaterIds.value.length" type="button" class="button-primary" @click="retryLoad">
          <RefreshCw :size="17" aria-hidden="true" /> Réessayer
        </button>
        <NuxtLink to="/cinemas" class="inline-flex h-10 items-center justify-center rounded-md border border-line bg-surface px-5 text-sm font-semibold text-ink hover:border-line-hover">
          Mes cinémas
        </NuxtLink>
      </div>
    </div>

    <template v-else-if="schedule">
      <header
        class="relative grid gap-6 border-b pb-8 sm:grid-cols-[144px_minmax(0,1fr)] sm:items-start"
        :class="backdropAvailable ? 'isolate overflow-hidden rounded-lg border-transparent px-4 pt-6 sm:px-6 lg:px-8' : 'border-line'"
      >
        <img
          v-if="backdropAvailable"
          :src="backdropUrl ?? undefined"
          alt=""
          aria-hidden="true"
          class="absolute inset-0 -z-20 size-full object-cover"
          @error="backdropFailed = true"
        />
        <div v-if="backdropAvailable" class="absolute inset-0 -z-10 bg-gradient-to-r from-black/95 via-black/80 to-black/70" aria-hidden="true" />
        <a
          v-if="tmdbUrl"
          :href="tmdbUrl"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="Voir ce film sur TMDB (nouvel onglet)"
          class="absolute right-0 top-0 z-20 inline-flex min-h-11 min-w-11 items-center justify-center rounded-md p-2 transition hover:bg-subtle focus-visible:ring-2 focus-visible:ring-accent"
          :class="backdropAvailable ? 'right-4 top-4 hover:bg-white/15 focus-visible:ring-accent-line focus-visible:ring-offset-black sm:right-6 lg:right-8' : undefined"
        >
          <img :src="tmdbLogo" alt="" class="h-auto w-20" />
        </a>
        <div
          class="aspect-[2/3] w-32 overflow-hidden rounded-md border shadow-sm sm:w-36"
          :class="backdropAvailable ? 'relative z-10 border-white/25 bg-black/40' : 'border-line bg-subtle'"
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
            class="flex h-full flex-col items-center justify-center gap-2 px-3 text-center"
            :class="backdropAvailable ? 'text-white/80' : 'text-muted'"
          >
            <Film :size="32" aria-hidden="true" />
            <span class="text-xs font-medium">Affiche indisponible</span>
          </div>
        </div>
        <div class="min-w-0" :class="[backdropAvailable ? 'relative z-10' : undefined, tmdbUrl ? 'sm:pr-28' : undefined]">
          <h1 class="text-2xl font-semibold tracking-tight sm:text-3xl" :class="backdropAvailable ? 'text-white' : 'text-ink'">{{ schedule.movie.title }}</h1>
          <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm" :class="backdropAvailable ? 'text-white/80' : 'text-muted'">
            <span>{{ schedule.movie.runtime_minutes }} min</span>
            <template v-if="releaseDateLabel">
              <span aria-hidden="true">·</span>
              <time :datetime="schedule.movie.release_date!">{{ releaseDateLabel }}</time>
            </template>
          </div>
          <ul v-if="schedule.movie.genres.length" class="mt-3 flex flex-wrap gap-2" aria-label="Genres">
            <li
              v-for="genre in schedule.movie.genres"
              :key="genre"
              class="rounded-full px-2.5 py-1 text-xs font-medium"
              :class="backdropAvailable ? 'bg-white/15 text-white' : 'bg-subtle text-ink'"
            >
              {{ genre }}
            </li>
          </ul>
          <div v-if="schedule.movie.overview?.trim()" class="mt-5 max-w-3xl">
            <h2 class="text-sm font-semibold" :class="backdropAvailable ? 'text-white' : 'text-ink'">Synopsis</h2>
            <p class="mt-1.5 text-sm leading-6" :class="backdropAvailable ? 'text-white/80' : 'text-muted'">{{ schedule.movie.overview }}</p>
          </div>
        </div>
      </header>

      <section class="mt-7" aria-labelledby="schedule-heading">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
          <div>
            <h2 id="schedule-heading" class="flex flex-wrap items-baseline gap-x-2 text-xl font-semibold text-ink">
              <span>Séances</span>
              <span class="font-medium text-muted"><span aria-hidden="true">·</span> {{ visibleShowtimeCount }} horaire{{ visibleShowtimeCount === 1 ? '' : 's' }}</span>
            </h2>
            <p class="mt-1 text-sm capitalize text-muted">{{ formatLongDate(selectedDate) }}</p>
          </div>
          <NuxtLink to="/cinemas" class="shrink-0 self-start text-sm font-medium text-accent hover:underline">Modifier mes cinémas</NuxtLink>
        </div>

        <div class="sticky top-[6.5rem] z-20 -mx-4 mt-4 bg-canvas/95 px-4 py-3 backdrop-blur sm:-mx-6 sm:px-6 lg:top-0 lg:-mx-10 lg:px-10">
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
              class="h-10 shrink-0 rounded-md px-4 text-sm font-semibold transition"
              :class="selectedDate === date ? 'bg-accent text-white' : 'text-muted hover:bg-subtle hover:text-ink'"
              @click="updateFilmQuery({ date: date === fallbackDate() ? undefined : date })"
              @keydown="selectAdjacentDate($event, index)"
            >
              {{ formatDateLabel(date) }}
            </button>
            <span v-if="availableDates.length === 0" class="inline-flex h-10 items-center text-sm text-muted">{{ formatDateLabel(selectedDate) }}</span>
          </div>

          <div class="mt-2 flex flex-col gap-2">
            <div v-if="languages.length > 1" class="flex flex-wrap items-center gap-2">
              <span id="language-filter-label" class="text-xs font-semibold uppercase tracking-wide text-muted">Langue</span>
              <div class="flex max-w-full gap-1 overflow-x-auto" role="group" aria-labelledby="language-filter-label">
                <button
                  v-for="option in languageOptions"
                  :key="option.value"
                  type="button"
                  class="h-8 shrink-0 rounded px-2 text-sm font-medium transition"
                  :class="activeLanguage === option.value ? 'bg-accent text-white' : 'text-muted hover:bg-subtle hover:text-ink'"
                  :aria-pressed="activeLanguage === option.value"
                  @click="updateFilmQuery({ language: option.value === 'ALL' ? undefined : option.value })"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>
            <div class="flex min-w-0 items-center gap-2">
              <template v-if="technologyFormats.length > 1">
                <span id="technology-filter-label" class="shrink-0 text-xs font-semibold uppercase tracking-wide text-muted">Technologie</span>
                <div class="flex min-w-0 gap-1 overflow-x-auto" role="group" aria-labelledby="technology-filter-label">
                  <button
                    v-for="option in technologyOptions"
                    :key="option.value"
                    type="button"
                    class="h-8 shrink-0 rounded px-2 text-sm font-medium transition"
                    :class="activeTechnology === option.value ? 'bg-accent text-white' : 'text-muted hover:bg-subtle hover:text-ink'"
                    :aria-pressed="activeTechnology === option.value"
                    @click="updateFilmQuery({ format: option.value === 'ALL' ? undefined : option.value })"
                  >
                    {{ option.label }}
                  </button>
                </div>
              </template>
              <button
                type="button"
                class="ml-auto inline-flex h-8 shrink-0 items-center gap-1 rounded px-2 text-xs font-medium transition"
                :class="sortByNextShowtime ? 'bg-accent text-white' : 'text-muted hover:bg-subtle hover:text-ink'"
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
          <div v-if="pending" class="state-panel mt-6" role="status" aria-live="polite">
            <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
            <p>Chargement des séances…</p>
          </div>

          <div v-else-if="errorMessage" class="state-panel mt-6" role="alert">
            <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
            <p class="max-w-lg">{{ errorMessage }}</p>
            <button type="button" class="button-primary" @click="loadSchedule">
              <RefreshCw :size="17" aria-hidden="true" /> Réessayer
            </button>
          </div>

          <div v-else-if="schedule.theaters.length === 0" class="state-panel mt-6">
            <CalendarDays :size="30" class="text-muted" aria-hidden="true" />
            <p>Aucune séance dans vos cinémas favoris à cette date.</p>
          </div>

          <div v-else-if="visibleTheaters.length === 0" class="state-panel mt-6">
            <CalendarDays :size="30" class="text-muted" aria-hidden="true" />
            <p>Aucune séance ne correspond à ces filtres.</p>
            <button type="button" class="button-primary" @click="resetFilters">Voir toutes les séances</button>
          </div>

          <div v-else class="mt-6 space-y-9">
            <section v-for="theater in visibleTheaters" :key="theater.id" :aria-labelledby="`theater-${theater.id}`">
              <div class="flex flex-wrap items-center justify-between gap-2 border-b border-line pb-3">
                <h3 :id="`theater-${theater.id}`" class="text-lg font-semibold text-ink"><BrandedText :text="theater.name" /></h3>
                <span class="flex items-center gap-1.5 text-sm text-muted"><MapPin :size="15" aria-hidden="true" /> {{ theater.city }}</span>
              </div>

              <ul class="mt-3 grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 sm:grid-cols-[repeat(auto-fill,minmax(168px,1fr))]">
                <li v-for="showtime in theater.showtimes" :key="showtime.id" class="min-w-0">
                  <BookingLink
                    v-slot="{ available }"
                    :url="showtime.booking_url"
                    :provider="showtime.provider"
                    :aria-label="bookingLabel(showtime, theater, showtime.timingState)"
                    unstyled
                    class="group relative flex h-full min-h-28 w-full scroll-mt-[17rem] flex-col items-start justify-between overflow-hidden rounded-lg border p-3 text-left transition lg:scroll-mt-36"
                    :class="showtime.timingState === 'past' ? 'opacity-60' : showtime.timingState === 'warning' ? 'ring-2 ring-amber-400 ring-offset-1' : undefined"
                    :available-class="showtime.timingState === 'past' ? 'border-line bg-surface text-ink shadow-sm' : 'border-line bg-surface text-ink shadow-sm hover:border-accent hover:bg-surface hover:shadow-md'"
                    unavailable-class="cursor-not-allowed border-dashed border-line bg-subtle text-muted shadow-none"
                  >
                    <div class="flex w-full items-baseline justify-between gap-2">
                      <span class="text-xl font-bold tracking-tight">{{ formatParisTime(showtime.start_time) }}</span>
                      <span class="text-xs font-normal text-muted">fin {{ formatParisTime(showtime.end_time) }}</span>
                    </div>
                    <div class="mt-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs font-medium text-muted">
                      <span>{{ showtime.language }}</span>
                      <span aria-hidden="true">·</span>
                      <ShowtimeFormat :format="showtime.format" />
                      <template v-if="showtime.room">
                        <span aria-hidden="true">·</span>
                        <span>{{ formatRoom(showtime.room) }}</span>
                      </template>
                    </div>
                    <span v-if="showtime.timingState === 'warning'" class="mt-2 inline-flex items-center gap-1 text-xs font-semibold text-amber-700">
                      <AlertTriangle :size="14" aria-hidden="true" /> Séance commencée
                    </span>
                    <span v-else-if="showtime.timingState === 'past'" class="sr-only">Séance passée</span>
                    <span v-if="!available" class="mt-2 text-xs font-semibold">Réservation indisponible</span>
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
