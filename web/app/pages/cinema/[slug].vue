<script setup lang="ts">
import { AlertTriangle, Building2, CalendarDays, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { TheaterShowtimesResponse, TimelineShowtime } from '~/types/api'
import { formatDateLabel, formatLongDate, formatParisTime } from '~/utils/date'
import { formatLabel } from '~/utils/formats'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import { calendarDate, singularQueryValue } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const response = ref<TheaterShowtimesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
let requestId = 0

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})
const requestedDate = computed(() => calendarDate(singularQueryValue(route.query.date)))

function isNotFoundError(cause: unknown): boolean {
  return getApiErrorStatus(cause) === 404 || getApiErrorCode(cause) === 'not_found'
}

async function fetchCinema() {
  try {
    return { kind: 'success' as const, response: await api.theaterShowtimes(slug.value, requestedDate.value), errorMessage: '' }
  } catch (error) {
    if (isNotFoundError(error)) return { kind: 'not-found' as const, response: null, errorMessage: '' }
    return { kind: 'upstream-error' as const, response: null, errorMessage: getFrenchApiError(error) }
  }
}

const initial = await useAsyncData(`cinema:${slug.value}:${requestedDate.value ?? 'default'}`, fetchCinema)
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
  const defaultDate = [...(response.value?.theater.available_dates ?? [])].sort()[0]
  router.push({ query: date === defaultDate ? {} : { date } })
}

watch([slug, () => route.query], () => void loadCinema())

const movieGroups = computed(() => {
  const groups = new Map<string, { movie: TimelineShowtime['movie']; showtimes: TimelineShowtime[] }>()
  for (const showtime of response.value?.showtimes ?? []) {
    const current = groups.get(showtime.movie.slug)
    if (current) current.showtimes.push(showtime)
    else groups.set(showtime.movie.slug, { movie: showtime.movie, showtimes: [showtime] })
  }
  return [...groups.values()]
})

const config = useRuntimeConfig()
const canonicalUrl = computed(() => absoluteSiteUrl(config.public.siteUrl, `/cinema/${encodeURIComponent(slug.value)}`))
const pageTitle = computed(() => response.value ? `${response.value.theater.name} - Séances - MesSeances` : 'Cinéma - MesSeances')
const pageDescription = computed(() => response.value
  ? `Consultez les séances et films programmés au ${response.value.theater.name} à ${response.value.theater.city}.`
  : 'Consultez les séances et films programmés dans ce cinéma.')
const robots = computed(() => response.value && !pending.value && !errorMessage.value && !notFound.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
const cinemaJsonLd = computed(() => {
  const current = response.value
  if (!current || pending.value || errorMessage.value || notFound.value) return null
  const theaterUrl = canonicalUrl.value
  const theaterId = `${theaterUrl}#cinema`
  const theaterNode: JsonLdNode = { '@type': 'MovieTheater', '@id': theaterId, name: current.theater.name, url: theaterUrl }
  if (current.theater.address.trim() && current.theater.city.trim() && current.theater.postal_code.trim()) theaterNode.address = current.theater.address.trim()
  const graph: JsonLdNode[] = [theaterNode]
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
      <header class="border-2 border-ink bg-surface p-5 shadow-[8px_8px_0_#27272a] sm:p-8">
        <p class="utility-label flex items-center gap-2"><MapPin :size="16" aria-hidden="true" /> {{ response.theater.city }}</p>
        <h1 class="mt-4 break-words text-[clamp(2.75rem,8vw,7rem)] font-black uppercase leading-[0.85] tracking-[-0.07em]">{{ response.theater.name }}</h1>
        <p v-if="response.theater.address || response.theater.postal_code" class="mt-6 max-w-2xl text-base font-bold leading-7">
          <span v-if="response.theater.address">{{ response.theater.address }}<br /></span>{{ response.theater.postal_code }} {{ response.theater.city }}
        </p>
      </header>

      <section class="mt-12" aria-labelledby="cinema-showtimes-heading">
        <div class="border-b-2 border-ink pb-5">
          <p class="utility-label">Programmation</p>
          <h2 id="cinema-showtimes-heading" class="mt-2 text-4xl font-black tracking-[-0.05em] sm:text-5xl">Séances</h2>
          <p v-if="response.date" class="mt-2 font-mono text-xs font-bold uppercase capitalize text-muted">{{ formatLongDate(response.date) }}</p>
        </div>
        <div v-if="response.theater.available_dates.length" class="mt-5 flex gap-2 overflow-x-auto pb-2" aria-label="Choisir une date">
          <button v-for="date in response.theater.available_dates" :key="date" type="button" class="date-button" :class="response.date === date ? 'date-button--active' : undefined" :aria-pressed="response.date === date" @click="selectDate(date)">{{ formatDateLabel(date) }}</button>
        </div>

        <div v-if="pending" class="discovery-state mt-8" role="status" aria-live="polite"><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /><p>Chargement des séances…</p></div>
        <div v-else-if="errorMessage" class="discovery-state mt-8" role="alert"><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /><h3>Impossible de charger ces séances</h3><p>{{ errorMessage }}</p><button type="button" class="discovery-action" @click="loadCinema"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></div>
        <div v-else-if="movieGroups.length === 0" class="discovery-state mt-8"><CalendarDays :size="36" aria-hidden="true" /><h3>Aucune séance à cette date</h3><p>Choisissez une autre date pour consulter la programmation.</p></div>
        <div v-else class="mt-8 space-y-8">
          <article v-for="group in movieGroups" :key="group.movie.slug" class="border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
            <header class="border-b-2 border-ink bg-[#f1efe8] p-4 sm:p-5">
              <h3 class="text-2xl font-black tracking-[-0.04em]"><NuxtLink :to="`/film/${encodeURIComponent(group.movie.slug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary">{{ group.movie.title }}</NuxtLink></h3>
              <p class="utility-label mt-1">{{ group.movie.runtime_minutes }} min</p>
            </header>
            <ul class="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 p-4 sm:p-5">
              <li v-for="showtime in group.showtimes" :key="showtime.id" class="border-2 border-ink p-3">
                <p class="flex justify-between gap-2"><strong class="text-2xl">{{ formatParisTime(showtime.start_time) }}</strong><span class="font-mono text-[9px] font-bold uppercase text-muted">fin {{ formatParisTime(showtime.end_time) }}</span></p>
                <p class="mt-4 font-mono text-[10px] font-bold uppercase text-muted">{{ showtime.language }} · {{ formatLabel(showtime.format) }}<template v-if="showtime.room"> · {{ showtime.room }}</template></p>
              </li>
            </ul>
          </article>
        </div>
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
