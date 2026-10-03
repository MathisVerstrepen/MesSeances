<script setup lang="ts">
import {
  AlertTriangle,
  Building2,
  CalendarDays,
  ChartNoAxesCombined,
  Clapperboard,
  Film,
  LoaderCircle,
  MapPin,
  RefreshCw,
  Ticket,
} from '@lucide/vue'
import type {
  CatalogMovie,
  MovieSort,
  TheaterShowtimesResponse,
} from '~/types/api'
import type { ResultGrouping, ResultLayout } from '~/types/showtimeResults'
import { publicCinemaImageUrl } from '~/utils/cinemaImage'
import { cinemaMovieTarget } from '~/utils/cinemaMovieTarget'
import { formatLongDate, todayInParis } from '~/utils/date'
import { cinemaDescription } from '~/utils/entityDescriptions'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import {
  filterAndSortCatalogMovies,
  movieCatalogSortValues,
} from '~/utils/movieCatalogPresentation'
import {
  calendarDate,
  enumQueryValue,
  mergeOwnedQuery,
  queriesEqual,
  singularQueryValue,
} from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { hasCanonicalShowtimeEnd } from '~/utils/showtimeEnd'
import {
  groupShowtimeResults,
  resultGroupingOptions,
  resultLayoutOptions,
  sortShowtimeResults,
  toTheaterShowtimeResults,
} from '~/utils/showtimeResults'

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const followAccount = useAccountSession()
const follows = useCinemaFollows()
const followFeedback = ref('')
useAccountLifetime(() => {
  followFeedback.value = ''
})
const followNotice = computed(() =>
  followAccount.session.value?.enabled === false
    ? ''
    : followAccount.errorMessage.value ||
      follows.error.value ||
      followFeedback.value,
)
const response = ref<TheaterShowtimesResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const cinemaMovies = ref<CatalogMovie[]>([])
const moviesPending = ref(false)
const moviesErrorMessage = ref('')
let requestId = 0
let moviesRequestId = 0
let loadedMoviesTheaterId = ''
const CATALOG_PAGE_SIZE = 100
const DISPLAY_QUERY_KEYS = ['grouping', 'layout', 'view'] as const
const FILMS_DEFAULT_SORT: MovieSort = 'showtimes_desc'
const FILMS_QUERY_KEYS = ['view', 'q', 'sort'] as const

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '')
})
watch(
  [slug, follows.owner, followAccount.revision],
  () => {
    followFeedback.value = ''
  },
  { flush: 'sync' },
)
async function retryFollow() {
  followFeedback.value = ''
  await follows.retry()
}
const requestedDate = computed(() =>
  calendarDate(singularQueryValue(route.query.date)),
)
const selectedDate = computed(() => requestedDate.value ?? todayInParis())
const currentView = computed(
  () =>
    enumQueryValue(singularQueryValue(route.query.view), [
      'films',
      'activity',
    ]) ?? 'showtimes',
)
const filmSearch = computed(() =>
  currentView.value === 'films'
    ? (singularQueryValue(route.query.q)?.trim() ?? '')
    : '',
)
const filmSort = computed<MovieSort>(() =>
  currentView.value === 'films'
    ? (enumQueryValue(
        singularQueryValue(route.query.sort),
        movieCatalogSortValues,
      ) ?? FILMS_DEFAULT_SORT)
    : FILMS_DEFAULT_SORT,
)
const displayedCinemaMovies = computed(() =>
  filterAndSortCatalogMovies(
    cinemaMovies.value,
    filmSearch.value,
    filmSort.value,
  ),
)
const resultGrouping = computed<ResultGrouping>(() =>
  singularQueryValue(route.query.grouping) === 'chronological'
    ? 'chronological'
    : 'movie',
)
const resultLayout = computed<ResultLayout>(() =>
  singularQueryValue(route.query.layout) === 'boxes' ? 'boxes' : 'lines',
)
const groupingOptions = resultGroupingOptions
const layoutOptions = resultLayoutOptions
const availableDates = computed(
  () =>
    response.value?.theater.available_dates.filter(
      (date) => date >= todayInParis(),
    ) ?? [],
)

