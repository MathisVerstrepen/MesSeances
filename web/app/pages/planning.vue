<script setup lang="ts">
import { AlertTriangle, CalendarDays, LoaderCircle, RefreshCw, Settings2 } from '@lucide/vue'
import type { Language, QueryFormat, TimelineResponse } from '~/types/api'
import { formatLongDate, todayInParis } from '~/utils/date'
import { formatOptions } from '~/utils/formats'
import { calendarDate, enumQueryValue, mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'

type TimelineMode = 'theater' | 'movie'
type TimelineZoom = 15 | 30 | 60

const OWNED_QUERY_KEYS = ['date', 'language', 'format', 'mode', 'zoom'] as const
const LANGUAGES: readonly Language[] = ['ALL', 'VOSTFR', 'VF']
const MODES: readonly TimelineMode[] = ['theater', 'movie']
const ZOOMS: readonly string[] = ['15', '30', '60']

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const preferences = usePageCinemaSelection()
const today = ref(todayInParis())
const date = ref(today.value)
const language = ref<Language>('ALL')
const mode = ref<TimelineMode>('theater')
const formatFilter = ref<QueryFormat>('ALL')
const zoom = ref<TimelineZoom>(30)
const timeline = ref<TimelineResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
let requestId = 0
let isMounted = false
let isInitializing = false
let lastTimelineKey = ''
let dayCheckTimer: number | undefined

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
  if (preferences.activeTheaterIds.value.length === 0) {
    timeline.value = null
    errorMessage.value = ''
    pending.value = false
    return
  }

  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await api.timeline({
      date: date.value,
      language: language.value,
      theaters: preferences.activeTheaterIds.value.join(',')
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
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    date: date.value === today.value ? undefined : date.value,
    language: language.value === 'ALL' ? undefined : language.value,
    format: formatFilter.value === 'ALL' ? undefined : formatFilter.value,
    mode: mode.value === 'theater' ? undefined : mode.value,
    zoom: zoom.value === 30 ? undefined : String(zoom.value)
  })
}

function hydrateRoute() {
  const queryDate = calendarDate(singularQueryValue(route.query.date))
  date.value = queryDate && queryDate >= today.value ? queryDate : today.value
  language.value = enumQueryValue(singularQueryValue(route.query.language), LANGUAGES) ?? 'ALL'
  formatFilter.value = enumQueryValue(singularQueryValue(route.query.format), formatOptions.map((option) => option.value)) ?? 'ALL'
  mode.value = enumQueryValue(singularQueryValue(route.query.mode), MODES) ?? 'theater'
  const queryZoom = enumQueryValue(singularQueryValue(route.query.zoom), ZOOMS)
  zoom.value = queryZoom === '15' ? 15 : queryZoom === '60' ? 60 : 30
  return timelineQuery()
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  const key = `${date.value}|${language.value}|${preferences.activeTheaterIds.value.join(',')}`
  if (preferences.isInitialized.value && key !== lastTimelineKey) {
    lastTimelineKey = key
    await loadTimeline()
  }
}

function updateTimelineQuery(values: Partial<Record<'date' | 'language' | 'format' | 'mode' | 'zoom', string>>) {
  const query = mergeOwnedQuery(route.query, Object.keys(values), values)
  if (!queriesEqual(route.query, query)) router.replace({ query })
}

function updateLanguage(event: Event) {
  if (!(event.currentTarget instanceof HTMLSelectElement)) return
  updateTimelineQuery({ language: event.currentTarget.value === 'ALL' ? undefined : event.currentTarget.value })
}

function createFallbackDates() {
  const [year, month, day] = today.value.split('-').map(Number)
  return Array.from({ length: 7 }, (_, offset) => {
    const value = new Date(Date.UTC(year!, month! - 1, day! + offset, 12))
    return [value.getUTCFullYear(), String(value.getUTCMonth() + 1).padStart(2, '0'), String(value.getUTCDate()).padStart(2, '0')].join('-')
  })
}

const dateOptions = computed(() => {
  const available = new Set(preferences.activeTheaters.value.flatMap((theater) => theater.available_dates))
  const options = available.size > 0 ? [...available].filter((option) => option >= today.value).sort() : createFallbackDates()
  if (date.value >= today.value && !options.includes(date.value)) options.push(date.value)
  return options.sort()
})

