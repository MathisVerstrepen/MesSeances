<script setup lang="ts">
import { AlertTriangle, CalendarSearch, ListFilter, LoaderCircle, Search, SlidersHorizontal } from '@lucide/vue'
import MovieSlotResultGroup from '~/components/MovieSlotResultGroup.vue'
import TimeRangeSlider from '~/components/TimeRangeSlider.vue'
import type { Language, QueryFormat, SlotResult } from '~/types/api'
import { createServiceTimeOptions, formatDateLabel, formatLongDate, todayInParis } from '~/utils/date'
import { formatOptions } from '~/utils/formats'
import { calendarDate, enumQueryValue, mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'

const OWNED_QUERY_KEYS = ['theaters', 'date', 'start_after', 'finish_before', 'language', 'format', 'include_ads', 'buffer_ads'] as const
const REQUIRED_QUERY_KEYS = ['theaters', 'date', 'start_after', 'finish_before'] as const
const LANGUAGES: readonly Language[] = ['ALL', 'VOSTFR', 'VF']
type ResultView = 'movie' | 'chronological'

const api = useMovieFlowApi()
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
  date: todayInParis(),
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
const selectedTheaterIds = ref<string[]>([])
const theaterValidationMessage = ref('')
const appliedSearch = ref<AppliedSearch | null>(null)
const resultView = computed<ResultView>(() => singularQueryValue(route.query.view) === 'chronological' ? 'chronological' : 'movie')
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
function createFallbackDates() {
  const [year, month, day] = todayInParis().split('-').map(Number)
  return Array.from({ length: 7 }, (_, offset) => {
    const value = new Date(Date.UTC(year!, month! - 1, day! + offset, 12))
    return [value.getUTCFullYear(), String(value.getUTCMonth() + 1).padStart(2, '0'), String(value.getUTCDate()).padStart(2, '0')].join('-')
  })
}
const dateOptions = computed(() => {
  const selected = new Set(selectedTheaterIds.value)
  const available = new Set(favoriteTheaters.value.filter((theater) => selected.has(theater.id)).flatMap((theater) => theater.available_dates ?? []))
  const options = available.size > 0 ? [...available].sort() : createFallbackDates()
  const selectedDate = calendarDate(form.date)
  if (selectedDate && !options.includes(selectedDate)) options.push(selectedDate)
  return options.sort()
})
let previousFavoriteIds: string[] = []
let isReady = false
let lastSearchKey = ''
let requestId = 0

interface AppliedSearch {
  theaterIds: string[]
  date: string
  startAfter: string
  finishBefore: string
  language: Language
  format: QueryFormat
  includeAds: boolean
}

function bareQuery() {
  const query = mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {})
  return mergeOwnedQuery(query, ['view'], {
    view: singularQueryValue(route.query.view) === 'chronological' ? 'chronological' : undefined
  })
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
  return mergeOwnedQuery(query, ['view'], {
    view: singularQueryValue(route.query.view) === 'chronological' ? 'chronological' : undefined
  })
}

async function setResultView(view: ResultView) {
  if (view === resultView.value) return
  await router.replace({
    query: mergeOwnedQuery(route.query, ['view'], { view: view === 'chronological' ? view : undefined })
  })
}

function editFilters() {
  document.querySelector('#search-filters')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function resetBareState() {
  requestId++
  form.date = todayInParis()
  form.startAfter = '12:00'
  form.finishBefore = '15:00'
  form.language = 'ALL'
  form.format = 'ALL'
  form.includeAds = true
  selectedTheaterIds.value = [...favoriteTheaterIds.value]
  previousFavoriteIds = [...favoriteTheaterIds.value]
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
  if (!theaterValue || !date || !startAfter || !finishBefore || !validTimes.has(startAfter) || !validTimes.has(finishBefore)) return null

  const requestedIds = theaterValue.split(',')
  if (requestedIds.some((id) => !id)) return null
  const requested = new Set(requestedIds)
  const theaterIds = favoriteTheaterIds.value.filter((id) => requested.has(id))
  if (theaterIds.length === 0) return null

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
  selectedTheaterIds.value = [...search.theaterIds]
  previousFavoriteIds = [...favoriteTheaterIds.value]
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
      searchedDate.value = search.date
      appliedSearch.value = { ...search, theaterIds: [...search.theaterIds] }
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
    if (isReady && OWNED_QUERY_KEYS.some((key) => key in route.query)) {
      applyRoute()
      return
    }
    const favorites = new Set(favoriteIds)
    const previousFavorites = new Set(previousFavoriteIds)
    const retainedIds = selectedTheaterIds.value.filter((id) => favorites.has(id))
    const newlyFavoritedIds = favoriteIds.filter((id) => !previousFavorites.has(id))

    selectedTheaterIds.value = favoriteIds.filter((id) => retainedIds.includes(id) || newlyFavoritedIds.includes(id))
    previousFavoriteIds = [...favoriteIds]
    if (selectedTheaterIds.value.length > 0) theaterValidationMessage.value = ''
  },
  { immediate: true }
)

