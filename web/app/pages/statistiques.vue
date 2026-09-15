<script setup lang="ts">
import { AlertTriangle, RefreshCw, X } from '@lucide/vue'
import type { HistoryStatisticsResponse, StatisticsBucket, StatisticsMovieRank, StatisticsOptions } from '~/types/api'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { queriesEqual } from '~/utils/routeQuery'
import { createStatisticsRequest, statisticsBucketLabel, statisticsChainLabels, statisticsCount, statisticsOptionsWithSelection, statisticsQueryKeys, statisticsShare, type StatisticsFilterKey, type StatisticsMultiFilterKey, type StatisticsScalarFilterKey } from '~/utils/statistics'
import { parseStatisticsPageQuery, statisticsCustomDraft, statisticsParisToday, statisticsPeriod, statisticsPeriods, statisticsPageDraft, statisticsPageDraftQuery, statisticsPageRoute, statisticsPageSignature } from '~/utils/statisticsHistory'

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const today = useState('statistics-paris-today', () => statisticsParisToday())
const signature = computed(() => statisticsPageSignature(route.query))

function errorMessage(cause: unknown) {
  const status = getApiErrorStatus(cause)
  if (getApiErrorCode(cause) === 'history_query_timeout') return 'La recherche historique prend trop de temps. Réduisez la période ou réessayez.'
  if (getApiErrorCode(cause) === 'history_busy') return 'Le service historique est occupé. Patientez un instant avant de réessayer.'
  if (getApiErrorCode(cause) === 'history_unavailable') return 'L’historique est temporairement indisponible. Réessayez plus tard.'
  if (status === 400) return 'Filtres invalides. Vérifiez les dates et les sélections, ou réinitialisez les filtres.'
  if (status === 429) return 'Trop de demandes. Patientez un instant avant de réessayer.'
  if (status === 503) return 'Les statistiques ne sont pas encore disponibles. Réessayez plus tard.'
  if (status !== undefined && status >= 500) return 'Impossible de charger les statistiques. Réessayez plus tard.'
  return getFrenchApiError(cause)
}

async function fetchStatistics(signal?: AbortSignal) {
  today.value = statisticsParisToday()
  const parsed = parseStatisticsPageQuery(route.query, today.value)
  if (parsed.error) return { response: null, message: parsed.error, status: 400 }
  try {
    return { response: await api.historyStatistics(parsed.query, signal), message: '', status: 200 }
  } catch (cause) {
    return { response: null, message: errorMessage(cause), status: getApiErrorStatus(cause) === 400 ? 400 : 502 }
  }
}

const initialController = new AbortController()
const initial = await useAsyncData(`statistics:${signature.value}`, () => fetchStatistics(initialController.signal), { lazy: true })
const data = shallowRef<HistoryStatisticsResponse | null>(initial.data.value?.response ?? null)
const historyData = computed(() => data.value)
const error = ref(initial.data.value?.message ?? '')
const options = shallowRef<StatisticsOptions | null>(data.value?.options ?? null)
const draft = ref(statisticsPageDraft(route.query, today.value))
const optionLimits = shallowRef<HistoryStatisticsResponse['limits']['options'] | undefined>(historyData.value?.limits.options)
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
    optionLimits.value = result.response.limits.options
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
    initialController.abort()
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
  draft.value = statisticsPageDraft(route.query, statisticsParisToday())
  validationError.value = ''
  void reload()
}, { flush: 'sync' })
function refreshCalendarDay() {
  if (document.visibilityState !== 'visible') return
  const current = statisticsParisToday()
  if (today.value === current) return
  today.value = current
  const { period } = statisticsPeriod(route.query)
  if (period === 'next7' || period === 'next30') void reload()
}
onMounted(() => { document.addEventListener('visibilitychange', refreshCalendarDay); refreshCalendarDay() })
onBeforeUnmount(() => { initialActive = false; initialController.abort(); request.cancel(); clearTimeout(skeletonTimer); document.removeEventListener('visibilitychange', refreshCalendarDay) })