function selectPlanningDate(selectedDate: string) {
  updateTimelineQuery({ date: selectedDate === today.value ? undefined : selectedDate })
}

const showtimeCount = computed(() => timeline.value?.theaters.reduce((total, theater) => total + theater.showtimes.filter((showtime) => matchesFormat(showtime.format)).length, 0) ?? 0)
const rawShowtimeCount = computed(() => timeline.value?.theaters.reduce((total, theater) => total + theater.showtimes.length, 0) ?? 0)

async function refreshPlanningDay() {
  const currentDay = todayInParis()
  if (currentDay === today.value) return
  today.value = currentDay
  await applyRoute()
}

function scheduleDayCheck() {
  if (dayCheckTimer) window.clearTimeout(dayCheckTimer)
  const delay = 60_000 - Date.now() % 60_000
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

watch(() => route.query, () => {
  if (isMounted) applyRoute()
})
watch(preferences.activeTheaterIds, () => {
  if (preferences.isInitialized.value && !isInitializing) applyRoute()
})

onMounted(async () => {
  isMounted = true
  scheduleDayCheck()
  document.addEventListener('visibilitychange', handleVisibilityChange)
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) await router.replace({ query: canonicalQuery })
  isInitializing = true
  await preferences.initialize()
  isInitializing = false
  if (preferences.isInitialized.value) await applyRoute()
  else await loadTimeline()
})

