<script setup lang="ts">
import { AlertTriangle, RefreshCw } from '@lucide/vue'
import type { StatisticsBucket, StatisticsMovieRank, StatisticsOptions, StatisticsResponse } from '~/types/api'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { queriesEqual } from '~/utils/routeQuery'
import { createStatisticsRequest, parseStatisticsQuery, statisticsBucketLabel, statisticsChainLabels, statisticsCount, statisticsDraft, statisticsDraftQuery, statisticsMaxSelections, statisticsOptionsWithSelection, statisticsQueryKeys, statisticsQuerySignature, statisticsRouteQuery, statisticsShare, type StatisticsFilterKey, type StatisticsMultiFilterKey, type StatisticsScalarFilterKey } from '~/utils/statistics'

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const today = useState('statistics-paris-today', () => new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Paris', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()))
const signature = computed(() => statisticsQuerySignature(route.query))

function errorMessage(cause: unknown) {
  const status = getApiErrorStatus(cause)
  if (status === 400) return 'Filtres invalides. Vérifiez les dates et les sélections, ou réinitialisez les filtres.'
  if (status === 429) return 'Trop de demandes. Patientez un instant avant de réessayer.'
  if (status === 503) return 'Les statistiques ne sont pas encore disponibles. Réessayez plus tard.'
  if (status !== undefined && status >= 500) return 'Impossible de charger les statistiques. Réessayez plus tard.'
  return getFrenchApiError(cause)
}

async function fetchStatistics(signal?: AbortSignal) {
  const parsed = parseStatisticsQuery(route.query, today.value)
  if (parsed.error) return { response: null, message: parsed.error, status: 400 }
  try {
    return { response: await api.statistics(parsed.query, signal), message: '', status: 200 }
  } catch (cause) {
    return { response: null, message: errorMessage(cause), status: getApiErrorStatus(cause) === 400 ? 400 : 502 }
  }
}

const initial = await useAsyncData(`statistics:${signature.value}`, () => fetchStatistics(), { lazy: true })
const data = shallowRef<StatisticsResponse | null>(initial.data.value?.response ?? null)
const error = ref(initial.data.value?.message ?? '')
const options = shallowRef<StatisticsOptions | null>(data.value?.options ?? null)
const draft = ref(statisticsDraft(route.query, data.value?.range))
const validationError = ref('')
const pending = ref(initial.pending.value)
const showSkeleton = ref(false)
let skeletonTimer: ReturnType<typeof setTimeout> | undefined
let initialActive = true
if (import.meta.server && initial.data.value?.status !== 200) {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, initial.data.value?.status ?? 502)
}

function acceptResult(result: Awaited<ReturnType<typeof fetchStatistics>>) {
  data.value = result.response
  error.value = result.message
  if (result.response) {
    options.value = result.response.options
    if (!draft.value.explicitDates) {
      draft.value.date = result.response.range.from
      draft.value.date_to = result.response.range.through
    }
  }
}
watch(pending, (loading) => {
  clearTimeout(skeletonTimer)
  showSkeleton.value = false
  if (loading) skeletonTimer = setTimeout(() => { showSkeleton.value = true }, 250)
}, { immediate: true, flush: 'sync' })
watch(initial.data, (result) => {
  if (!initialActive || !result) return
  acceptResult(result)
  pending.value = false
})
watch(initial.pending, (loading) => { if (initialActive) pending.value = loading })
const request = createStatisticsRequest<Awaited<ReturnType<typeof fetchStatistics>>>({
  start() {
    initialActive = false
    data.value = null
    error.value = ''
    pending.value = true
  },
  success: acceptResult,
  error(cause) { error.value = errorMessage(cause) },
  finish() {
    pending.value = false
  }
})
function reload() { return request.run(signal => fetchStatistics(signal)) }
watch(signature, () => {
  draft.value = statisticsDraft(route.query, data.value?.range)
  validationError.value = ''
  void reload()
}, { flush: 'sync' })
onBeforeUnmount(() => { request.cancel(); clearTimeout(skeletonTimer) })