function formatCinemaCity(city: string): string {
  // Recase only all-uppercase source labels; preserve mixed casing and accents.
  if (city !== city.toLocaleUpperCase('fr-FR')) return city
  return city
    .toLocaleLowerCase('fr-FR')
    .replace(/(^|[\s'’-])\p{L}/gu, (letter) =>
      letter.toLocaleUpperCase('fr-FR'),
    )
}

function normalizeLocationPart(value: string): string {
  return value
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLocaleLowerCase('fr-FR')
    .replace(/[^a-z0-9]+/g, ' ')
    .trim()
}

const displayLocation = computed(() => {
  const theater = response.value?.theater
  if (!theater) return { address: '', locality: '' }

  const address = theater.address.trim()
  const postalCode = theater.postal_code.trim()
  const city = theater.city.trim()
  const normalizedAddress = ` ${normalizeLocationPart(address)} `
  const normalizedLocality = normalizeLocationPart(
    [postalCode, city].filter(Boolean).join(' '),
  )
  const hasFullLocality = Boolean(
    postalCode && city && normalizedAddress.includes(` ${normalizedLocality} `),
  )
  const locality = hasFullLocality
    ? ''
    : [postalCode, city].filter(Boolean).join(' ')

  return { address, locality }
})

async function fetchCinema() {
  try {
    return {
      kind: 'success' as const,
      response: await api.theaterShowtimes(slug.value, selectedDate.value),
      errorMessage: '',
    }
  } catch (error) {
    if (isNotFoundError(error))
      return { kind: 'not-found' as const, response: null, errorMessage: '' }
    return {
      kind: 'upstream-error' as const,
      response: null,
      errorMessage: getFrenchApiError(error),
    }
  }
}

const initial = await useAsyncData(
  `cinema:${slug.value}:${selectedDate.value}`,
  fetchCinema,
)
const initialState = initial.data.value
response.value = initialState?.response ?? null
notFound.value = initialState?.kind === 'not-found'
errorMessage.value = initialState?.errorMessage ?? ''
pending.value = false
if (import.meta.server && initialState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event)
    setResponseStatus(event, initialState?.kind === 'not-found' ? 404 : 502)
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
  if (state.response && currentView.value === 'films')
    void loadMovies(state.response.theater.id)
}

async function fetchMovies(theaterId: string): Promise<CatalogMovie[]> {
  const query = {
    currently_screened: true,
    theaters: theaterId,
    sort: 'showtimes_desc' as const,
    page_size: CATALOG_PAGE_SIZE,
  }
  const firstPage = await api.movies({ ...query, page: 1 })
  const pageCount = Math.ceil(firstPage.total / CATALOG_PAGE_SIZE)
  if (pageCount <= 1) return firstPage.items

  const remainingPages = await Promise.all(
    Array.from({ length: pageCount - 1 }, (_, index) =>
      api.movies({
        ...query,
        page: index + 2,
      }),
    ),
  )
  return [firstPage, ...remainingPages].flatMap((page) => page.items)
}

async function fetchMoviesState(theaterId: string) {
  try {
    return {
      kind: 'success' as const,
      movies: await fetchMovies(theaterId),
      errorMessage: '',
    }
  } catch (error) {
    return {
      kind: 'upstream-error' as const,
      movies: [],
      errorMessage: getFrenchApiError(error),
    }
  }
}

async function loadMovies(theaterId: string, force = false) {
  if (
    !force &&
    loadedMoviesTheaterId === theaterId &&
    !moviesErrorMessage.value
  )
    return
  const currentRequest = ++moviesRequestId
  moviesPending.value = true
  moviesErrorMessage.value = ''
  const state = await fetchMoviesState(theaterId)
  if (currentRequest !== moviesRequestId) return
  cinemaMovies.value = state.movies
  loadedMoviesTheaterId = state.kind === 'success' ? theaterId : ''
  moviesErrorMessage.value = state.errorMessage
  moviesPending.value = false
}

function viewQuery(view: 'showtimes' | 'films' | 'activity') {
  return mergeOwnedQuery(route.query, FILMS_QUERY_KEYS, {
    view: view === 'showtimes' ? undefined : view,
    q: undefined,
    sort: undefined,
  })
}

function cinemaFilmsQuery(search: string, nextSort: MovieSort) {
  return mergeOwnedQuery(route.query, FILMS_QUERY_KEYS, {
    view: 'films',
    q: search || undefined,
    sort: nextSort === FILMS_DEFAULT_SORT ? undefined : nextSort,
  })
}

async function applyFilmsRoute() {
  if (currentView.value !== 'films') return
  const query = cinemaFilmsQuery(filmSearch.value, filmSort.value)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
}

function submitFilmSearch(search: string) {
  const query = cinemaFilmsQuery(search, filmSort.value)
  if (!queriesEqual(route.query, query)) void router.replace({ query })
}

function changeFilmSort(sort: MovieSort) {
  const query = cinemaFilmsQuery(filmSearch.value, sort)
  if (!queriesEqual(route.query, query)) void router.replace({ query })
}

function clearFilmSearch() {
  submitFilmSearch('')
}

function selectDate(date: string) {
  router.replace({
    query: mergeOwnedQuery(route.query, ['date', ...DISPLAY_QUERY_KEYS], {
      date: date === todayInParis() ? undefined : date,
      grouping:
        resultGrouping.value === 'chronological' ? 'chronological' : undefined,
      layout: resultLayout.value === 'boxes' ? 'boxes' : undefined,
    }),
  })
}

async function setResultGrouping(grouping: string) {
  if (grouping !== 'movie' && grouping !== 'chronological') return
  if (grouping === resultGrouping.value) return
  await router.push({
    query: mergeOwnedQuery(route.query, DISPLAY_QUERY_KEYS, {
      grouping: grouping === 'chronological' ? grouping : undefined,
      layout: resultLayout.value === 'boxes' ? 'boxes' : undefined,
    }),
  })
}

async function setResultLayout(layout: string) {
  if (layout !== 'lines' && layout !== 'boxes') return
  if (layout === resultLayout.value) return
  await router.push({
    query: mergeOwnedQuery(route.query, DISPLAY_QUERY_KEYS, {
      grouping:
        resultGrouping.value === 'chronological' ? 'chronological' : undefined,
      layout: layout === 'boxes' ? layout : undefined,
    }),
  })
}

if (currentView.value === 'films' && response.value) {
  moviesPending.value = true
  const initialTheaterId = response.value.theater.id
  const initialMovies = await useAsyncData(
    `cinema-movies:${slug.value}:${initialTheaterId}`,
    () => fetchMoviesState(initialTheaterId),
  )
  const initialMoviesState = initialMovies.data.value
  cinemaMovies.value = initialMoviesState?.movies ?? []
  moviesErrorMessage.value = initialMoviesState?.errorMessage ?? ''
  loadedMoviesTheaterId =
    initialMoviesState?.kind === 'success' ? initialTheaterId : ''
  moviesPending.value = false
  if (import.meta.server && initialMoviesState?.kind === 'upstream-error') {
    const event = useRequestEvent()
    if (event) setResponseStatus(event, 502)
  }
}

watch([slug, selectedDate], ([nextSlug], [previousSlug]) => {
  if (nextSlug !== previousSlug) {
    moviesRequestId++
    cinemaMovies.value = []
    moviesErrorMessage.value = ''
    moviesPending.value = false
    loadedMoviesTheaterId = ''
  }
  void loadCinema()
})
watch(currentView, (view) => {
  const theaterId = response.value?.theater.id
  if (view === 'films' && theaterId) void loadMovies(theaterId)
})
watch(
  () => route.query,
  () => void applyFilmsRoute(),
)
onMounted(() => void applyFilmsRoute())

const normalizedResults = computed(() =>
  response.value ? toTheaterShowtimeResults(response.value) : [],
)
const movieGroups = computed(() =>
  groupShowtimeResults(sortShowtimeResults(normalizedResults.value)),
)

const config = useRuntimeConfig()
const cinemaImageUrl = computed(() =>
  publicCinemaImageUrl(
    response.value?.theater.image ?? null,
    config.public.apiBase,
  ),
)
const cinemaImage = ref<HTMLImageElement | null>(null)
const failedCinemaImageUrl = ref('')
const hasCinemaImage = computed(
  () =>
    Boolean(cinemaImageUrl.value) &&
    failedCinemaImageUrl.value !== cinemaImageUrl.value,
)

function markCinemaImageFailed(image: HTMLImageElement | null) {
  if (image?.getAttribute('src') === cinemaImageUrl.value)
    failedCinemaImageUrl.value = cinemaImageUrl.value
}

function onCinemaImageError(event: Event) {
  // SAFETY: This handler is attached only to the hero img element's error event.
  markCinemaImageFailed(event.target as HTMLImageElement | null)
}

function checkCinemaImage() {
  const image = cinemaImage.value
  if (image?.complete && image.naturalWidth === 0) markCinemaImageFailed(image)
}

watch(
  cinemaImageUrl,
  () => {
    failedCinemaImageUrl.value = ''
  },
  { flush: 'sync' },
)
watch(cinemaImage, checkCinemaImage, { flush: 'post' })
onMounted(checkCinemaImage)

const canonicalUrl = computed(() =>
  absoluteSiteUrl(
    config.public.siteUrl,
    `/cinema/${encodeURIComponent(slug.value)}`,
  ),
)
const pageTitle = computed(() =>
  response.value
    ? `${response.value.theater.name}, ${response.value.theater.city} : séances et horaires`
    : 'Cinéma - MesSeances',
)
const pageDescription = computed(() =>
  response.value
    ? cinemaDescription({
        name: response.value.theater.name,
        provider: response.value.theater.provider,
        city: response.value.theater.city,
        address: response.value.theater.address,
        postalCode: response.value.theater.postal_code,
        availableDateCount: response.value.theater.available_dates.length,
      })
    : 'Consultez les séances et films programmés dans ce cinéma.',
)
const robots = computed(() =>
  response.value &&
  !pending.value &&
  !errorMessage.value &&
  !notFound.value &&
  Object.keys(route.query).length === 0
    ? 'index,follow'
    : 'noindex,follow',
)
const cinemaJsonLd = computed(() => {
  const current = response.value
  if (!current || pending.value || errorMessage.value || notFound.value)
    return null
  const theaterUrl = canonicalUrl.value
  const theaterId = `${theaterUrl}#cinema`
  const theaterNode: JsonLdNode = {
    '@type': 'MovieTheater',
    '@id': theaterId,
    name: current.theater.name,
    url: theaterUrl,
    description: pageDescription.value,
  }
  if (
    current.theater.address.trim() &&
    current.theater.city.trim() &&
    current.theater.postal_code.trim()
  )
    theaterNode.address = current.theater.address.trim()
  const cityUrl = absoluteSiteUrl(
    config.public.siteUrl,
    `/ville/${encodeURIComponent(current.theater.city_slug)}/cinemas`,
  )
  const graph: JsonLdNode[] = [
    theaterNode,
    {
      '@type': 'BreadcrumbList',
      '@id': `${theaterUrl}#breadcrumb`,
      itemListElement: [
        {
          '@type': 'ListItem',
          position: 1,
          name: 'Accueil',
          item: absoluteSiteUrl(config.public.siteUrl, '/'),
        },
        {
          '@type': 'ListItem',
          position: 2,
          name: current.theater.city,
          item: cityUrl,
        },
        {
          '@type': 'ListItem',
          position: 3,
          name: current.theater.name,
          item: theaterUrl,
        },
      ],
    },
  ]
  const movieIds = new Map<string, string>()
  for (const group of movieGroups.value) {
    const movie = group.results[0]
    if (!movie) continue
    const movieUrl = absoluteSiteUrl(
      config.public.siteUrl,
      `/film/${encodeURIComponent(movie.movieSlug)}`,
    )
    const movieId = `${movieUrl}#movie`
    movieIds.set(movie.movieSlug, movieId)
    graph.push({
      '@type': 'Movie',
      '@id': movieId,
      name: movie.movieTitle,
      url: movieUrl,
    })
  }
  const seen = new Set<string>()
  for (const showtime of current.showtimes) {
    const id = showtime.id.trim()
    const movieId = movieIds.get(showtime.movie.slug)
    const start = Date.parse(showtime.start_time)
    const end = Date.parse(showtime.end_time)
    const unknownEnd = end === start
    if (
      !id ||
      seen.has(id) ||
      !movieId ||
      !showtime.movie.title.trim() ||
      !Number.isFinite(start) ||
      !Number.isFinite(end) ||
      (end <= start && !unknownEnd)
    )
      continue
    seen.add(id)
    const event: JsonLdNode = {
      '@type': 'ScreeningEvent',
      '@id': `${theaterUrl}#screening-${encodeURIComponent(id)}`,
      name: `${showtime.movie.title} à ${current.theater.name}`,
      startDate: showtime.start_time,
      location: { '@id': theaterId },
      workPresented: { '@id': movieId },
    }
    if (hasCanonicalShowtimeEnd(showtime.start_time, showtime.end_time))
      event.endDate = showtime.end_time
    graph.push(event)
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
  ogType: 'website',
})
useHead(() => ({
  link: [{ rel: 'canonical', href: canonicalUrl.value }],
  script: cinemaJsonLd.value
    ? [
        {
          key: 'cinema-jsonld',
          type: 'application/ld+json',
          innerHTML: cinemaJsonLd.value,
        },
      ]
    : [],
}))
</script>

<template>
  <main
    class="mx-auto min-h-[70vh] max-w-[1440px] bg-[#f8f7f2] px-4 py-8 [background-image:linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] [background-size:28px_28px] sm:px-6 sm:py-10 lg:px-10 lg:py-14"
  >
    <EditorialStatePanel
      v-if="pending && !response"
      semantic="status"
      live="polite"
      size="standard"
      shadow="large"
      class="discovery-state mx-auto max-w-3xl font-bold"
    >
      <template #icon
        ><LoaderCircle
          :size="34"
          class="animate-spin"
          aria-hidden="true"
        /></template
      >
      <p>Chargement du cinéma…</p>
    </EditorialStatePanel>
    <EditorialStatePanel
      v-else-if="notFound"
      semantic="alert"
      size="standard"
      shadow="large"
      class="discovery-state mx-auto max-w-3xl font-bold"
    >
      <template #icon><Building2 :size="36" aria-hidden="true" /></template>
      <template #heading
        ><h1 class="text-2xl font-black">Cinéma introuvable</h1></template
      >
      <p>Ce cinéma n’est pas disponible dans la programmation actuelle.</p>
      <template #actions
        ><NuxtLink
          to="/cinemas"
          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
          >Voir les cinémas</NuxtLink
        ></template
      >
    </EditorialStatePanel>
    <EditorialStatePanel
      v-else-if="errorMessage && !response"
      semantic="alert"
      size="standard"
      shadow="large"
      class="discovery-state mx-auto max-w-3xl font-bold"
    >
      <template #icon
        ><AlertTriangle
          :size="34"
          class="text-primary"
          aria-hidden="true"
        /></template
      >
      <template #heading
        ><h1 class="text-2xl font-black">
          Impossible de charger ce cinéma
        </h1></template
      >
      <p>{{ errorMessage }}</p>
      <template #actions
        ><button
          type="button"
          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
          @click="loadCinema"
        >
          <RefreshCw :size="17" aria-hidden="true" />
          Réessayer
        </button></template
      >
    </EditorialStatePanel>

    <template v-else-if="response">
      <Breadcrumbs
        :items="[
          { label: 'Accueil', to: '/' },
          { label: response.theater.city, to: `/ville/${encodeURIComponent(response.theater.city_slug)}/cinemas` },
          { label: response.theater.name }
        ]"
      />
      <header class="border-2 border-ink bg-surface shadow-[8px_8px_0_#27272a]">
        <div
          class="relative flex min-w-0 overflow-hidden p-5 lg:p-8"
          :class="hasCinemaImage
              ? 'min-h-[240px] items-end bg-[#f8f7f2] [background-image:linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] [background-size:28px_28px] lg:min-h-[380px]'
              : 'min-h-[200px] items-center bg-[#f1efe8] lg:min-h-[288px]'"
        >
          <div
            v-if="!hasCinemaImage"
            aria-hidden="true"
            class="pointer-events-none absolute -right-8 top-0 flex h-full w-48 items-center justify-center text-ink/10 sm:right-4 sm:w-64 lg:right-6 lg:w-80"
          >
            <Clapperboard
              :stroke-width="1"
              class="size-48 -rotate-12 sm:size-64 lg:size-80"
            />
            <Ticket
              :stroke-width="1.5"
              class="absolute bottom-4 right-6 size-20 rotate-12 fill-highlight/40 text-ink/10 sm:bottom-6 sm:size-24 lg:right-0"
            />
          </div>
          <img
            v-if="hasCinemaImage"
            :key="cinemaImageUrl"
            ref="cinemaImage"
            :src="cinemaImageUrl"
            :width="response.theater.image?.width"
            :height="response.theater.image?.height"
            alt=""
            aria-hidden="true"
            loading="eager"
            fetchpriority="high"
            decoding="async"
            referrerpolicy="no-referrer"
            class="absolute inset-0 h-full w-full object-cover object-[center_40%]"
            @error="onCinemaImageError"
          >
          <div
            class="relative w-full min-w-0 lg:pr-28"
            :class="hasCinemaImage ? 'text-white' : 'text-ink'"
          >
            <div
              v-if="hasCinemaImage"
              aria-hidden="true"
              class="pointer-events-none absolute -inset-x-5 -bottom-5 -top-10 bg-[linear-gradient(to_top,rgba(39,39,42,0.95)_0%,rgba(39,39,42,0.82)_calc(100%_-_4rem),transparent_100%)] lg:-inset-x-8 lg:-bottom-8"
            />
            <h1
              class="relative break-words text-[2.125rem] font-black leading-[1.05] tracking-[-0.05em] [overflow-wrap:anywhere] lg:text-[3.75rem]"
            >
              <TheaterName
                :name="response.theater.name"
                :provider="response.theater.provider"
                variant="hero"
              />
            </h1>
            <p
              v-if="response.theater.city"
              class="relative mt-2 break-words text-base font-bold [overflow-wrap:anywhere] lg:hidden"
            >
              {{ formatCinemaCity(response.theater.city) }}
            </p>
            <p
              v-if="displayLocation.address || response.theater.city"
              class="relative mt-2 hidden break-words text-base font-bold [overflow-wrap:anywhere] lg:block"
            >
              {{
                displayLocation.address
                ? [displayLocation.address, displayLocation.locality].filter(Boolean).join(' ')
                : formatCinemaCity(response.theater.city)
              }}
            </p>
          </div>
          <div
            class="absolute bottom-8 right-8 hidden items-center gap-2 lg:flex"
          >
            <CinemaFollowButton
              :theater-id="response.theater.id"
              @feedback="followFeedback = $event"
            />
            <NuxtLink
              :to="{ path: '/statistiques', query: { period: 'all', theater: [response.theater.id] } }"
              aria-label="Statistiques"
              title="Statistiques"
              class="inline-flex size-11 items-center justify-center border-2 border-ink bg-surface text-ink hover:bg-highlight focus-visible:ring-3 focus-visible:ring-surface focus-visible:outline-3 focus-visible:outline-solid focus-visible:outline-offset-3 focus-visible:outline-ink"
            >
              <ChartNoAxesCombined :size="20" aria-hidden="true" />
            </NuxtLink>
          </div>
        </div>

        <div
          class="flex items-center gap-4 border-t-2 border-ink px-4 py-3 sm:px-6 lg:hidden"
        >
          <dl
            v-if="displayLocation.address || displayLocation.locality"
            class="min-w-0 flex-1"
          >
            <div class="flex min-w-0 items-center gap-3">
              <dt class="shrink-0">
                <MapPin
                  :size="20"
                  class="shrink-0 text-primary"
                  aria-hidden="true"
                />
                <span class="sr-only">Adresse</span>
              </dt>
              <dd
                class="min-w-0 break-words text-sm font-bold leading-5 [overflow-wrap:anywhere]"
              >
                {{
                  [displayLocation.address, displayLocation.locality].filter(Boolean).join(' ')
                }}
              </dd>
            </div>
          </dl>
          <div class="ml-auto flex shrink-0 items-center gap-2">
            <CinemaFollowButton
              :theater-id="response.theater.id"
              @feedback="followFeedback = $event"
            />
            <NuxtLink
              :to="{ path: '/statistiques', query: { period: 'all', theater: [response.theater.id] } }"
              aria-label="Statistiques"
              title="Statistiques"
              class="inline-flex size-11 shrink-0 items-center justify-center border-2 border-ink bg-surface text-ink hover:bg-highlight focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
            >
              <ChartNoAxesCombined :size="20" aria-hidden="true" />
            </NuxtLink>
          </div>
        </div>
      </header>

      <div
        v-if="followNotice"
        class="mt-4 max-w-3xl border-l-2 border-primary pl-4 text-sm text-primary"
        role="alert"
      >
        <p>{{ followNotice }}</p>
        <button
          type="button"
          class="min-h-11 font-semibold underline focus-visible:outline-2 focus-visible:outline-offset-2"
          :disabled="follows.saving.value || followAccount.revalidating.value"
          @click="retryFollow"
        >
          Réessayer
        </button>
      </div>

      <section
        class="mt-6 lg:mt-8"
        :aria-labelledby="`cinema-${currentView}-heading`"
      >
        <div
          class="flex flex-col gap-5 border-b-2 border-ink pb-5 sm:flex-row sm:items-end sm:justify-between"
        >
          <div>
            <h2
              v-if="currentView === 'showtimes'"
              id="cinema-showtimes-heading"
              class="text-4xl font-black tracking-[-0.05em] sm:text-5xl"
            >
              Séances
            </h2>
            <h2
              v-else-if="currentView === 'films'"
              id="cinema-films-heading"
              class="text-4xl font-black tracking-[-0.05em] sm:text-5xl"
            >
              Films
            </h2>
            <h2
              v-else
              id="cinema-activity-heading"
              class="text-4xl font-black tracking-[-0.05em] sm:text-5xl"
            >
              Activité
            </h2>
            <p
              v-if="currentView === 'showtimes' && response.date"
              class="mt-2 font-mono text-xs font-bold uppercase capitalize text-muted"
            >
              <time :datetime="response.date">{{
                formatLongDate(response.date)
              }}</time>
            </p>
          </div>
          <div class="flex items-center gap-3 self-stretch sm:self-auto">
            <nav
              class="grid min-w-0 flex-1 grid-cols-3 border-2 border-ink bg-surface sm:flex-none"
              aria-label="Vue de la programmation"
            >
              <NuxtLink
                :to="{ query: viewQuery('showtimes') }"
                class="inline-flex min-h-11 items-center justify-center px-[0.9rem] py-[0.6rem] font-mono text-[0.7rem] font-black uppercase tracking-[0.08em] [transition:background-color_150ms_ease,color_150ms_ease] hover:bg-ink hover:text-surface focus-visible:relative focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent motion-reduce:transition-none"
                :class="currentView === 'showtimes' ? 'bg-ink text-surface' : ''"
                :aria-current="currentView === 'showtimes' ? 'page' : undefined"
              >
                Séances
              </NuxtLink>
              <NuxtLink
                :to="{ query: viewQuery('films') }"
                class="inline-flex min-h-11 items-center justify-center border-l-2 border-ink px-[0.9rem] py-[0.6rem] font-mono text-[0.7rem] font-black uppercase tracking-[0.08em] [transition:background-color_150ms_ease,color_150ms_ease] hover:bg-ink hover:text-surface focus-visible:relative focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent motion-reduce:transition-none"
                :class="currentView === 'films' ? 'bg-ink text-surface' : ''"
                :aria-current="currentView === 'films' ? 'page' : undefined"
              >
                Films
              </NuxtLink>
              <NuxtLink
                :to="{ query: viewQuery('activity') }"
                class="inline-flex min-h-11 items-center justify-center border-l-2 border-ink px-[0.9rem] py-[0.6rem] font-mono text-[0.7rem] font-black uppercase tracking-[0.08em] [transition:background-color_150ms_ease,color_150ms_ease] hover:bg-ink hover:text-surface focus-visible:relative focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent motion-reduce:transition-none"
                :class="currentView === 'activity' ? 'bg-ink text-surface' : ''"
                :aria-current="currentView === 'activity' ? 'page' : undefined"
              >
                Activité
              </NuxtLink>
            </nav>
            <ShareButton class="shrink-0" />
          </div>
        </div>

        <template v-if="currentView === 'showtimes'">
          <div
            class="mt-5 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between"
          >
            <ShowtimeDateBar
              :selected-date="selectedDate"
              :available-dates="availableDates"
              :today="todayInParis()"
              :disabled="availableDates.length === 0"
              @select="selectDate"
            />
            <div
              v-if="!pending && !errorMessage && normalizedResults.length"
              class="grid grid-cols-2 border-2 border-ink bg-surface divide-x-2 divide-ink lg:hidden"
              role="group"
              aria-label="Réglages d’affichage des séances"
            >
              <ResultSettingMenu
                id="cinema-mobile-result-grouping"
                label="Groupement"
                :current-value="resultGrouping"
                :options="groupingOptions"
                @select="setResultGrouping"
              />
              <ResultSettingMenu
                id="cinema-mobile-result-layout"
                label="Vue"
                :current-value="resultLayout"
                :options="layoutOptions"
                @select="setResultLayout"
              />
            </div>
            <div
              v-if="!pending && !errorMessage && normalizedResults.length"
              class="hidden shrink-0 items-stretch border-2 border-ink bg-surface divide-x-2 divide-ink lg:flex"
              role="group"
              aria-label="Réglages d’affichage des séances"
            >
              <ResultSettingMenu
                id="cinema-desktop-result-grouping"
                class="w-40"
                label="Groupement"
                :current-value="resultGrouping"
                :options="groupingOptions"
                @select="setResultGrouping"
              />
              <ResultSettingMenu
                id="cinema-desktop-result-layout"
                class="w-32"
                label="Vue"
                :current-value="resultLayout"
                :options="layoutOptions"
                @select="setResultLayout"
              />
            </div>
          </div>

          <EditorialStatePanel
            v-if="pending"
            semantic="status"
            live="polite"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon
              ><LoaderCircle
                :size="34"
                class="animate-spin"
                aria-hidden="true"
              /></template
            >
            <p>Chargement des séances…</p></EditorialStatePanel
          >
          <EditorialStatePanel
            v-else-if="errorMessage"
            semantic="alert"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon
              ><AlertTriangle
                :size="34"
                class="text-primary"
                aria-hidden="true"
              /></template
            ><template #heading
              ><h3 class="text-2xl font-black">
                Impossible de charger ces séances
              </h3></template
            >
            <p>{{ errorMessage }}</p>
            <template #actions
              ><button
                type="button"
                class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
                @click="loadCinema"
              >
                <RefreshCw :size="17" aria-hidden="true" />
                Réessayer
              </button></template
            ></EditorialStatePanel
          >
          <EditorialStatePanel
            v-else-if="normalizedResults.length === 0"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon
              ><CalendarDays :size="36" aria-hidden="true" /></template
            ><template #heading
              ><h3 class="text-2xl font-black">
                Aucune séance à cette date
              </h3></template
            >
            <p>
              Choisissez une autre date pour consulter la programmation.
            </p></EditorialStatePanel
          >
          <ShowtimeResults
            v-else
            :results="normalizedResults"
            :grouping="resultGrouping"
            :layout="resultLayout"
            scope="single-theater"
          />
        </template>

        <CinemaActivity
          v-else-if="currentView === 'activity'"
          :key="slug"
          :slug="slug"
        />

        <template v-else>
          <MovieCatalogControls
            v-if="cinemaMovies.length && !moviesPending && !moviesErrorMessage"
            class="mt-5 border-2 border-ink bg-[#ffcf3f] p-4 shadow-[6px_6px_0_#27272a]"
            compact
            :search="filmSearch"
            :sort="filmSort"
            :pending="moviesPending"
            input-id="cinema-film-search"
            @search="submitFilmSearch"
            @sort="changeFilmSort"
          />
          <EditorialStatePanel
            v-if="moviesPending"
            semantic="status"
            live="polite"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon
              ><LoaderCircle
                :size="34"
                class="animate-spin"
                aria-hidden="true"
              /></template
            >
            <p>Chargement des films…</p></EditorialStatePanel
          >
          <EditorialStatePanel
            v-else-if="moviesErrorMessage"
            semantic="alert"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon
              ><AlertTriangle
                :size="34"
                class="text-primary"
                aria-hidden="true"
              /></template
            ><template #heading
              ><h3 class="text-2xl font-black">
                Impossible de charger ces films
              </h3></template
            >
            <p>{{ moviesErrorMessage }}</p>
            <template #actions
              ><button
                type="button"
                class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
                @click="loadMovies(response.theater.id, true)"
              >
                <RefreshCw :size="17" aria-hidden="true" />
                Réessayer
              </button></template
            ></EditorialStatePanel
          >
          <EditorialStatePanel
            v-else-if="cinemaMovies.length === 0"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon><Film :size="36" aria-hidden="true" /></template
            ><template #heading
              ><h3 class="text-2xl font-black">
                Aucun film à l’affiche
              </h3></template
            >
            <p>
              Ce cinéma ne propose aucun film actuellement.
            </p></EditorialStatePanel
          >
          <EditorialStatePanel
            v-else-if="displayedCinemaMovies.length === 0"
            size="standard"
            shadow="large"
            class="discovery-state mx-auto mt-8 max-w-3xl font-bold"
            ><template #icon><Film :size="36" aria-hidden="true" /></template
            ><template #heading
              ><h3 class="text-2xl font-black">Aucun résultat</h3></template
            >
            <p>Aucun film ne correspond à la recherche « {{ filmSearch }} ».</p>
            <template #actions
              ><button
                type="button"
                class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
                @click="clearFilmSearch"
              >
                Effacer la recherche
              </button></template
            ></EditorialStatePanel
          >
          <template v-else>
            <p
              class="mt-5 border-y-2 border-ink py-4 text-right font-mono text-[11px] font-bold uppercase tracking-[0.14em]"
            >
              {{ displayedCinemaMovies.length }} film{{
                displayedCinemaMovies.length > 1 ? 's' : ''
              }}
            </p>
            <ul
              class="catalog-grid mt-8 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6"
              :aria-label="`Films à l’affiche au cinéma ${response.theater.name}`"
            >
              <li
                v-for="movie in displayedCinemaMovies"
                :key="movie.slug"
                class="min-w-0"
              >
                <MovieCatalogCard
                  :movie="movie"
                  :to="cinemaMovieTarget(movie.slug, response.theater.id)"
                />
              </li>
            </ul>
          </template>
        </template>
      </section>
    </template>
  </main>
</template>
