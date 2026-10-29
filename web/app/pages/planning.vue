<script setup lang="ts">
import { isBroadTheaterSelection } from '~/utils/cinemaSelection'
import {
  AlertTriangle,
  CalendarDays,
  Clock3,
  LoaderCircle,
  RefreshCw,
} from '@lucide/vue'
import type { Language, QueryFormat, TimelineResponse } from '~/types/api'
import { formatLongDate, todayInParis } from '~/utils/date'
import {
  calendarDate,
  enumQueryValue,
  mergeOwnedQuery,
  queriesEqual,
  singularQueryValue,
} from '~/utils/routeQuery'
import { queryFormatValues, queryLanguageValues } from '~/utils/showtimeFilters'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { projectTimeline } from '~/utils/watchedDiscovery'

type TimelineMode = 'theater' | 'movie'
type TimelineZoom = 15 | 30 | 60

const OWNED_QUERY_KEYS = ['date', 'language', 'format', 'mode', 'zoom'] as const
const MODES: readonly TimelineMode[] = ['theater', 'movie']
const ZOOMS: readonly string[] = ['15', '30', '60']
const THEATER_BATCH_SIZE = 10

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const preferences = usePageCinemaSelection()
const discovery = useWatchedDiscovery('planning')
const today = ref(todayInParis())
const date = ref(today.value)
const language = ref<Language>('ALL')
const mode = ref<TimelineMode>('theater')
const formatFilter = ref<QueryFormat>('ALL')
const zoom = ref<TimelineZoom>(30)
const timeline = ref<TimelineResponse | null>(null)
const timelineMatrix = ref<{
  scrollToNow: () => void
  canScrollToNow: boolean
} | null>(null)
const displayedTimeline = computed(() =>
  !timeline.value || discovery.blocked.value
    ? null
    : discovery.hide.value
      ? projectTimeline(timeline.value, discovery.watched.slugs.value)
      : timeline.value,
)
const pending = ref(true)
const errorMessage = ref('')
const appendPending = ref(false)
const appendError = ref('')
const fallbackTheaterIds = ref<string[]>([])
const queriedTheaterCount = ref(0)
const fallbackScope = computed(
  () => preferences.activeTheaterIds.value.length === 0,
)
const hasMoreTheaters = computed(
  () =>
    fallbackScope.value &&
    queriedTheaterCount.value < fallbackTheaterIds.value.length,
)
let requestId = 0
let isMounted = false
let isUnmounted = false
let isInitializing = false
let lastTimelineKey = ''
let dayCheckTimer: number | undefined

function matchesFormat(format: string) {
  if (formatFilter.value === 'ALL') return true
  return format.toUpperCase() === formatFilter.value
}

function resetTimeline() {
  requestId++
  timeline.value = null
  appendPending.value = false
  appendError.value = ''
  fallbackTheaterIds.value = []
  queriedTheaterCount.value = 0
  errorMessage.value = ''
}

