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
        <p class="utility-label text-ink">Préférences · locales</p>
        <h1 id="cinemas-title" class="cinemas-title mt-5 max-w-6xl text-[clamp(4rem,11vw,10rem)] font-black uppercase leading-[0.76] tracking-[-0.085em]">
          Mes<br /><span>cinémas</span><span class="text-primary">.</span>
        </h1>
        <div class="selection-counter">
          <strong>{{ draftFavoriteTheaterIds.length }}</strong>
          <span>cinéma{{ draftFavoriteTheaterIds.length > 1 ? 's' : '' }} sélectionné{{ draftFavoriteTheaterIds.length > 1 ? 's' : '' }}</span>
        </div>
        <span class="title-mark" aria-hidden="true"></span>
      </div>
    </section>

    <section class="cinemas-canvas border-b-2 border-ink" aria-label="Sélection des cinémas favoris">
      <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12">
        <div class="search-workspace grid gap-4 border-2 border-ink bg-[#f1efe8] p-4 shadow-[7px_7px_0_#27272a] sm:p-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
          <label class="block w-full text-ink">
            <span class="utility-label block">Rechercher un cinéma ou une ville</span>
            <span class="mt-2 flex min-w-0">
              <span class="grid size-[3.25rem] shrink-0 place-items-center border-2 border-r-0 border-ink bg-[#ffcf3f]" aria-hidden="true">
                <Search :size="19" stroke-width="2.5" />
              </span>
              <input
                :value="search"
                type="search"
                class="cinema-search min-w-0 flex-1"
                autocomplete="off"
                placeholder="Nom du cinéma ou ville"
                @input="updateSearch"
              />
            </span>
          </label>
          <button
            v-if="!isNearbyMode"
            type="button"
            class="location-action"
            :disabled="locationStatus === 'requesting'"
            :aria-busy="locationStatus === 'requesting'"
            @click="useCurrentPosition"
          >
            <LoaderCircle v-if="locationStatus === 'requesting'" :size="18" class="cinema-spinner animate-spin" aria-hidden="true" />
            <LocateFixed v-else :size="18" aria-hidden="true" />
            {{ locationStatus === 'requesting' ? 'Localisation…' : 'Utiliser ma position' }}
          </button>
          <button v-else type="button" class="location-action location-action--secondary" @click="showByCity">
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
        <p v-if="locationError" class="validation-alert mt-5" role="alert">
          <AlertTriangle :size="19" aria-hidden="true" />
          {{ locationError }}
        </p>

        <div v-if="directoryTheaters.length > 0 && !directoryError" class="selection-toolbar mt-7">
          <div class="view-switch" role="group" aria-label="Mode d’affichage des cinémas">
            <button type="button" :aria-pressed="viewMode === 'list'" @click="viewMode = 'list'"><List :size="16" aria-hidden="true" /> Liste</button>
            <button type="button" :aria-pressed="viewMode === 'map'" @click="viewMode = 'map'"><MapIcon :size="16" aria-hidden="true" /> Carte</button>
          </div>
          <div class="selection-controls">
            <button
              type="button"
              class="filter-action"
              :aria-pressed="selectedOnly"
              @click="selectedOnly = !selectedOnly"
            >
              <ListFilter :size="17" aria-hidden="true" />
              <span>Sélectionnés uniquement</span>
              <span class="filter-action__state" aria-hidden="true"><Check v-if="selectedOnly" :size="14" stroke-width="3" /></span>
            </button>
            <div class="bulk-actions" role="group" aria-label="Modifier les cinémas affichés">
              <ClientOnly>
                <button
                  type="button"
                  class="group-action"
                  :disabled="!preferencesReady || displayedTheaters.length === 0 || displayedTheaters.every((theater) => selectedIds.has(theater.id))"
                  @click="updateDisplayedSelection(true)"
                ><CheckCheck :size="16" aria-hidden="true" /> Tout sélectionner</button>
                <button
                  type="button"
                  class="group-action group-action--secondary"
                  :disabled="!preferencesReady || displayedTheaters.length === 0 || displayedTheaters.every((theater) => !selectedIds.has(theater.id))"
                  @click="updateDisplayedSelection(false)"
                ><X :size="16" aria-hidden="true" /> Désélectionner</button>
                <template #fallback>
                  <button type="button" class="group-action" disabled><CheckCheck :size="16" aria-hidden="true" /> Tout sélectionner</button>
                  <button type="button" class="group-action group-action--secondary" disabled><X :size="16" aria-hidden="true" /> Désélectionner</button>
                </template>
              </ClientOnly>
            </div>
          </div>
        </div>

        <p class="sr-only" aria-live="polite">{{ statusMessage }}</p>

        <EditorialStatePanel v-if="directoryTheaters.length === 0 && isLoading" semantic="status" live="polite" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><LoaderCircle :size="34" class="cinema-spinner animate-spin" aria-hidden="true" /></template>
          <p>Chargement des cinémas…</p>
        </EditorialStatePanel>

        <EditorialStatePanel v-else-if="directoryError" semantic="alert" size="tall" shadow="large" class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10">
          <template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template>
          <p class="max-w-lg">{{ directoryError }}</p>
          <template #actions><button type="button" class="state-button" @click="retryDirectory"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template>
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
          <template #actions><button type="button" class="state-button" @click="selectedOnly = false">Afficher tous les cinémas</button></template>
        </EditorialStatePanel>

        <div v-else class="mt-10">
          <p v-if="error" class="validation-alert mb-7" role="alert">
            <AlertTriangle :size="19" aria-hidden="true" />
            {{ error }}
          </p>
          <div class="mb-7 flex flex-col gap-2 border-b-2 border-ink py-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
            <h2 class="text-xl font-black tracking-[-0.035em] sm:text-2xl">{{ isNearbyMode ? 'Cinémas à proximité' : 'Cinémas disponibles' }}</h2>
            <p class="utility-label">{{ visibleTheaterCount }} cinéma{{ visibleTheaterCount > 1 ? 's' : '' }}</p>
          </div>

          <template v-if="viewMode === 'list'">
            <div v-if="isNearbyMode" class="theater-grid grid border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a] sm:grid-cols-2">
            <div
              v-for="row in nearbyRows"
              :key="row.theater.id"
              class="theater-option border-b-2 border-ink p-4 sm:p-5"
              :class="selectedIds.has(row.theater.id) ? 'theater-option--selected' : 'bg-surface'"
            >
              <div v-if="row.distanceKm !== null" class="mb-3 flex min-h-6 flex-wrap items-center gap-2 font-mono text-[11px] font-black uppercase">
                <span v-if="row.distanceKm !== null">{{ formatTheaterDistance(row.distanceKm) }}</span>
                <span v-if="row.isNearest" class="nearest-marker">Le plus proche</span>
              </div>
              <label class="group flex cursor-pointer items-start gap-4">
                <input type="checkbox" class="peer sr-only" :checked="selectedIds.has(row.theater.id)" @change="toggleTheater(row.theater.id)" />
                <span class="theater-check mt-0.5 grid size-7 shrink-0 place-items-center border-2 border-ink bg-surface" aria-hidden="true"><Check v-if="selectedIds.has(row.theater.id)" :size="18" stroke-width="3" /></span>
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
                  <p class="utility-label mt-1">{{ group.theaters.length }} cinéma{{ group.theaters.length > 1 ? 's' : '' }}</p>
                </div>
                <div class="grid grid-cols-2 gap-2 sm:flex" role="group" :aria-label="`Modifier les favoris à ${group.city}`">
                  <ClientOnly>
                    <button
                      type="button"
                      class="group-action"
                      :disabled="!preferencesReady || group.theaters.every((theater) => selectedIds.has(theater.id))"
                      @click="updateGroup(group.theaters, true)"
                    >
                      Tout sélectionner
                    </button>
                    <button
                      type="button"
                      class="group-action group-action--secondary"
                      :disabled="!preferencesReady || group.theaters.every((theater) => !selectedIds.has(theater.id))"
                      @click="updateGroup(group.theaters, false)"
                    >
                      Désélectionner
                    </button>
                    <template #fallback>
                      <button type="button" class="group-action" disabled>Tout sélectionner</button>
                      <button type="button" class="group-action group-action--secondary" disabled>Désélectionner</button>
                    </template>
                  </ClientOnly>
                </div>
              </header>

              <div class="theater-grid grid sm:grid-cols-2">
                <div
                  v-for="theater in group.theaters"
                  :key="theater.id"
                  class="theater-option border-b-2 border-ink p-4 sm:p-5"
                  :class="[
                    selectedIds.has(theater.id) ? 'theater-option--selected' : 'bg-surface',
                    group.theaters.length === 1 ? 'theater-option--full sm:col-span-2' : ''
                  ]"
                >
                  <label class="group flex cursor-pointer items-start gap-4">
                    <input type="checkbox" class="peer sr-only" :checked="selectedIds.has(theater.id)" @change="toggleTheater(theater.id)" />
                    <span class="theater-check mt-0.5 grid size-7 shrink-0 place-items-center border-2 border-ink bg-surface" aria-hidden="true"><Check v-if="selectedIds.has(theater.id)" :size="18" stroke-width="3" /></span>
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
              <div class="map-boundary-failure" role="alert">
                <strong>La carte ne peut pas être affichée.</strong>
                <button type="button" class="state-button" @click="recoverMapBoundary(clearError)">Afficher la liste</button>
              </div>
            </template>
          </NuxtErrorBoundary>
        </div>
      </div>
    </section>
  </main>
