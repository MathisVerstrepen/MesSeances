<script setup lang="ts">
import { AlertTriangle, CalendarDays, LoaderCircle, RefreshCw, Settings2 } from '@lucide/vue'
import type { Language, QueryFormat, TimelineResponse } from '~/types/api'
import { formatDateLabel, formatLongDate, todayInParis } from '~/utils/date'
import { formatOptions } from '~/utils/formats'
import { calendarDate, enumQueryValue, mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'

type TimelineMode = 'theater' | 'movie'
type TimelineZoom = 15 | 30 | 60

const OWNED_QUERY_KEYS = ['date', 'language', 'format', 'mode', 'zoom'] as const
const LANGUAGES: readonly Language[] = ['ALL', 'VOSTFR', 'VF']
const MODES: readonly TimelineMode[] = ['theater', 'movie']
const ZOOMS: readonly string[] = ['15', '30', '60']

const api = useMovieFlowApi()
const route = useRoute()
const router = useRouter()
const preferences = useCinemaPreferences()
const date = ref(todayInParis())
const language = ref<Language>('ALL')
const mode = ref<TimelineMode>('theater')
const formatFilter = ref<QueryFormat>('ALL')
const zoom = ref<TimelineZoom>(60)
const timeline = ref<TimelineResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
let requestId = 0
let isMounted = false
let isInitializing = false
let lastTimelineKey = ''

function matchesFormat(format: string) {
  if (formatFilter.value === 'ALL') return true
  return format.toUpperCase() === formatFilter.value
}

async function loadTimeline() {
  if (preferences.error.value) {
    timeline.value = null
    errorMessage.value = preferences.error.value
    pending.value = false
    return
  }
  if (!preferences.isInitialized.value) return

  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await api.timeline({
      date: date.value,
      language: language.value,
      theaters: preferences.favoriteTheaterIds.value.join(',')
    })
    if (currentRequest === requestId) timeline.value = response
  } catch (error) {
    if (currentRequest === requestId) {
      timeline.value = null
      errorMessage.value = getFrenchApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

async function retryTimeline() {
  pending.value = true
  errorMessage.value = ''
  await preferences.initialize()
  await loadTimeline()
}

function timelineQuery() {
  const today = todayInParis()
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    date: date.value === today ? undefined : date.value,
    language: language.value === 'ALL' ? undefined : language.value,
    format: formatFilter.value === 'ALL' ? undefined : formatFilter.value,
    mode: mode.value === 'theater' ? undefined : mode.value,
    zoom: zoom.value === 60 ? undefined : String(zoom.value)
  })
}

function hydrateRoute() {
  const queryDate = calendarDate(singularQueryValue(route.query.date))
  date.value = queryDate ?? todayInParis()
  language.value = enumQueryValue(singularQueryValue(route.query.language), LANGUAGES) ?? 'ALL'
  formatFilter.value = enumQueryValue(singularQueryValue(route.query.format), formatOptions.map((option) => option.value)) ?? 'ALL'
  mode.value = enumQueryValue(singularQueryValue(route.query.mode), MODES) ?? 'theater'
  const queryZoom = enumQueryValue(singularQueryValue(route.query.zoom), ZOOMS)
  zoom.value = queryZoom === '15' ? 15 : queryZoom === '30' ? 30 : 60
  return timelineQuery()
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  const key = `${date.value}|${language.value}|${preferences.favoriteTheaterIds.value.join(',')}`
  if (preferences.isInitialized.value && key !== lastTimelineKey) {
    lastTimelineKey = key
    await loadTimeline()
  }
}

function updateTimelineQuery(values: Partial<Record<'date' | 'language' | 'format' | 'mode' | 'zoom', string>>) {
  const query = mergeOwnedQuery(route.query, Object.keys(values), values)
  if (!queriesEqual(route.query, query)) router.push({ query })
}

function updateLanguage(event: Event) {
  if (!(event.currentTarget instanceof HTMLSelectElement)) return
  updateTimelineQuery({ language: event.currentTarget.value === 'ALL' ? undefined : event.currentTarget.value })
}

function createFallbackDates() {
  const [year, month, day] = todayInParis().split('-').map(Number)
  return Array.from({ length: 7 }, (_, offset) => {
    const value = new Date(Date.UTC(year!, month! - 1, day! + offset, 12))
    return [value.getUTCFullYear(), String(value.getUTCMonth() + 1).padStart(2, '0'), String(value.getUTCDate()).padStart(2, '0')].join('-')
  })
}

const dateOptions = computed(() => {
  const available = new Set(preferences.favoriteTheaters.value.flatMap((theater) => theater.available_dates))
  const options = available.size > 0 ? [...available].sort() : createFallbackDates()
  if (!options.includes(date.value)) options.push(date.value)
  return options.sort()
})

const showtimeCount = computed(() => timeline.value?.theaters.reduce((total, theater) => total + theater.showtimes.filter((showtime) => matchesFormat(showtime.format)).length, 0) ?? 0)
const rawShowtimeCount = computed(() => timeline.value?.theaters.reduce((total, theater) => total + theater.showtimes.length, 0) ?? 0)

watch(() => route.query, () => {
  if (isMounted) applyRoute()
})
watch(preferences.favoriteTheaterIds, () => {
  if (preferences.isInitialized.value && !isInitializing) applyRoute()
})

onMounted(async () => {
  isMounted = true
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) await router.replace({ query: canonicalQuery })
  isInitializing = true
  await preferences.initialize()
  isInitializing = false
  if (preferences.isInitialized.value) await applyRoute()
  else await loadTimeline()
})
</script>