async function loadTimeline() {
  if (isUnmounted) return
  resetTimeline()
  const currentRequest = requestId
  if (preferences.error.value) {
    timeline.value = null
    errorMessage.value = preferences.error.value
    pending.value = false
    return
  }
  if (!preferences.isInitialized.value) {
    pending.value = true
    return
  }
  pending.value = true
  errorMessage.value = ''
  if (fallbackScope.value) {
    fallbackTheaterIds.value = preferences.theaters.value.map(
      (theater) => theater.id,
    )
    if (fallbackTheaterIds.value.length === 0) {
      pending.value = false
      return
    }
  }
  const batch = fallbackTheaterIds.value.slice(0, THEATER_BATCH_SIZE)
  try {
    const response = await api.timeline({
      date: date.value,
      language: language.value,
      theaters: fallbackScope.value
        ? batch.join(',')
        : isBroadTheaterSelection(
              preferences.activeTheaterIds.value,
              preferences.theaters.value,
            )
          ? undefined
          : preferences.activeTheaterIds.value.join(','),
    })
    if (currentRequest === requestId) {
      timeline.value = response
      queriedTheaterCount.value = batch.length
    }
  } catch (error) {
    if (currentRequest === requestId) {
      timeline.value = null
      errorMessage.value = getFrenchApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

async function loadMore() {
  if (
    isUnmounted ||
    pending.value ||
    appendPending.value ||
    !preferences.isInitialized.value ||
    preferences.error.value ||
    !hasMoreTheaters.value ||
    !timeline.value
  )
    return
  const currentRequest = requestId
  const currentTimeline = timeline.value
  const batch = fallbackTheaterIds.value.slice(
    queriedTheaterCount.value,
    queriedTheaterCount.value + THEATER_BATCH_SIZE,
  )
  appendPending.value = true
  appendError.value = ''
  try {
    const response = await api.timeline({
      date: date.value,
      language: language.value,
      theaters: batch.join(','),
    })
    if (currentRequest !== requestId) return
    // Offsets are relative to the API's fixed per-date window. Never mix windows.
    if (
      response.date !== currentTimeline.date ||
      response.timezone !== currentTimeline.timezone ||
      response.window_start_time !== currentTimeline.window_start_time ||
      response.window_end_time !== currentTimeline.window_end_time
    )
      throw new Error('Inconsistent timeline window')
    const existingIds = new Set(
      currentTimeline.theaters.map((theater) => theater.id),
    )
    timeline.value = {
      ...currentTimeline,
      theaters: [
        ...currentTimeline.theaters,
        ...response.theaters.filter((theater) => {
          if (existingIds.has(theater.id)) return false
          existingIds.add(theater.id)
          return true
        }),
      ],
    }
    queriedTheaterCount.value += batch.length
  } catch (error) {
    if (currentRequest === requestId)
      appendError.value = getFrenchApiError(error)
  } finally {
    if (currentRequest === requestId) appendPending.value = false
  }
}

async function retryTimeline() {
  if (isUnmounted) return
  pending.value = true
  errorMessage.value = ''
  await preferences.retrySynchronization()
  await loadTimeline()
}

function timelineQuery() {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    date: date.value === today.value ? undefined : date.value,
    language: language.value === 'ALL' ? undefined : language.value,
    format: formatFilter.value === 'ALL' ? undefined : formatFilter.value,
    mode: mode.value === 'theater' ? undefined : mode.value,
    zoom: zoom.value === 30 ? undefined : String(zoom.value),
  })
}

function hydrateRoute() {
  const queryDate = calendarDate(singularQueryValue(route.query.date))
  date.value = queryDate && queryDate >= today.value ? queryDate : today.value
  language.value =
    enumQueryValue(
      singularQueryValue(route.query.language),
      queryLanguageValues,
    ) ?? 'ALL'
  formatFilter.value =
    enumQueryValue(singularQueryValue(route.query.format), queryFormatValues) ??
    'ALL'
  mode.value =
    enumQueryValue(singularQueryValue(route.query.mode), MODES) ?? 'theater'
  const queryZoom = enumQueryValue(singularQueryValue(route.query.zoom), ZOOMS)
  zoom.value = queryZoom === '15' ? 15 : queryZoom === '60' ? 60 : 30
  return timelineQuery()
}

async function applyRoute() {
  if (isUnmounted) return
  const canonicalQuery = hydrateRoute()
  const key = `${date.value}|${language.value}|${preferences.activeTheaterIds.value.join(',')}`
  let loading: Promise<void> | undefined
  if (preferences.isInitialized.value && key !== lastTimelineKey) {
    lastTimelineKey = key
    loading = loadTimeline()
  }
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
  }
  await loading
}

function updateTimelineQuery(
  values: Partial<
    Record<
      'date' | 'language' | 'format' | 'mode' | 'zoom' | 'shared_theaters',
      string
    >
  >,
) {
  const query = mergeOwnedQuery(route.query, Object.keys(values), values)
  if (!queriesEqual(route.query, query)) router.replace({ query })
}

function createFallbackDates() {
  const [year, month, day] = today.value.split('-').map(Number)
  return Array.from({ length: 7 }, (_, offset) => {
    const value = new Date(Date.UTC(year!, month! - 1, day! + offset, 12))
    return [
      value.getUTCFullYear(),
      String(value.getUTCMonth() + 1).padStart(2, '0'),
      String(value.getUTCDate()).padStart(2, '0'),
    ].join('-')
  })
}

const dateOptions = computed(() => {
  const available = new Set(
    preferences.activeTheaters.value.flatMap(
      (theater) => theater.available_dates,
    ),
  )
  const options =
    available.size > 0
      ? [...available].filter((option) => option >= today.value).sort()
      : createFallbackDates()
  if (date.value >= today.value && !options.includes(date.value))
    options.push(date.value)
  return options.sort()
})

