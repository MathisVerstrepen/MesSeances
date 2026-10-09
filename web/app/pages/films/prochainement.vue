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
  formatReleaseMonth,
  formatReleaseWeek,
  groupUpcomingMovies,
  parseUpcomingRoute,
  resolvedUpcomingRoute,
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
let scrollAfterLoad: { state: UpcomingRouteState } | null = null
let removeNavigationFailureHook: (() => void) | undefined
let removeNavigationErrorHook: (() => void) | undefined
const groups = computed(() =>
  groupUpcomingMovies(catalog.value?.items ?? [], pagination.value.view),
)
const totalPages = computed(() => Math.max(1, catalog.value?.total_pages ?? 0))
const isHistory = computed(() => pagination.value.view === 'history')
const yearOptions = computed(() =>
  [
    ...new Set([
      ...(catalog.value?.available_years ?? []),
      ...(pagination.value.year === null ? [] : [pagination.value.year]),
    ]),
  ].sort((left, right) => right - left),
)
const monthOptions = computed(() =>
  [
    ...new Set([
      ...(catalog.value?.available_months ?? []),
      ...(pagination.value.month === null ? [] : [pagination.value.month]),
    ]),
  ].sort((left, right) => left - right),
)

async function fetchCatalog(state: UpcomingRouteState) {
  let response = await api.upcomingMovies(upcomingApiQuery(state))
  const lastPage = Math.max(1, response.total_pages)
  if (state.page > lastPage)
    response = await api.upcomingMovies(
      upcomingApiQuery({ ...resolvedUpcomingRoute(response), page: lastPage }),
    )
  // Validate verified dates before committing a response. Never substitute the general release date.
  groupUpcomingMovies(response.items, response.view)
  return response
}