watch(selectedTheaterIds, (ids) => {
  if (ids.length > 0) theaterValidationMessage.value = ''
})

watch(() => route.query, () => {
  if (isReady) applyRoute()
})

async function initializePreferences() {
  await initialize()
  if (!isInitialized.value) return
  isReady = true
  await applyRoute()
}

onMounted(initializePreferences)

async function submitSearch() {
  const selectedIds = favoriteTheaterIds.value.filter((id) => selectedTheaterIds.value.includes(id))
  if (selectedIds.length === 0) {
    theaterValidationMessage.value = 'Sélectionnez au moins un cinéma favori.'
    return
  }

  if (!calendarDate(form.date) || !validTimes.has(form.startAfter) || !validTimes.has(form.finishBefore)) return

  const search: AppliedSearch = {
    theaterIds: selectedIds,
    date: form.date,
    startAfter: form.startAfter,
    finishBefore: form.finishBefore,
    language: form.language,
    format: form.format,
    includeAds: form.includeAds
  }
  const query = submittedQuery(search)
  if (queriesEqual(route.query, query)) {
    if (errorMessage.value) await runSearch(search)
    return
  }
  await router.push({ query })
}
</script>

<template>
  <main class="mx-auto max-w-[1280px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Trouver une séance</h1>

    <div class="mt-7 grid gap-8 lg:grid-cols-[320px_minmax(0,1fr)] lg:gap-10 lg:items-start">
      <form id="search-filters" class="min-w-0 scroll-mt-28 lg:sticky lg:top-6 lg:border-r lg:border-line lg:pr-8" @submit.prevent="submitSearch">
        <div class="mb-5 flex items-center gap-2.5 border-b border-line pb-4">
          <SlidersHorizontal :size="18" class="text-accent" aria-hidden="true" />
          <h2 class="font-semibold text-ink">Votre disponibilité</h2>
        </div>

        <div class="space-y-5">
          <fieldset :aria-invalid="theaterValidationMessage || preferencesError ? 'true' : undefined" :aria-describedby="theaterValidationMessage || preferencesError ? 'theater-selection-message' : undefined">
            <legend class="float-left mb-1.5 text-sm font-medium text-ink">Cinémas</legend>
            <NuxtLink to="/cinemas" class="float-right mb-1.5 text-sm font-medium text-accent underline-offset-4 hover:underline">Gérer mes favoris</NuxtLink>
            <div v-if="preferencesError && !isInitialized" id="theater-selection-message" class="clear-both rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">
              <p>{{ preferencesError }}</p>
              <button type="button" class="mt-3 font-semibold underline underline-offset-4" @click="initializePreferences">Réessayer</button>
            </div>
            <div v-else-if="isLoading || !isInitialized" class="clear-both flex min-h-10 items-center gap-2 rounded-md border border-line px-3 text-sm text-muted">
              <LoaderCircle :size="16" class="animate-spin" aria-hidden="true" /> Chargement des cinémas…
            </div>
            <div v-else-if="favoriteTheaters.length" class="clear-both max-h-44 space-y-1 overflow-y-auto rounded-md border border-line bg-surface p-2">
              <label v-for="theater in favoriteTheaters" :key="theater.id" class="flex cursor-pointer items-start gap-2.5 rounded px-2 py-2 text-sm text-ink hover:bg-subtle">
                <input v-model="selectedTheaterIds" type="checkbox" :value="theater.id" class="mt-0.5 size-4 accent-accent" />
                <span><BrandedText :text="theater.name" /> <span class="text-muted">· {{ theater.city }}</span></span>
              </label>
            </div>
            <p v-else class="clear-both rounded-md border border-line bg-subtle px-3 py-2 text-sm text-muted">Aucun cinéma favori.</p>
            <p v-if="theaterValidationMessage" id="theater-selection-message" class="mt-1.5 text-sm text-red-700" role="alert">{{ theaterValidationMessage }}</p>
          </fieldset>

          <fieldset class="min-w-0">
            <legend class="mb-1.5 text-sm font-medium text-ink">Date de la séance</legend>
            <div class="max-w-full overflow-x-auto pb-1 [scrollbar-width:thin]">
              <div class="flex min-w-max gap-2" role="group" aria-label="Choisir la date de la séance">
                <button
                  v-for="option in dateOptions"
                  :key="option"
                  type="button"
                  class="h-10 shrink-0 rounded-full border px-3 text-sm font-medium capitalize transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
                  :class="form.date === option ? 'border-accent bg-accent text-white' : 'border-line bg-surface text-ink hover:border-stone-400'"
                  :aria-label="`Choisir ${formatLongDate(option)}`"
                  :aria-pressed="form.date === option"
                  @click="form.date = option"
                >
                  {{ formatDateLabel(option) }}
                </button>
              </div>
            </div>
          </fieldset>

          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Technologie</span>
            <select v-model="form.format" class="field">
              <option v-for="option in formatOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>

          <TimeRangeSlider v-model:start="form.startAfter" v-model:end="form.finishBefore" :options="timeOptions" />

          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Langue</span>
            <select v-model="form.language" class="field">
              <option value="ALL">Toutes</option>
              <option value="VOSTFR">VOSTFR</option>
              <option value="VF">VF</option>
            </select>
          </label>

          <label class="flex cursor-pointer items-start gap-3 rounded-md border border-line bg-subtle p-3 text-sm text-ink">
            <input v-model="form.includeAds" type="checkbox" class="mt-0.5 size-4 accent-accent" />
            <span>Inclure les publicités (+20 min)</span>
          </label>

          <button type="submit" class="button-primary w-full" :disabled="pending || isLoading || !isInitialized">
            <LoaderCircle v-if="pending" :size="18" class="animate-spin" aria-hidden="true" />
            <Search v-else :size="18" aria-hidden="true" />
            {{ pending ? 'Recherche…' : 'Trouver une séance' }}
          </button>
        </div>
      </form>

      <section aria-live="polite" aria-label="Résultats de recherche">
        <div class="mb-4 flex items-end justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-muted">Résultats</p>
            <h2 class="mt-1 text-xl font-semibold capitalize text-ink">{{ searchedDate ? formatLongDate(searchedDate) : 'Lancez votre recherche' }}</h2>
          </div>
        </div>

        <div v-if="results" class="sticky top-[6.5rem] z-20 mb-5 rounded-lg border border-line bg-surface/95 p-3 shadow-sm backdrop-blur lg:top-0">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div class="min-w-0">
              <div class="flex items-center justify-between gap-3">
                <p class="shrink-0 font-semibold text-ink">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</p>
                <button type="button" class="inline-flex h-10 shrink-0 items-center gap-1.5 rounded px-2 text-sm font-medium text-accent transition hover:bg-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent lg:hidden" @click="editFilters">
                  <ListFilter :size="16" aria-hidden="true" /> Filtres
                </button>
              </div>
              <ul class="mt-1 flex flex-wrap gap-x-2 gap-y-1 text-sm text-muted" aria-label="Filtres appliqués">
                <li v-for="(item, index) in activeFilterSummary" :key="item" class="flex items-center gap-2 capitalize">
                  <span v-if="index > 0" aria-hidden="true">·</span>
                  <span>{{ item }}</span>
                </li>
              </ul>
            </div>
            <div class="grid w-full grid-cols-2 rounded-md bg-subtle p-1 sm:w-auto" role="group" aria-label="Affichage des résultats">
                <button
                  v-for="option in [{ value: 'movie' as const, label: 'Par film' }, { value: 'chronological' as const, label: 'Chronologique' }]"
                  :key="option.value"
                  type="button"
                  class="h-9 rounded px-2 text-sm font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-1 sm:px-3"
                  :class="resultView === option.value ? 'bg-ink text-white shadow-sm' : 'text-muted hover:text-ink'"
                  :aria-pressed="resultView === option.value"
                  @click="setResultView(option.value)"
                >
                  {{ option.label }}
                </button>
            </div>
          </div>
        </div>

        <div v-if="pending" class="state-panel" role="status">
          <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
          <p>Recherche des séances compatibles…</p>
        </div>
        <div v-else-if="errorMessage" class="state-panel" role="alert">
          <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
          <p class="max-w-lg">{{ errorMessage }}</p>
        </div>
        <div v-else-if="results?.length === 0" class="state-panel">
          <CalendarSearch :size="30" class="text-muted" aria-hidden="true" />
          <p>Aucune séance ne tient entièrement dans ce créneau.</p>
        </div>
        <div v-else-if="results && resultView === 'movie'" class="space-y-4">
          <MovieSlotResultGroup v-for="group in movieGroups" :key="group.key" :results="group.slots" />
        </div>
        <div v-else-if="results" class="divide-y divide-line rounded-lg border border-line bg-surface">
          <SlotResultCard v-for="result in chronologicalResults" :key="result.showtime.id" :result="result" />
        </div>
        <div v-else class="state-panel">
          <CalendarSearch :size="30" class="text-accent" aria-hidden="true" />
          <p>Définissez votre créneau pour voir les séances compatibles.</p>
        </div>
      </section>
    </div>

  </main>
</template>
