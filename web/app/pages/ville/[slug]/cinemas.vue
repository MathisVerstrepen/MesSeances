<script setup lang="ts">
import { AlertTriangle, Building2, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { CityDetailResponse, MoviesResponse, MovieSort } from '~/types/api'
import { cityDescription } from '~/utils/entityDescriptions'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import { movieCatalogSortValues } from '~/utils/movieCatalogPresentation'
import { enumQueryValue, mergeOwnedQuery, positiveSafeInteger, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const PAGE_SIZE = 24
const DEFAULT_SORT: MovieSort = 'title_asc'
const OWNED_QUERY_KEYS = ['q', 'sort', 'page'] as const

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const detail = ref<CityDetailResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const posterVersion = ref(0)
const catalog = ref<MoviesResponse | null>(null)
const catalogPending = ref(true)
const catalogErrorMessage = ref('')
const appliedSearch = ref('')
const sort = ref<MovieSort>(DEFAULT_SORT)
const page = ref(1)
let cityRequestId = 0
let catalogRequestId = 0
let isMounted = false

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})

async function fetchCity() {
  try {
    return { kind: 'success' as const, detail: await api.city(slug.value), errorMessage: '' }
  } catch (error) {
    if (isNotFoundError(error)) return { kind: 'not-found' as const, detail: null, errorMessage: '' }
    return { kind: 'upstream-error' as const, detail: null, errorMessage: getFrenchApiError(error) }
  }
}

function cityCatalogQuery(search: string, nextSort: MovieSort, nextPage: number) {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    q: search || undefined,
    sort: nextSort === DEFAULT_SORT ? undefined : nextSort,
    page: nextPage === 1 ? undefined : String(nextPage)
  })
}

function hydrateCatalogRoute() {
  const search = singularQueryValue(route.query.q)?.trim() ?? ''
  const nextSort = enumQueryValue(singularQueryValue(route.query.sort), movieCatalogSortValues) ?? DEFAULT_SORT
  const nextPage = positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1
  appliedSearch.value = search
  sort.value = nextSort
  page.value = nextPage
  return cityCatalogQuery(search, nextSort, nextPage)
}

async function fetchCatalog(currentDetail: CityDetailResponse) {
  if (currentDetail.theaters.length === 0) return { catalog: null, errorMessage: '' }
  try {
    return {
      catalog: await api.movies({
        currently_screened: true,
        theaters: currentDetail.theaters.map((theater) => theater.id).join(','),
        search: appliedSearch.value || undefined,
        sort: sort.value,
        page: page.value,
        page_size: PAGE_SIZE
      }),
      errorMessage: ''
    }
  } catch (error) {
    return { catalog: null, errorMessage: getFrenchApiError(error) }
  }
}

hydrateCatalogRoute()
const initial = await useAsyncData(`city:${slug.value}:${appliedSearch.value}:${sort.value}:${page.value}`, async () => {
  const city = await fetchCity()
  if (city.kind !== 'success' || !city.detail) return { city, catalog: null, catalogErrorMessage: '' }
  const movieCatalog = await fetchCatalog(city.detail)
  return { city, catalog: movieCatalog.catalog, catalogErrorMessage: movieCatalog.errorMessage }
})
const initialState = initial.data.value
detail.value = initialState?.city.detail ?? null
notFound.value = initialState?.city.kind === 'not-found'
errorMessage.value = initialState?.city.errorMessage ?? ''
catalog.value = initialState?.catalog ?? null
catalogErrorMessage.value = initialState?.catalogErrorMessage ?? ''
pending.value = false
catalogPending.value = false
if (import.meta.server && initialState?.city.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, initialState?.city.kind === 'not-found' ? 404 : 502)
}

const totalPages = computed(() => Math.max(1, Math.ceil((catalog.value?.total ?? 0) / PAGE_SIZE)))