</template>

<style scoped>
.cinemas-canvas {
  background-color: #f8f7f2;
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.07) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.07) 1px, transparent 1px);
  background-size: 28px 28px;
}

.utility-label {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.15em;
  text-transform: uppercase;
}

.cinemas-title {
  font-family: "Noto Sans Variable", sans-serif;
}

.cinemas-title span:first-of-type {
  -webkit-text-stroke: 2px #27272a;
  color: transparent;
}

.title-mark {
  position: absolute;
  right: 8%;
  bottom: 22%;
  width: clamp(2.5rem, 5vw, 4.75rem);
  aspect-ratio: 1;
  transform: rotate(8deg);
  border: 2px solid #27272a;
  background: var(--color-highlight);
  box-shadow: 5px 5px 0 #27272a;
}

.selection-counter {
  position: absolute;
  right: 15%;
  bottom: 14%;
  display: flex;
  max-width: 11rem;
  align-items: center;
  gap: 0.65rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.65rem 0.75rem;
  box-shadow: 4px 4px 0 #27272a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.6rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  line-height: 1.25;
  text-transform: uppercase;
}

.selection-counter strong {
  font-family: ui-sans-serif, system-ui, sans-serif;
  font-size: 1.75rem;
  line-height: 1;
}

.cinema-search {
  height: 3.25rem;
  border: 2px solid #27272a;
  border-radius: 0;
  background: #fff;
  padding: 0 0.9rem;
  color: #27272a;
  font-size: 0.95rem;
  font-weight: 700;
  outline: none;
}