const initialState = { ...pagination.value }
const initial = await useAsyncData(
  `upcoming:${JSON.stringify(upcomingRouteQuery(pagination.value))}`,
  async () => {
    try {
      return { catalog: await fetchCatalog(initialState), errorMessage: '' }
    } catch (error) {
      return {
        catalog: null,
        errorMessage:
          initialState.view === 'history' &&
          getApiErrorCode(error) === 'upcoming_unavailable'
            ? 'L’historique des sorties n’est pas encore disponible. Réessayez plus tard.'
            : getFrenchApiError(error),
      }
    }
  },
)
catalog.value = initial.data.value?.catalog ?? null
errorMessage.value = initial.data.value?.errorMessage ?? ''
if (catalog.value) pagination.value = resolvedUpcomingRoute(catalog.value)
if (import.meta.server && errorMessage.value) {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

async function loadCatalog() {
  const currentRequest = ++requestId
  const state = { ...pagination.value }
  const scrollIntent = scrollAfterLoad
  let loaded = false
  pending.value = true
  catalog.value = null
  errorMessage.value = ''
  try {
    const response = await fetchCatalog(state)
    if (currentRequest !== requestId) return
    catalog.value = response
    pagination.value = resolvedUpcomingRoute(response)
    if (scrollIntent && scrollAfterLoad === scrollIntent)
      scrollIntent.state = { ...pagination.value }
    const query = upcomingRouteQuery(pagination.value)
    if (!queriesEqual(route.query, query)) await router.replace({ query })
    loaded = true
  } catch (error) {
    if (currentRequest !== requestId) return
    catalog.value = null
    errorMessage.value =
      state.view === 'history' &&
      getApiErrorCode(error) === 'upcoming_unavailable'
        ? 'L’historique des sorties n’est pas encore disponible. Réessayez plus tard.'
        : getFrenchApiError(error)
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
  scrollAfterLoad = { state: { ...pagination.value, page: nextPage } }
}

function changeYear(event: Event) {
  if (!(event.target instanceof HTMLSelectElement)) return
  const year = Number(event.target.value)
  void router.push({
    query: upcomingRouteQuery({
      ...pagination.value,
      year,
      month: null,
      page: 1,
    }),
  })
}

function changeMonth(event: Event) {
  if (!(event.target instanceof HTMLSelectElement)) return
  const value = event.target.value
  void router.push({
    query: upcomingRouteQuery({
      ...pagination.value,
      month: value ? Number(value) : null,
      page: 1,
    }),
  })
}

watch(
  () => route.query,
  async () => {
    if (!mounted) return
    const next = parseUpcomingRoute(route.query)
    // Keep a failed pagination's intent for retry, but never carry it into history/other loads.
    if (
      scrollAfterLoad &&
      !queriesEqual(
        upcomingRouteQuery(scrollAfterLoad.state),
        upcomingRouteQuery(next),
      )
    )
      scrollAfterLoad = null
    const changed = !queriesEqual(
      upcomingRouteQuery(pagination.value),
      upcomingRouteQuery(next),
    )
    pagination.value = next
    const query = upcomingRouteQuery(next)
    if (!queriesEqual(route.query, query)) void router.replace({ query })
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
const title = computed(() =>
  isHistory.value
    ? 'Films déjà sortis au cinéma - MesSeances'
    : 'Films prochainement au cinéma - MesSeances',
)
const description = computed(() =>
  isHistory.value
    ? 'Les films déjà sortis au cinéma en France, semaine par semaine.'
    : 'Les prochaines sorties françaises au cinéma, semaine par semaine, pour l’année à venir.',
)
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
    !isHistory.value &&
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
          {{ isHistory ? 'Déjà sortis' : 'Prochainement'
          }}<span class="text-primary">.</span>
        </h1>
        <div class="mt-8 flex flex-wrap items-end gap-4 sm:gap-6">
          <nav
            aria-label="Période des sorties"
            class="inline-flex border-2 border-ink font-bold"
          >
            <NuxtLink
              :to="{ query: upcomingRouteQuery({ view: 'upcoming', year: null, month: null, page: 1 }) }"
              :aria-current="!isHistory ? 'page' : undefined"
              class="inline-flex min-h-11 items-center px-4 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
              :class="!isHistory ? 'bg-ink text-white' : 'hover:bg-primary/10'"
              >À venir</NuxtLink
            >
            <NuxtLink
              :to="{ query: upcomingRouteQuery({ view: 'history', year: null, month: null, page: 1 }) }"
              :aria-current="isHistory ? 'page' : undefined"
              class="inline-flex min-h-11 items-center border-l-2 border-ink px-4 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
              :class="isHistory ? 'bg-ink text-white' : 'hover:bg-primary/10'"
              >Déjà sortis</NuxtLink
            >
          </nav>
          <template v-if="isHistory">
            <div class="flex min-w-0 flex-col gap-2 text-sm font-bold">
              <label for="release-year">Année</label>
              <select
                id="release-year"
                :value="pagination.year ?? ''"
                :disabled="pending || !catalog?.available_years.length"
                class="min-h-11 max-w-full border-2 border-ink bg-surface px-3 text-base disabled:opacity-60 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                @change="changeYear"
              >
                <option v-if="pagination.year === null" value="" selected>
                  Année
                </option>
                <option
                  v-for="year in yearOptions"
                  :key="year"
                  :value="year"
                  :selected="pagination.year === year"
                >
                  {{ year }}
                </option>
              </select>
            </div>
            <div class="flex min-w-0 flex-col gap-2 text-sm font-bold">
              <label for="release-month">Mois</label>
              <select
                id="release-month"
                :value="pagination.month ?? ''"
                :disabled="pending || pagination.year === null || !catalog?.available_months.length"
                class="min-h-11 max-w-full border-2 border-ink bg-surface px-3 text-base disabled:opacity-60 focus-visible:outline-2 focus-visible:outline-offset-3 focus-visible:outline-ink"
                @change="changeMonth"
              >
                <option value="" :selected="pagination.month === null">
                  Tous les mois
                </option>
                <option
                  v-for="month in monthOptions"
                  :key="month"
                  :value="month"
                  :selected="pagination.month === month"
                >
                  {{ formatReleaseMonth(month) }}
                </option>
              </select>
            </div>
          </template>
        </div>
        <div
          v-if="catalog"
          class="mt-6 flex flex-wrap items-center gap-x-6 gap-y-2 font-mono text-xs font-bold uppercase tracking-wide"
        >
          <p v-if="catalog.view === 'upcoming'">
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
          <p>
            {{
              isHistory ? 'Aucune sortie pour cette période' : 'Aucune sortie annoncée'
            }}
          </p>
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
            :previous-to="pagination.page > 1 ? { query: upcomingRouteQuery({ ...pagination, page: pagination.page - 1 }) } : null"
            :next-to="pagination.page < totalPages ? { query: upcomingRouteQuery({ ...pagination, page: pagination.page + 1 }) } : null"
            :pending="pending"
            @navigate="followPageLink"
          />
        </template>
      </div>
    </div>
  </main>
</template>