async function loadCatalog() {
  const currentDetail = detail.value
  const currentRequest = ++catalogRequestId
  catalogPending.value = true
  catalogErrorMessage.value = ''
  if (!currentDetail || currentDetail.theaters.length === 0) {
    catalog.value = null
    catalogPending.value = false
    return
  }
  const state = await fetchCatalog(currentDetail)
  if (currentRequest !== catalogRequestId) return
  if (state.catalog) {
    const lastPage = Math.max(1, Math.ceil(state.catalog.total / PAGE_SIZE))
    if (page.value > lastPage) {
      catalogPending.value = false
      const query = cityCatalogQuery(appliedSearch.value, sort.value, lastPage)
      if (!queriesEqual(route.query, query)) await router.replace({ query })
      return
    }
  }
  catalog.value = state.catalog
  catalogErrorMessage.value = state.errorMessage
  catalogPending.value = false
}

async function loadCity() {
  const currentRequest = ++cityRequestId
  catalogRequestId += 1
  pending.value = true
  errorMessage.value = ''
  notFound.value = false
  const state = await fetchCity()
  if (currentRequest !== cityRequestId) return
  if (state.kind === 'success') posterVersion.value += 1
  detail.value = state.detail
  notFound.value = state.kind === 'not-found'
  errorMessage.value = state.errorMessage
  pending.value = false
  catalog.value = null
  if (state.detail) await loadCatalog()
  else catalogPending.value = false
}

async function applyCatalogRoute() {
  const canonicalQuery = hydrateCatalogRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  await loadCatalog()
}

function submitSearch(search: string) {
  const query = cityCatalogQuery(search, sort.value, 1)
  if (queriesEqual(route.query, query)) {
    if (catalogErrorMessage.value) void loadCatalog()
    return
  }
  void router.replace({ query })
}

function changeSort(nextSort: MovieSort) {
  if (nextSort === sort.value) return
  void router.replace({ query: cityCatalogQuery(appliedSearch.value, nextSort, 1) })
}

function followPageLink(event: MouseEvent, nextPage: number) {
  if (catalogPending.value || nextPage < 1 || nextPage > totalPages.value || nextPage === page.value) event.preventDefault()
}

watch(slug, () => void loadCity())
watch(() => route.query, () => {
  if (isMounted) void applyCatalogRoute()
})
onMounted(async () => {
  isMounted = true
  const canonicalQuery = hydrateCatalogRoute()
  if (!queriesEqual(route.query, canonicalQuery)) await router.replace({ query: canonicalQuery })
  else if (catalog.value && page.value > totalPages.value) await router.replace({ query: cityCatalogQuery(appliedSearch.value, sort.value, totalPages.value) })
})

const config = useRuntimeConfig()
const canonicalUrl = computed(() => absoluteSiteUrl(config.public.siteUrl, `/ville/${encodeURIComponent(slug.value)}/cinemas`))
const pageTitle = computed(() => detail.value ? `Cinémas à ${detail.value.city.name} - MesSeances` : 'Cinémas par ville - MesSeances')
const pageDescription = computed(() => detail.value
  ? cityDescription(detail.value.city.name, detail.value.theaters.length, detail.value.movies.length)
  : 'Découvrez les cinémas et films actuellement programmés dans cette ville.')
