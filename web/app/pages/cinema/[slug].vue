<script setup lang="ts">
import { AlertTriangle, Building2, CalendarDays, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { TheaterShowtimesResponse, TimelineShowtime } from '~/types/api'
import { formatDateLabel, formatLongDate, formatParisTime, todayInParis } from '~/utils/date'
import { cinemaDescription } from '~/utils/entityDescriptions'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import { calendarDate, mergeOwnedQuery, singularQueryValue } from '~/utils/routeQuery'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const response = ref<TheaterShowtimesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const failedMediaUrls = ref(new Set<string>())
const movieGroupList = ref<HTMLElement | null>(null)
let requestId = 0
const DISPLAY_QUERY_KEYS = ['grouping', 'layout', 'view'] as const
type ResultGrouping = 'movie' | 'chronological'
type ResultLayout = 'lines' | 'boxes'

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})
const requestedDate = computed(() => calendarDate(singularQueryValue(route.query.date)))
const selectedDate = computed(() => requestedDate.value ?? todayInParis())
const resultGrouping = computed<ResultGrouping>(() => singularQueryValue(route.query.grouping) === 'chronological' ? 'chronological' : 'movie')
const resultLayout = computed<ResultLayout>(() => singularQueryValue(route.query.layout) === 'boxes' ? 'boxes' : 'lines')
const groupingOptions: [{ value: ResultGrouping; label: string }, { value: ResultGrouping; label: string }] = [
  { value: 'movie', label: 'Par film' },
  { value: 'chronological', label: 'Chronologique' }
]
const layoutOptions: [{ value: ResultLayout; label: string }, { value: ResultLayout; label: string }] = [
  { value: 'lines', label: 'Lignes' },
  { value: 'boxes', label: 'Boîtes' }
]

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
  await nextTick()
  detectFailedMedia()
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

const chronologicalShowtimes = computed(() => [...(response.value?.showtimes ?? [])].sort((first, second) => {
  const timeDifference = Date.parse(first.start_time) - Date.parse(second.start_time)
  return timeDifference || first.id.localeCompare(second.id)
}))

const movieGroups = computed(() => {
  const groups = new Map<string, { movie: TimelineShowtime['movie']; showtimes: TimelineShowtime[]; posterUrl: string | null; backdropUrl: string | null }>()
  for (const showtime of chronologicalShowtimes.value) {
    const current = groups.get(showtime.movie.slug)
    if (current) {
      current.showtimes.push(showtime)
      current.posterUrl ||= safePosterUrl(showtime.poster_url)
      current.backdropUrl ||= safeBackdropUrl(showtime.backdrop_url)
    } else {
      groups.set(showtime.movie.slug, {
        movie: showtime.movie,
        showtimes: [showtime],
        posterUrl: safePosterUrl(showtime.poster_url),
        backdropUrl: safeBackdropUrl(showtime.backdrop_url)
      })
    }
  }
  return [...groups.values()]
})

function mediaAvailable(url: string | null): url is string {
  return Boolean(url) && !failedMediaUrls.value.has(url!)
}

function markMediaFailed(url: string) {
  failedMediaUrls.value = new Set([...failedMediaUrls.value, url])
}

function showtimePosterUrl(showtime: TimelineShowtime): string | null {
  return safePosterUrl(showtime.poster_url)
}

function showtimeBackdropUrl(showtime: TimelineShowtime): string | null {
  return safeBackdropUrl(showtime.backdrop_url)
}

function detectFailedMedia() {
  for (const image of movieGroupList.value?.querySelectorAll<HTMLImageElement>('img[data-media-url]') ?? []) {
    if (image.complete && image.naturalWidth === 0 && image.dataset.mediaUrl) markMediaFailed(image.dataset.mediaUrl)
  }
}

function bookingLabel(showtime: TimelineShowtime): string {
  return `Séance de ${showtime.movie.title} à ${formatParisTime(showtime.start_time)} au ${response.value?.theater.name ?? 'cinéma'}, réserver`
}

function formatRoom(room: string): string {
  const roomName = room.trim().replace(/^salle\b\s*/i, '')
  return roomName ? `Salle ${roomName}` : 'Salle'
}