onBeforeUnmount(() => {
  if (dayCheckTimer) window.clearTimeout(dayCheckTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/planning')
const pageTitle = 'Planning des séances - MesSeances'
const pageDescription = 'Visualisez les séances de vos cinémas sur une frise et organisez votre sortie.'

useSeoMeta({
  title: pageTitle,
  description: pageDescription,
  robots: 'noindex,follow'
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="min-h-[calc(100vh-4.5rem)] bg-[#f8f7f2] px-2 py-3 text-ink [background-image:linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] [background-size:28px_28px] sm:px-4 sm:py-4 lg:px-6">
    <h1 class="sr-only">Planning des séances</h1>

    <section class="sticky top-[4.5rem] z-20 border-2 border-ink bg-[#f1efe8]/95 shadow-[6px_6px_0_#27272a] backdrop-blur max-xl:relative max-xl:top-auto" aria-label="Filtres du planning">
      <div class="flex flex-wrap items-center gap-2 border-b-2 border-ink p-2 sm:flex-nowrap sm:gap-3 sm:p-3">
        <ShowtimeDateBar :selected-date="date" :available-dates="dateOptions" :today="today" @select="selectPlanningDate" />
        <NuxtLink to="/cinemas" class="ml-auto inline-flex min-h-10 shrink-0 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.7rem] font-mono text-[0.65rem] font-black tracking-[0.08em] text-surface uppercase hover:bg-primary focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent sm:ml-0">
          <Settings2 :size="17" aria-hidden="true" />
          <span class="hidden sm:inline">Cinémas</span>
          <strong class="grid h-6 min-w-6 place-items-center bg-highlight text-ink">{{ preferences.activeTheaterIds.value.length }}</strong>
        </NuxtLink>
        <ShareButton class="shrink-0" />
      </div>

      <div class="grid grid-cols-1 gap-3 p-2 sm:grid-cols-2 sm:p-3 xl:grid-cols-[auto_minmax(0,1fr)_auto_auto] xl:items-end">
        <fieldset>
          <legend class="mb-1.5 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]">Affichage</legend>
          <div class="flex w-max max-w-full border-2 border-ink bg-surface p-[0.2rem]">
            <button v-for="option in [{ value: 'theater', label: 'Par cinéma' }, { value: 'movie', label: 'Par film' }]" :key="option.value" type="button" class="min-h-9 px-[0.7rem] text-xs font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent" :class="mode === option.value ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'hover:bg-[#e8e6de]'" :aria-pressed="mode === option.value" @click="updateTimelineQuery({ mode: option.value === 'theater' ? undefined : option.value })">{{ option.label }}</button>
          </div>
        </fieldset>

        <fieldset class="min-w-0">
          <legend class="mb-1.5 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]">Format</legend>
          <div class="flex w-max max-w-full overflow-x-auto border-2 border-ink bg-surface p-[0.2rem] [scrollbar-width:thin]">
            <button v-for="option in formatOptions" :key="option.value" type="button" class="min-h-9 shrink-0 px-[0.7rem] text-xs font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent" :class="formatFilter === option.value ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'hover:bg-[#e8e6de]'" :aria-label="option.label" :aria-pressed="formatFilter === option.value" @click="updateTimelineQuery({ format: option.value === 'ALL' ? undefined : option.value })">
              <BrandLogo v-if="option.brand" :brand="option.brand" decorative :class="formatFilter === option.value ? 'brightness-0 invert' : ''" />
              <span v-else>{{ option.label }}</span>
            </button>
          </div>
        </fieldset>

        <label class="block">
          <span class="mb-1.5 block font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]">Langue</span>
          <select :value="language" class="h-11 min-w-32 rounded-none border-2 border-ink bg-surface pr-8 pl-[0.7rem] text-[0.8rem] font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent" @change="updateLanguage">
            <option value="ALL">Toutes</option>
            <option value="VOSTFR">VOSTFR</option>
            <option value="VF">VF</option>
          </select>
        </label>

        <fieldset>
          <legend class="mb-1.5 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]">Zoom</legend>
          <div class="flex w-max max-w-full border-2 border-ink bg-surface p-[0.2rem]">
            <button v-for="option in [{ value: 15, label: '15 min' }, { value: 30, label: '30 min' }, { value: 60, label: '1 h' }]" :key="option.value" type="button" class="min-h-9 px-[0.7rem] text-xs font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent" :class="zoom === option.value ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'hover:bg-[#e8e6de]'" :aria-pressed="zoom === option.value" @click="updateTimelineQuery({ zoom: option.value === 30 ? undefined : String(option.value) })">{{ option.label }}</button>
          </div>
        </fieldset>
      </div>
    </section>

    <SharedTheaterNotice v-if="preferences.isInitialized.value && preferences.isSharedSelectionDifferent.value" class="mt-4" />

    <section class="mt-4 min-w-0" aria-labelledby="planning-date-title">
      <div class="mb-2 flex items-end justify-between gap-4 border-b-2 border-ink px-1 pb-2">
        <div>
          <p class="font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]">Séances</p>
          <h2 id="planning-date-title" class="mt-1 text-xl font-black capitalize tracking-[-0.035em] sm:text-2xl">{{ formatLongDate(date) }}</h2>
        </div>
        <p v-if="timeline && !pending" class="text-right font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]">{{ showtimeCount }} séance{{ showtimeCount > 1 ? 's' : '' }}</p>
      </div>

      <EditorialStatePanel v-if="pending" semantic="status" live="polite" size="viewport" shadow="small" class="planning-state font-extrabold">
        <template #icon><LoaderCircle :size="30" class="animate-spin motion-reduce:animate-none" aria-hidden="true" /></template>
        <p>Chargement des séances…</p>
      </EditorialStatePanel>

      <EditorialStatePanel v-else-if="errorMessage" semantic="alert" size="viewport" shadow="small" class="planning-state font-extrabold">
        <template #icon><AlertTriangle :size="32" class="text-primary" aria-hidden="true" /></template>
        <p class="max-w-lg">{{ errorMessage }}</p>
        <template #actions><button type="button" class="inline-flex min-h-10 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.7rem] font-mono text-[0.65rem] font-black tracking-[0.08em] text-surface uppercase hover:bg-primary focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent" @click="retryTimeline"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template>
      </EditorialStatePanel>

      <EditorialStatePanel v-else-if="!timeline || rawShowtimeCount === 0" size="viewport" shadow="small" class="planning-state font-extrabold">
        <template #icon><CalendarDays :size="32" aria-hidden="true" /></template>
        <p>Aucune séance pour cette date et cette langue.</p>
      </EditorialStatePanel>

      <TimelineMatrix v-else :timeline="timeline" :mode="mode" :format-filter="formatFilter" :zoom="zoom" />
    </section>
  </main>
</template>