function selectPlanningDate(selectedDate: string) {
  updateTimelineQuery({
    date: selectedDate === today.value ? undefined : selectedDate,
  })
}

const showtimeCount = computed(
  () =>
    displayedTimeline.value?.theaters.reduce(
      (total, theater) =>
        total +
        theater.showtimes.filter((showtime) => matchesFormat(showtime.format))
          .length,
      0,
    ) ?? 0,
)
const rawShowtimeCount = computed(
  () =>
    displayedTimeline.value?.theaters.reduce(
      (total, theater) => total + theater.showtimes.length,
      0,
    ) ?? 0,
)

async function refreshPlanningDay() {
  const currentDay = todayInParis()
  if (currentDay === today.value) return
  today.value = currentDay
  await applyRoute()
}
watch([discovery.key, pending, appendPending, formatFilter], () => {
  if (
    discovery.hide.value &&
    !discovery.blocked.value &&
    !pending.value &&
    !appendPending.value &&
    !appendError.value &&
    showtimeCount.value === 0 &&
    hasMoreTheaters.value
  )
    void loadMore()
})

function scheduleDayCheck() {
  if (dayCheckTimer) window.clearTimeout(dayCheckTimer)
  const delay = 60_000 - (Date.now() % 60_000)
  dayCheckTimer = window.setTimeout(() => {
    dayCheckTimer = undefined
    void refreshPlanningDay()
    scheduleDayCheck()
  }, delay)
}

function handleVisibilityChange() {
  if (document.visibilityState !== 'visible') return
  void refreshPlanningDay()
  scheduleDayCheck()
}

watch(
  () => route.query,
  () => {
    if (isMounted) applyRoute()
  },
  { flush: 'sync' },
)
watch(
  [
    preferences.activeTheaterIds,
    preferences.selectionScopeKey,
    preferences.theaters,
    preferences.isInitialized,
    preferences.error,
  ],
  () => {
    resetTimeline()
    lastTimelineKey = ''
    if (isMounted && !isInitializing) {
      if (preferences.isInitialized.value) void applyRoute()
      else void loadTimeline()
    }
  },
  { flush: 'sync' },
)

onMounted(async () => {
  isMounted = true
  scheduleDayCheck()
  document.addEventListener('visibilitychange', handleVisibilityChange)
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery))
    await router.replace({ query: canonicalQuery })
  if (isUnmounted) return
  isInitializing = true
  await preferences.initialize()
  if (isUnmounted) return
  isInitializing = false
  if (preferences.isInitialized.value) await applyRoute()
  else await loadTimeline()
})

