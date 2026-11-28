<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  ArrowRight,
  Building2,
  List,
  LoaderCircle,
  LocateFixed,
  Map as MapIcon,
  Pencil,
  RefreshCw,
  Search,
  SlidersHorizontal,
  X,
} from '@lucide/vue'
import type { CinemaChain, Theater } from '~/types/api'
import {
  buildCinemaDirectoryIndex,
  directoryFacets,
  DIRECTORY_RADIUS_OPTIONS,
  matchesDirectoryFilters,
  matchesDirectorySearch,
  theaterInPlace,
  type DirectoryPlace,
  type DirectoryRadius,
  type DirectorySuggestion,
} from '~/utils/cinemaDirectoryIndex'
import {
  activeDirectoryPlace,
  cinemaDirectoryQuery,
  directoryPlaceChanges,
  directoryScopeKey,
  isDirectoryHome,
  parseCinemaDirectoryQuery,
  type CinemaDirectoryState,
} from '~/utils/cinemaDirectoryQuery'
import { createCinemaDirectoryLocation } from '~/utils/cinemaDirectoryLocation'
import { cinemaChainLabel } from '~/utils/cinemaDirectoryChains'
import { cinemaTechnologyOptions } from '~/utils/cinemaDirectoryTechnologies'
import { updateTheaterSelection } from '~/utils/cinemaSelection'
import type { FormatBrand } from '~/utils/formats'
import { frenchDepartment, frenchRegion } from '~/utils/frenchTerritories'
import { serializeJsonLd } from '~/utils/jsonLd'
import { queriesEqual } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import {
  buildOpenStreetMapPositionUrl,
  formatPositionAccuracy,
  formatPositionCoordinate,
  formatTheaterDistance,
  sortTheatersByDistance,
} from '~/utils/theaterDistance'

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()

const initialDirectory = await useAsyncData('cinema-directory', async () => {
  try {
    return {
      kind: 'success' as const,
      theaters: await api.theaters(),
      errorMessage: '',
    }
  } catch (cause) {
    const theaters: Theater[] = []
    return {
      kind: 'upstream-error' as const,
      theaters,
      errorMessage: getFrenchApiError(cause),
    }
  }
})
const initialDirectoryState = initialDirectory.data.value
const directoryTheaters = ref<Theater[]>(initialDirectoryState?.theaters ?? [])
const directoryError = ref(initialDirectoryState?.errorMessage ?? '')
if (import.meta.server && initialDirectoryState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, 502)
}

const {
  favoriteTheaterIds,
  isLoading,
  error,
  syncError,
  writesBlocked,
  isSaving,
  selectionScopeKey,
  retrySynchronization,
  initialize,
  setFavoriteTheaterIds,
} = useCinemaPreferences()

const PAGE_SIZE = 24
const statusMessage = ref('')
const draftFavoriteTheaterIds = ref<string[]>([])
const preferencesReady = ref(false)
const pickerMode = ref<'where' | 'manage' | null>(null)
const visibleCount = ref(PAGE_SIZE)
let isUnmounted = false
let isMounted = false

const index = computed(() => buildCinemaDirectoryIndex(directoryTheaters.value))
const routeState = computed(() => parseCinemaDirectoryQuery(route.query))
const isHome = computed(() => isDirectoryHome(routeState.value))
const isNearby = computed(() => routeState.value.location === 'nearby')
const place = computed(() =>
  isNearby.value ? null : activeDirectoryPlace(routeState.value),
)

const location = createCinemaDirectoryLocation(() =>
  import.meta.client ? navigator.geolocation : undefined,
)
const { locationStatus, locationError, userPosition, locationAccuracyMeters } =
  location
const usedPositionMapUrl = computed(() =>
  userPosition.value ? buildOpenStreetMapPositionUrl(userPosition.value) : null,
)

// ---------- favorites ----------
const selectedIds = computed(() => new Set(draftFavoriteTheaterIds.value))
const favoriteTheaters = computed(() =>
  directoryTheaters.value.filter((theater) =>
    selectedIds.value.has(theater.id),
  ),
)
const selectionDisabled = computed(
  () => writesBlocked.value || !preferencesReady.value,
)

async function applyDraftSelection(nextIds: string[]): Promise<boolean> {
  if (writesBlocked.value || isUnmounted) return false
  const scope = selectionScopeKey.value
  statusMessage.value = ''
  draftFavoriteTheaterIds.value = nextIds
  const saved = await setFavoriteTheaterIds(nextIds)
  if (isUnmounted || scope !== selectionScopeKey.value) return false
  if (!saved) {
    statusMessage.value = 'La sélection n’a pas pu être enregistrée.'
    return false
  }
  draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
  return true
}

function toggleTheater(id: string) {
  const theater = directoryTheaters.value.find((item) => item.id === id)
  if (!theater) return
  void applyDraftSelection(
    updateTheaterSelection(
      draftFavoriteTheaterIds.value,
      [theater],
      !selectedIds.value.has(id),
    ),
  )
}

async function addFromPicker(ids: string[]) {
  const theaters = directoryTheaters.value.filter((theater) =>
    ids.includes(theater.id),
  )
  const saved = await applyDraftSelection(
    updateTheaterSelection(draftFavoriteTheaterIds.value, theaters, true),
  )
  if (!saved) return
  pickerMode.value = null
  statusMessage.value = `${theaters.length} cinéma${theaters.length > 1 ? 's ajoutés' : ' ajouté'} à vos cinémas.`
}

