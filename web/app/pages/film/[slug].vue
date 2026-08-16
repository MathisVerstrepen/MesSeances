<script setup lang="ts">
import { AlertTriangle, CalendarDays, ExternalLink, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { ApiErrorResponse, MovieShowtimesResponse } from '~/types/api'
import { formatDateLabel, formatLongDate, formatParisTime, todayInParis } from '~/utils/date'

const route = useRoute()
const api = useMovieFlowApi()
const preferences = useCinemaPreferences()
const schedule = ref<MovieShowtimesResponse | null>(null)
const selectedDate = ref('')
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const posterFailed = ref(false)
let requestId = 0

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})

const availableDates = computed(() => [...new Set(preferences.favoriteTheaters.value.flatMap((theater) => theater.available_dates))].sort())
const showtimeCount = computed(() => schedule.value?.theaters.reduce((total, theater) => total + theater.showtimes.length, 0) ?? 0)
const posterAvailable = computed(() => Boolean(schedule.value?.movie.poster_url?.trim()) && !posterFailed.value)
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
  return typeof id === 'number' && Number.isFinite(id) && id > 0 ? `https://www.themoviedb.org/movie/${id}` : ''
})

function chooseDate(): boolean {
  if (availableDates.value.includes(selectedDate.value)) return false
  const today = todayInParis()
  selectedDate.value = availableDates.value.includes(today) ? today : availableDates.value[0] ?? today
  return true
}

function isNotFoundError(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const candidate = error as { status?: number; statusCode?: number; data?: ApiErrorResponse }
  return candidate.status === 404 || candidate.statusCode === 404 || candidate.data?.error?.code === 'not_found'
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
    if (currentRequest === requestId) schedule.value = response
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

  if (!chooseDate()) await loadSchedule()
}

async function retryLoad() {
  if (!preferences.isInitialized.value) await initializePreferencesAndLoad()
  else await loadSchedule()
}

watch(selectedDate, loadSchedule)
watch(
  () => preferences.favoriteTheaterIds.value.join(','),
  () => {
    if (!preferences.isInitialized.value) return
    if (!chooseDate()) loadSchedule()
  }
)
watch(slug, () => {
  schedule.value = null
  posterFailed.value = false
  loadSchedule()
})

onMounted(initializePreferencesAndLoad)

useHead(() => ({
  title: schedule.value?.movie.title ? `${schedule.value.movie.title} — MovieFlow` : 'Séances du film — MovieFlow'
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
        <NuxtLink to="/cinemas" class="inline-flex h-10 items-center justify-center rounded-md border border-line bg-surface px-5 text-sm font-semibold text-ink hover:border-stone-400">
          Mes cinémas
        </NuxtLink>
      </div>
    </div>

    <template v-else-if="schedule">
      <header class="grid gap-6 border-b border-line pb-8 sm:grid-cols-[144px_minmax(0,1fr)] sm:items-start">
        <div class="aspect-[2/3] w-32 overflow-hidden rounded-md border border-line bg-subtle shadow-sm sm:w-36">
          <img
            v-if="posterAvailable"
            :src="schedule.movie.poster_url!"
            :alt="`Affiche de ${schedule.movie.title}`"
            class="h-full w-full object-cover"
            @error="posterFailed = true"
          />
          <div v-else class="flex h-full flex-col items-center justify-center gap-2 px-3 text-center text-muted">
            <Film :size="32" aria-hidden="true" />
            <span class="text-xs font-medium">Affiche indisponible</span>
          </div>
        </div>
        <div class="min-w-0">
          <NuxtLink to="/films" class="text-sm font-medium text-accent hover:underline">Films à l’affiche</NuxtLink>
          <h1 class="mt-2 text-2xl font-semibold tracking-tight text-ink sm:text-3xl">{{ schedule.movie.title }}</h1>
          <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted">
            <span>{{ schedule.movie.runtime_minutes }} min</span>
            <template v-if="releaseDateLabel">
              <span aria-hidden="true">·</span>
              <time :datetime="schedule.movie.release_date!">{{ releaseDateLabel }}</time>
            </template>
          </div>
          <ul v-if="schedule.movie.genres.length" class="mt-3 flex flex-wrap gap-2" aria-label="Genres">
            <li v-for="genre in schedule.movie.genres" :key="genre" class="rounded-full bg-subtle px-2.5 py-1 text-xs font-medium text-stone-700">
              {{ genre }}
            </li>
          </ul>
          <div v-if="schedule.movie.overview?.trim()" class="mt-5 max-w-3xl">
            <h2 class="text-sm font-semibold text-ink">Synopsis</h2>
            <p class="mt-1.5 text-sm leading-6 text-muted">{{ schedule.movie.overview }}</p>
          </div>
          <a
            v-if="tmdbUrl"
            :href="tmdbUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="mt-4 inline-flex items-center gap-1.5 text-sm font-semibold text-accent underline-offset-2 hover:underline focus-visible:rounded-sm"
          >
            Voir sur TMDB <ExternalLink :size="15" aria-hidden="true" />
          </a>
        </div>
      </header>

      <section class="mt-7" aria-labelledby="schedule-heading">
        <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
          <div>
            <h2 id="schedule-heading" class="text-xl font-semibold text-ink">Séances</h2>
            <p class="mt-1 text-sm capitalize text-muted">{{ formatLongDate(selectedDate) }}</p>
          </div>
          <label class="block w-full text-sm font-medium text-ink sm:w-56">
            <span class="mb-1.5 flex items-center gap-2"><CalendarDays :size="16" class="text-muted" aria-hidden="true" /> Date</span>
            <select v-model="selectedDate" class="field" :disabled="pending">
              <option v-for="date in availableDates" :key="date" :value="date">{{ formatDateLabel(date) }}</option>
              <option v-if="availableDates.length === 0" :value="selectedDate">{{ formatDateLabel(selectedDate) }}</option>
            </select>
          </label>
        </div>

        <div class="mt-5 flex flex-wrap items-center justify-between gap-3 border-y border-line py-3 text-sm text-muted">
          <span>{{ showtimeCount }} séance{{ showtimeCount > 1 ? 's' : '' }}</span>
          <NuxtLink to="/cinemas" class="font-medium text-accent hover:underline">Modifier mes cinémas</NuxtLink>
        </div>

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

        <div v-else class="mt-6 space-y-8">
          <section v-for="theater in schedule.theaters" :key="theater.id" :aria-labelledby="`theater-${theater.id}`">
            <div class="flex flex-wrap items-baseline justify-between gap-2 border-b border-line pb-3">
              <h3 :id="`theater-${theater.id}`" class="font-semibold text-ink">{{ theater.name }}</h3>
              <span class="flex items-center gap-1.5 text-sm text-muted"><MapPin :size="15" aria-hidden="true" /> {{ theater.city }}</span>
            </div>

            <ul class="divide-y divide-line">
              <li v-for="showtime in theater.showtimes" :key="showtime.id" class="flex flex-col gap-4 py-4 sm:flex-row sm:items-center sm:justify-between">
                <div class="min-w-0">
                  <p class="text-lg font-semibold text-ink">{{ formatParisTime(showtime.start_time) }}</p>
                  <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted">
                    <span>{{ showtime.language }}</span>
                    <span>{{ showtime.format }}</span>
                    <span v-if="showtime.room">Salle {{ showtime.room }}</span>
                  </div>
                </div>
                <BookingLink :url="showtime.booking_url" class="shrink-0 self-start sm:self-auto" />
              </li>
            </ul>
          </section>
        </div>
      </section>
    </template>
  </main>
</template>