<template>
  <main class="mx-auto max-w-[1440px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <div class="flex flex-col justify-between gap-5 lg:flex-row lg:items-center">
      <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Planning des séances</h1>
      <NuxtLink to="/cinemas" class="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-stone-400 hover:bg-subtle">
        <Settings2 :size="17" aria-hidden="true" /> Personnaliser
        <span class="rounded-full bg-subtle px-2 py-0.5 text-xs text-muted">{{ preferences.favoriteTheaterIds.value.length }}</span>
      </NuxtLink>
    </div>

    <div class="mt-6 overflow-x-auto pb-1 [scrollbar-width:thin]">
      <div class="flex min-w-max gap-2" aria-label="Choisir une date">
        <button
          v-for="option in dateOptions"
          :key="option"
          type="button"
          class="h-10 rounded-full border px-4 text-sm font-medium capitalize transition"
          :class="date === option ? 'border-accent bg-accent text-white' : 'border-line bg-surface text-ink hover:border-stone-400'"
          :aria-pressed="date === option"
          @click="updateTimelineQuery({ date: option === todayInParis() ? undefined : option })"
        >
          {{ formatDateLabel(option) }}
        </button>
      </div>
    </div>

    <div class="mt-5 flex flex-col gap-4 border-y border-line py-4 xl:flex-row xl:items-end xl:justify-between">
      <div class="flex flex-wrap gap-x-6 gap-y-4">
        <fieldset>
          <legend class="mb-1.5 text-xs font-semibold text-muted">Affichage</legend>
          <div class="inline-flex rounded-md border border-line bg-surface p-1">
            <button v-for="option in [{ value: 'theater', label: 'Par cinéma' }, { value: 'movie', label: 'Par film' }]" :key="option.value" type="button" class="h-8 rounded px-3 text-sm font-medium transition" :class="mode === option.value ? 'bg-ink text-white' : 'text-muted hover:text-ink'" :aria-pressed="mode === option.value" @click="updateTimelineQuery({ mode: option.value === 'theater' ? undefined : option.value })">{{ option.label }}</button>
          </div>
        </fieldset>

        <fieldset>
          <legend class="mb-1.5 text-xs font-semibold text-muted">Format</legend>
          <div class="inline-flex max-w-[calc(100vw-2rem)] overflow-x-auto rounded-md border border-line bg-surface p-1 sm:max-w-[calc(100vw-3rem)]">
            <button v-for="option in formatOptions" :key="option.value" type="button" class="h-8 shrink-0 rounded px-3 text-sm font-medium transition" :class="formatFilter === option.value ? 'bg-ink text-white' : 'text-muted hover:text-ink'" :aria-label="option.label" :aria-pressed="formatFilter === option.value" @click="updateTimelineQuery({ format: option.value === 'ALL' ? undefined : option.value })">
              <BrandLogo v-if="option.brand" :brand="option.brand" decorative :class="formatFilter === option.value ? 'brightness-0 invert' : ''" />
              <span v-else>{{ option.label }}</span>
            </button>
          </div>
        </fieldset>

        <label class="block text-xs font-semibold text-muted">
          <span class="mb-1.5 block">Langue</span>
          <select :value="language" class="field min-w-32" @change="updateLanguage">
            <option value="ALL">Toutes</option>
            <option value="VOSTFR">VOSTFR</option>
            <option value="VF">VF</option>
          </select>
        </label>
      </div>

      <fieldset>
        <legend class="mb-1.5 text-xs font-semibold text-muted">Zoom</legend>
        <div class="inline-flex rounded-md border border-line bg-surface p-1">
          <button v-for="option in [{ value: 15, label: '15 min' }, { value: 30, label: '30 min' }, { value: 60, label: '1 h' }]" :key="option.value" type="button" class="h-8 rounded px-3 text-sm font-medium transition" :class="zoom === option.value ? 'bg-ink text-white' : 'text-muted hover:text-ink'" :aria-pressed="zoom === option.value" @click="updateTimelineQuery({ zoom: option.value === 60 ? undefined : String(option.value) })">{{ option.label }}</button>
        </div>
      </fieldset>
    </div>

    <div class="mb-3 mt-6 flex items-baseline justify-between gap-4">
      <h2 class="text-base font-semibold capitalize text-ink">{{ formatLongDate(date) }}</h2>
      <p v-if="timeline && !pending" class="text-sm text-muted">{{ showtimeCount }} séance{{ showtimeCount > 1 ? 's' : '' }}</p>
    </div>

    <div v-if="pending" class="state-panel" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement des séances…</p>
    </div>

    <div v-else-if="errorMessage" class="state-panel" role="alert">
      <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
      <p class="max-w-lg">{{ errorMessage }}</p>
      <button type="button" class="button-primary" @click="retryTimeline">
        <RefreshCw :size="17" aria-hidden="true" /> Réessayer
      </button>
    </div>

    <div v-else-if="!timeline || rawShowtimeCount === 0" class="state-panel">
      <CalendarDays :size="28" class="text-muted" aria-hidden="true" />
      <p>Aucune séance pour cette date et cette langue.</p>
    </div>

    <TimelineMatrix v-else :timeline="timeline" :mode="mode" :format-filter="formatFilter" :zoom="zoom" />

  </main>
</template>
