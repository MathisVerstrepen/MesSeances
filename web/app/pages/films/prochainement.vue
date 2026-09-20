<script setup lang="ts">
import {
  AlertTriangle,
  CalendarDays,
  LoaderCircle,
  RefreshCw,
} from '@lucide/vue'
import type { UpcomingMoviesResponse } from '~/types/api'
import { queriesEqual } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import {
  formatFrenchReleaseDate,
  formatReleaseWeek,
  groupUpcomingMovies,
  parseUpcomingRoute,
  upcomingApiQuery,
  upcomingRouteQuery,
} from '~/utils/upcomingMovies'
import type { UpcomingRouteState } from '~/utils/upcomingMovies'

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const pagination = ref(parseUpcomingRoute(route.query))
const catalog = ref<UpcomingMoviesResponse | null>(null)
const pending = ref(false)
const errorMessage = ref('')
let requestId = 0
let mounted = false
let scrollAfterLoad: { page: number } | null = null
let removeNavigationFailureHook: (() => void) | undefined
let removeNavigationErrorHook: (() => void) | undefined
const groups = computed(() => groupUpcomingMovies(catalog.value?.items ?? []))
const totalPages = computed(() => Math.max(1, catalog.value?.total_pages ?? 0))

async function fetchCatalog(state: UpcomingRouteState) {
  let response = await api.upcomingMovies(upcomingApiQuery(state))
  const lastPage = Math.max(1, response.total_pages)
  if (state.page > lastPage)
    response = await api.upcomingMovies(upcomingApiQuery({ page: lastPage }))
  // Validate verified dates before committing a response. Never substitute the general release date.
  groupUpcomingMovies(response.items)
  return response
}