const robots = computed(() => detail.value && !pending.value && !errorMessage.value && !notFound.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
const cityJsonLd = computed(() => {
  const current = detail.value
  if (!current || pending.value || errorMessage.value || notFound.value) return null
  const cityUrl = canonicalUrl.value
  const graph: JsonLdNode[] = [{
    '@type': 'BreadcrumbList',
    '@id': `${cityUrl}#breadcrumb`,
    itemListElement: [
      { '@type': 'ListItem', position: 1, name: 'Accueil', item: absoluteSiteUrl(config.public.siteUrl, '/') },
      { '@type': 'ListItem', position: 2, name: 'Cinémas', item: absoluteSiteUrl(config.public.siteUrl, '/cinemas') },
      { '@type': 'ListItem', position: 3, name: current.city.name, item: cityUrl }
    ]
  }]
  if (Object.keys(route.query).length === 0 && current.theaters.length > 0) {
    graph.push({
      '@type': 'ItemList',
      '@id': `${cityUrl}#cinema-list`,
      itemListElement: current.theaters.map((theater, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(config.public.siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`)
      }))
    })
  }
  if (Object.keys(route.query).length === 0 && current.movies.length > 0) {
    graph.push({
      '@type': 'ItemList',
      '@id': `${cityUrl}#film-list`,
      itemListElement: current.movies.map((movie, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(config.public.siteUrl, `/film/${encodeURIComponent(movie.slug)}`)
      }))
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
  script: cityJsonLd.value ? [{ key: 'city-jsonld', type: 'application/ld+json', innerHTML: cityJsonLd.value }] : []
}))
</script>

<template>
  <main class="mx-auto min-h-[70vh] max-w-[1440px] bg-[#f8f7f2] px-4 py-8 [background-image:linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] [background-size:28px_28px] sm:px-6 sm:py-10 lg:px-10 lg:py-14">
    <EditorialStatePanel v-if="pending && !detail" semantic="status" live="polite" size="standard" shadow="large" class="city-state mx-auto max-w-3xl font-bold"><template #icon><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /></template><p>Chargement de la ville…</p></EditorialStatePanel>
    <EditorialStatePanel v-else-if="notFound" semantic="alert" size="standard" shadow="large" class="city-state mx-auto max-w-3xl font-bold"><template #icon><MapPin :size="36" aria-hidden="true" /></template><template #heading><h1 class="text-2xl font-black">Ville introuvable</h1></template><p>Cette ville n’est pas disponible dans la programmation actuelle.</p><template #actions><NuxtLink to="/cinemas" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase">Voir les cinémas</NuxtLink></template></EditorialStatePanel>
    <EditorialStatePanel v-else-if="errorMessage && !detail" semantic="alert" size="standard" shadow="large" class="city-state mx-auto max-w-3xl font-bold"><template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template><template #heading><h1 class="text-2xl font-black">Impossible de charger cette ville</h1></template><p>{{ errorMessage }}</p><template #actions><button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase" @click="loadCity"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template></EditorialStatePanel>

    <template v-else-if="detail">
      <Breadcrumbs
        :items="[
          { label: 'Accueil', to: '/' },
          { label: 'Cinémas', to: '/cinemas' },
          { label: detail.city.name }
        ]"
      />
      <header class="border-2 border-ink bg-surface shadow-[8px_8px_0_#27272a]">
        <div class="grid lg:grid-cols-[minmax(0,1.55fr)_minmax(17rem,0.65fr)]">
          <div class="flex min-w-0 items-end justify-between gap-4 p-5 sm:p-8 lg:p-10">
            <div class="min-w-0">
              <p class="font-mono text-[0.68rem] font-black uppercase tracking-[0.1em]">Cinémas · ville</p>
              <h1 class="mt-4 break-words text-[clamp(2.5rem,5.5vw,5rem)] font-black uppercase leading-[0.9] tracking-[-0.065em]">{{ detail.city.name }}<span class="text-primary">.</span></h1>
            </div>
            <ShareButton class="shrink-0" />
          </div>

          <dl class="grid border-t-2 border-ink sm:grid-cols-2 lg:grid-cols-1 lg:border-l-2 lg:border-t-0">
            <div class="min-w-0 p-5 sm:p-6">
              <dt class="flex items-center gap-3 font-mono text-[0.68rem] font-black uppercase tracking-[0.1em] text-muted"><Building2 :size="20" class="shrink-0 text-primary" aria-hidden="true" /> Cinémas</dt>
              <dd class="mt-2 pl-8 text-2xl font-black leading-none">{{ detail.theaters.length }}</dd>
            </div>
            <div class="min-w-0 border-t-2 border-ink p-5 sm:border-l-2 sm:border-t-0 sm:p-6 lg:border-l-0 lg:border-t-2">
              <dt class="flex items-center gap-3 font-mono text-[0.68rem] font-black uppercase tracking-[0.1em] text-muted"><Film :size="20" class="shrink-0 text-primary" aria-hidden="true" /> Films</dt>
              <dd class="mt-2 pl-8 text-2xl font-black leading-none">{{ detail.movies.length }}</dd>
            </div>
          </dl>
        </div>

        <div class="border-t-2 border-ink bg-[#f1efe8] px-5 py-4 sm:px-8 sm:py-5 lg:px-10">
          <p class="max-w-4xl text-sm font-semibold leading-6 sm:text-base sm:leading-7">{{ pageDescription }}</p>
        </div>
      </header>

      <section class="mt-12" aria-labelledby="city-cinemas-heading">
        <div class="border-b-2 border-ink pb-5"><h2 id="city-cinemas-heading" class="text-4xl font-black tracking-[-0.05em] sm:text-5xl">Cinémas</h2></div>
        <EditorialStatePanel v-if="detail.theaters.length === 0" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><Building2 :size="36" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Aucun cinéma disponible</h3></template><p>La programmation actuelle ne contient aucun cinéma dans cette ville.</p></EditorialStatePanel>
        <ul v-else class="mt-8 grid gap-5 md:grid-cols-2">
          <li v-for="theater in detail.theaters" :key="theater.id" class="border-2 border-ink bg-surface p-5 shadow-[6px_6px_0_#27272a]">
            <h3 class="text-2xl font-black tracking-[-0.04em]"><NuxtLink :to="`/cinema/${encodeURIComponent(theater.slug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary"><BrandedText :text="theater.name" /></NuxtLink></h3>
            <p v-if="theater.address || theater.postal_code" class="mt-3 break-words text-sm font-semibold leading-6"><span v-if="theater.address">{{ theater.address }}<br /></span>{{ theater.postal_code }} {{ theater.city }}</p>
          </li>
        </ul>
      </section>

      <section class="mt-14" aria-labelledby="city-films-heading">
        <div class="border-b-2 border-ink pb-4"><h2 id="city-films-heading" class="text-4xl font-black tracking-[-0.05em] sm:text-5xl">Films à l’affiche</h2></div>
        <MovieCatalogControls v-if="detail.theaters.length" class="mt-8 border-2 border-ink bg-[#ffcf3f] p-4 shadow-[7px_7px_0_#27272a] sm:p-6" :search="appliedSearch" :sort="sort" :pending="catalogPending" input-id="city-film-search" @search="submitSearch" @sort="changeSort" />

        <EditorialStatePanel v-if="detail.theaters.length === 0" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><Film :size="36" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Aucun film disponible</h3></template><p>Aucun film n’est programmé dans cette ville sur la période actuelle.</p></EditorialStatePanel>
        <EditorialStatePanel v-else-if="catalogPending" semantic="status" live="polite" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /></template><p>Chargement des films…</p></EditorialStatePanel>
        <EditorialStatePanel v-else-if="catalogErrorMessage" semantic="alert" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template><p>{{ catalogErrorMessage }}</p><template #actions><button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase" @click="loadCatalog"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template></EditorialStatePanel>
        <EditorialStatePanel v-else-if="!catalog?.items.length" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><Film :size="36" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Aucun film disponible</h3></template><p>{{ appliedSearch ? 'Aucun film ne correspond à cette recherche.' : 'Aucun film n’est programmé dans cette ville sur la période actuelle.' }}</p><template v-if="appliedSearch" #actions><button type="button" class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase" @click="submitSearch('')">Effacer la recherche</button></template></EditorialStatePanel>
        <template v-else>
          <div class="mt-8 flex items-end justify-between gap-4 border-b-2 border-ink pb-4">
            <h3 class="text-2xl font-black tracking-[-0.04em]">{{ appliedSearch ? `Résultats pour « ${appliedSearch} »` : 'Tous les films' }}</h3>
            <p class="shrink-0 font-mono text-[11px] font-bold uppercase tracking-[0.14em]">{{ catalog.total }} film{{ catalog.total > 1 ? 's' : '' }}</p>
          </div>
          <ul class="mt-8 grid grid-cols-2 gap-x-4 gap-y-8 sm:grid-cols-3 sm:gap-x-6 lg:grid-cols-4 xl:grid-cols-6" aria-label="Films à l’affiche">
            <li v-for="movie in catalog.items" :key="movie.slug" class="min-w-0">
              <MovieCatalogCard :movie="movie" :to="`/film/${encodeURIComponent(movie.slug)}`" :poster-reset-key="posterVersion" />
            </li>
          </ul>
          <MovieCatalogPagination
            :page="page"
            :total-pages="totalPages"
            :previous-to="page > 1 ? { query: cityCatalogQuery(appliedSearch, sort, page - 1) } : null"
            :next-to="page < totalPages ? { query: cityCatalogQuery(appliedSearch, sort, page + 1) } : null"
            :pending="catalogPending"
            @navigate="followPageLink"
          />
        </template>
      </section>
    </template>
  </main>
</template>