onBeforeUnmount(() => {
  isMounted = false
  isUnmounted = true
  requestId++
  if (dayCheckTimer) window.clearTimeout(dayCheckTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/planning')
const pageTitle = 'Planning des séances - MesSeances'
const pageDescription =
  'Visualisez les séances de vos cinémas sur une frise et organisez votre sortie.'

useSeoMeta({
  title: pageTitle,
  description: pageDescription,
  robots: 'noindex,follow',
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main
    id="planning-catalog"
    :aria-busy="pending"
    class="min-h-[calc(100vh-4.5rem)] bg-[#f8f7f2] px-2 py-3 text-ink [background-image:linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] [background-size:28px_28px] sm:px-4 sm:py-4 lg:px-6"
  >
    <h1 class="sr-only">Planning des séances</h1>

    <section
      class="sticky top-[4.5rem] z-20 border-2 border-ink bg-[#f1efe8]/95 shadow-[6px_6px_0_#27272a] backdrop-blur max-xl:relative max-xl:top-auto"
      aria-label="Filtres du planning"
    >
      <div
        class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 p-2 min-[384px]:grid-cols-[minmax(0,1fr)_auto_auto] sm:gap-3 sm:p-3"
      >
        <ShowtimeDateBar
          class="col-span-2 min-w-0 min-[384px]:col-span-1 [&_.calendar-trigger]:size-11 [&_.date-button]:h-11"
          :selected-date="date"
          :available-dates="dateOptions"
          :today="today"
          @select="selectPlanningDate"
        />
        <PlanningSettings
          :mode="mode"
          :format="formatFilter"
          :language="language"
          :zoom="zoom"
          :theater-ids="preferences.activeTheaterIds.value"
          :favorite-theater-ids="preferences.favoriteTheaterIds.value"
          :theaters="preferences.theaters.value"
          :ready="preferences.isInitialized.value"
          :loading="preferences.isLoading.value"
          :error="preferences.error.value"
          @apply="updateTimelineQuery"
          @retry="retryTimeline"
        />
        <ShareButton class="shrink-0" />
      </div>
    </section>

    <SharedTheaterNotice
      v-if="preferences.isInitialized.value && preferences.isSharedSelectionDifferent.value"
      class="mt-4"
    />

    <section class="mt-4 min-w-0" aria-labelledby="planning-date-title">
      <div
        class="mb-2 flex items-end justify-between gap-4 border-b-2 border-ink px-1 pb-2"
      >
        <div class="min-w-0">
          <p
            class="font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]"
          >
            Séances
          </p>
          <h2
            id="planning-date-title"
            class="mt-1 text-xl font-black capitalize tracking-[-0.035em] sm:text-2xl"
          >
            {{ formatLongDate(date) }}
          </h2>
        </div>
        <button
          v-if="timelineMatrix?.canScrollToNow"
          type="button"
          class="inline-flex min-h-9 shrink-0 items-center justify-center gap-1.5 border-2 border-ink bg-surface px-2.5 font-mono text-[10px] font-black uppercase tracking-[0.06em] hover:bg-highlight focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-accent"
          aria-label="Revenir à l’heure actuelle"
          @click="timelineMatrix?.scrollToNow()"
        >
          <Clock3 :size="14" aria-hidden="true" />
          Maintenant
        </button>
      </div>

      <WatchedReadiness v-if="discovery.blocked.value" />
      <EditorialStatePanel
        v-else-if="pending"
        semantic="status"
        live="polite"
        size="viewport"
        shadow="small"
        class="planning-state font-extrabold"
      >
        <template #icon
          ><LoaderCircle
            :size="30"
            class="animate-spin motion-reduce:animate-none"
            aria-hidden="true"
          /></template
        >
        <p>Chargement des séances…</p>
      </EditorialStatePanel>

      <EditorialStatePanel
        v-else-if="errorMessage"
        semantic="alert"
        size="viewport"
        shadow="small"
        class="planning-state font-extrabold"
      >
        <template #icon
          ><AlertTriangle
            :size="32"
            class="text-primary"
            aria-hidden="true"
          /></template
        >
        <p class="max-w-lg">{{ errorMessage }}</p>
        <template #actions
          ><button
            type="button"
            class="inline-flex min-h-10 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.7rem] font-mono text-[0.65rem] font-black tracking-[0.08em] text-surface uppercase hover:bg-primary focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
            @click="retryTimeline"
          >
            <RefreshCw :size="17" aria-hidden="true" />
            Réessayer
          </button></template
        >
      </EditorialStatePanel>

      <EditorialStatePanel
        v-else-if="!displayedTimeline || rawShowtimeCount === 0"
        size="viewport"
        shadow="small"
        class="planning-state font-extrabold"
      >
        <template #icon
          ><CalendarDays :size="32" aria-hidden="true" /></template
        >
        <p>Aucune séance pour cette date et cette langue.</p>
      </EditorialStatePanel>

      <TimelineMatrix
        v-else
        ref="timelineMatrix"
        :timeline="displayedTimeline!"
        :mode="mode"
        :format-filter="formatFilter"
        :zoom="zoom"
      />
      <div
        v-if="!pending && !errorMessage && preferences.isInitialized.value && fallbackScope"
        class="mt-8 flex flex-wrap items-center justify-center gap-3"
      >
        <p
          v-if="appendError"
          role="alert"
          class="w-full text-center text-sm font-bold text-primary"
        >
          {{ appendError }}
        </p>
        <button
          v-if="hasMoreTheaters"
          type="button"
          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-4 py-2 text-sm font-bold text-white hover:bg-primary focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2 disabled:cursor-wait disabled:opacity-60"
          :disabled="appendPending"
          :aria-busy="appendPending"
          @click="loadMore"
        >
          {{
            appendPending ? 'Chargement…' : appendError ? 'Réessayer' : 'Charger plus'
          }}
        </button>
        <NuxtLink
          to="/cinemas"
          class="inline-flex min-h-11 items-center justify-center border-2 border-ink px-4 py-2 text-sm font-bold hover:bg-highlight focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
          >Configurer mes cinémas</NuxtLink
        >
      </div>
    </section>
  </main>
</template>