onMounted(() => nextTick(detectFailedMedia))

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
    const movieUrl = absoluteSiteUrl(config.public.siteUrl, `/film/${encodeURIComponent(group.movie.slug)}`)
    const movieId = `${movieUrl}#movie`
    movieIds.set(group.movie.slug, movieId)
    graph.push({ '@type': 'Movie', '@id': movieId, name: group.movie.title, url: movieUrl })
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
    <div v-if="pending && !response" class="discovery-state" role="status" aria-live="polite">
      <LoaderCircle :size="34" class="animate-spin" aria-hidden="true" />
      <p>Chargement du cinéma…</p>
    </div>
    <div v-else-if="notFound" class="discovery-state" role="alert">
      <Building2 :size="36" aria-hidden="true" />
      <h1>Cinéma introuvable</h1>
      <p>Ce cinéma n’est pas disponible dans la programmation actuelle.</p>
      <NuxtLink to="/cinemas" class="discovery-action">Voir les cinémas</NuxtLink>
    </div>
    <div v-else-if="errorMessage && !response" class="discovery-state" role="alert">
      <AlertTriangle :size="34" class="text-primary" aria-hidden="true" />
      <h1>Impossible de charger ce cinéma</h1>
      <p>{{ errorMessage }}</p>
      <button type="button" class="discovery-action" @click="loadCinema"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button>
    </div>

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
        <div v-if="response.theater.available_dates.length || (!pending && !errorMessage && chronologicalShowtimes.length)" class="mt-5 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div v-if="response.theater.available_dates.length" class="flex min-w-0 gap-2 overflow-x-auto pb-2" aria-label="Choisir une date">
            <button v-for="date in response.theater.available_dates" :key="date" type="button" class="date-button" :class="response.date === date ? 'date-button--active' : undefined" :aria-pressed="response.date === date" @click="selectDate(date)">{{ formatDateLabel(date) }}</button>
          </div>
          <div v-if="!pending && !errorMessage && chronologicalShowtimes.length" class="grid grid-cols-2 border-2 border-ink bg-surface divide-x-2 divide-ink lg:hidden" role="group" aria-label="Réglages d’affichage des séances">
            <ResultSettingMenu id="cinema-mobile-result-grouping" label="Groupement" :current-value="resultGrouping" :options="groupingOptions" @select="setResultGrouping" />
            <ResultSettingMenu id="cinema-mobile-result-layout" label="Vue" :current-value="resultLayout" :options="layoutOptions" @select="setResultLayout" />
          </div>
          <div v-if="!pending && !errorMessage && chronologicalShowtimes.length" class="hidden shrink-0 items-stretch border-2 border-ink bg-surface divide-x-2 divide-ink lg:flex" role="group" aria-label="Réglages d’affichage des séances">
            <ResultSettingMenu id="cinema-desktop-result-grouping" class="w-40" label="Groupement" :current-value="resultGrouping" :options="groupingOptions" @select="setResultGrouping" />
            <ResultSettingMenu id="cinema-desktop-result-layout" class="w-32" label="Vue" :current-value="resultLayout" :options="layoutOptions" @select="setResultLayout" />
          </div>
        </div>

        <div v-if="pending" class="discovery-state mt-8" role="status" aria-live="polite"><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /><p>Chargement des séances…</p></div>
        <div v-else-if="errorMessage" class="discovery-state mt-8" role="alert"><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /><h3>Impossible de charger ces séances</h3><p>{{ errorMessage }}</p><button type="button" class="discovery-action" @click="loadCinema"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></div>
        <div v-else-if="movieGroups.length === 0" class="discovery-state mt-8"><CalendarDays :size="36" aria-hidden="true" /><h3>Aucune séance à cette date</h3><p>Choisissez une autre date pour consulter la programmation.</p></div>
        <div v-else-if="resultGrouping === 'movie'" ref="movieGroupList" class="mt-8 space-y-8">
          <article v-for="group in movieGroups" :key="group.movie.slug" :data-movie-slug="group.movie.slug" class="border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
            <header class="relative isolate grid min-h-48 grid-cols-[96px_minmax(0,1fr)] items-end gap-5 overflow-hidden border-b-2 border-ink p-4 sm:min-h-56 sm:grid-cols-[120px_minmax(0,1fr)] sm:p-5" :class="mediaAvailable(group.backdropUrl) ? 'text-white' : 'bg-[#f1efe8] text-ink'">
              <img v-if="mediaAvailable(group.backdropUrl)" :src="group.backdropUrl" alt="" aria-hidden="true" :data-media-url="group.backdropUrl" :data-movie-slug="group.movie.slug" data-media-kind="backdrop" class="absolute inset-0 -z-20 size-full object-cover" @error="markMediaFailed(group.backdropUrl)" />
              <div v-if="mediaAvailable(group.backdropUrl)" class="absolute inset-0 -z-10 bg-black/80" aria-hidden="true" />
              <div class="aspect-[2/3] w-24 overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[5px_5px_0_#27272a] sm:w-[120px]">
                <img v-if="mediaAvailable(group.posterUrl)" :src="group.posterUrl" :alt="`Affiche de ${group.movie.title}`" :data-media-url="group.posterUrl" :data-movie-slug="group.movie.slug" data-media-kind="poster" class="size-full object-cover" @error="markMediaFailed(group.posterUrl)" />
                <div v-else :data-poster-fallback="group.movie.slug" class="flex size-full flex-col items-center justify-center gap-2 px-2 text-center text-muted">
                  <Film :size="28" aria-hidden="true" />
                  <span class="text-[10px] font-bold">Affiche indisponible</span>
                </div>
              </div>
              <div class="min-w-0 pb-1">
                <h3 class="break-words text-2xl font-black tracking-[-0.04em] sm:text-3xl"><NuxtLink :to="`/film/${encodeURIComponent(group.movie.slug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary">{{ group.movie.title }}</NuxtLink></h3>
                <p class="utility-label mt-1" :class="mediaAvailable(group.backdropUrl) ? 'text-white' : undefined">{{ group.movie.runtime_minutes }} min</p>
              </div>
            </header>
            <ul v-if="resultLayout === 'lines'" class="divide-y-2 divide-ink" :aria-label="`Séances de ${group.movie.title}`">
              <li v-for="showtime in group.showtimes" :key="showtime.id" class="grid gap-x-4 gap-y-2 p-4 hover:bg-[#f1efe8] sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:px-5">
                <div class="min-w-0">
                  <p class="text-xl font-black tabular-nums tracking-[-0.035em] text-ink">{{ formatParisTime(showtime.start_time) }} → {{ formatParisTime(showtime.end_time) }}</p>
                  <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted"><span>{{ showtime.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="showtime.format" /><template v-if="showtime.room"><span aria-hidden="true">·</span><span>{{ formatRoom(showtime.room) }}</span></template></div>
                </div>
                <BookingLink :url="showtime.booking_url" :provider="showtime.provider" :aria-label="bookingLabel(showtime)" :data-showtime-id="showtime.id" unstyled class="inline-flex min-h-11 items-center justify-end border-b-2 border-transparent font-mono text-[10px] font-black uppercase tracking-[0.1em]" available-class="text-ink hover:border-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2" unavailable-class="text-muted">
                  <template #default="{ available }">{{ available ? 'Réserver' : 'Réservation indisponible' }}</template>
                </BookingLink>
              </li>
            </ul>
            <ul v-else class="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 p-4 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4 sm:p-5" :aria-label="`Séances de ${group.movie.title}`">
              <li v-for="showtime in group.showtimes" :key="showtime.id" class="min-w-0">
                <BookingLink v-slot="{ available }" :url="showtime.booking_url" :provider="showtime.provider" :aria-label="bookingLabel(showtime)" :data-showtime-id="showtime.id" unstyled class="group flex h-full min-h-32 w-full flex-col items-start justify-between overflow-hidden border-2 p-3 text-left" available-class="border-ink bg-surface text-ink shadow-[4px_4px_0_#27272a] hover:bg-[#f1efe8]" unavailable-class="cursor-not-allowed border-dashed border-muted bg-[#e8e6de] text-muted shadow-none">
                  <div class="flex w-full items-baseline justify-between gap-2"><span class="text-2xl font-black tracking-[-0.045em]">{{ formatParisTime(showtime.start_time) }}</span><span class="font-mono text-[9px] font-bold uppercase text-muted">fin {{ formatParisTime(showtime.end_time) }}</span></div>
                  <div class="mt-5 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted"><span>{{ showtime.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="showtime.format" /><template v-if="showtime.room"><span aria-hidden="true">·</span><span>{{ formatRoom(showtime.room) }}</span></template></div>
                  <span v-if="!available" class="mt-2 text-xs font-black">Réservation indisponible</span>
                </BookingLink>
              </li>
            </ul>
          </article>
        </div>
        <div v-else-if="resultLayout === 'lines'" ref="movieGroupList" class="mt-8 divide-y-2 divide-ink border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]" aria-label="Séances par ordre chronologique">
          <article v-for="showtime in chronologicalShowtimes" :key="showtime.id" class="relative overflow-hidden p-4 hover:bg-[#f1efe8] sm:p-5">
            <img v-if="mediaAvailable(showtimeBackdropUrl(showtime))" :src="showtimeBackdropUrl(showtime)!" alt="" aria-hidden="true" :data-media-url="showtimeBackdropUrl(showtime)!" :data-movie-slug="showtime.movie.slug" data-media-kind="backdrop" class="pointer-events-none absolute inset-y-0 right-0 h-full w-1/2 object-cover opacity-[0.06]" @error="markMediaFailed(showtimeBackdropUrl(showtime)!)" />
            <div class="pointer-events-none absolute inset-0 bg-surface/80" aria-hidden="true" />
            <div class="relative grid grid-cols-[3rem_minmax(0,1fr)] gap-x-3 gap-y-2 sm:grid-cols-[3.25rem_minmax(10rem,auto)_minmax(0,1fr)_auto] sm:items-center sm:gap-4">
              <div class="row-span-2 flex aspect-[2/3] w-12 items-center justify-center overflow-hidden border-2 border-ink bg-[#e8e6de] sm:row-span-1 sm:w-[3.25rem]">
                <img v-if="mediaAvailable(showtimePosterUrl(showtime))" :src="showtimePosterUrl(showtime)!" :alt="`Affiche de ${showtime.movie.title}`" :data-media-url="showtimePosterUrl(showtime)!" :data-movie-slug="showtime.movie.slug" data-media-kind="poster" class="size-full object-cover" @error="markMediaFailed(showtimePosterUrl(showtime)!)" />
                <Film v-else :size="18" class="text-muted" aria-hidden="true" />
              </div>
              <p class="col-start-2 border-l-2 border-ink pl-3 text-xl font-black tabular-nums tracking-[-0.035em] text-ink sm:col-start-auto">{{ formatParisTime(showtime.start_time) }} → {{ formatParisTime(showtime.end_time) }}</p>
              <div class="col-start-2 min-w-0 sm:col-start-auto">
                <h3 class="truncate text-base font-black tracking-[-0.02em] text-ink"><NuxtLink :to="`/film/${encodeURIComponent(showtime.movie.slug)}`" class="underline-offset-4 hover:text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">{{ showtime.movie.title }}</NuxtLink></h3>
                <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted"><span>{{ showtime.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="showtime.format" /><template v-if="showtime.room"><span aria-hidden="true">·</span><span>{{ formatRoom(showtime.room) }}</span></template></div>
              </div>
              <BookingLink :url="showtime.booking_url" :provider="showtime.provider" :aria-label="bookingLabel(showtime)" :data-showtime-id="showtime.id" unstyled class="col-span-2 mt-1 inline-flex min-h-11 items-center justify-end border-b-2 border-transparent font-mono text-[10px] font-black uppercase tracking-[0.1em] sm:col-span-1 sm:mt-0" available-class="text-ink hover:border-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2" unavailable-class="text-muted">
                <template #default="{ available }">{{ available ? 'Réserver' : 'Réservation indisponible' }}</template>
              </BookingLink>
            </div>
          </article>
        </div>
        <ul v-else ref="movieGroupList" class="mt-8 grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4" aria-label="Séances par ordre chronologique">
          <li v-for="showtime in chronologicalShowtimes" :key="showtime.id" class="min-w-0">
            <article class="flex h-full min-h-48 min-w-0 flex-col border-2 border-ink bg-surface p-3 text-left shadow-[4px_4px_0_#27272a]">
              <div class="relative -mx-3 -mt-3 mb-3 flex h-24 items-center justify-center overflow-hidden border-b-2 border-ink bg-[#e8e6de]">
                <img v-if="mediaAvailable(showtimeBackdropUrl(showtime))" :src="showtimeBackdropUrl(showtime)!" alt="" aria-hidden="true" :data-media-url="showtimeBackdropUrl(showtime)!" :data-movie-slug="showtime.movie.slug" data-media-kind="backdrop" class="size-full object-cover" @error="markMediaFailed(showtimeBackdropUrl(showtime)!)" />
                <img v-else-if="mediaAvailable(showtimePosterUrl(showtime))" :src="showtimePosterUrl(showtime)!" alt="" aria-hidden="true" :data-media-url="showtimePosterUrl(showtime)!" :data-movie-slug="showtime.movie.slug" data-media-kind="poster" class="size-full object-contain" @error="markMediaFailed(showtimePosterUrl(showtime)!)" />
                <Film v-else :size="24" class="text-muted" aria-hidden="true" />
              </div>
              <h3 class="mb-3 line-clamp-2 text-sm font-black leading-tight tracking-[-0.02em] text-ink"><NuxtLink :to="`/film/${encodeURIComponent(showtime.movie.slug)}`" class="hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">{{ showtime.movie.title }}</NuxtLink></h3>
              <p class="text-2xl font-black tabular-nums tracking-[-0.045em] text-ink">{{ formatParisTime(showtime.start_time) }} → {{ formatParisTime(showtime.end_time) }}</p>
              <div class="mt-4 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted"><span>{{ showtime.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="showtime.format" /><template v-if="showtime.room"><span aria-hidden="true">·</span><span>{{ formatRoom(showtime.room) }}</span></template></div>
              <BookingLink :url="showtime.booking_url" :provider="showtime.provider" :aria-label="bookingLabel(showtime)" :data-showtime-id="showtime.id" unstyled class="mt-auto inline-flex min-h-11 items-end pt-3 font-mono text-[10px] font-black uppercase tracking-[0.1em]" available-class="text-ink underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2" unavailable-class="text-muted">
                <template #default="{ available }">{{ available ? 'Réserver' : 'Réservation indisponible' }}</template>
              </BookingLink>
            </article>
          </li>
        </ul>
      </section>
    </template>
  </main>
</template>

<style scoped>
.discovery-page { min-height: 70vh; background-color: #f8f7f2; background-image: linear-gradient(rgba(39,39,42,.07) 1px,transparent 1px),linear-gradient(90deg,rgba(39,39,42,.07) 1px,transparent 1px); background-size: 28px 28px; }
.discovery-state { margin-inline: auto; display: flex; min-height: 20rem; max-width: 48rem; flex-direction: column; align-items: center; justify-content: center; gap: 1rem; border: 2px solid #27272a; background: #fff; padding: 2rem; text-align: center; font-weight: 700; box-shadow: 8px 8px 0 #27272a; }
.discovery-state h1,.discovery-state h3 { font-size: 1.5rem; font-weight: 900; }
.discovery-action,.date-button { display: inline-flex; min-height: 2.75rem; align-items: center; justify-content: center; gap: .5rem; border: 2px solid #27272a; padding: .65rem .9rem; font-family: ui-monospace,monospace; font-size: .7rem; font-weight: 900; text-transform: uppercase; }
.discovery-action,.date-button--active { background: #27272a; color: #fff; }
.date-button { flex-shrink: 0; background: #fff; }
.date-button--active { background: #27272a; }
.breadcrumb,.utility-label { font-family: ui-monospace,monospace; font-size: .68rem; font-weight: 900; letter-spacing: .1em; text-transform: uppercase; }
.breadcrumb { margin-bottom: 1.5rem; color: var(--color-muted); }
.breadcrumb a:hover { color: var(--color-primary); }
</style>
