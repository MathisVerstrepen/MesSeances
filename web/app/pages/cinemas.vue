<script setup lang="ts">
import { AlertTriangle, Building2, Check, CheckCheck, List, ListFilter, LoaderCircle, LocateFixed, Map as MapIcon, RefreshCw, Search, X } from '@lucide/vue'
import type { Theater } from '~/types/api'
import { groupTheatersByCityIdentity, updateTheaterSelection } from '~/utils/cinemaSelection'
import { serializeJsonLd } from '~/utils/jsonLd'
import { mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'
import { absoluteSiteUrl } from '~/utils/siteUrl'
import { buildOpenStreetMapPositionUrl, formatPositionAccuracy, formatPositionCoordinate, formatTheaterDistance, isValidGeographicPoint, sortTheatersByDistance, type GeographicPoint } from '~/utils/theaterDistance'

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const OWNED_QUERY_KEYS = ['q'] as const

const initialDirectory = await useAsyncData('cinema-directory', async () => {
  try {
    return { kind: 'success' as const, theaters: await api.theaters(), errorMessage: '' }
  } catch (cause) {
    const theaters: Theater[] = []
    return { kind: 'upstream-error' as const, theaters, errorMessage: getFrenchApiError(cause) }
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
  initialize,
  setFavoriteTheaterIds
} = useCinemaPreferences()

type LocationStatus = 'idle' | 'requesting' | 'active' | 'failed'
type ViewMode = 'list' | 'map'

const search = ref('')
const statusMessage = ref('')
const locationStatus = ref<LocationStatus>('idle')
const locationError = ref('')
const userPosition = ref<GeographicPoint | null>(null)
const locationAccuracyMeters = ref<number | null>(null)
const viewMode = ref<ViewMode>('list')
const selectedOnly = ref(false)
const draftFavoriteTheaterIds = ref<string[]>([])
const preferencesReady = ref(false)
let isUnmounted = false

function cinemaQuery(value: string) {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, { q: value || undefined })
}

function hydrateRoute() {
  const value = singularQueryValue(route.query.q)?.trim() ?? ''
  if (search.value.trim() !== value) search.value = value
  return cinemaQuery(value)
}

function updateSearch(event: Event) {
  if (!(event.currentTarget instanceof HTMLInputElement)) return
  search.value = event.currentTarget.value
  const query = cinemaQuery(search.value.trim())
  if (!queriesEqual(route.query, query)) router.replace({ query })
}

const selectedIds = computed(() => new Set(draftFavoriteTheaterIds.value))
const normalizedSearch = computed(() => search.value.trim().toLocaleLowerCase('fr-FR'))
const searchResults = computed(() => directoryTheaters.value.filter((theater) => {
  const searchable = `${theater.name} ${theater.city}`.toLocaleLowerCase('fr-FR')
  return !normalizedSearch.value || searchable.includes(normalizedSearch.value)
}))
const displayedTheaters = computed(() => selectedOnly.value
  ? searchResults.value.filter((theater) => selectedIds.value.has(theater.id))
  : searchResults.value)
const isNearbyMode = computed(() => locationStatus.value === 'active' && userPosition.value !== null)
const usedPositionMapUrl = computed(() => userPosition.value ? buildOpenStreetMapPositionUrl(userPosition.value) : null)
const visibleGroups = computed(() => groupTheatersByCityIdentity(displayedTheaters.value))

const nearbyRows = computed(() => userPosition.value
  ? sortTheatersByDistance(displayedTheaters.value, userPosition.value)
  : [])
const visibleTheaterCount = computed(() => displayedTheaters.value.length)

function failLocation(message: string) {
  if (isUnmounted) return
  userPosition.value = null
  locationAccuracyMeters.value = null
  locationStatus.value = 'failed'
  locationError.value = message
}

function handleLocationSuccess(position: GeolocationPosition) {
  if (isUnmounted) return
  const point = {
    latitude: position.coords.latitude,
    longitude: position.coords.longitude
  }
  if (!isValidGeographicPoint(point)) {
    failLocation('Position indisponible. Vérifiez que la localisation est activée, puis réessayez.')
    return
  }
  userPosition.value = point
  locationAccuracyMeters.value = Number.isFinite(position.coords.accuracy) && position.coords.accuracy >= 0
    ? position.coords.accuracy
    : null
  locationStatus.value = 'active'
  locationError.value = ''
}

function handleLocationError(error: GeolocationPositionError) {
  if (error.code === 1) {
    failLocation('Localisation refusée. Autorisez l’accès à votre position dans les réglages du navigateur, puis réessayez.')
    return
  }
  if (error.code === 3) {
    failLocation('La localisation a pris trop de temps. Réessayez.')
    return
  }
  failLocation('Position indisponible. Vérifiez que la localisation est activée, puis réessayez.')
}

function useCurrentPosition() {
  if (locationStatus.value === 'requesting') return
  locationError.value = ''
  userPosition.value = null
  locationAccuracyMeters.value = null

  if (!import.meta.client || !navigator.geolocation) {
    failLocation('La localisation n’est pas disponible dans ce navigateur. Continuez avec la liste par ville.')
    return
  }

  locationStatus.value = 'requesting'
  try {
    navigator.geolocation.getCurrentPosition(handleLocationSuccess, handleLocationError, {
      enableHighAccuracy: false,
      timeout: 8000,
      maximumAge: 600000
    })
  } catch {
    failLocation('Position indisponible. Vérifiez que la localisation est activée, puis réessayez.')
  }
}

function showByCity() {
  userPosition.value = null
  locationAccuracyMeters.value = null
  locationStatus.value = 'idle'
  locationError.value = ''
}

function reportSaved() {
  const count = draftFavoriteTheaterIds.value.length
  statusMessage.value = `${count} cinéma${count > 1 ? 's' : ''} enregistré${count > 1 ? 's' : ''}.`
}

function applyDraftSelection(nextIds: string[]) {
  draftFavoriteTheaterIds.value = nextIds
  if (nextIds.length === 0) {
    statusMessage.value = 'Aucun cinéma sélectionné. Vos cinémas enregistrés restent inchangés.'
    return
  }

  if (!setFavoriteTheaterIds(nextIds)) {
    statusMessage.value = 'La sélection n’a pas pu être enregistrée.'
    return
  }

  draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
  reportSaved()
}

function toggleTheater(id: string) {
  const theater = directoryTheaters.value.find((item) => item.id === id)
  if (!theater) return
  applyDraftSelection(updateTheaterSelection(
    draftFavoriteTheaterIds.value,
    [theater],
    !selectedIds.value.has(id)
  ))
}

function showList() {
  viewMode.value = 'list'
}

function recoverMapBoundary(clearError: () => void) {
  showList()
  clearError()
}

function updateGroup(groupTheaters: readonly Theater[], select: boolean) {
  applyDraftSelection(updateTheaterSelection(draftFavoriteTheaterIds.value, groupTheaters, select))
}

function updateDisplayedSelection(select: boolean) {
  applyDraftSelection(updateTheaterSelection(draftFavoriteTheaterIds.value, displayedTheaters.value, select))
}

async function loadPreferences() {
  await initialize(directoryTheaters.value)
  if (!isUnmounted) draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
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

hydrateRoute()
watch(() => route.query, () => {
  const query = hydrateRoute()
  if (!queriesEqual(route.query, query)) router.replace({ query })
})
onMounted(async () => {
  const query = hydrateRoute()
  if (!queriesEqual(route.query, query)) await router.replace({ query })
  await loadPreferences()
  if (!isUnmounted) preferencesReady.value = true
})
onBeforeUnmount(() => {
  isUnmounted = true
})

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/cinemas')
const pageTitle = 'Cinémas et villes - MesSeances'
const pageDescription = 'Annuaire des cinémas et des villes disponibles sur MesSeances.'
const robots = computed(() => directoryTheaters.value.length > 0 && !directoryError.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
const cinemasJsonLd = computed(() => {
  if (directoryError.value || Object.keys(route.query).length > 0) return null
  const theaters = searchResults.value
  if (theaters.length === 0) return null
  return serializeJsonLd({
    '@context': 'https://schema.org',
    '@graph': [{
      '@type': 'ItemList',
      '@id': `${canonicalUrl}#cinema-list`,
      itemListElement: theaters.map((theater, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(config.public.siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`)
      }))
    }]
  })
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
  link: [{ rel: 'canonical', href: canonicalUrl }],
  script: cinemasJsonLd.value ? [{ key: 'cinemas-jsonld', type: 'application/ld+json', innerHTML: cinemasJsonLd.value }] : []
}))
</script>

<template>
  <main class="cinemas-page bg-[#f8f7f2] text-ink">
    <section class="border-b-2 border-ink bg-surface" aria-labelledby="cinemas-title">
      <div class="relative mx-auto max-w-[1440px] overflow-hidden px-4 pb-10 pt-12 sm:px-6 sm:pb-14 sm:pt-16 lg:px-10 lg:pb-16 lg:pt-20">
        <p class="font-mono text-[0.65rem] font-black uppercase tracking-[0.15em] text-ink">Préférences · locales</p>
        <h1 id="cinemas-title" class="mt-5 max-w-6xl [font-family:'Noto_Sans_Variable',sans-serif] text-[clamp(4rem,11vw,10rem)] font-black uppercase leading-[0.76] tracking-[-0.085em] [&>span:first-of-type]:text-transparent [&>span:first-of-type]:[-webkit-text-stroke:2px_#27272a]">
          Mes<br /><span>cinémas</span><span class="text-primary">.</span>
        </h1>
        <div class="absolute right-[15%] bottom-[14%] flex max-w-44 items-center gap-[0.65rem] border-2 border-ink bg-surface px-3 py-[0.65rem] font-mono text-[0.6rem] leading-[1.25] font-black uppercase tracking-[0.08em] shadow-[4px_4px_0_#27272a] max-sm:relative max-sm:right-auto max-sm:bottom-auto max-sm:mt-8 max-sm:max-w-52">
          <strong class="font-sans text-[1.75rem] leading-none">{{ draftFavoriteTheaterIds.length }}</strong>
          <span>cinéma{{ draftFavoriteTheaterIds.length > 1 ? 's' : '' }} sélectionné{{ draftFavoriteTheaterIds.length > 1 ? 's' : '' }}</span>
        </div>
        <span class="absolute right-[8%] bottom-[22%] aspect-square w-[clamp(2.5rem,5vw,4.75rem)] rotate-[8deg] border-2 border-ink bg-highlight shadow-[5px_5px_0_#27272a] max-sm:right-5 max-sm:bottom-6" aria-hidden="true"></span>
      </div>
    </section>

    <section class="border-b-2 border-ink bg-[#f8f7f2] bg-[linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] bg-[size:28px_28px]" aria-label="Sélection de mes cinémas">
      <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12">
        <div class="search-workspace grid gap-4 border-2 border-ink bg-[#f1efe8] p-4 shadow-[7px_7px_0_#27272a] sm:p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
          <label class="block w-full text-ink">
            <span class="block font-mono text-[0.65rem] font-black uppercase tracking-[0.15em]">Rechercher un cinéma ou une ville</span>
            <span class="mt-2 flex min-w-0">
              <span class="grid size-[3.25rem] shrink-0 place-items-center border-2 border-r-0 border-ink bg-[#ffcf3f]" aria-hidden="true">
                <Search :size="19" stroke-width="2.5" />
              </span>
              <input
                :value="search"
                type="search"
                class="h-[3.25rem] min-w-0 flex-1 rounded-none border-2 border-ink bg-surface px-[0.9rem] text-[0.95rem] font-bold text-ink outline-none focus:shadow-[inset_0_0_0_3px_var(--color-highlight)]"
                autocomplete="off"
                placeholder="Nom du cinéma ou ville"
                @input="updateSearch"
              />
            </span>
          </label>
          <button
            v-if="!isNearbyMode"
            type="button"
            class="inline-flex min-h-[3.25rem] items-center justify-center gap-2 border-2 border-ink bg-ink px-4 py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white enabled:hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="locationStatus === 'requesting'"
            :aria-busy="locationStatus === 'requesting'"
            @click="useCurrentPosition"
          >
            <LoaderCircle v-if="locationStatus === 'requesting'" :size="18" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
            <LocateFixed v-else :size="18" aria-hidden="true" />
            {{ locationStatus === 'requesting' ? 'Localisation…' : 'Utiliser ma position' }}
          </button>
          <button v-else type="button" class="inline-flex min-h-[3.25rem] items-center justify-center gap-2 border-2 border-ink bg-surface px-4 py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink hover:bg-[#e8e6de] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="showByCity">
            Afficher par ville
          </button>
        </div>

        <p v-if="locationStatus === 'requesting'" class="mt-5 font-bold" role="status" aria-live="polite">Recherche de votre position…</p>
        <p v-if="isNearbyMode" class="mt-5 text-sm font-semibold leading-relaxed" role="status" aria-live="polite">
          <a
            v-if="userPosition && usedPositionMapUrl"
            :href="usedPositionMapUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="font-bold underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
          >Position utilisée : latitude {{ formatPositionCoordinate(userPosition.latitude) }} · longitude {{ formatPositionCoordinate(userPosition.longitude) }} <span aria-hidden="true">↗</span><span class="sr-only"> (ouvre OpenStreetMap dans un nouvel onglet)</span></a>
          <span> · {{ formatPositionAccuracy(locationAccuracyMeters) }}</span>
        </p>
        <p v-if="locationError" class="mt-5 flex items-center gap-[0.65rem] border-2 border-primary bg-primary-soft px-4 py-[0.9rem] text-sm font-extrabold text-primary-hover shadow-[4px_4px_0_#991b1b]" role="alert">
          <AlertTriangle :size="19" aria-hidden="true" />
          {{ locationError }}
        </p>

        <div v-if="directoryTheaters.length > 0 && !directoryError" class="selection-toolbar mt-7 flex flex-wrap items-center justify-between gap-3 border-y-2 border-ink py-4 max-sm:items-stretch">
          <div class="view-switch inline-grid h-11 grid-cols-[repeat(2,minmax(5.5rem,1fr))] border-2 border-ink bg-surface max-sm:w-full" role="group" aria-label="Mode d’affichage des cinémas">
            <button type="button" class="inline-flex h-full min-h-0 items-center justify-center gap-[0.45rem] px-[0.9rem] py-[0.55rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink aria-pressed:bg-ink aria-pressed:text-white aria-pressed:shadow-[inset_0_-4px_0_var(--color-highlight)]" :aria-pressed="viewMode === 'list'" @click="viewMode = 'list'"><List :size="16" aria-hidden="true" /> Liste</button>
            <button type="button" class="inline-flex h-full min-h-0 items-center justify-center gap-[0.45rem] border-l-2 border-ink px-[0.9rem] py-[0.55rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink aria-pressed:bg-ink aria-pressed:text-white aria-pressed:shadow-[inset_0_-4px_0_var(--color-highlight)]" :aria-pressed="viewMode === 'map'" @click="viewMode = 'map'"><MapIcon :size="16" aria-hidden="true" /> Carte</button>
          </div>
          <div class="selection-controls inline-flex max-w-full flex-wrap items-center justify-end gap-3 max-sm:w-full">
            <button
              type="button"
              class="inline-flex h-11 min-h-11 items-center justify-center gap-[0.55rem] border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink hover:bg-highlight focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink aria-pressed:bg-highlight aria-pressed:text-ink aria-pressed:shadow-[4px_4px_0_#27272a] max-sm:w-full"
              :aria-pressed="selectedOnly"
              @click="selectedOnly = !selectedOnly"
            >
              <ListFilter :size="17" aria-hidden="true" />
              <span>Sélectionnés uniquement</span>
              <span class="grid size-5 shrink-0 place-items-center border-2 border-current bg-surface" aria-hidden="true"><Check v-if="selectedOnly" :size="14" stroke-width="3" /></span>
            </button>
            <div class="bulk-actions inline-grid h-11 max-w-full grid-cols-2 gap-[0.35rem] border-2 border-dashed border-ink bg-[#f1efe8] p-[0.15rem] max-sm:w-full" role="group" aria-label="Modifier les cinémas affichés">
              <ClientOnly>
                <button
                  type="button"
                  class="inline-flex h-full min-h-0 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink enabled:hover:bg-ink enabled:hover:text-white focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2"
                  :disabled="!preferencesReady || displayedTheaters.length === 0 || displayedTheaters.every((theater) => selectedIds.has(theater.id))"
                  @click="updateDisplayedSelection(true)"
                ><CheckCheck :size="16" aria-hidden="true" /> Tout sélectionner</button>
                <button
                  type="button"
                  class="inline-flex h-full min-h-0 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink enabled:hover:bg-ink enabled:hover:text-white focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2"
                  :disabled="!preferencesReady || displayedTheaters.length === 0 || displayedTheaters.every((theater) => !selectedIds.has(theater.id))"
                  @click="updateDisplayedSelection(false)"
                ><X :size="16" aria-hidden="true" /> Désélectionner</button>
                <template #fallback>
                  <button type="button" class="inline-flex h-full min-h-0 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2" disabled><CheckCheck :size="16" aria-hidden="true" /> Tout sélectionner</button>
                  <button type="button" class="inline-flex h-full min-h-0 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2" disabled><X :size="16" aria-hidden="true" /> Désélectionner</button>
                </template>
              </ClientOnly>
            </div>
          </div>
        </div>

        <p class="sr-only" aria-live="polite">{{ statusMessage }}</p>

        <EditorialStatePanel v-if="directoryTheaters.length === 0 && isLoading" semantic="status" live="polite" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><LoaderCircle :size="34" class="animate-spin motion-reduce:animate-none" aria-hidden="true" /></template>
          <p>Chargement des cinémas…</p>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="directoryError" semantic="alert" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template>
          <p class="max-w-lg">{{ directoryError }}</p>
          <template #actions><button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="retryDirectory"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="directoryTheaters.length === 0" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><Building2 :size="36" aria-hidden="true" /></template>
          <p>Aucun cinéma disponible.</p>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="searchResults.length === 0" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><Search :size="34" aria-hidden="true" /></template>
          <p>Aucun cinéma ne correspond à votre recherche.</p>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="selectedOnly && visibleTheaterCount === 0" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><Building2 :size="36" aria-hidden="true" /></template>
          <p>Aucun cinéma sélectionné parmi les résultats affichés.</p>
          <template #actions><button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="selectedOnly = false">Afficher tous les cinémas</button></template>
        </EditorialStatePanel>

        <div v-else class="mt-10">
          <p v-if="error" class="mb-7 flex items-center gap-[0.65rem] border-2 border-primary bg-primary-soft px-4 py-[0.9rem] text-sm font-extrabold text-primary-hover shadow-[4px_4px_0_#991b1b]" role="alert">
            <AlertTriangle :size="19" aria-hidden="true" />
            {{ error }}
          </p>
          <div class="mb-7 flex flex-col gap-2 border-b-2 border-ink py-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
            <h2 class="text-xl font-black tracking-[-0.035em] sm:text-2xl">{{ isNearbyMode ? 'Cinémas à proximité' : 'Cinémas disponibles' }}</h2>
            <p class="font-mono text-[0.65rem] font-black uppercase tracking-[0.15em]">{{ visibleTheaterCount }} cinéma{{ visibleTheaterCount > 1 ? 's' : '' }}</p>
          </div>

          <template v-if="viewMode === 'list'">
            <div v-if="isNearbyMode" class="theater-grid grid border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a] sm:grid-cols-2">
            <div
              v-for="row in nearbyRows"
              :key="row.theater.id"
              class="border-b-2 border-ink p-4 odd:border-r-2 last:border-b-0 [&:nth-last-child(2):nth-child(odd)]:border-b-0 sm:p-5 max-sm:odd:border-r-0 max-sm:[&:nth-last-child(2):nth-child(odd)]:border-b-2"
              :class="selectedIds.has(row.theater.id) ? 'bg-[#f1efe8] shadow-[inset_5px_0_0_var(--color-highlight)] [&_.theater-check]:bg-ink [&_.theater-check]:text-white [&_.theater-check]:shadow-[3px_3px_0_var(--color-highlight)]' : 'bg-surface'"
            >
              <div v-if="row.distanceKm !== null" class="mb-3 flex min-h-6 flex-wrap items-center gap-2 font-mono text-[11px] font-black uppercase">
                <span v-if="row.distanceKm !== null">{{ formatTheaterDistance(row.distanceKm) }}</span>
                <span v-if="row.isNearest" class="border-2 border-ink bg-highlight px-[0.4rem] py-[0.15rem]">Le plus proche</span>
              </div>
              <label class="group flex cursor-pointer items-start gap-4">
                <input type="checkbox" class="peer sr-only" :checked="selectedIds.has(row.theater.id)" @change="toggleTheater(row.theater.id)" />
                <span class="theater-check mt-0.5 grid size-7 shrink-0 place-items-center border-2 border-ink bg-surface peer-focus-visible:outline-3 peer-focus-visible:outline-offset-3 peer-focus-visible:outline-accent" aria-hidden="true"><Check v-if="selectedIds.has(row.theater.id)" :size="18" stroke-width="3" /></span>
                <span class="min-w-0"><BrandedText :text="row.theater.name" class="block text-base font-black leading-tight tracking-[-0.02em] text-ink sm:text-lg" /><span class="mt-2 block text-sm font-medium leading-relaxed text-ink"><template v-if="row.theater.address">{{ row.theater.address }}, </template>{{ row.theater.postal_code }} {{ row.theater.city }}</span></span>
              </label>
              <NuxtLink :to="`/cinema/${encodeURIComponent(row.theater.slug)}`" class="mt-3 inline-flex min-h-11 items-center font-mono text-[11px] font-black uppercase underline decoration-2 underline-offset-4 hover:text-primary">Voir les séances</NuxtLink>
            </div>
            </div>

            <div v-else class="space-y-8">
              <section v-for="group in visibleGroups" :key="group.citySlug" class="city-section border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
              <header class="grid gap-4 border-b-2 border-ink bg-[#f1efe8] p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:p-5">
                <div class="min-w-0">
                  <h3 class="text-2xl font-black uppercase tracking-[-0.045em] sm:text-3xl">
                    <NuxtLink :to="`/ville/${encodeURIComponent(group.citySlug)}/cinemas`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary">{{ group.city }}</NuxtLink>
                  </h3>
                  <p class="mt-1 font-mono text-[0.65rem] font-black uppercase tracking-[0.15em]">{{ group.theaters.length }} cinéma{{ group.theaters.length > 1 ? 's' : '' }}</p>
                </div>
                <div class="grid grid-cols-2 gap-2 sm:flex" role="group" :aria-label="`Modifier mes cinémas à ${group.city}`">
                  <ClientOnly>
                    <button
                      type="button"
                      class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white enabled:hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40"
                      :disabled="!preferencesReady || group.theaters.every((theater) => selectedIds.has(theater.id))"
                      @click="updateGroup(group.theaters, true)"
                    >
                      Tout sélectionner
                    </button>
                    <button
                      type="button"
                      class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink enabled:hover:bg-[#e8e6de] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40"
                      :disabled="!preferencesReady || group.theaters.every((theater) => !selectedIds.has(theater.id))"
                      @click="updateGroup(group.theaters, false)"
                    >
                      Désélectionner
                    </button>
                    <template #fallback>
                      <button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white disabled:cursor-not-allowed disabled:opacity-40" disabled>Tout sélectionner</button>
                      <button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink disabled:cursor-not-allowed disabled:opacity-40" disabled>Désélectionner</button>
                    </template>
                  </ClientOnly>
                </div>
              </header>

              <div class="theater-grid grid sm:grid-cols-2">
                <div
                  v-for="theater in group.theaters"
                  :key="theater.id"
                  class="border-b-2 border-ink p-4 odd:border-r-2 last:border-b-0 [&:nth-last-child(2):nth-child(odd)]:border-b-0 sm:p-5 max-sm:odd:border-r-0 max-sm:[&:nth-last-child(2):nth-child(odd)]:border-b-2"
                  :class="[
                    selectedIds.has(theater.id) ? 'bg-[#f1efe8] shadow-[inset_5px_0_0_var(--color-highlight)] [&_.theater-check]:bg-ink [&_.theater-check]:text-white [&_.theater-check]:shadow-[3px_3px_0_var(--color-highlight)]' : 'bg-surface',
                    group.theaters.length === 1 ? '!border-r-0 sm:col-span-2' : ''
                  ]"
                >
                  <label class="group flex cursor-pointer items-start gap-4">
                    <input type="checkbox" class="peer sr-only" :checked="selectedIds.has(theater.id)" @change="toggleTheater(theater.id)" />
                    <span class="theater-check mt-0.5 grid size-7 shrink-0 place-items-center border-2 border-ink bg-surface peer-focus-visible:outline-3 peer-focus-visible:outline-offset-3 peer-focus-visible:outline-accent" aria-hidden="true"><Check v-if="selectedIds.has(theater.id)" :size="18" stroke-width="3" /></span>
                    <span class="min-w-0"><BrandedText :text="theater.name" class="block text-base font-black leading-tight tracking-[-0.02em] text-ink sm:text-lg" /><span class="mt-2 block text-sm font-medium leading-relaxed text-ink"><template v-if="theater.address">{{ theater.address }}, </template>{{ theater.postal_code }} {{ theater.city }}</span></span>
                  </label>
                  <NuxtLink :to="`/cinema/${encodeURIComponent(theater.slug)}`" class="mt-3 inline-flex min-h-11 items-center font-mono text-[11px] font-black uppercase underline decoration-2 underline-offset-4 hover:text-primary">Voir les séances</NuxtLink>
                </div>
              </div>
              </section>
            </div>
          </template>

          <NuxtErrorBoundary v-else>
            <LazyCinemaTheaterMap
              :theaters="displayedTheaters"
              :favorite-theater-ids="draftFavoriteTheaterIds"
              :user-position="userPosition"
              @show-list="showList"
              @toggle-favorite="toggleTheater"
            />
            <template #error="{ clearError }">
              <div class="flex flex-wrap items-center justify-between gap-4 border-2 border-primary bg-primary-soft p-4 text-primary-hover shadow-[4px_4px_0_#991b1b]" role="alert">
                <strong>La carte ne peut pas être affichée.</strong>
                <button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="recoverMapBoundary(clearError)">Afficher la liste</button>
              </div>
            </template>
          </NuxtErrorBoundary>
        </div>
      </div>
    </section>
  </main>
</template>