function removeFromPicker(id: string) {
  void applyDraftSelection(
    draftFavoriteTheaterIds.value.filter((value) => value !== id),
  )
}

async function loadPreferences() {
  await initialize(directoryTheaters.value)
  if (!isUnmounted)
    draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
}

async function retryDirectory() {
  directoryError.value = ''
  try {
    directoryTheaters.value = await api.theaters()
    await loadPreferences()
  } catch (cause) {
    directoryError.value = getFrenchApiError(cause)
  }
}

// ---------- navigation ----------
function directoryLocation(changes: Partial<CinemaDirectoryState>) {
  return { path: '/cinemas', query: cinemaDirectoryQuery(route.query, changes) }
}

function placeLocation(target: DirectoryPlace | null) {
  return directoryLocation(directoryPlaceChanges(target))
}

function navigate(changes: Partial<CinemaDirectoryState>) {
  const query = cinemaDirectoryQuery(route.query, changes)
  if (!queriesEqual(route.query, query)) router.push({ query })
}

function selectSuggestion(suggestion: DirectorySuggestion) {
  switch (suggestion.kind) {
    case 'arrondissement':
      return navigate(
        directoryPlaceChanges({
          kind: 'arrondissement',
          number: suggestion.number,
        }),
      )
    case 'city':
      return navigate(
        directoryPlaceChanges({ kind: 'city', slug: suggestion.slug }),
      )
    case 'department':
      return navigate(
        directoryPlaceChanges({ kind: 'department', code: suggestion.code }),
      )
    case 'region':
      return navigate(
        directoryPlaceChanges({ kind: 'region', slug: suggestion.slug }),
      )
    case 'chain':
      return navigate({
        ...directoryPlaceChanges(null),
        chains: [suggestion.chain],
        technologies: [],
      })
    case 'technology':
      return navigate({
        ...directoryPlaceChanges(null),
        chains: [],
        technologies: [suggestion.technology],
      })
    case 'theater':
      return navigateTo(
        `/cinema/${encodeURIComponent(suggestion.theater.slug)}`,
      )
    case 'search':
      return navigate({
        ...directoryPlaceChanges(null),
        search: suggestion.query,
      })
  }
}

function useCurrentPosition() {
  if (isNearby.value) location.retry()
  else navigate(directoryPlaceChanges('nearby'))
}

function toggleChain(chain: CinemaChain) {
  const chains = routeState.value.chains
  navigate({
    chains: chains.includes(chain)
      ? chains.filter((value) => value !== chain)
      : [...chains, chain],
  })
}

function toggleTechnology(technology: FormatBrand) {
  const technologies = routeState.value.technologies
  navigate({
    technologies: technologies.includes(technology)
      ? technologies.filter((value) => value !== technology)
      : [...technologies, technology],
  })
}

function setRadius(radius: DirectoryRadius) {
  navigate({ radius })
}

function setView(view: 'list' | 'map') {
  navigate({ view })
}

function recoverMapBoundary(clearError: () => void) {
  setView('list')
  clearError()
}

// ---------- results ----------
const placeTheaters = computed(() => {
  const state = routeState.value
  const current = place.value
  return directoryTheaters.value.filter(
    (theater) =>
      (!current || theaterInPlace(index.value, theater, current)) &&
      matchesDirectorySearch(index.value, theater, state.search),
  )
})

const distanceRows = computed(() =>
  isNearby.value && userPosition.value
    ? sortTheatersByDistance(placeTheaters.value, userPosition.value).filter(
        (row) => row.distanceKm !== null,
      )
    : [],
)

const scopedTheaters = computed(() => {
  if (!isNearby.value) return placeTheaters.value
  const radius = routeState.value.radius
  return distanceRows.value
    .filter((row) => (row.distanceKm ?? Number.POSITIVE_INFINITY) <= radius)
    .map((row) => row.theater)
})

const facets = computed(() =>
  directoryFacets(
    scopedTheaters.value,
    routeState.value.chains,
    routeState.value.technologies,
  ),
)

const resultTheaters = computed(() =>
  scopedTheaters.value.filter((theater) =>
    matchesDirectoryFilters(
      theater,
      routeState.value.chains,
      routeState.value.technologies,
    ),
  ),
)

const resultRows = computed(() => {
  if (isNearby.value) {
    const ids = new Set(resultTheaters.value.map((theater) => theater.id))
    const rows = distanceRows.value.filter((row) => ids.has(row.theater.id))
    return rows.map((row, position) => ({ ...row, isNearest: position === 0 }))
  }
  return [...resultTheaters.value]
    .sort(
      (left, right) =>
        left.city.localeCompare(right.city, 'fr-FR') ||
        left.name.localeCompare(right.name, 'fr-FR'),
    )
    .map((theater) => ({ theater, distanceKm: null, isNearest: false }))
})
const visibleRows = computed(() =>
  resultRows.value.slice(0, visibleCount.value),
)