async function apply() {
  const parsed = statisticsDraftQuery(draft.value, today.value)
  validationError.value = parsed.error
  if (parsed.error) return
  const query = statisticsRouteQuery(route.query, parsed.query)
  if (!queriesEqual(route.query, query)) await router.push({ query })
  else if (error.value) await reload()
}
async function reset() {
  const query = statisticsRouteQuery(route.query)
  draft.value = statisticsDraft(query)
  validationError.value = ''
  if (!queriesEqual(route.query, query)) await router.push({ query })
  else await reload()
}

const primaryFilters: { key: StatisticsMultiFilterKey; label: string; all: string }[] = [
  { key: 'city', label: 'Ville', all: 'Toutes les villes' },
  { key: 'theater', label: 'Cinéma', all: 'Tous les cinémas' }
]
const advancedFilters: { key: StatisticsScalarFilterKey; label: string; all: string }[] = [
  { key: 'chain', label: 'Circuit', all: 'Tous les circuits' },
  { key: 'language', label: 'Version', all: 'Toutes les versions' },
  { key: 'format', label: 'Format', all: 'Tous les formats' },
  { key: 'genre', label: 'Genre', all: 'Tous les genres' },
  { key: 'pass', label: 'Pass accepté', all: 'Tous les pass' }
]
const advancedOpen = ref(advancedFilters.some(filter => Boolean(draft.value[filter.key])))
watch(signature, () => { if (advancedFilters.some(filter => Boolean(draft.value[filter.key]))) advancedOpen.value = true })
const filterOptions = computed(() => {
  const source = options.value
  const values = {
    city: source?.cities.map(city => ({ value: city.slug, label: city.name })) ?? [],
    theater: source?.theaters.map(theater => ({ value: theater.id, label: `${theater.name} · ${theater.city}` })) ?? [],
    chain: source?.chains.map(chain => ({ value: chain, label: statisticsChainLabels[chain] })) ?? [],
    language: source?.languages.map(value => ({ value, label: statisticsBucketLabel(value, value, 'language') })) ?? [],
    format: source?.formats.map(value => ({ value, label: statisticsBucketLabel(value, value, 'format') })) ?? [],
    genre: source?.genres ?? [],
    pass: source?.passes.map(value => ({ value, label: value })) ?? []
  } satisfies Record<StatisticsFilterKey, { value: string; label: string }[]>
  return Object.fromEntries([
    ...primaryFilters.map(filter => [filter.key, statisticsOptionsWithSelection(values[filter.key], draft.value[filter.key])]),
    ...advancedFilters.map(filter => [filter.key, statisticsOptionsWithSelection(values[filter.key], draft.value[filter.key] ? [draft.value[filter.key]] : [])])
  ])
})