async function apply() {
  const current = statisticsParisToday()
  const dayChanged = today.value !== current
  const parsed = statisticsPageDraftQuery(draft.value, current)
  validationError.value = parsed.error
  if (parsed.error) return
  const query = statisticsPageRoute(route.query, draft.value.period, parsed.query)
  const changed = statisticsPageSignature(query) !== signature.value
  if (!queriesEqual(route.query, query)) await router.push({ query })
  if (!changed && (error.value || (dayChanged && (draft.value.period === 'next7' || draft.value.period === 'next30')))) await reload()
}
async function reset() {
  const query = statisticsPageRoute(route.query)
  const changed = statisticsPageSignature(query) !== signature.value
  draft.value = statisticsPageDraft(query, statisticsParisToday())
  validationError.value = ''
  if (!queriesEqual(route.query, query)) await router.push({ query })
  if (!changed) await reload()
}
function changePeriod() {
  validationError.value = ''
  if (draft.value.period === 'custom') draft.value = statisticsCustomDraft(draft.value, data.value?.range)
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
watch(signature, () => {
  const selected = advancedFilters.some(filter => Boolean(draft.value[filter.key]))
  advancedOpen.value = selected
})
const rawFilterOptions = computed(() => {
  const source = options.value
  return {
    city: source?.cities.map(city => ({ value: city.slug, label: city.name })) ?? [],
    theater: source?.theaters.map(theater => ({ value: theater.id, label: `${theater.name} · ${theater.city}` })) ?? [],
    chain: source?.chains.map(chain => ({ value: chain, label: statisticsChainLabels[chain] })) ?? [],
    language: source?.languages.map(value => ({ value, label: statisticsBucketLabel(value, value, 'language') })) ?? [],
    format: source?.formats.map(value => ({ value, label: statisticsBucketLabel(value, value, 'format') })) ?? [],
    genre: source?.genres ?? [],
    pass: source?.passes.map(value => ({ value, label: value })) ?? []
  } satisfies Record<StatisticsFilterKey, { value: string; label: string }[]>
})
const filterOptions = computed(() => {
  const values = rawFilterOptions.value
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
const concentration = computed(() => data.value ? [
  { value: 'top', label: `Les ${data.value.concentration.top_movie_count} films les plus programmés`, count: data.value.concentration.top_showtime_count },
  { value: 'other', label: 'Les autres films', count: data.value.concentration.other_showtime_count }
] : [])
function dateLabel(value: string) {
  return new Intl.DateTimeFormat('fr-FR', { timeZone: 'Europe/Paris', day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(`${value}T12:00:00Z`))
}
const generatedLabel = computed(() => data.value ? new Intl.DateTimeFormat('fr-FR', { timeZone: 'Europe/Paris', dateStyle: 'long', timeStyle: 'short' }).format(new Date(data.value.generated_at)) : '')
function timestampLabel(value: string) { return new Intl.DateTimeFormat('fr-FR', { timeZone: 'Europe/Paris', dateStyle: 'long', timeStyle: 'short' }).format(new Date(value)) }
const controlClass = 'mt-2 h-12 w-full min-w-0 rounded-none border-2 border-ink bg-surface px-3 text-sm font-bold focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink'
const labelClass = 'min-w-0 text-xs font-extrabold uppercase tracking-wide'
const buttonClass = 'inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink px-4 py-3 text-sm font-extrabold focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40'
const headingClass = 'mb-7 text-2xl font-black tracking-tight sm:text-3xl'
const sectionClass = 'min-w-0 border-t-2 border-ink py-8 sm:py-12'
const canonicalUrl = absoluteSiteUrl(useRuntimeConfig().public.siteUrl, '/statistiques')
useSeoMeta({
  title: 'Statistiques cinéma | MesSeances',
  description: 'Explorez les séances collectées par MesSeances : films, horaires, versions, formats et offre locale.',
  robots: () => !data.value || error.value || route.query.period !== undefined || route.query.mode !== undefined || statisticsQueryKeys.some(key => route.query[key] !== undefined) ? 'noindex,follow' : 'index,follow',
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
        <div v-if="draft.film" class="mb-5 flex min-w-0 items-center gap-1">
          <p class="min-w-0 font-bold [overflow-wrap:anywhere]">Film : {{ draft.film }}</p>
          <button type="button" class="inline-flex size-11 shrink-0 items-center justify-center text-ink hover:bg-highlight focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" aria-label="Retirer le film" title="Retirer le film" @click="draft.film = ''">
            <X :size="16" aria-hidden="true" />
          </button>
        </div>
        <div class="grid min-w-0 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          <label :class="labelClass">Période
            <select v-model="draft.period" :class="controlClass" @change="changePeriod"><option v-for="choice in statisticsPeriods" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select>
          </label>
          <template v-if="draft.period === 'custom'">
            <label :class="labelClass">Du
              <input v-model="draft.date" type="date" required :class="controlClass" :aria-invalid="Boolean(validationError)" :aria-describedby="validationError ? 'statistics-date-error' : undefined" />
            </label>
            <label :class="labelClass">Au
              <input v-model="draft.date_to" type="date" required :class="controlClass" :aria-invalid="Boolean(validationError)" :aria-describedby="validationError ? 'statistics-date-error' : undefined" />
            </label>
          </template>
          <StatisticsHistorySelect v-for="filter in primaryFilters" :id="`statistics-${filter.key}`" :key="`${signature}:${filter.key}`" v-model="draft[filter.key]" :kind="filter.key" :label="filter.label" :all-label="filter.all" :options="rawFilterOptions[filter.key]" :has-more="optionLimits?.[filter.key === 'city' ? 'cities' : 'theaters']" />
        </div>
        <details class="mt-5" :open="advancedOpen">
          <summary class="w-fit cursor-pointer py-3 text-sm font-extrabold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink">Filtres avancés</summary>
          <div class="mt-3 grid min-w-0 gap-5 sm:grid-cols-2 lg:grid-cols-3">
            <template v-for="filter in advancedFilters" :key="`${signature}:${filter.key}`">
              <StatisticsHistorySelect v-if="filter.key === 'genre' || filter.key === 'pass'" :id="`statistics-${filter.key}`" :model-value="draft[filter.key] ? [draft[filter.key]] : []" :kind="filter.key" :label="filter.label" :all-label="filter.all" :options="rawFilterOptions[filter.key]" :has-more="optionLimits?.[filter.key === 'genre' ? 'genres' : 'passes']" single @update:model-value="draft[filter.key] = $event[0] ?? ''" />
            <label v-else :class="labelClass">{{ filter.label }}
              <select v-model="draft[filter.key]" :class="controlClass"><option value="">{{ filter.all }}</option><option v-for="option in filterOptions[filter.key]" :key="option.value" :value="option.value">{{ option.label }}</option></select>
            </label>
            </template>
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
            <p v-if="data.range" class="text-base font-extrabold">Du {{ dateLabel(data.range.from) }} au {{ dateLabel(data.range.through) }}</p>
            <template v-if="historyData">
              <p v-if="!historyData.coverage.collection_started_at">La collecte historique n’a pas encore commencé.</p>
              <p v-else>Début de la collecte : <time :datetime="historyData.coverage.collection_started_at">{{ timestampLabel(historyData.coverage.collection_started_at) }}</time>.<template v-if="historyData.coverage.last_publication_at"> Dernière réception réussie : <time :datetime="historyData.coverage.last_publication_at">{{ timestampLabel(historyData.coverage.last_publication_at) }}</time>. Calculées le <time :datetime="data.generated_at">{{ generatedLabel }}</time>.</template></p>
            </template>
            <details>
              <summary class="w-fit cursor-pointer py-2 font-extrabold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink">Périmètre et méthode</summary>
              <div class="mt-3 max-w-4xl space-y-3">
                <template v-if="historyData">
                  <p>Toutes les séances enregistrées par MesSéances depuis le début de la collecte historique sont conservées. « Depuis le début de la collecte » inclut les séances passées et futures enregistrées, sans filtre de date.</p>
                  <p>Aucune donnée antérieure au début de la collecte n’a été importée. La couverture varie selon le cinéma, la date et le fournisseur ; son exhaustivité et sa continuité sont inconnues. Les annonces disparues restent conservées : leur absence ne signifie pas une annulation connue. Ce n’est ni un total national ni une mesure de fréquentation.</p>
                  <p>Une séance est comptée une fois par fournisseur, identifiant source, cinéma et jour de programmation. Les dernières valeurs reçues remplacent les précédentes pour cette identité, sans ajouter une séance. Un identifiant source différent peut compter séparément une annonce corrigée ; aucune fusion n’est déduite du film et de l’horaire. Des séances simultanées distinctes restent comptées.</p>
                  <p>Les métadonnées utilisent les dernières informations conservées des cinémas et la classification publique actuelle des films. Une correction peut donc changer les statistiques passées sans ajouter de séance. L’identité des films suit les identifiants durables des fournisseurs ; leur éventuelle réutilisation n’est pas résolue par cet historique.</p>
                  <p>Les dates de réception indiquent une publication réussie, pas un nouveau relevé chez le fournisseur. La date source indique la génération annoncée par le fournisseur.</p>
                  <ul class="space-y-2">
                    <li v-for="provider in historyData.coverage.providers" :key="provider.provider"><strong>{{ statisticsChainLabels[provider.provider] }}</strong> : début {{ timestampLabel(provider.collection_started_at) }} ; dernière réception {{ timestampLabel(provider.last_publication_at) }} ; génération source {{ timestampLabel(provider.source_generated_at) }}.</li>
                  </ul>
                  <p>L’identité publique d’un film regroupe ses copies chez les fournisseurs, sans fusionner les films sur leur seul titre. Villes et cinémas sont comptés uniquement lorsqu’ils ont des séances correspondantes.</p>
                </template>
                <p>Les dates sont des jours de programmation, en Europe/Paris. Les séances après minuit restent rattachées au jour de programmation précédent. Toutes les dimensions de filtre se croisent ; chaque version et format reste distinct, sans déduction d’équipement.</p>
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
            <p class="font-extrabold">Aucune séance enregistrée pour ces filtres.</p>
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
                <div class="min-w-0"><h3 class="mb-6 text-lg font-extrabold">{{ historyData?.limits.genres ? '100 premiers genres' : 'Genres' }}</h3><StatisticsBarChart :rows="buckets(data.genres)" :total="data.totals.movies" label="Genres, part des films" unit="films" /></div>
                <div class="min-w-0"><h3 class="mb-6 text-lg font-extrabold">Durées</h3><StatisticsBarChart :rows="buckets(data.runtimes)" :total="data.totals.movies" label="Durées, part des films" unit="films" /></div>
              </div>
            </section>
            <section :class="sectionClass" aria-labelledby="statistics-local"><h2 id="statistics-local" :class="headingClass">L’offre locale</h2><StatisticsLocalTable :key="signature" :local="data.local" :limits="historyData?.limits.local" /></section>
            <section :class="sectionClass" aria-labelledby="statistics-concentration"><h2 id="statistics-concentration" :class="headingClass">Concentration des séances</h2><StatisticsBarChart :rows="concentration" :total="data.totals.showtimes" label="Films les plus programmés et autres films, part des séances" unit="séances" /></section>
          </template>
        </template>
      </div>
    </div>
  </main>
</template>