const radiusOptions = computed(() => {
  const filtered = distanceRows.value.filter((row) =>
    matchesDirectoryFilters(
      row.theater,
      routeState.value.chains,
      routeState.value.technologies,
    ),
  )
  return DIRECTORY_RADIUS_OPTIONS.map((radius) => ({
    radius,
    count: filtered.filter(
      (row) => (row.distanceKm ?? Number.POSITIVE_INFINITY) <= radius,
    ).length,
  }))
})
const nearestOutside = computed(() => {
  if (!isNearby.value || resultRows.value.length > 0) return null
  const row = distanceRows.value.find((candidate) =>
    matchesDirectoryFilters(
      candidate.theater,
      routeState.value.chains,
      routeState.value.technologies,
    ),
  )
  if (!row || row.distanceKm === null) return null
  const radius =
    DIRECTORY_RADIUS_OPTIONS.find(
      (option) => option >= (row.distanceKm ?? Number.POSITIVE_INFINITY),
    ) ?? null
  return { row, radius }
})

const hasActiveFilters = computed(
  () =>
    routeState.value.chains.length > 0 ||
    routeState.value.technologies.length > 0,
)

const resultCountLabel = computed(() => {
  const count = resultRows.value.length
  const base = `${count} cinéma${count > 1 ? 's' : ''}`
  return isNearby.value
    ? `${base} à moins de ${routeState.value.radius} km`
    : base
})

// ---------- headings, breadcrumbs, drill-down ----------
type Crumb = { label: string; to?: ReturnType<typeof placeLocation> }

const heading = computed<{ title: string; crumbs: Crumb[] }>(() => {
  const state = routeState.value
  const current = place.value
  const regionCrumb = (slug: string): Crumb => ({
    label: frenchRegion(slug)?.name ?? slug,
    to: placeLocation({ kind: 'region', slug }),
  })
  const departmentCrumb = (code: string): Crumb => ({
    label: frenchDepartment(code)?.name ?? code,
    to: placeLocation({ kind: 'department', code }),
  })
  if (isNearby.value) return { title: 'Autour de vous', crumbs: [] }
  if (current?.kind === 'arrondissement') {
    const row = index.value.arrondissements.find(
      (item) => item.number === current.number,
    )
    return {
      title:
        row?.label ??
        (current.number === 1 ? 'Paris 1er' : `Paris ${current.number}e`),
      crumbs: [
        regionCrumb('ile-de-france'),
        { label: 'Paris', to: placeLocation({ kind: 'city', slug: 'paris' }) },
      ],
    }
  }
  if (current?.kind === 'city') {
    const city = index.value.citiesBySlug.get(current.slug)
    const department = city?.departmentCode
      ? frenchDepartment(city.departmentCode)
      : undefined
    return {
      title: city?.name ?? 'Ville introuvable',
      crumbs: department
        ? [regionCrumb(department.region), departmentCrumb(department.code)]
        : [],
    }
  }
  if (current?.kind === 'department') {
    const department = frenchDepartment(current.code)
    return {
      title: department
        ? `${department.name} (${department.code})`
        : current.code,
      crumbs: department ? [regionCrumb(department.region)] : [],
    }
  }
  if (current?.kind === 'region')
    return {
      title: frenchRegion(current.slug)?.name ?? current.slug,
      crumbs: [],
    }
  if (state.search) return { title: `« ${state.search} »`, crumbs: [] }
  if (state.technologies.length === 1 && state.chains.length === 0) {
    const label = cinemaTechnologyOptions.find(
      (option) => option.value === state.technologies[0],
    )?.label
    return { title: `Salles ${label ?? state.technologies[0]}`, crumbs: [] }
  }
  const [chain] = state.chains
  if (chain && state.chains.length === 1 && state.technologies.length === 0)
    return { title: cinemaChainLabel(chain), crumbs: [] }
  return { title: 'Tous les cinémas', crumbs: [] }
})

const cityPage = computed(() => {
  const current = place.value
  if (current?.kind !== 'city') return null
  const city = index.value.citiesBySlug.get(current.slug)
  return city
    ? { name: city.name, to: `/ville/${encodeURIComponent(city.slug)}/cinemas` }
    : null
})

const drill = computed(() => {
  const current = place.value
  if (current?.kind === 'region')
    return {
      title: 'Départements',
      items: index.value.departments
        .filter((department) => department.region === current.slug)
        .map((department) => ({
          key: department.code,
          label: department.name,
          count: department.count,
          to: placeLocation({ kind: 'department', code: department.code }),
        })),
    }
  if (current?.kind === 'department')
    return {
      title: 'Villes du département',
      items: index.value.cities
        .filter((city) => city.departmentCode === current.code)
        .slice(0, 24)
        .map((city) => ({
          key: city.slug,
          label: city.name,
          count: city.count,
          to: placeLocation({ kind: 'city', slug: city.slug }),
        })),
    }
  if (
    (current?.kind === 'city' && current.slug === 'paris') ||
    current?.kind === 'arrondissement'
  )
    return {
      title: 'Arrondissements',
      items: index.value.arrondissements.map((row) => ({
        key: String(row.number),
        label: row.number === 1 ? '1er' : `${row.number}e`,
        count: row.count,
        to: placeLocation({ kind: 'arrondissement', number: row.number }),
        current:
          current.kind === 'arrondissement' && current.number === row.number,
      })),
    }
  return null
})

const filterProps = computed(() => ({
  drill: drill.value,
  radiusOptions: isNearby.value ? radiusOptions.value : null,
  radius: routeState.value.radius,
  showRadiusCounts: !!userPosition.value,
  facets: facets.value,
  showChains:
    facets.value.chains.length > 1 || routeState.value.chains.length > 0,
}))
const activeFilterCount = computed(
  () => routeState.value.chains.length + routeState.value.technologies.length,
)