const initial = await useAsyncData(
  `upcoming:${JSON.stringify(upcomingRouteQuery(pagination.value))}`,
  async () => {
    try {
      return { catalog: await fetchCatalog(pagination.value), errorMessage: '' }
    } catch (error) {
      return { catalog: null, errorMessage: getFrenchApiError(error) }
    }
  },
)
catalog.value = initial.data.value?.catalog ?? null
errorMessage.value = initial.data.value?.errorMessage ?? ''
if (catalog.value) pagination.value.page = catalog.value.page
if (import.meta.server && errorMessage.value) {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

async function loadCatalog() {
  const currentRequest = ++requestId
  const scrollIntent = scrollAfterLoad
  let loaded = false
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await fetchCatalog(pagination.value)
    if (currentRequest !== requestId) return
    catalog.value = response
    pagination.value.page = response.page
    if (scrollIntent && scrollAfterLoad === scrollIntent)
      scrollIntent.page = response.page
    const query = upcomingRouteQuery(pagination.value)
    if (!queriesEqual(route.query, query)) await router.replace({ query })
    loaded = true
  } catch (error) {
    if (currentRequest !== requestId) return
    catalog.value = null
    errorMessage.value = getFrenchApiError(error)
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
  // The loading panel must be gone before scrolling, otherwise the browser clamps to its height.
  await nextTick()
  if (
    loaded &&
    mounted &&
    currentRequest === requestId &&
    scrollIntent &&
    scrollAfterLoad === scrollIntent
  ) {
    scrollAfterLoad = null
    window.scrollTo({
      top: 0,
      behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches
        ? 'instant'
        : 'smooth',
    })
  }
}

function followPageLink(event: MouseEvent, nextPage: number) {
  // NuxtLink already started navigation before emitting this event. Do not push again.
  if (
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  )
    return
  if (
    pending.value ||
    scrollAfterLoad ||
    nextPage < 1 ||
    nextPage > totalPages.value ||
    nextPage === pagination.value.page
  ) {
    event.preventDefault()
    return
  }
  scrollAfterLoad = { page: nextPage }
}

watch(
  () => route.query,
  async () => {
    if (!mounted) return
    const next = parseUpcomingRoute(route.query)
    // Keep a failed pagination's intent for retry, but never carry it into history/other loads.
    if (scrollAfterLoad && scrollAfterLoad.page !== next.page)
      scrollAfterLoad = null
    const changed = !queriesEqual(
      upcomingRouteQuery(pagination.value),
      upcomingRouteQuery(next),
    )
    pagination.value = next
    const query = upcomingRouteQuery(next)
    if (!queriesEqual(route.query, query)) await router.replace({ query })
    if (changed) await loadCatalog()
  },
)
onMounted(async () => {
  removeNavigationFailureHook = router.afterEach((_to, _from, failure) => {
    if (failure) scrollAfterLoad = null
  })
  removeNavigationErrorHook = router.onError(() => {
    scrollAfterLoad = null
  })
  const query = upcomingRouteQuery(pagination.value)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
  mounted = true
})
onBeforeUnmount(() => {
  mounted = false
  scrollAfterLoad = null
  requestId++
  removeNavigationFailureHook?.()
  removeNavigationErrorHook?.()
})

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(
  config.public.siteUrl,
  '/films/prochainement',
)
const title = 'Films prochainement au cinéma - MesSeances'
const description =
  'Les prochaines sorties françaises au cinéma, semaine par semaine, pour l’année à venir.'
useSeoMeta({
  title,
  description,
  ogTitle: title,
  ogDescription: description,
  ogUrl: canonicalUrl,
  ogType: 'website',
  ogLocale: 'fr_FR',
  robots: computed(() =>
    catalog.value &&
    !errorMessage.value &&
    Object.keys(route.query).length === 0
      ? 'index,follow'
      : 'noindex,follow',
  ),
})
useHead({ link: [{ rel: 'canonical', href: canonicalUrl }] })
</script>

<template>
  <main class="min-h-screen bg-[#f8f7f2] text-ink">
    <FilmCatalogTabs active="upcoming" />
    <header class="border-b-2 border-ink bg-surface">
      <div class="mx-auto max-w-[1440px] px-4 py-12 sm:px-6 sm:py-16 lg:px-10">
        <h1
          class="text-[clamp(2.4rem,8vw,7.5rem)] font-black uppercase leading-[0.9] tracking-[-0.065em]"
        >
          Prochainement<span class="text-primary">.</span>
        </h1>
        <div
          v-if="catalog"
          class="mt-6 flex flex-wrap items-center gap-x-6 gap-y-2 font-mono text-xs font-bold uppercase tracking-wide"
        >
          <p>
            Du
            <time :datetime="catalog.window.from">{{
              formatFrenchReleaseDate(catalog.window.from)
            }}</time>
            au
            <time :datetime="catalog.window.through">{{
              formatFrenchReleaseDate(catalog.window.through)
            }}</time>
          </p>
          <p>{{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}</p>
        </div>
      </div>
    </header>
    <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 lg:px-10">
      <div aria-live="polite" :aria-busy="pending">
        <EditorialStatePanel
          v-if="pending"
          semantic="status"
          size="tall"
          class="mt-8 font-bold"
        >
          <template #icon
            ><LoaderCircle
              :size="32"
              class="animate-spin motion-reduce:animate-none"
              aria-hidden="true"
            /></template
          >
          <p>Chargement des sorties…</p>
        </EditorialStatePanel>
        <EditorialStatePanel
          v-else-if="errorMessage"
          semantic="alert"
          size="tall"
          class="mt-8 font-bold"
        >
          <template #icon
            ><AlertTriangle :size="32" aria-hidden="true" /></template
          >
          <p>{{ errorMessage }}</p>
          <template #actions
            ><button
              type="button"
              class="inline-flex min-h-11 items-center gap-2 border-2 border-ink bg-ink px-4 font-mono text-xs font-black uppercase text-white hover:bg-primary focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
              @click="loadCatalog"
            >
              <RefreshCw :size="16" aria-hidden="true" />Réessayer
            </button></template
          >
        </EditorialStatePanel>
        <EditorialStatePanel
          v-else-if="!catalog?.items.length"
          size="tall"
          class="mt-8 font-bold"
        >
          <template #icon
            ><CalendarDays :size="32" aria-hidden="true" /></template
          >
          <p>Aucune sortie annoncée</p>
        </EditorialStatePanel>
        <template v-else>
          <section
            v-for="group in groups"
            :key="group.weekStart"
            class="mt-8"
            :aria-labelledby="`week-${group.weekStart}`"
          >
            <h2
              :id="`week-${group.weekStart}`"
              class="border-b-2 border-ink pb-3 text-3xl font-black tracking-tight"
            >
              {{ formatReleaseWeek(group.weekStart) }}
            </h2>
            <ul
              class="mt-6 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6"
            >
              <li
                v-for="movie in group.movies"
                :key="movie.slug"
                class="min-w-0"
              >
                <MovieCatalogCard
                  :movie="movie"
                  :to="`/film/${encodeURIComponent(movie.slug)}`"
                >
                  <template #release
                    ><p class="mt-2 text-xs font-bold leading-relaxed">
                      Sortie le
                      <time :datetime="movie.french_release_date">{{
                        formatFrenchReleaseDate(movie.french_release_date)
                      }}</time>
                    </p></template
                  >
                </MovieCatalogCard>
              </li>
            </ul>
          </section>
          <MovieCatalogPagination
            :page="pagination.page"
            :total-pages="totalPages"
            :previous-to="pagination.page > 1 ? { query: upcomingRouteQuery({ page: pagination.page - 1 }) } : null"
            :next-to="pagination.page < totalPages ? { query: upcomingRouteQuery({ page: pagination.page + 1 }) } : null"
            :pending="pending"
            @navigate="followPageLink"
          />
        </template>
      </div>
    </div>
  </main>
</template>
