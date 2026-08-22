<script setup lang="ts">
import { AlertTriangle, CalendarDays, LoaderCircle, RefreshCw, Settings2 } from '@lucide/vue'
import type { Language, QueryFormat, TimelineResponse } from '~/types/api'
import { formatDateLabel, formatLongDate, todayInParis } from '~/utils/date'
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
const preferences = useCinemaPreferences()
const date = ref(todayInParis())
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
    zoom: zoom.value === 30 ? undefined : String(zoom.value)
  })
}

function hydrateRoute() {
  const queryDate = calendarDate(singularQueryValue(route.query.date))
  date.value = queryDate ?? todayInParis()
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

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/planning')
const pageTitle = 'Planning des séances - MesSeances'
const pageDescription = 'Visualisez les séances de vos cinémas sur une frise et organisez votre sortie.'

useSeoMeta({
  title: pageTitle,
  description: pageDescription,
  robots: computed(() => Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="planning-page min-h-[calc(100vh-4.5rem)] px-2 py-3 text-ink sm:px-4 sm:py-4 lg:px-6">
    <h1 class="sr-only">Planning des séances</h1>

    <section class="control-dock sticky top-[4.5rem] z-20 border-2 border-ink bg-[#f1efe8]/95 shadow-[6px_6px_0_#27272a] backdrop-blur" aria-label="Filtres du planning">
      <div class="flex items-center gap-3 border-b-2 border-ink p-2 sm:p-3">
        <span class="hidden size-9 shrink-0 place-items-center border-2 border-ink bg-[#ffcf3f] sm:grid" aria-hidden="true">
          <CalendarDays :size="18" stroke-width="2.5" />
        </span>
        <div class="min-w-0 flex-1 overflow-x-auto [scrollbar-width:thin]">
          <div class="flex min-w-max" aria-label="Choisir une date">
            <button
              v-for="option in dateOptions"
              :key="option"
              type="button"
              class="date-button h-9 border-2 border-r-0 border-ink bg-surface px-3 font-mono text-[10px] font-black uppercase capitalize tracking-[0.06em] last:border-r-2 sm:h-10 sm:px-4"
              :class="date === option ? 'date-button--active' : 'hover:bg-[#e8e6de]'"
              :aria-pressed="date === option"
              @click="updateTimelineQuery({ date: option === todayInParis() ? undefined : option })"
            >
              {{ formatDateLabel(option) }}
            </button>
          </div>
        </div>
        <NuxtLink to="/cinemas" class="personalize-button shrink-0">
          <Settings2 :size="17" aria-hidden="true" />
          <span class="hidden sm:inline">Cinémas</span>
          <strong>{{ preferences.favoriteTheaterIds.value.length }}</strong>
        </NuxtLink>
      </div>

      <div class="grid gap-3 p-2 sm:p-3 xl:grid-cols-[auto_minmax(0,1fr)_auto_auto] xl:items-end">
        <fieldset>
          <legend class="utility-label mb-1.5">Affichage</legend>
          <div class="control-group">
            <button v-for="option in [{ value: 'theater', label: 'Par cinéma' }, { value: 'movie', label: 'Par film' }]" :key="option.value" type="button" class="control-button" :class="mode === option.value ? 'control-button--active' : ''" :aria-pressed="mode === option.value" @click="updateTimelineQuery({ mode: option.value === 'theater' ? undefined : option.value })">{{ option.label }}</button>
          </div>
        </fieldset>

        <fieldset class="min-w-0">
          <legend class="utility-label mb-1.5">Format</legend>
          <div class="control-group max-w-full overflow-x-auto [scrollbar-width:thin]">
            <button v-for="option in formatOptions" :key="option.value" type="button" class="control-button shrink-0" :class="formatFilter === option.value ? 'control-button--active' : ''" :aria-label="option.label" :aria-pressed="formatFilter === option.value" @click="updateTimelineQuery({ format: option.value === 'ALL' ? undefined : option.value })">
              <BrandLogo v-if="option.brand" :brand="option.brand" decorative :class="formatFilter === option.value ? 'brightness-0 invert' : ''" />
              <span v-else>{{ option.label }}</span>
            </button>
          </div>
        </fieldset>

        <label class="block">
          <span class="utility-label mb-1.5 block">Langue</span>
          <select :value="language" class="editorial-select" @change="updateLanguage">
            <option value="ALL">Toutes</option>
            <option value="VOSTFR">VOSTFR</option>
            <option value="VF">VF</option>
          </select>
        </label>

        <fieldset>
          <legend class="utility-label mb-1.5">Zoom</legend>
          <div class="control-group">
            <button v-for="option in [{ value: 15, label: '15 min' }, { value: 30, label: '30 min' }, { value: 60, label: '1 h' }]" :key="option.value" type="button" class="control-button" :class="zoom === option.value ? 'control-button--active' : ''" :aria-pressed="zoom === option.value" @click="updateTimelineQuery({ zoom: option.value === 30 ? undefined : String(option.value) })">{{ option.label }}</button>
          </div>
        </fieldset>
      </div>
    </section>

    <section class="mt-4 min-w-0" aria-labelledby="planning-date-title">
      <div class="mb-2 flex items-end justify-between gap-4 border-b-2 border-ink px-1 pb-2">
        <div>
          <p class="utility-label">Séances</p>
          <h2 id="planning-date-title" class="mt-1 text-xl font-black capitalize tracking-[-0.035em] sm:text-2xl">{{ formatLongDate(date) }}</h2>
        </div>
        <p v-if="timeline && !pending" class="utility-label text-right">{{ showtimeCount }} séance{{ showtimeCount > 1 ? 's' : '' }}</p>
      </div>

      <div v-if="pending" class="planning-state" role="status" aria-live="polite">
        <LoaderCircle :size="30" class="planning-spinner animate-spin" aria-hidden="true" />
        <p>Chargement des séances…</p>
      </div>

      <div v-else-if="errorMessage" class="planning-state" role="alert">
        <AlertTriangle :size="32" class="text-primary" aria-hidden="true" />
        <p class="max-w-lg">{{ errorMessage }}</p>
        <button type="button" class="state-button" @click="retryTimeline">
          <RefreshCw :size="17" aria-hidden="true" /> Réessayer
        </button>
      </div>

      <div v-else-if="!timeline || rawShowtimeCount === 0" class="planning-state">
        <CalendarDays :size="32" aria-hidden="true" />
        <p>Aucune séance pour cette date et cette langue.</p>
      </div>

      <TimelineMatrix v-else :timeline="timeline" :mode="mode" :format-filter="formatFilter" :zoom="zoom" />
    </section>
  </main>
</template>

<style scoped>
.planning-page {
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

.date-button--active,
.control-button--active {
  background: #27272a;
  color: #fff;
  box-shadow: inset 0 -3px 0 var(--color-highlight);
}

.date-button:focus-visible,
.control-button:focus-visible,
.personalize-button:focus-visible,
.editorial-select:focus-visible,
.state-button:focus-visible {
  z-index: 1;
  outline: 3px solid #1f6f78;
  outline-offset: 2px;
}

.personalize-button,
.state-button {
  display: inline-flex;
  min-height: 2.5rem;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border: 2px solid #27272a;
  background: #27272a;
  padding: 0 0.7rem;
  color: #fff;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.personalize-button:hover,
.state-button:hover {
  background: #991b1b;
}

.personalize-button strong {
  display: grid;
  min-width: 1.5rem;
  height: 1.5rem;
  place-items: center;
  background: var(--color-highlight);
  color: #27272a;
}

.control-group {
  display: flex;
  width: max-content;
  max-width: 100%;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.2rem;
}

.control-button {
  min-height: 2.25rem;
  padding: 0 0.7rem;
  color: #27272a;
  font-size: 0.75rem;
  font-weight: 800;
}

.control-button.control-button--active {
  color: #fff;
}

.control-button:hover:not(.control-button--active) {
  background: #e8e6de;
}

.editorial-select {
  height: 2.75rem;
  min-width: 8rem;
  border: 2px solid #27272a;
  border-radius: 0;
  background: #fff;
  padding: 0 2rem 0 0.7rem;
  color: #27272a;
  font-size: 0.8rem;
  font-weight: 800;
}

.planning-state {
  display: flex;
  min-height: max(22rem, calc(100vh - 23rem));
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 2rem;
  text-align: center;
  font-weight: 800;
  box-shadow: 6px 6px 0 #27272a;
}

@media (max-width: 1279px) {
  .control-dock {
    position: relative;
    top: auto;
  }

  .control-dock > div:last-child {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 639px) {
  .control-dock > div:last-child {
    grid-template-columns: minmax(0, 1fr);
  }

  .planning-state {
    min-height: 20rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .planning-spinner {
    animation: none;
  }
}
</style>