.cinema-search:focus {
  box-shadow: inset 0 0 0 3px var(--color-highlight);
}

.state-button:focus-visible,
.filter-action:focus-visible,
.group-action:focus-visible,
.location-action:focus-visible,
.view-switch button:focus-visible {
  outline: 3px solid #27272a;
  outline-offset: 3px;
}

.selection-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border-block: 2px solid #27272a;
  padding-block: 1rem;
}

.view-switch,
.filter-action,
.bulk-actions {
  box-sizing: border-box;
  height: 2.75rem;
}

.selection-controls {
  display: inline-flex;
  max-width: 100%;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.75rem;
}

.filter-action {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  gap: 0.55rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.6rem 0.8rem;
  color: #27272a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.62rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.filter-action:hover,
.filter-action[aria-pressed="true"] {
  background: var(--color-highlight);
  color: #27272a;
}

.filter-action[aria-pressed="true"] {
  box-shadow: 4px 4px 0 #27272a;
}

.filter-action__state {
  display: grid;
  width: 1.25rem;
  height: 1.25rem;
  flex: none;
  place-items: center;
  border: 2px solid currentColor;
  background: #fff;
}

.validation-alert {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  border: 2px solid #991b1b;
  background: #fef2f2;
  padding: 0.9rem 1rem;
  color: #7f1d1d;
  font-size: 0.875rem;
  font-weight: 800;
  box-shadow: 4px 4px 0 #991b1b;
}

.state-button,
.group-action,
.location-action {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border: 2px solid #27272a;
  background: #27272a;
  padding: 0.6rem 0.8rem;
  color: #fff;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.62rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.state-button:hover,
.group-action:hover:not(:disabled),
.location-action:hover:not(:disabled) {
  background: #991b1b;
}

.group-action--secondary {
  background: #fff;
  color: #27272a;
}

.location-action {
  min-height: 3.25rem;
  padding-inline: 1rem;
}

.location-action--secondary {
  background: #fff;
  color: #27272a;
}

.location-action--secondary:hover:not(:disabled) {
  background: #e8e6de;
}

.group-action--secondary:hover:not(:disabled) {
  background: #e8e6de;
}

.group-action:disabled,
.location-action:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.bulk-actions {
  display: inline-grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.35rem;
  max-width: 100%;
  border: 2px dashed #27272a;
  background: #f1efe8;
  padding: 0.15rem;
}

.bulk-actions .group-action {
  height: 100%;
  min-width: 0;
  min-height: 0;
  border-width: 0;
  background: transparent;
  color: #27272a;
}

.bulk-actions .group-action:hover:not(:disabled),
.bulk-actions .group-action--secondary:hover:not(:disabled) {
  background: #27272a;
  color: #fff;
}

.nearest-marker {
  border: 2px solid #27272a;
  background: var(--color-highlight);
  padding: 0.15rem 0.4rem;
}

.view-switch {
  display: inline-grid;
  grid-template-columns: repeat(2, minmax(5.5rem, 1fr));
  border: 2px solid #27272a;
  background: #fff;
}

.view-switch button {
  display: inline-flex;
  height: 100%;
  min-height: 0;
  align-items: center;
  justify-content: center;
  gap: 0.45rem;
  padding: 0.55rem 0.9rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.view-switch button + button {
  border-left: 2px solid #27272a;
}

.view-switch button[aria-pressed="true"] {
  background: #27272a;
  color: #fff;
  box-shadow: inset 0 -4px 0 var(--color-highlight);
}

.map-boundary-failure {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  border: 2px solid #991b1b;
  background: #fef2f2;
  padding: 1rem;
  color: #7f1d1d;
  box-shadow: 4px 4px 0 #991b1b;
}

.theater-option:nth-child(odd) {
  border-right: 2px solid #27272a;
}

.theater-option:last-child,
.theater-option:nth-last-child(2):nth-child(odd) {
  border-bottom: 0;
}

.theater-option--full {
  border-right: 0 !important;
}

.theater-option--selected {
  background: #f1efe8;
  box-shadow: inset 5px 0 0 var(--color-highlight);
}

.theater-option input:focus-visible + .theater-check {
  outline: 3px solid #1f6f78;
  outline-offset: 3px;
}

.theater-option--selected .theater-check {
  background: #27272a;
  color: #fff;
  box-shadow: 3px 3px 0 var(--color-highlight);
}

@media (max-width: 639px) {
  .title-mark {
    right: 1.25rem;
    bottom: 1.5rem;
  }

  .selection-counter {
    position: relative;
    right: auto;
    bottom: auto;
    margin-top: 2rem;
    max-width: 13rem;
  }

  .selection-toolbar {
    align-items: stretch;
  }

  .selection-controls {
    width: 100%;
  }

  .view-switch,
  .filter-action,
  .bulk-actions {
    width: 100%;
  }

  .bulk-actions .group-action {
    padding-inline: 0.5rem;
  }

  .theater-option:nth-child(odd) {
    border-right: 0;
  }

  .theater-option:nth-last-child(2):nth-child(odd) {
    border-bottom: 2px solid #27272a;
  }
}

@media (prefers-reduced-motion: reduce) {
  .cinema-spinner {
    animation: none;
  }
}
</style>