// ---------- mobile filter sheet ----------
const filtersOpen = ref(false)
const filtersDialog = ref<HTMLDialogElement | null>(null)
let rootOverflowBeforeFilters: string | null = null

function openFilters() {
  if (filtersOpen.value || !filtersDialog.value) return
  filtersOpen.value = true
  rootOverflowBeforeFilters = document.documentElement.style.overflow
  document.documentElement.style.overflow = 'hidden'
  filtersDialog.value.showModal()
}

function closeFilters() {
  if (!filtersOpen.value) return
  filtersOpen.value = false
  if (filtersDialog.value?.open) filtersDialog.value.close()
  if (rootOverflowBeforeFilters !== null) {
    document.documentElement.style.overflow = rootOverflowBeforeFilters
    rootOverflowBeforeFilters = null
  }
}

function clearFilters() {
  navigate({ chains: [], technologies: [] })
}

// ---------- home ----------
const majorCities = computed(() =>
  index.value.cities.filter((city) => city.count >= 3).slice(0, 14),
)
const totalLabel = computed(
  () =>
    `${directoryTheaters.value.length} cinémas · ${index.value.cities.length} villes`,
)

function countLabel(count: number) {
  return `${count} cinéma${count > 1 ? 's' : ''}`
}

// ---------- lifecycle ----------
watch(
  [favoriteTheaterIds, selectionScopeKey],
  () => {
    draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
    statusMessage.value = ''
  },
  { flush: 'sync' },
)
watch(
  isNearby,
  (nearby) => {
    if (isMounted) location.setNearbyMode(nearby)
  },
  { flush: 'sync' },
)
watch(
  () => directoryScopeKey(route.query),
  () => closeFilters(),
)
watch(
  () => route.query,
  () => {
    visibleCount.value = PAGE_SIZE
    const query = cinemaDirectoryQuery(route.query)
    if (!queriesEqual(route.query, query)) router.replace({ query })
  },
)
onMounted(async () => {
  isMounted = true
  location.setNearbyMode(isNearby.value)
  const query = cinemaDirectoryQuery(route.query)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
  await loadPreferences()
  if (!isUnmounted) preferencesReady.value = true
})
onBeforeUnmount(() => {
  isUnmounted = true
  isMounted = false
  location.dispose()
  closeFilters()
})

// ---------- SEO ----------
const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/cinemas')
const pageTitle = 'Cinémas et villes - MesSeances'
const pageDescription =
  'Trouvez un cinéma par ville, code postal, département, région, enseigne ou format, et choisissez vos cinémas sur MesSeances.'
const robots = computed(() =>
  directoryTheaters.value.length > 0 &&
  !directoryError.value &&
  Object.keys(route.query).length === 0
    ? 'index,follow'
    : 'noindex,follow',
)
const cinemasJsonLd = computed(() => {
  if (directoryError.value || Object.keys(route.query).length > 0) return null
  const theaters = directoryTheaters.value
  if (theaters.length === 0) return null
  return serializeJsonLd({
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'ItemList',
        '@id': `${canonicalUrl}#cinema-list`,
        itemListElement: theaters.map((theater, position) => ({
          '@type': 'ListItem',
          position: position + 1,
          url: absoluteSiteUrl(
            config.public.siteUrl,
            `/cinema/${encodeURIComponent(theater.slug)}`,
          ),
        })),
      },
    ],
  })
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
  link: [{ rel: 'canonical', href: canonicalUrl }],
  script: cinemasJsonLd.value
    ? [
        {
          key: 'cinemas-jsonld',
          type: 'application/ld+json',
          innerHTML: cinemasJsonLd.value,
        },
      ]
    : [],
}))
</script>

