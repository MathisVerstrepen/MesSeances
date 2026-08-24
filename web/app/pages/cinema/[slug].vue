<script setup lang="ts">
import { AlertTriangle, Building2, CalendarDays, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { TheaterShowtimesResponse } from '~/types/api'
import type { ResultGrouping, ResultLayout } from '~/types/showtimeResults'
import { formatLongDate, todayInParis } from '~/utils/date'
import { cinemaDescription } from '~/utils/entityDescriptions'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import { calendarDate, mergeOwnedQuery, singularQueryValue } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { groupShowtimeResults, resultGroupingOptions, resultLayoutOptions, sortShowtimeResults, toTheaterShowtimeResults } from '~/utils/showtimeResults'

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const response = ref<TheaterShowtimesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
let requestId = 0
const DISPLAY_QUERY_KEYS = ['grouping', 'layout', 'view'] as const

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})
const requestedDate = computed(() => calendarDate(singularQueryValue(route.query.date)))
const selectedDate = computed(() => requestedDate.value ?? todayInParis())
const resultGrouping = computed<ResultGrouping>(() => singularQueryValue(route.query.grouping) === 'chronological' ? 'chronological' : 'movie')
const resultLayout = computed<ResultLayout>(() => singularQueryValue(route.query.layout) === 'boxes' ? 'boxes' : 'lines')
const groupingOptions = resultGroupingOptions
const layoutOptions = resultLayoutOptions
const availableDates = computed(() => response.value?.theater.available_dates.filter((date) => date >= todayInParis()) ?? [])

function normalizeLocationPart(value: string): string {
  return value.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLocaleLowerCase('fr-FR').replace(/[^a-z0-9]+/g, ' ').trim()
}

const displayLocation = computed(() => {
  const theater = response.value?.theater
  if (!theater) return { address: '', locality: '' }

  const address = theater.address.trim()
  const postalCode = theater.postal_code.trim()
  const city = theater.city.trim()
  const normalizedAddress = ` ${normalizeLocationPart(address)} `
  const normalizedLocality = normalizeLocationPart([postalCode, city].filter(Boolean).join(' '))
  const hasFullLocality = Boolean(postalCode && city && normalizedAddress.includes(` ${normalizedLocality} `))
  const locality = hasFullLocality ? '' : [postalCode, city].filter(Boolean).join(' ')

  return { address, locality }
})

function isNotFoundError(cause: unknown): boolean {
  return getApiErrorStatus(cause) === 404 || getApiErrorCode(cause) === 'not_found'
}

async function fetchCinema() {
  try {
    return { kind: 'success' as const, response: await api.theaterShowtimes(slug.value, selectedDate.value), errorMessage: '' }
  } catch (error) {
    if (isNotFoundError(error)) return { kind: 'not-found' as const, response: null, errorMessage: '' }
    return { kind: 'upstream-error' as const, response: null, errorMessage: getFrenchApiError(error) }
  }
}

const initial = await useAsyncData(`cinema:${slug.value}:${selectedDate.value}`, fetchCinema)
const initialState = initial.data.value
response.value = initialState?.response ?? null
notFound.value = initialState?.kind === 'not-found'
errorMessage.value = initialState?.errorMessage ?? ''
pending.value = false
if (import.meta.server && initialState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, initialState?.kind === 'not-found' ? 404 : 502)
}

async function loadCinema() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  notFound.value = false
  const state = await fetchCinema()
  if (currentRequest !== requestId) return
  response.value = state.response
  notFound.value = state.kind === 'not-found'
  errorMessage.value = state.errorMessage
  pending.value = false
}

function selectDate(date: string) {
  router.push({
    query: mergeOwnedQuery(route.query, ['date', ...DISPLAY_QUERY_KEYS], {
      date: date === todayInParis() ? undefined : date,
      grouping: resultGrouping.value === 'chronological' ? 'chronological' : undefined,
      layout: resultLayout.value === 'boxes' ? 'boxes' : undefined
    })
  })
}

async function setResultGrouping(grouping: string) {
  if (grouping !== 'movie' && grouping !== 'chronological') return
  if (grouping === resultGrouping.value) return
  await router.push({
    query: mergeOwnedQuery(route.query, DISPLAY_QUERY_KEYS, {
      grouping: grouping === 'chronological' ? grouping : undefined,
      layout: resultLayout.value === 'boxes' ? 'boxes' : undefined
    })
  })
}