function buckets(rows: StatisticsBucket[], kind?: 'language' | 'format') {
  return rows.map(row => ({ ...row, label: row.value === 'unknown' && !kind ? row.label : statisticsBucketLabel(row.value, row.label, kind) }))
}
function movieBars(rows: StatisticsMovieRank[], metric: 'showtime_count' | 'theater_count') {
  return rows.map(movie => ({ value: movie.slug, label: movie.title, href: `/film/${encodeURIComponent(movie.slug)}`, count: movie[metric], detail: metric === 'showtime_count' ? `${statisticsCount(movie.theater_count)} cinémas` : `${statisticsCount(movie.showtime_count)} séances` }))
}
const totals = computed(() => data.value ? [
  { label: 'Séances', count: data.value.totals.showtimes }, { label: 'Films', count: data.value.totals.movies },
  { label: 'Cinémas', count: data.value.totals.theaters }, { label: 'Villes', count: data.value.totals.cities }
] : [])
const incompleteWindow = computed(() => data.value && (data.value.coverage.intersection?.from !== data.value.range.from || data.value.coverage.intersection?.through !== data.value.range.through))
const concentration = computed(() => data.value ? [
  { value: 'top', label: `Les ${data.value.concentration.top_movie_count} films les plus programmés`, count: data.value.concentration.top_showtime_count },
  { value: 'other', label: 'Les autres films', count: data.value.concentration.other_showtime_count }
] : [])
function dateLabel(value: string) {
  return new Intl.DateTimeFormat('fr-FR', { timeZone: 'Europe/Paris', day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(`${value}T12:00:00Z`))
}
const generatedLabel = computed(() => data.value ? new Intl.DateTimeFormat('fr-FR', { timeZone: 'Europe/Paris', dateStyle: 'long', timeStyle: 'short' }).format(new Date(data.value.generated_at)) : '')
const controlClass = 'mt-2 h-12 w-full min-w-0 rounded-none border-2 border-ink bg-surface px-3 text-sm font-bold focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink'
const labelClass = 'min-w-0 text-xs font-extrabold uppercase tracking-wide'
const buttonClass = 'inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink px-4 py-3 text-sm font-extrabold focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40'
const headingClass = 'mb-7 text-2xl font-black tracking-tight sm:text-3xl'
const sectionClass = 'min-w-0 border-t-2 border-ink py-8 sm:py-12'
const canonicalUrl = absoluteSiteUrl(useRuntimeConfig().public.siteUrl, '/statistiques')
useSeoMeta({
  title: 'Statistiques cinéma | MesSeances',
  description: 'Explorez les séances collectées par MesSeances : films, horaires, versions, formats et offre locale.',
  robots: () => !data.value || error.value || statisticsQueryKeys.some(key => route.query[key] !== undefined) ? 'noindex,follow' : 'index,follow',
  ogTitle: 'Statistiques cinéma | MesSeances', ogUrl: canonicalUrl, ogType: 'website'
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="min-w-0 bg-[#f8f7f2] text-ink">
    <header class="border-b-2 border-ink bg-surface">
      <div class="mx-auto max-w-[1440px] px-4 py-12 sm:px-6 sm:py-16 lg:px-10">
        <p class="font-mono text-xs font-black uppercase tracking-[0.15em]">La programmation en chiffres</p>
        <h1 class="mt-5 break-words text-[clamp(2.5rem,7vw,6.5rem)] font-black uppercase leading-none tracking-[-0.06em]">Statistiques<span class="text-primary">.</span></h1>
      </div>
    </header>
    <div class="mx-auto min-w-0 max-w-[1440px] px-4 py-8 sm:px-6 lg:px-10">
      <form class="border-2 border-ink bg-[#f1efe8] p-4 shadow-[5px_5px_0_#27272a] sm:p-6" aria-label="Filtres des statistiques" novalidate @submit.prevent="apply">
        <div class="grid min-w-0 gap-5 sm:grid-cols-2 lg:grid-cols-4">
          <label :class="labelClass">Du
            <input v-model="draft.date" type="date" :min="today" :class="controlClass" :aria-invalid="Boolean(validationError)" :aria-describedby="validationError ? 'statistics-date-error' : undefined" @input="draft.explicitDates = true" />
          </label>
          <label :class="labelClass">Au
            <input v-model="draft.date_to" type="date" :min="draft.date || today" :class="controlClass" :aria-invalid="Boolean(validationError)" :aria-describedby="validationError ? 'statistics-date-error' : undefined" @input="draft.explicitDates = true" />
          </label>
          <StatisticsMultiSelect v-for="filter in primaryFilters" :id="`statistics-${filter.key}`" :key="filter.key" v-model="draft[filter.key]" :label="filter.label" :all-label="filter.all" :options="filterOptions[filter.key] ?? []" :max-selections="statisticsMaxSelections" />
        </div>
        <details class="mt-5" :open="advancedOpen">
          <summary class="w-fit cursor-pointer py-3 text-sm font-extrabold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink">Filtres avancés</summary>
          <div class="mt-3 grid min-w-0 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            <label v-for="filter in advancedFilters" :key="filter.key" :class="labelClass">{{ filter.label }}
              <select v-model="draft[filter.key]" :class="controlClass"><option value="">{{ filter.all }}</option><option v-for="option in filterOptions[filter.key]" :key="option.value" :value="option.value">{{ option.label }}</option></select>
            </label>
          </div>
        </details>
        <p v-if="validationError" id="statistics-date-error" role="alert" class="mt-4 font-bold text-primary-hover">{{ validationError }}</p>
        <div class="mt-5 flex flex-wrap gap-3">
          <button type="submit" :class="[buttonClass, 'bg-ink text-white hover:bg-primary']">Appliquer</button>
          <button type="button" :class="[buttonClass, 'bg-surface hover:bg-highlight']" @click="reset">Réinitialiser</button>
        </div>
      </form>

      <div class="mt-10 min-h-40" :aria-busy="pending">
        <div v-if="pending" role="status" class="min-h-64">
          <span class="sr-only">Chargement des statistiques…</span>
          <div v-if="showSkeleton" aria-hidden="true" class="grid animate-pulse gap-5 motion-reduce:animate-none sm:grid-cols-2 lg:grid-cols-4">
            <div v-for="index in 4" :key="index" class="h-32 border-t-2 border-ink bg-ink/10"></div>
            <div class="h-72 bg-ink/10 sm:col-span-2 lg:col-span-4"></div>
          </div>
        </div>
        <EditorialStatePanel v-else-if="error" semantic="alert" size="tall" shadow="large" class="my-12">
          <template #icon><AlertTriangle :size="32" class="text-primary" aria-hidden="true" /></template>
          <p class="max-w-xl font-bold">{{ error }}</p>
          <template #actions>
            <button type="button" :class="[buttonClass, 'bg-ink text-white hover:bg-primary']" @click="reload"><RefreshCw :size="16" aria-hidden="true" />Réessayer</button>
            <button type="button" :class="[buttonClass, 'bg-surface hover:bg-highlight']" @click="reset">Réinitialiser</button>
          </template>
        </EditorialStatePanel>
        <template v-else-if="data">
          <div class="mb-8 space-y-3 text-sm leading-relaxed">
            <p class="text-base font-extrabold">Du {{ dateLabel(data.range.from) }} au {{ dateLabel(data.range.through) }}</p>
            <p>Instantané généré le <time :datetime="data.generated_at">{{ generatedLabel }}</time> (Europe/Paris). Fenêtre collectée : du {{ dateLabel(data.coverage.snapshot_window.from) }} au {{ dateLabel(data.coverage.snapshot_window.through) }}.</p>
            <p v-if="data.coverage.stale" role="status" class="flex items-start gap-3 border-2 border-primary bg-primary-soft p-4 font-extrabold text-primary-hover"><AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />Données anciennes : cet instantané n’est plus à jour. La programmation peut avoir changé.</p>
            <p v-if="incompleteWindow" class="border-l-4 border-ink bg-highlight/30 p-4 font-bold">{{ data.coverage.intersection ? 'Une partie de la période sélectionnée est hors de la fenêtre collectée.' : 'La période sélectionnée est entièrement hors de la fenêtre collectée.' }} Cela ne permet pas de conclure à une absence de séances.</p>
            <details>
              <summary class="w-fit cursor-pointer py-2 font-extrabold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink">Périmètre et méthode</summary>
              <div class="mt-3 max-w-4xl space-y-3">
                <p>Ces chiffres décrivent uniquement les séances actuellement collectées par MesSeances, y compris celles déjà commencées aujourd’hui. La couverture varie selon le cinéma, la date et le fournisseur ; son exhaustivité est inconnue. Une séance absente ne prouve pas une absence de programmation. Ce n’est ni un total national, ni une série historique, ni une mesure de fréquentation.</p>
                <p>Une séance est comptée une fois par identifiant et fournisseur. Des séances simultanées distinctes restent comptées. L’identité publique d’un film regroupe ses copies chez les fournisseurs, sans fusionner les films sur leur seul titre. Villes et cinémas sont comptés uniquement lorsqu’ils ont des séances correspondantes.</p>
                <p>Les dates sont des jours de programmation, en Europe/Paris. Les séances après minuit restent rattachées au jour de programmation précédent. Toutes les dimensions de filtre se croisent ; chaque version et format reste distinct, sans déduction d’équipement.</p>
                <p>Le filtre de pass désigne les cinémas qui l’acceptent. Il ne garantit pas l’éligibilité de chaque séance, ni l’absence de supplément.</p>
              </div>
            </details>
          </div>

          <section :class="sectionClass" aria-labelledby="statistics-totals">
            <h2 id="statistics-totals" :class="headingClass">En chiffres</h2>
            <dl class="grid grid-cols-2 gap-x-5 gap-y-8 lg:grid-cols-4">
              <div v-for="total in totals" :key="total.label" class="min-w-0 border-l-4 border-ink pl-4">
                <dt class="font-mono text-xs font-black uppercase tracking-wide">{{ total.label }}</dt><dd class="mt-3 break-words text-[clamp(1.75rem,4vw,3.5rem)] font-black leading-none tracking-tight tabular-nums">{{ statisticsCount(total.count) }}</dd>
              </div>
            </dl>
          </section>
          <EditorialStatePanel v-if="data.totals.showtimes === 0" size="tall" class="my-8" semantic="status">
            <p class="font-extrabold">Aucune séance pour ces filtres</p>
            <p class="text-sm">Part des films les plus programmés : {{ statisticsShare(0, 0) }}.</p>
            <template #actions><button type="button" :class="[buttonClass, 'bg-ink text-white hover:bg-primary']" @click="reset">Réinitialiser</button></template>
          </EditorialStatePanel>
          <template v-else>
            <section :class="sectionClass" aria-labelledby="statistics-top">
              <h2 id="statistics-top" :class="headingClass">Films les plus programmés</h2>
              <div class="grid min-w-0 gap-10 lg:grid-cols-2 lg:gap-16">
                <div class="min-w-0"><h3 class="mb-6 text-lg font-extrabold">Par séances</h3><StatisticsBarChart :rows="movieBars(data.top_movies.by_showtimes, 'showtime_count')" label="Films les plus programmés par séances" unit="séances" ordered /></div>
                <div class="min-w-0"><h3 class="mb-6 text-lg font-extrabold">Par cinémas</h3><StatisticsBarChart :rows="movieBars(data.top_movies.by_theaters, 'theater_count')" label="Films les plus programmés par cinémas" unit="cinémas" ordered /></div>
              </div>
            </section>
            <section :class="sectionClass" aria-labelledby="statistics-hours"><h2 id="statistics-hours" :class="headingClass">Quand voir un film</h2><StatisticsHeatmap :cells="data.heatmap" /></section>
            <section :class="sectionClass" aria-labelledby="statistics-versions"><h2 id="statistics-versions" :class="headingClass">Versions</h2><StatisticsBarChart :rows="buckets(data.versions, 'language')" :total="data.totals.showtimes" label="Versions, part des séances" unit="séances" /></section>
            <section :class="sectionClass" aria-labelledby="statistics-formats"><h2 id="statistics-formats" :class="headingClass">Formats</h2><StatisticsBarChart :rows="buckets(data.formats, 'format')" :total="data.totals.showtimes" label="Formats, part des séances" unit="séances" /></section>
            <section :class="sectionClass" aria-labelledby="statistics-genres">
              <h2 id="statistics-genres" :class="headingClass">Genres et durées</h2>
              <p class="mb-7 text-sm">Parts calculées sur les films distincts. Un film peut avoir plusieurs genres : leurs parts peuvent dépasser 100 % au total.</p>
              <div class="grid min-w-0 gap-10 lg:grid-cols-2 lg:gap-16">
                <div class="min-w-0"><h3 class="mb-6 text-lg font-extrabold">Genres</h3><StatisticsBarChart :rows="buckets(data.genres)" :total="data.totals.movies" label="Genres, part des films" unit="films" /></div>
                <div class="min-w-0"><h3 class="mb-6 text-lg font-extrabold">Durées</h3><StatisticsBarChart :rows="buckets(data.runtimes)" :total="data.totals.movies" label="Durées, part des films" unit="films" /></div>
              </div>
            </section>
            <section :class="sectionClass" aria-labelledby="statistics-local"><h2 id="statistics-local" :class="headingClass">L’offre locale</h2><StatisticsLocalTable :local="data.local" /></section>
            <section :class="sectionClass" aria-labelledby="statistics-concentration"><h2 id="statistics-concentration" :class="headingClass">Concentration des séances</h2><StatisticsBarChart :rows="concentration" :total="data.totals.showtimes" label="Films les plus programmés et autres films, part des séances" unit="séances" /></section>
          </template>
        </template>
      </div>
    </div>
  </main>
</template>