<template>
  <main class="cinemas-page bg-[#f8f7f2] text-ink">
    <section
      class="border-b-2 border-ink bg-surface"
      aria-labelledby="cinemas-title"
    >
      <div
        class="relative mx-auto max-w-[1440px] overflow-hidden px-4 pt-10 pb-8 sm:px-6 sm:pt-14 sm:pb-12 lg:px-10 lg:pt-16 lg:pb-14"
      >
        <p
          class="font-mono text-[0.65rem] font-black tracking-[0.15em] text-ink uppercase"
        >
          Annuaire<template v-if="directoryTheaters.length">
            · {{ totalLabel }}</template
          >
        </p>
        <h1
          id="cinemas-title"
          class="mt-5 max-w-6xl [font-family:'Noto_Sans_Variable',sans-serif] text-[clamp(3.25rem,9vw,8rem)] leading-[0.8] font-black tracking-[-0.085em] uppercase"
        >
          <span>cinémas</span><span class="text-primary">.</span>
        </h1>
        <span
          class="absolute right-[8%] bottom-[22%] aspect-square w-[clamp(2.5rem,5vw,4.75rem)] rotate-[8deg] border-2 border-ink bg-highlight shadow-[5px_5px_0_#27272a] max-sm:right-5 max-sm:bottom-6"
          aria-hidden="true"
        ></span>
      </div>
    </section>

    <div
      class="border-b-2 border-ink bg-[#f8f7f2] bg-[linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] bg-[size:28px_28px]"
    >
      <div
        class="mx-auto max-w-[1440px] px-4 pt-8 pb-16 sm:px-6 sm:pt-10 lg:px-10 lg:pt-12 lg:pb-24"
      >
        <CinemaDirectorySearch
          :index="directoryTheaters.length ? index : null"
          :initial-query="routeState.search"
          :locating="locationStatus === 'requesting'"
          @select="selectSuggestion"
          @nearby="useCurrentPosition"
        />

        <div class="mt-4 space-y-3" aria-live="polite">
          <p
            v-if="isSaving || statusMessage"
            class="text-sm font-bold"
            role="status"
          >
            {{ isSaving ? 'Enregistrement…' : statusMessage }}
          </p>
          <div
            v-if="error || syncError"
            class="flex flex-wrap items-center gap-3 text-sm font-bold"
            role="alert"
          >
            <AlertTriangle :size="19" aria-hidden="true" />
            <span>{{ error || syncError }}</span>
            <button
              type="button"
              class="min-h-11 border-2 border-ink px-3"
              :disabled="isSaving"
              @click="retrySynchronization"
            >
              Réessayer
            </button>
          </div>
        </div>

        <EditorialStatePanel
          v-if="directoryTheaters.length === 0 && isLoading"
          semantic="status"
          live="polite"
          size="tall"
          shadow="large"
          class="mx-auto mt-16 mb-4 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon
            ><LoaderCircle
              :size="34"
              class="animate-spin motion-reduce:animate-none"
              aria-hidden="true"
            /></template
          >
          <p>Chargement des cinémas…</p>
        </EditorialStatePanel>

        <EditorialStatePanel
          v-else-if="directoryError"
          semantic="alert"
          size="tall"
          shadow="large"
          class="mx-auto mt-16 mb-4 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon
            ><AlertTriangle
              :size="34"
              class="text-primary"
              aria-hidden="true"
            /></template
          >
          <p class="max-w-lg">{{ directoryError }}</p>
          <template #actions
            ><button
              type="button"
              class="editorial-button"
              @click="retryDirectory"
            >
              <RefreshCw :size="17" aria-hidden="true" />
              Réessayer
            </button></template
          >
        </EditorialStatePanel>

        <EditorialStatePanel
          v-else-if="directoryTheaters.length === 0"
          size="tall"
          shadow="large"
          class="mx-auto mt-16 mb-4 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon><Building2 :size="36" aria-hidden="true" /></template>
          <p>Aucun cinéma disponible.</p>
        </EditorialStatePanel>

        <!-- Accueil de l'annuaire -->
        <template v-else-if="isHome">
          <section class="mt-12" aria-labelledby="favorite-cinemas-title">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h2
                id="favorite-cinemas-title"
                class="editorial-title text-[clamp(1.75rem,4vw,2.75rem)]"
              >
                Mes cinémas<span class="text-primary">.</span>
              </h2>
              <button
                v-if="preferencesReady && favoriteTheaters.length > 0"
                type="button"
                class="inline-flex min-h-11 shrink-0 items-center font-mono text-xs font-bold tracking-wider uppercase hover:text-primary disabled:opacity-60"
                :disabled="selectionDisabled"
                @click="pickerMode = 'manage'"
              >
                <span
                  class="inline-flex items-center gap-1.5 border-b-2 border-current pb-1 sm:gap-2"
                >
                  <span
                    >Modifier<span class="hidden sm:inline">
                      ma sélection</span
                    ></span
                  >
                  <Pencil :size="14" aria-hidden="true" />
                </span>
              </button>
            </div>

            <div v-if="!preferencesReady" class="mt-5" role="status">
              <p class="text-sm font-medium text-muted">
                Chargement de vos cinémas…
              </p>
              <div class="mt-4 flex gap-4 overflow-hidden" aria-hidden="true">
                <div
                  v-for="item in 3"
                  :key="item"
                  class="h-48 w-[85%] shrink-0 rounded-[0.4rem] border-2 border-ink/25 bg-[#e8e6de] sm:h-52 sm:w-[calc((100%-1rem)/2)] lg:w-[calc((100%-2rem)/3)]"
                />
              </div>
            </div>

            <CinemaFavoriteCards
              v-else-if="favoriteTheaters.length > 0"
              :theaters="favoriteTheaters"
              label="Mes cinémas sélectionnés"
            />

            <div
              v-else
              class="mt-5 flex flex-wrap items-center justify-between gap-5 border-2 border-ink bg-accent-soft p-5 shadow-[6px_6px_0_var(--color-accent)] sm:p-6"
            >
              <div class="max-w-2xl">
                <p
                  class="font-mono text-[0.65rem] font-black tracking-[0.15em] text-accent-hover uppercase"
                >
                  Première visite
                </p>
                <p class="editorial-heading mt-2 text-2xl">
                  Choisissez vos cinémas en quelques secondes.
                </p>
                <p class="mt-2 text-[0.95rem] leading-relaxed font-semibold">
                  Indiquez où vous allez au cinéma, cochez les salles proches :
                  les films et le planning se filtrent ensuite sur votre
                  sélection.
                </p>
              </div>
              <button
                type="button"
                class="editorial-button"
                :disabled="selectionDisabled"
                @click="pickerMode = 'where'"
              >
                Choisir mes cinémas <ArrowRight :size="16" aria-hidden="true" />
              </button>
            </div>
            <p
              v-if="preferencesReady && favoriteTheaters.length > 0"
              class="mt-1"
            >
              <button
                type="button"
                class="inline-flex min-h-11 items-center gap-1.5 text-sm font-bold underline decoration-2 underline-offset-4 hover:text-primary disabled:opacity-60"
                :disabled="selectionDisabled"
                @click="pickerMode = 'where'"
              >
                Ajouter des cinémas près d’un lieu
                <ArrowRight :size="14" aria-hidden="true" />
              </button>
            </p>
          </section>

          <section class="mt-14" aria-labelledby="explore-title">
            <h2 id="explore-title" class="editorial-heading text-2xl">
              Explorer
            </h2>
            <div class="mt-4 grid gap-5 md:grid-cols-2">
              <div
                class="flex flex-col items-start justify-between gap-4 border-2 border-ink bg-surface p-5 shadow-[6px_6px_0_#27272a]"
              >
                <div>
                  <h3 class="text-lg font-black">Près de vous</h3>
                  <p class="mt-1 text-[0.95rem] leading-relaxed font-semibold">
                    Partagez votre position pour voir les salles triées par
                    distance, ou tapez votre code postal dans la recherche.
                  </p>
                </div>
                <button
                  type="button"
                  class="editorial-button"
                  @click="useCurrentPosition"
                >
                  <LocateFixed :size="17" aria-hidden="true" />
                  Utiliser ma position
                </button>
              </div>
              <div
                class="flex flex-col items-start justify-between gap-4 border-2 border-ink bg-surface p-5 shadow-[6px_6px_0_#27272a]"
              >
                <div>
                  <h3 class="text-lg font-black">Carte de France</h3>
                  <p class="mt-1 text-[0.95rem] leading-relaxed font-semibold">
                    Les {{ directoryTheaters.length }} cinémas référencés sur
                    une carte interactive, avec vos cinémas mis en évidence.
                  </p>
                </div>
                <NuxtLink
                  :to="directoryLocation({ view: 'map' })"
                  class="editorial-button"
                >
                  <MapIcon :size="17" aria-hidden="true" />
                  Voir la carte
                </NuxtLink>
              </div>
            </div>
          </section>

          <section
            v-if="majorCities.length"
            class="mt-14"
            aria-labelledby="major-cities-title"
          >
            <h2 id="major-cities-title" class="editorial-heading text-2xl">
              Grandes villes
            </h2>
            <ul class="mt-4 flex flex-wrap gap-2">
              <li v-for="city in majorCities" :key="city.slug">
                <NuxtLink
                  :to="placeLocation({ kind: 'city', slug: city.slug })"
                  class="inline-flex min-h-11 items-center gap-2 border-2 border-ink bg-surface px-3.5 text-[0.95rem] font-extrabold hover:bg-[#fff3c4]"
                >
                  {{ city.name }}
                  <span
                    class="font-mono text-[0.68rem] font-black text-muted"
                    >{{
                      city.count
                    }}</span
                  >
                </NuxtLink>
              </li>
            </ul>
          </section>

          <section class="mt-14" aria-labelledby="regions-title">
            <h2 id="regions-title" class="editorial-heading text-2xl">
              Par région
            </h2>
            <ul class="mt-4 grid gap-x-10 sm:grid-cols-2">
              <li
                v-for="region in index.regions"
                :key="region.slug"
                class="flex items-baseline justify-between gap-4 py-2"
              >
                <NuxtLink
                  :to="placeLocation({ kind: 'region', slug: region.slug })"
                  class="text-lg font-black tracking-[-0.02em] underline decoration-transparent decoration-2 underline-offset-4 hover:text-primary hover:decoration-current"
                >
                  {{ region.name }}
                </NuxtLink>
                <span class="shrink-0 text-sm font-semibold text-muted">
                  {{ countLabel(region.count) }}
                </span>
              </li>
            </ul>
          </section>
        </template>

        <!-- Résultats -->
        <section
          v-else
          id="cinema-results"
          class="mt-10"
          aria-labelledby="cinema-results-title"
        >
          <nav
            aria-label="Fil d’Ariane de l’annuaire"
            class="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm font-bold"
          >
            <NuxtLink
              to="/cinemas"
              class="inline-flex min-h-11 items-center gap-1.5 underline decoration-2 underline-offset-4 hover:text-primary"
            >
              <ArrowLeft :size="15" aria-hidden="true" />
              Tous les cinémas
            </NuxtLink>
            <template v-for="crumb in heading.crumbs" :key="crumb.label">
              <span class="text-line-hover" aria-hidden="true">/</span>
              <NuxtLink
                v-if="crumb.to"
                :to="crumb.to"
                class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary"
                >{{
                  crumb.label
                }}</NuxtLink
              >
              <span v-else class="text-muted">{{ crumb.label }}</span>
            </template>
          </nav>

          <div class="mt-3 flex flex-wrap items-end justify-between gap-4">
            <div class="min-w-0">
              <h2
                id="cinema-results-title"
                class="editorial-title text-[clamp(2rem,4.5vw,3.25rem)] [overflow-wrap:anywhere]"
              >
                {{ heading.title }}
              </h2>
              <p
                class="mt-2.5 font-mono text-[0.68rem] font-black tracking-[0.12em] uppercase"
              >
                {{ resultCountLabel }}
              </p>
              <NuxtLink
                v-if="cityPage"
                :to="cityPage.to"
                class="mt-2 inline-flex min-h-11 items-center gap-1.5 text-sm font-bold underline decoration-2 underline-offset-4 hover:text-primary"
              >
                Films à l’affiche à {{ cityPage.name }}
                <ArrowRight :size="14" aria-hidden="true" />
              </NuxtLink>
            </div>
            <div class="flex items-center gap-2">
              <div
                role="group"
                aria-label="Mode d’affichage des cinémas"
                class="inline-grid h-11 grid-cols-2 border-2 border-ink bg-surface"
              >
                <button
                  type="button"
                  class="inline-flex items-center justify-center gap-2 px-4 font-mono text-[0.68rem] font-black tracking-[0.08em] uppercase"
                  :class="routeState.view === 'list' ? 'bg-ink text-white' : 'hover:bg-highlight'"
                  :aria-pressed="routeState.view === 'list'"
                  @click="setView('list')"
                >
                  <List :size="16" aria-hidden="true" />
                  Liste
                </button>
                <button
                  type="button"
                  class="inline-flex items-center justify-center gap-2 border-l-2 border-ink px-4 font-mono text-[0.68rem] font-black tracking-[0.08em] uppercase"
                  :class="routeState.view === 'map' ? 'bg-ink text-white' : 'hover:bg-highlight'"
                  :aria-pressed="routeState.view === 'map'"
                  @click="setView('map')"
                >
                  <MapIcon :size="16" aria-hidden="true" />
                  Carte
                </button>
              </div>
              <button
                type="button"
                class="relative inline-flex h-11 items-center justify-center gap-2 border-2 border-ink bg-surface px-4 font-mono text-[0.68rem] font-black tracking-[0.08em] uppercase hover:bg-highlight aria-expanded:bg-ink aria-expanded:text-white lg:hidden"
                aria-controls="cinema-filters"
                aria-haspopup="dialog"
                :aria-expanded="filtersOpen"
                :aria-label="activeFilterCount ? `Filtres, ${activeFilterCount} actif${activeFilterCount > 1 ? 's' : ''}` : 'Filtres'"
                @click="openFilters"
              >
                <SlidersHorizontal :size="16" aria-hidden="true" />
                Filtres
                <span
                  v-if="activeFilterCount"
                  class="absolute -top-[0.6rem] -right-[0.6rem] inline-flex h-[1.4rem] min-w-[1.4rem] items-center justify-center border-2 border-ink bg-highlight px-[0.2rem] text-[0.65rem] leading-none text-ink"
                  aria-hidden="true"
                  >{{
                    activeFilterCount
                  }}</span
                >
              </button>
            </div>
          </div>

          <div v-if="isNearby" class="mt-5 space-y-3" aria-live="polite">
            <p
              v-if="locationStatus === 'requesting'"
              class="font-bold"
              role="status"
            >
              Recherche de votre position…
            </p>
            <p
              v-if="userPosition"
              class="text-sm leading-relaxed font-semibold"
              role="status"
            >
              <a
                v-if="usedPositionMapUrl"
                :href="usedPositionMapUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="font-bold underline decoration-2 underline-offset-4 hover:text-primary"
                >Position utilisée : latitude
                {{ formatPositionCoordinate(userPosition.latitude) }}
                · longitude
                {{ formatPositionCoordinate(userPosition.longitude) }}
                <span aria-hidden="true">↗</span
                ><span class="sr-only">
                  (ouvre OpenStreetMap dans un nouvel onglet)</span
                ></a
              >
              <span>
                · {{ formatPositionAccuracy(locationAccuracyMeters) }}</span
              >
            </p>
            <div
              v-if="locationError"
              class="flex flex-wrap items-center gap-3 border-2 border-primary bg-primary-soft px-4 py-3.5 text-sm font-extrabold text-primary-hover shadow-[4px_4px_0_#991b1b]"
              role="alert"
            >
              <AlertTriangle :size="19" aria-hidden="true" />
              <span class="min-w-0 flex-1">{{ locationError }}</span>
              <button
                type="button"
                class="editorial-button-outline"
                @click="location.retry()"
              >
                Réessayer
              </button>
            </div>
          </div>

          <CinemaDirectoryFilters
            variant="inline"
            class="hidden lg:block"
            v-bind="filterProps"
            @radius="setRadius"
            @technology="toggleTechnology"
            @chain="toggleChain"
          >
            <NuxtLink
              v-if="hasActiveFilters"
              :to="directoryLocation({ chains: [], technologies: [] })"
              class="inline-flex min-h-10 items-center self-center text-sm font-bold underline decoration-2 underline-offset-4 hover:text-primary"
            >
              Effacer les filtres
            </NuxtLink>
          </CinemaDirectoryFilters>

          <EditorialStatePanel
            v-if="resultRows.length === 0 && !(isNearby && !userPosition)"
            size="tall"
            shadow="large"
            class="mx-auto mt-10 mb-4 max-w-3xl font-extrabold"
          >
            <template #icon><Search :size="34" aria-hidden="true" /></template>
            <p v-if="nearestOutside">
              Aucune salle à moins de {{ routeState.radius }} km. La plus proche
              est à
              {{ formatTheaterDistance(nearestOutside.row.distanceKm) }}.
            </p>
            <p v-else>Aucun cinéma ne correspond à votre recherche.</p>
            <template #actions>
              <button
                v-if="nearestOutside?.radius"
                type="button"
                class="editorial-button"
                @click="setRadius(nearestOutside.radius)"
              >
                Élargir à {{ nearestOutside.radius }} km
              </button>
              <NuxtLink
                v-else-if="hasActiveFilters"
                :to="directoryLocation({ chains: [], technologies: [] })"
                class="editorial-button-outline"
              >
                Effacer les filtres
              </NuxtLink>
              <NuxtLink v-else to="/cinemas" class="editorial-button-outline">
                Retour à l’annuaire
              </NuxtLink>
            </template>
          </EditorialStatePanel>

          <template v-else-if="resultRows.length > 0">
            <div
              v-if="routeState.view === 'list'"
              class="mt-6 border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]"
            >
              <ul aria-labelledby="cinema-results-title">
                <CinemaDirectoryRow
                  v-for="row in visibleRows"
                  :key="row.theater.id"
                  :theater="row.theater"
                  :distance-km="row.distanceKm"
                  :is-nearest="row.isNearest"
                  :selected="selectedIds.has(row.theater.id)"
                  :disabled="selectionDisabled"
                  @toggle="toggleTheater(row.theater.id)"
                />
              </ul>
              <div
                v-if="resultRows.length > visibleRows.length"
                class="flex flex-wrap items-center justify-between gap-3 border-t-2 border-ink bg-subtle px-4 py-3.5 sm:px-5"
              >
                <span class="text-sm font-bold">
                  {{ visibleRows.length }}
                  sur {{ resultRows.length }} affichés
                </span>
                <button
                  type="button"
                  class="editorial-button-outline"
                  @click="visibleCount += PAGE_SIZE"
                >
                  Afficher
                  {{
                    Math.min(PAGE_SIZE, resultRows.length - visibleRows.length)
                  }}
                  de plus
                </button>
              </div>
            </div>

            <NuxtErrorBoundary v-else>
              <div class="mt-6">
                <LazyCinemaTheaterMap
                  :selection-disabled="selectionDisabled"
                  :theaters="resultTheaters"
                  :favorite-theater-ids="draftFavoriteTheaterIds"
                  :user-position="userPosition"
                  @show-list="setView('list')"
                  @toggle-favorite="toggleTheater"
                />
              </div>
              <template #error="{ clearError }">
                <div
                  class="mt-6 flex flex-wrap items-center justify-between gap-4 border-2 border-primary bg-primary-soft p-4 text-primary-hover shadow-[4px_4px_0_#991b1b]"
                  role="alert"
                >
                  <strong>La carte ne peut pas être affichée.</strong>
                  <button
                    type="button"
                    class="editorial-button"
                    @click="recoverMapBoundary(clearError)"
                  >
                    Afficher la liste
                  </button>
                </div>
              </template>
            </NuxtErrorBoundary>
          </template>
        </section>
      </div>
    </div>

    <dialog
      id="cinema-filters"
      ref="filtersDialog"
      aria-labelledby="cinema-filters-title"
      class="fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none overflow-hidden border-0 bg-transparent p-0 text-ink backdrop:bg-black/60 open:flex open:items-end"
      @cancel.prevent="closeFilters()"
      @close="closeFilters()"
      @click.self="closeFilters()"
    >
      <div
        class="flex max-h-[calc(100dvh-1rem)] min-h-0 w-full flex-col overflow-hidden border-2 border-b-0 border-ink bg-[#f8f7f2] shadow-[0_-8px_0_#27272a]"
      >
        <header
          class="flex shrink-0 items-center gap-3 border-b-2 border-ink bg-[#ffcf3f] px-4 py-4 sm:px-6"
        >
          <SlidersHorizontal :size="20" aria-hidden="true" />
          <h2
            id="cinema-filters-title"
            class="text-xl font-black tracking-[-0.035em]"
          >
            Filtres
          </h2>
          <button
            type="button"
            class="ml-auto grid size-11 shrink-0 place-items-center border-2 border-ink bg-surface hover:bg-highlight"
            aria-label="Fermer les filtres"
            @click="closeFilters()"
          >
            <X :size="20" aria-hidden="true" />
          </button>
        </header>
        <div
          class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-5 sm:px-6"
        >
          <CinemaDirectoryFilters
            v-if="filtersOpen"
            variant="sheet"
            v-bind="filterProps"
            @radius="setRadius"
            @technology="toggleTechnology"
            @chain="toggleChain"
            @navigate="closeFilters()"
          />
        </div>
        <footer
          class="flex shrink-0 gap-3 border-t-2 border-ink bg-[#f8f7f2] px-4 pt-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:px-6 [&>button]:flex-1"
        >
          <button
            type="button"
            class="editorial-button-outline"
            :disabled="!hasActiveFilters"
            @click="clearFilters"
          >
            Effacer
          </button>
          <button
            type="button"
            class="editorial-button"
            @click="closeFilters()"
          >
            Voir {{ countLabel(resultRows.length) }}
          </button>
        </footer>
      </div>
    </dialog>

    <CinemaPickerDialog
      :open="pickerMode"
      :index="directoryTheaters.length ? index : null"
      :selected-ids="draftFavoriteTheaterIds"
      :disabled="selectionDisabled"
      @close="pickerMode = null"
      @add="addFromPicker"
      @remove="removeFromPicker"
    />
  </main>
</template>