async function setResultLayout(layout: string) {
  if (layout !== 'lines' && layout !== 'boxes') return
  if (layout === resultLayout.value) return
  await router.push({
    query: mergeOwnedQuery(route.query, DISPLAY_QUERY_KEYS, {
      grouping: resultGrouping.value === 'chronological' ? 'chronological' : undefined,
      layout: layout === 'boxes' ? layout : undefined
    })
  })
}

watch([slug, selectedDate], () => void loadCinema())

const normalizedResults = computed(() => response.value ? toTheaterShowtimeResults(response.value) : [])
const movieGroups = computed(() => groupShowtimeResults(sortShowtimeResults(normalizedResults.value)))

const config = useRuntimeConfig()
const canonicalUrl = computed(() => absoluteSiteUrl(config.public.siteUrl, `/cinema/${encodeURIComponent(slug.value)}`))
const pageTitle = computed(() => response.value ? `${response.value.theater.name}, ${response.value.theater.city} : séances et horaires` : 'Cinéma - MesSeances')
const pageDescription = computed(() => response.value
  ? cinemaDescription({
      name: response.value.theater.name,
      provider: response.value.theater.provider,
      city: response.value.theater.city,
      address: response.value.theater.address,
      postalCode: response.value.theater.postal_code,
      availableDateCount: response.value.theater.available_dates.length
    })
  : 'Consultez les séances et films programmés dans ce cinéma.')
const robots = computed(() => response.value && !pending.value && !errorMessage.value && !notFound.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
const cinemaJsonLd = computed(() => {
  const current = response.value
  if (!current || pending.value || errorMessage.value || notFound.value) return null
  const theaterUrl = canonicalUrl.value
  const theaterId = `${theaterUrl}#cinema`
  const theaterNode: JsonLdNode = { '@type': 'MovieTheater', '@id': theaterId, name: current.theater.name, url: theaterUrl, description: pageDescription.value }
  if (current.theater.address.trim() && current.theater.city.trim() && current.theater.postal_code.trim()) theaterNode.address = current.theater.address.trim()
  const cityUrl = absoluteSiteUrl(config.public.siteUrl, `/ville/${encodeURIComponent(current.theater.city_slug)}/cinemas`)
  const graph: JsonLdNode[] = [
    theaterNode,
    {
      '@type': 'BreadcrumbList',
      '@id': `${theaterUrl}#breadcrumb`,
      itemListElement: [
        { '@type': 'ListItem', position: 1, name: 'Accueil', item: absoluteSiteUrl(config.public.siteUrl, '/') },
        { '@type': 'ListItem', position: 2, name: current.theater.city, item: cityUrl },
        { '@type': 'ListItem', position: 3, name: current.theater.name, item: theaterUrl }
      ]
    }
  ]
  const movieIds = new Map<string, string>()
  for (const group of movieGroups.value) {
    const movie = group.results[0]
    if (!movie) continue
    const movieUrl = absoluteSiteUrl(config.public.siteUrl, `/film/${encodeURIComponent(movie.movieSlug)}`)
    const movieId = `${movieUrl}#movie`
    movieIds.set(movie.movieSlug, movieId)
    graph.push({ '@type': 'Movie', '@id': movieId, name: movie.movieTitle, url: movieUrl })
  }
  const seen = new Set<string>()
  for (const showtime of current.showtimes) {
    const id = showtime.id.trim()
    const movieId = movieIds.get(showtime.movie.slug)
    const start = Date.parse(showtime.start_time)
    const end = Date.parse(showtime.end_time)
    if (!id || seen.has(id) || !movieId || !showtime.movie.title.trim() || !Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue
    seen.add(id)
    graph.push({
      '@type': 'ScreeningEvent',
      '@id': `${theaterUrl}#screening-${encodeURIComponent(id)}`,
      name: `${showtime.movie.title} à ${current.theater.name}`,
      startDate: showtime.start_time,
      endDate: showtime.end_time,
      location: { '@id': theaterId },
      workPresented: { '@id': movieId }
    })
  }
  return serializeJsonLd({ '@context': 'https://schema.org', '@graph': graph })
})

useSeoMeta({
  robots,
  title: pageTitle,
  description: pageDescription,
  ogTitle: pageTitle,
  ogDescription: pageDescription,
  ogUrl: canonicalUrl,
  ogType: 'website'
})
useHead(() => ({
  link: [{ rel: 'canonical', href: canonicalUrl.value }],
  script: cinemaJsonLd.value ? [{ key: 'cinema-jsonld', type: 'application/ld+json', innerHTML: cinemaJsonLd.value }] : []
}))
</script>

<template>
  <main class="discovery-page mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-14">
    <EditorialStatePanel v-if="pending && !response" semantic="status" live="polite" size="standard" shadow="large" class="discovery-state mx-auto max-w-3xl font-bold">
      <template #icon><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /></template>
      <p>Chargement du cinéma…</p>
    </EditorialStatePanel>
    <EditorialStatePanel v-else-if="notFound" semantic="alert" size="standard" shadow="large" class="discovery-state mx-auto max-w-3xl font-bold">
      <template #icon><Building2 :size="36" aria-hidden="true" /></template>
      <template #heading><h1 class="text-2xl font-black">Cinéma introuvable</h1></template>
      <p>Ce cinéma n’est pas disponible dans la programmation actuelle.</p>
      <template #actions><NuxtLink to="/cinemas" class="discovery-action">Voir les cinémas</NuxtLink></template>
    </EditorialStatePanel>
    <EditorialStatePanel v-else-if="errorMessage && !response" semantic="alert" size="standard" shadow="large" class="discovery-state mx-auto max-w-3xl font-bold">
      <template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template>
      <template #heading><h1 class="text-2xl font-black">Impossible de charger ce cinéma</h1></template>
      <p>{{ errorMessage }}</p>
      <template #actions><button type="button" class="discovery-action" @click="loadCinema"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template>
    </EditorialStatePanel>

    <template v-else-if="response">
      <nav class="breadcrumb" aria-label="Fil d’Ariane">
        <ol class="flex flex-wrap items-center gap-2">
          <li><NuxtLink to="/">Accueil</NuxtLink></li><li aria-hidden="true">/</li>
          <li><NuxtLink :to="`/ville/${encodeURIComponent(response.theater.city_slug)}/cinemas`">{{ response.theater.city }}</NuxtLink></li><li aria-hidden="true">/</li>
          <li aria-current="page">{{ response.theater.name }}</li>
        </ol>
      </nav>
      <header class="border-2 border-ink bg-surface shadow-[8px_8px_0_#27272a]">
        <div class="grid lg:grid-cols-[minmax(0,1.55fr)_minmax(17rem,0.65fr)]">
          <div class="min-w-0 p-5 sm:p-8 lg:p-10">
            <p class="utility-label flex items-center gap-2"><MapPin :size="16" aria-hidden="true" /> {{ response.theater.city }}</p>
            <h1 class="mt-4 break-words text-[clamp(2.5rem,5.5vw,5rem)] font-black uppercase leading-[0.9] tracking-[-0.065em]">{{ response.theater.name }}</h1>
          </div>

          <dl class="grid border-t-2 border-ink sm:grid-cols-2 lg:grid-cols-1 lg:border-l-2 lg:border-t-0">
            <div class="min-w-0 p-5 sm:p-6">
              <dt class="utility-label flex items-center gap-3 text-muted"><MapPin :size="20" class="shrink-0 text-primary" aria-hidden="true" /> Adresse</dt>
              <dd class="mt-2 break-words pl-8 text-sm font-bold leading-6">
                <span v-if="displayLocation.address" class="block">{{ displayLocation.address }}</span>
                <span v-if="displayLocation.locality" class="block">{{ displayLocation.locality }}</span>
              </dd>
            </div>
            <div class="min-w-0 border-t-2 border-ink p-5 sm:border-l-2 sm:border-t-0 sm:p-6 lg:border-l-0 lg:border-t-2">
              <dt class="utility-label flex items-center gap-3 text-muted"><CalendarDays :size="20" class="shrink-0 text-primary" aria-hidden="true" /> Programmation</dt>
              <dd class="mt-2 pl-8 text-sm font-bold leading-6">{{ response.theater.available_dates.length }} date{{ response.theater.available_dates.length > 1 ? 's' : '' }} disponible{{ response.theater.available_dates.length > 1 ? 's' : '' }}</dd>
            </div>
          </dl>
        </div>

        <div class="border-t-2 border-ink bg-[#f1efe8] px-5 py-4 sm:px-8 sm:py-5 lg:px-10">
          <p class="max-w-4xl text-sm font-semibold leading-6 sm:text-base sm:leading-7">{{ pageDescription }}</p>
        </div>
      </header>

      <section class="mt-12" aria-labelledby="cinema-showtimes-heading">
        <div class="flex items-end justify-between gap-4 border-b-2 border-ink pb-5">
          <div>
            <p class="utility-label">Programmation</p>
            <h2 id="cinema-showtimes-heading" class="mt-2 text-4xl font-black tracking-[-0.05em] sm:text-5xl">Séances</h2>
            <p v-if="response.date" class="mt-2 font-mono text-xs font-bold uppercase capitalize text-muted"><time :datetime="response.date">{{ formatLongDate(response.date) }}</time></p>
          </div>
          <ShareButton class="shrink-0" />
        </div>
        <div v-if="availableDates.length || (!pending && !errorMessage && normalizedResults.length)" class="mt-5 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <ShowtimeDateBar v-if="availableDates.length" :selected-date="response.date ?? selectedDate" :available-dates="availableDates" :today="todayInParis()" @select="selectDate" />
          <div v-if="!pending && !errorMessage && normalizedResults.length" class="grid grid-cols-2 border-2 border-ink bg-surface divide-x-2 divide-ink lg:hidden" role="group" aria-label="Réglages d’affichage des séances">
            <ResultSettingMenu id="cinema-mobile-result-grouping" label="Groupement" :current-value="resultGrouping" :options="groupingOptions" @select="setResultGrouping" />
            <ResultSettingMenu id="cinema-mobile-result-layout" label="Vue" :current-value="resultLayout" :options="layoutOptions" @select="setResultLayout" />
          </div>
          <div v-if="!pending && !errorMessage && normalizedResults.length" class="hidden shrink-0 items-stretch border-2 border-ink bg-surface divide-x-2 divide-ink lg:flex" role="group" aria-label="Réglages d’affichage des séances">
            <ResultSettingMenu id="cinema-desktop-result-grouping" class="w-40" label="Groupement" :current-value="resultGrouping" :options="groupingOptions" @select="setResultGrouping" />
            <ResultSettingMenu id="cinema-desktop-result-layout" class="w-32" label="Vue" :current-value="resultLayout" :options="layoutOptions" @select="setResultLayout" />
          </div>
        </div>

        <EditorialStatePanel v-if="pending" semantic="status" live="polite" size="standard" shadow="large" class="discovery-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /></template><p>Chargement des séances…</p></EditorialStatePanel>
        <EditorialStatePanel v-else-if="errorMessage" semantic="alert" size="standard" shadow="large" class="discovery-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Impossible de charger ces séances</h3></template><p>{{ errorMessage }}</p><template #actions><button type="button" class="discovery-action" @click="loadCinema"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template></EditorialStatePanel>
        <EditorialStatePanel v-else-if="normalizedResults.length === 0" size="standard" shadow="large" class="discovery-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><CalendarDays :size="36" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Aucune séance à cette date</h3></template><p>Choisissez une autre date pour consulter la programmation.</p></EditorialStatePanel>
        <ShowtimeResults v-else :results="normalizedResults" :grouping="resultGrouping" :layout="resultLayout" scope="single-theater" />
      </section>
    </template>
  </main>
</template>

<style scoped>
.discovery-page { min-height: 70vh; background-color: #f8f7f2; background-image: linear-gradient(rgba(39,39,42,.07) 1px,transparent 1px),linear-gradient(90deg,rgba(39,39,42,.07) 1px,transparent 1px); background-size: 28px 28px; }
.discovery-action { display: inline-flex; min-height: 2.75rem; align-items: center; justify-content: center; gap: .5rem; border: 2px solid #27272a; background: #27272a; padding: .65rem .9rem; color: #fff; font-family: ui-monospace,monospace; font-size: .7rem; font-weight: 900; text-transform: uppercase; }
.breadcrumb,.utility-label { font-family: ui-monospace,monospace; font-size: .68rem; font-weight: 900; letter-spacing: .1em; text-transform: uppercase; }
.breadcrumb { margin-bottom: 1.5rem; color: var(--color-muted); }
.breadcrumb a:hover { color: var(--color-primary); }
</style>
