<script setup lang="ts">
import {
  AlertTriangle,
  ArrowRight,
  Building2,
  Check,
  CheckCheck,
  List,
  LoaderCircle,
  LocateFixed,
  Map as MapIcon,
  Minus,
  RefreshCw,
  Search,
  SlidersHorizontal,
  X,
} from '@lucide/vue'
import type { Theater } from '~/types/api'
import { theaterDisplayName } from '~/utils/theaterDisplayName'
import {
  cinemaListSections,
  groupTheatersByCityIdentity,
  updateTheaterSelection,
} from '~/utils/cinemaSelection'
import { serializeJsonLd } from '~/utils/jsonLd'
import { queriesEqual } from '~/utils/routeQuery'
import {
  cinemaDirectoryQuery,
  parseCinemaDirectoryQuery,
  type CinemaDirectoryState,
} from '~/utils/cinemaDirectoryQuery'
import { createCinemaDirectoryLocation } from '~/utils/cinemaDirectoryLocation'
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

const search = ref('')
const statusMessage = ref('')
const routeState = computed(() => parseCinemaDirectoryQuery(route.query))
const viewMode = computed(() => routeState.value.view)
const locationMode = computed(() => routeState.value.location)
const location = createCinemaDirectoryLocation(() =>
  import.meta.client ? navigator.geolocation : undefined,
)
const { locationStatus, locationError, userPosition, locationAccuracyMeters } =
  location
const draftFavoriteTheaterIds = ref<string[]>([])
const preferencesReady = ref(false)
let isUnmounted = false
let isMounted = false
const settingsOpen = ref(false)
const settingsDialog = ref<HTMLDialogElement | null>(null)
const settingsCloseButton = ref<HTMLButtonElement | null>(null)
let settingsTrigger: HTMLElement | null = null
let desktopMediaQuery: MediaQueryList | null = null
let bodyOverflowBeforeLock: string | null = null

async function openSettings(event: MouseEvent) {
  if (desktopMediaQuery?.matches || settingsOpen.value || isUnmounted) return
  settingsTrigger =
    event.currentTarget instanceof HTMLElement ? event.currentTarget : null
  settingsOpen.value = true
  await nextTick()
  if (!settingsOpen.value || isUnmounted) return
  settingsDialog.value?.showModal()
  bodyOverflowBeforeLock = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  settingsCloseButton.value?.focus({ preventScroll: true })
}

function closeSettings({ restoreFocus = true } = {}) {
  if (!settingsOpen.value) return
  settingsOpen.value = false
  settingsDialog.value?.close()
  if (bodyOverflowBeforeLock !== null) {
    document.body.style.overflow = bodyOverflowBeforeLock
    bodyOverflowBeforeLock = null
  }
  if (restoreFocus && settingsTrigger?.isConnected)
    nextTick(() => settingsTrigger?.focus({ preventScroll: true }))
}

function handleSettingsBackdrop(event: MouseEvent) {
  const dialog = settingsDialog.value
  if (!dialog || event.target !== dialog) return
  const bounds = dialog.getBoundingClientRect()
  if (
    event.clientX < bounds.left ||
    event.clientX > bounds.right ||
    event.clientY < bounds.top ||
    event.clientY > bounds.bottom
  )
    closeSettings()
}

function handleSettingsViewport(event: MediaQueryListEvent) {
  if (event.matches) closeSettings({ restoreFocus: false })
}

function handleSettingsKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !settingsOpen.value || !settingsDialog.value)
    return
  const focusable = [
    ...settingsDialog.value.querySelectorAll<HTMLElement>(
      'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])',
    ),
  ].filter(
    (element) =>
      !element.hasAttribute('disabled') && element.getClientRects().length > 0,
  )
  const first = focusable[0]
  const last = focusable.at(-1)
  if (!first || !last) return
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function hydrateRoute() {
  const value = routeState.value.search
  if (search.value.trim() !== value) search.value = value
  return cinemaDirectoryQuery(route.query)
}

function updateSearch(event: Event) {
  if (!(event.currentTarget instanceof HTMLInputElement)) return
  search.value = event.currentTarget.value
  const query = cinemaDirectoryQuery(route.query, { search: search.value })
  if (!queriesEqual(route.query, query)) router.replace({ query })
}

const selectedIds = computed(() => new Set(draftFavoriteTheaterIds.value))
const normalizedSearch = computed(() =>
  search.value.trim().toLocaleLowerCase('fr-FR'),
)
const searchResults = computed(() =>
  directoryTheaters.value.filter((theater) => {
    const searchable = `${theater.name} ${theater.city}`.toLocaleLowerCase(
      'fr-FR',
    )
    return (
      !normalizedSearch.value || searchable.includes(normalizedSearch.value)
    )
  }),
)
const displayedTheaters = searchResults
const isNearbyMode = computed(
  () => locationStatus.value === 'active' && userPosition.value !== null,
)
const usedPositionMapUrl = computed(() =>
  userPosition.value ? buildOpenStreetMapPositionUrl(userPosition.value) : null,
)
const nearbyRows = computed(() =>
  userPosition.value
    ? sortTheatersByDistance(searchResults.value, userPosition.value)
    : [],
)
const listSections = computed(() =>
  cinemaListSections(
    displayedTheaters.value,
    selectedIds.value,
    (theater) => theater.id,
  ).map((section) => {
    const ids = new Set(section.rows.map((theater) => theater.id))
    return {
      key: section.key,
      groups: groupTheatersByCityIdentity(section.rows),
      nearbyRows: nearbyRows.value.filter((row) => ids.has(row.theater.id)),
    }
  }),
)
const visibleTheaterCount = computed(() => displayedTheaters.value.length)

function setDisplayMode(
  changes: Partial<Pick<CinemaDirectoryState, 'view' | 'location'>>,
) {
  const query = cinemaDirectoryQuery(route.query, {
    search: search.value,
    ...changes,
  })
  if (!queriesEqual(route.query, query)) router.push({ query })
}

function useCurrentPosition() {
  if (locationMode.value === 'nearby') location.retry()
  else setDisplayMode({ location: 'nearby' })
}

function showByCity() {
  setDisplayMode({ location: 'city' })
}

async function applyDraftSelection(nextIds: string[]) {
  if (writesBlocked.value || isUnmounted) return
  const scope = selectionScopeKey.value
  statusMessage.value = ''
  draftFavoriteTheaterIds.value = nextIds
  const saved = await setFavoriteTheaterIds(nextIds)
  if (isUnmounted || scope !== selectionScopeKey.value) return
  if (!saved) {
    statusMessage.value = 'La sélection n’a pas pu être enregistrée.'
    return
  }

  draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
}

async function toggleTheater(id: string, event?: Event) {
  if (writesBlocked.value || isUnmounted) return
  const theater = directoryTheaters.value.find((item) => item.id === id)
  if (!theater) return
  const target = event?.currentTarget
  const focusedInput =
    target instanceof HTMLElement && document.activeElement === target
      ? target
      : null
  const fullListRow = focusedInput?.closest('#cinema-section-all')
    ? focusedInput.closest<HTMLElement>('.theater-grid > div')
    : null
  const rowTop = fullListRow?.getBoundingClientRect().top
  const scope = selectionScopeKey.value
  // Native anchoring can miss the first/last summary and pending-feedback height changes.
  function restoreRowPosition() {
    if (
      !fullListRow?.isConnected ||
      rowTop === undefined ||
      isUnmounted ||
      scope !== selectionScopeKey.value ||
      (document.activeElement !== focusedInput &&
        document.activeElement !== document.body)
    )
      return
    const shift = fullListRow.getBoundingClientRect().top - rowTop
    if (Math.abs(shift) > 1)
      window.scrollBy({ top: shift, behavior: 'instant' })
  }
  const pending = applyDraftSelection(
    updateTheaterSelection(
      draftFavoriteTheaterIds.value,
      [theater],
      !selectedIds.value.has(id),
    ),
  )
  await nextTick()
  restoreRowPosition()
  const anchoredScrollY = fullListRow ? window.scrollY : null
  await pending
  await nextTick()
  if (anchoredScrollY === window.scrollY) restoreRowPosition()
  if (
    focusedInput?.isConnected &&
    !writesBlocked.value &&
    !isUnmounted &&
    scope === selectionScopeKey.value &&
    document.activeElement === document.body
  )
    focusedInput.focus({ preventScroll: true })
}

function showList() {
  setDisplayMode({ view: 'list' })
}

function recoverMapBoundary(clearError: () => void) {
  showList()
  clearError()
}

function groupSelectionState(groupTheaters: readonly Theater[]) {
  if (groupTheaters.every((theater) => selectedIds.value.has(theater.id)))
    return 'all'
  return groupTheaters.some((theater) => selectedIds.value.has(theater.id))
    ? 'some'
    : 'none'
}

function updateGroup(groupTheaters: readonly Theater[], select: boolean) {
  applyDraftSelection(
    updateTheaterSelection(
      draftFavoriteTheaterIds.value,
      groupTheaters,
      select,
    ),
  )
}

function updateDisplayedSelection(select: boolean) {
  applyDraftSelection(
    updateTheaterSelection(
      draftFavoriteTheaterIds.value,
      displayedTheaters.value,
      select,
    ),
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

hydrateRoute()
watch(
  [favoriteTheaterIds, selectionScopeKey],
  () => {
    draftFavoriteTheaterIds.value = [...favoriteTheaterIds.value]
    statusMessage.value = ''
  },
  { flush: 'sync' },
)
watch(
  locationMode,
  (mode) => {
    if (isMounted) location.setNearbyMode(mode === 'nearby')
  },
  { flush: 'sync' },
)
watch(
  () => route.query,
  () => {
    const query = hydrateRoute()
    if (!queriesEqual(route.query, query)) router.replace({ query })
  },
)
onMounted(async () => {
  isMounted = true
  location.setNearbyMode(locationMode.value === 'nearby')
  desktopMediaQuery = window.matchMedia('(min-width: 1024px)')
  desktopMediaQuery.addEventListener('change', handleSettingsViewport)
  const query = hydrateRoute()
  if (!queriesEqual(route.query, query)) await router.replace({ query })
  await loadPreferences()
  if (!isUnmounted) preferencesReady.value = true
})
onBeforeUnmount(() => {
  isUnmounted = true
  isMounted = false
  location.dispose()
  closeSettings({ restoreFocus: false })
  desktopMediaQuery?.removeEventListener('change', handleSettingsViewport)
})

const config = useRuntimeConfig()
const canonicalUrl = absoluteSiteUrl(config.public.siteUrl, '/cinemas')
const pageTitle = 'Cinémas et villes - MesSeances'
const pageDescription =
  'Annuaire des cinémas et des villes disponibles sur MesSeances.'
const robots = computed(() =>
  directoryTheaters.value.length > 0 &&
  !directoryError.value &&
  Object.keys(route.query).length === 0
    ? 'index,follow'
    : 'noindex,follow',
)
const cinemasJsonLd = computed(() => {
  if (directoryError.value || Object.keys(route.query).length > 0) return null
  const theaters = searchResults.value
  if (theaters.length === 0) return null
  return serializeJsonLd({
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'ItemList',
        '@id': `${canonicalUrl}#cinema-list`,
        itemListElement: theaters.map((theater, index) => ({
          '@type': 'ListItem',
          position: index + 1,
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
        class="relative mx-auto max-w-[1440px] overflow-hidden px-4 pb-10 pt-12 sm:px-6 sm:pb-14 sm:pt-16 lg:px-10 lg:pb-16 lg:pt-20"
      >
        <p
          class="font-mono text-[0.65rem] font-black uppercase tracking-[0.15em] text-ink"
        >
          Mes préférences
        </p>
        <h1
          id="cinemas-title"
          class="mt-5 max-w-6xl [font-family:'Noto_Sans_Variable',sans-serif] text-[clamp(4rem,11vw,10rem)] font-black uppercase leading-[0.76] tracking-[-0.085em] [&>span:first-of-type]:text-transparent [&>span:first-of-type]:[-webkit-text-stroke:2px_#27272a]"
        >
          Mes<br><span>cinémas</span><span class="text-primary">.</span>
        </h1>
        <div
          class="absolute right-[15%] bottom-[14%] hidden max-w-44 items-center gap-[0.65rem] border-2 border-ink bg-surface px-3 py-[0.65rem] font-mono text-[0.6rem] leading-[1.25] font-black uppercase tracking-[0.08em] shadow-[4px_4px_0_#27272a] lg:flex"
        >
          <strong class="font-sans text-[1.75rem] leading-none">{{
            draftFavoriteTheaterIds.length
          }}</strong>
          <span
            >cinéma{{ draftFavoriteTheaterIds.length > 1 ? 's' : '' }}
            sélectionné{{ draftFavoriteTheaterIds.length > 1 ? 's' : '' }}</span
          >
        </div>
        <span
          class="absolute right-[8%] bottom-[22%] aspect-square w-[clamp(2.5rem,5vw,4.75rem)] rotate-[8deg] border-2 border-ink bg-highlight shadow-[5px_5px_0_#27272a] max-sm:right-5 max-sm:bottom-6"
          aria-hidden="true"
        ></span>
      </div>
    </section>

    <section
      class="border-b-2 border-ink bg-[#f8f7f2] bg-[linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] bg-[size:28px_28px]"
      aria-label="Sélection de mes cinémas"
    >
      <div
        class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12"
      >
        <div
          class="search-workspace grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-2 border-2 border-ink bg-[#f1efe8] p-4 shadow-[7px_7px_0_#27272a] sm:p-6 lg:items-end lg:gap-x-4"
        >
          <label
            for="cinema-directory-search"
            class="col-span-full font-mono text-[0.65rem] font-black uppercase tracking-[0.15em] text-ink"
            >Rechercher un cinéma ou une ville</label
          >
          <div
            class="flex min-w-0"
            :class="isNearbyMode ? 'col-span-full lg:col-span-1' : ''"
          >
            <span
              class="grid size-[3.25rem] shrink-0 place-items-center border-2 border-r-0 border-ink bg-[#ffcf3f]"
              aria-hidden="true"
            >
              <Search :size="19" stroke-width="2.5" />
            </span>
            <input
              id="cinema-directory-search"
              :value="search"
              type="search"
              class="h-[3.25rem] min-w-0 flex-1 rounded-none border-2 border-ink bg-surface px-[0.9rem] text-[0.95rem] font-bold text-ink outline-none focus:shadow-[inset_0_0_0_3px_var(--color-highlight)]"
              autocomplete="off"
              placeholder="Nom du cinéma ou ville"
              @input="updateSearch"
            >
          </div>
          <div
            class="items-center gap-3"
            :class="isNearbyMode ? 'hidden lg:flex' : 'flex'"
          >
            <button
              v-if="!isNearbyMode"
              type="button"
              class="inline-flex min-h-[3.25rem] w-[3.25rem] shrink-0 items-center justify-center gap-2 border-2 border-ink bg-ink py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white enabled:hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40 lg:w-auto lg:px-4"
              :aria-label="locationStatus === 'requesting' ? 'Localisation…' : 'Utiliser ma position'"
              :title="locationStatus === 'requesting' ? 'Localisation…' : 'Utiliser ma position'"
              :disabled="locationStatus === 'requesting'"
              :aria-busy="locationStatus === 'requesting'"
              @click="useCurrentPosition"
            >
              <LoaderCircle
                v-if="locationStatus === 'requesting'"
                :size="18"
                class="animate-spin motion-reduce:animate-none"
                aria-hidden="true"
              />
              <LocateFixed v-else :size="18" aria-hidden="true" />
              <span class="hidden lg:inline">
                {{
                  locationStatus === 'requesting' ? 'Localisation…' : 'Utiliser ma position'
                }}
              </span>
            </button>
            <Teleport to="#cinema-settings-location" :disabled="!settingsOpen">
              <div
                v-if="locationMode === 'nearby' && (locationStatus === 'active' || locationStatus === 'failed')"
                class="gap-3"
                :class="settingsOpen ? 'grid' : 'hidden lg:flex'"
              >
                <button
                  type="button"
                  class="inline-flex min-h-[3.25rem] items-center justify-center gap-2 border-2 border-ink bg-surface px-4 py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink hover:bg-[#e8e6de] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
                  @click="showByCity"
                >
                  Afficher par ville
                </button>
              </div>
            </Teleport>
          </div>
        </div>

        <Teleport to="#cinema-settings-location" :disabled="!settingsOpen">
          <p
            v-if="locationStatus === 'requesting'"
            class="mt-5 font-bold"
            role="status"
            aria-live="polite"
          >
            Recherche de votre position…
          </p>
          <p
            v-if="isNearbyMode"
            class="mt-5 text-sm font-semibold leading-relaxed"
            role="status"
            aria-live="polite"
          >
            <a
              v-if="userPosition && usedPositionMapUrl"
              :href="usedPositionMapUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="font-bold underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
              >Position utilisée : latitude
              {{ formatPositionCoordinate(userPosition.latitude) }}
              · longitude
              {{ formatPositionCoordinate(userPosition.longitude) }}
              <span aria-hidden="true">↗</span
              ><span class="sr-only">
                (ouvre OpenStreetMap dans un nouvel onglet)</span
              ></a
            >
            <span> · {{ formatPositionAccuracy(locationAccuracyMeters) }}</span>
          </p>
          <p
            v-if="locationError"
            class="mt-5 flex items-center gap-[0.65rem] border-2 border-primary bg-primary-soft px-4 py-[0.9rem] text-sm font-extrabold text-primary-hover shadow-[4px_4px_0_#991b1b]"
            role="alert"
          >
            <AlertTriangle :size="19" aria-hidden="true" />
            {{ locationError }}
          </p>
        </Teleport>

        <Teleport to="#cinema-settings-controls" :disabled="!settingsOpen">
          <div
            v-if="directoryTheaters.length > 0 && !directoryError"
            class="selection-toolbar flex-wrap items-center justify-between gap-3"
            :class="settingsOpen ? 'flex' : 'mt-7 hidden border-y-2 border-ink py-4 lg:flex'"
          >
            <div
              class="view-switch inline-grid h-11 grid-cols-[repeat(2,minmax(5.5rem,1fr))] border-2 border-ink bg-surface max-sm:w-full"
              role="group"
              aria-label="Mode d’affichage des cinémas"
            >
              <button
                type="button"
                class="inline-flex h-full min-h-0 items-center justify-center gap-[0.45rem] px-[0.9rem] py-[0.55rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink aria-pressed:bg-ink aria-pressed:text-white aria-pressed:shadow-[inset_0_-4px_0_var(--color-highlight)]"
                :aria-pressed="viewMode === 'list'"
                @click="showList"
              >
                <List :size="16" aria-hidden="true" />
                Liste
              </button>
              <button
                type="button"
                class="inline-flex h-full min-h-0 items-center justify-center gap-[0.45rem] border-l-2 border-ink px-[0.9rem] py-[0.55rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink aria-pressed:bg-ink aria-pressed:text-white aria-pressed:shadow-[inset_0_-4px_0_var(--color-highlight)]"
                :aria-pressed="viewMode === 'map'"
                @click="setDisplayMode({ view: 'map' })"
              >
                <MapIcon :size="16" aria-hidden="true" />
                Carte
              </button>
            </div>
            <div
              class="selection-controls inline-flex max-w-full flex-wrap items-center justify-end gap-3 max-sm:w-full"
            >
              <div
                class="bulk-actions inline-grid max-w-full grid-cols-1 gap-[0.35rem] border-2 border-dashed border-ink bg-[#f1efe8] p-[0.15rem] max-sm:w-full sm:h-11 sm:grid-cols-2"
                role="group"
                aria-label="Modifier les cinémas affichés"
              >
                <ClientOnly>
                  <button
                    type="button"
                    class="inline-flex h-full min-h-11 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink enabled:hover:bg-ink enabled:hover:text-white focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2 sm:min-h-0"
                    :disabled="!preferencesReady || writesBlocked || displayedTheaters.length === 0 || displayedTheaters.every((theater) => selectedIds.has(theater.id))"
                    @click="updateDisplayedSelection(true)"
                  >
                    <CheckCheck :size="16" aria-hidden="true" />
                    Tout sélectionner
                  </button>
                  <button
                    type="button"
                    class="inline-flex h-full min-h-11 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink enabled:hover:bg-ink enabled:hover:text-white focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2 sm:min-h-0"
                    :disabled="!preferencesReady || writesBlocked || displayedTheaters.length === 0 || displayedTheaters.every((theater) => !selectedIds.has(theater.id))"
                    @click="updateDisplayedSelection(false)"
                  >
                    <X :size="16" aria-hidden="true" />
                    Désélectionner
                  </button>
                  <template #fallback>
                    <button
                      type="button"
                      class="inline-flex h-full min-h-0 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2"
                      disabled
                    >
                      <CheckCheck :size="16" aria-hidden="true" />
                      Tout sélectionner
                    </button>
                    <button
                      type="button"
                      class="inline-flex h-full min-h-0 min-w-0 items-center justify-center gap-2 border-0 bg-transparent px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink disabled:cursor-not-allowed disabled:opacity-40 max-sm:px-2"
                      disabled
                    >
                      <X :size="16" aria-hidden="true" />
                      Désélectionner
                    </button>
                  </template>
                </ClientOnly>
              </div>
            </div>
          </div>
        </Teleport>

        <Teleport to="#cinema-settings-feedback" :disabled="!settingsOpen">
          <p
            v-if="isSaving || statusMessage"
            class="mt-4 text-sm font-bold"
            role="status"
          >
            {{ isSaving ? 'Enregistrement…' : statusMessage }}
          </p>
          <div
            v-if="error || syncError"
            class="mt-4 flex flex-wrap items-center gap-3 text-sm font-bold"
            role="alert"
          >
            <AlertTriangle :size="19" aria-hidden="true" />
            <span>{{ error || syncError }}</span>
            <button
              type="button"
              class="min-h-11 border-2 border-ink px-3 focus-visible:outline-2 focus-visible:outline-offset-3"
              :disabled="isSaving"
              @click="retrySynchronization"
            >
              Réessayer
            </button>
          </div>
        </Teleport>

        <div
          class="mb-7 mt-10 flex items-center justify-between gap-3 border-b-2 border-ink py-4"
        >
          <div
            class="min-w-0 flex-1 sm:flex sm:items-end sm:justify-between sm:gap-6"
          >
            <h2 class="text-xl font-black tracking-[-0.035em] sm:text-2xl">
              {{ isNearbyMode ? 'Cinémas à proximité' : 'Cinémas disponibles' }}
            </h2>
            <p
              class="mt-2 font-mono text-[0.65rem] font-black uppercase tracking-[0.15em] sm:mt-0"
            >
              {{ visibleTheaterCount }} cinéma{{
                visibleTheaterCount > 1 ? 's' : ''
              }}
            </p>
          </div>
          <button
            type="button"
            class="editorial-button-outline size-11 shrink-0 p-0 lg:hidden"
            aria-label="Réglages des cinémas"
            aria-controls="cinema-settings"
            aria-haspopup="dialog"
            :aria-expanded="settingsOpen"
            @click="openSettings"
          >
            <SlidersHorizontal :size="20" aria-hidden="true" />
          </button>
        </div>

        <EditorialStatePanel
          v-if="directoryTheaters.length === 0 && isLoading"
          semantic="status"
          live="polite"
          size="tall"
          shadow="large"
          class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
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
          class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
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
              class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
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
          class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon><Building2 :size="36" aria-hidden="true" /></template>
          <p>Aucun cinéma disponible.</p>
        </EditorialStatePanel>

        <EditorialStatePanel
          v-else-if="searchResults.length === 0"
          size="tall"
          shadow="large"
          class="cinema-state mx-auto mb-4 mt-16 max-w-3xl font-extrabold max-sm:mt-10"
        >
          <template #icon><Search :size="34" aria-hidden="true" /></template>
          <p>Aucun cinéma ne correspond à votre recherche.</p>
        </EditorialStatePanel>

        <div v-else>
          <div v-if="viewMode === 'list'" class="space-y-10">
            <section
              v-for="section in listSections"
              :key="section.key"
              :id="`cinema-section-${section.key}`"
              :aria-labelledby="`cinema-section-${section.key}-title`"
            >
              <h3
                :id="`cinema-section-${section.key}-title`"
                class="editorial-heading mb-5"
              >
                {{
                  section.key === 'selected' ? 'Cinémas sélectionnés' : 'Tous les cinémas'
                }}
              </h3>
              <div
                v-if="isNearbyMode"
                class="theater-grid grid border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a] sm:grid-cols-2"
              >
                <div
                  v-for="row in section.nearbyRows"
                  :key="row.theater.id"
                  class="border-b-2 border-ink p-4 odd:border-r-2 last:border-b-0 [&:nth-last-child(2):nth-child(odd)]:border-b-0 sm:p-5 max-sm:odd:border-r-0 max-sm:[&:nth-last-child(2):nth-child(odd)]:border-b-2"
                  :class="selectedIds.has(row.theater.id) ? 'bg-[#f1efe8] shadow-[inset_5px_0_0_var(--color-highlight)] [&_.theater-check]:bg-ink [&_.theater-check]:text-white [&_.theater-check]:shadow-[3px_3px_0_var(--color-highlight)]' : 'bg-surface'"
                >
                  <div
                    v-if="row.distanceKm !== null"
                    class="mb-3 flex min-h-6 flex-wrap items-center gap-2 font-mono text-[11px] font-black uppercase"
                  >
                    <span v-if="row.distanceKm !== null">{{
                      formatTheaterDistance(row.distanceKm)
                    }}</span>
                    <span
                      v-if="row.isNearest"
                      class="border-2 border-ink bg-highlight px-[0.4rem] py-[0.15rem]"
                      >Le plus proche</span
                    >
                  </div>
                  <div class="flex items-start gap-4">
                    <label
                      class="flex min-h-11 shrink-0 cursor-pointer items-start pt-0.5"
                    >
                      <input
                        type="checkbox"
                        class="peer sr-only"
                        :aria-label="`Sélectionner ${theaterDisplayName(row.theater)}`"
                        :checked="selectedIds.has(row.theater.id)"
                        :disabled="writesBlocked"
                        @change="toggleTheater(row.theater.id, $event)"
                      >
                      <span
                        class="theater-check grid size-7 place-items-center border-2 border-ink bg-surface peer-focus-visible:outline-3 peer-focus-visible:outline-offset-3 peer-focus-visible:outline-accent"
                        aria-hidden="true"
                        ><Check
                          v-if="selectedIds.has(row.theater.id)"
                          :size="18"
                          stroke-width="3"
                        /></span
                      >
                    </label>
                    <NuxtLink
                      :to="`/cinema/${encodeURIComponent(row.theater.slug)}`"
                      :aria-label="`Voir les séances : ${theaterDisplayName(row.theater)}`"
                      class="group flex min-h-11 min-w-0 flex-1 items-start gap-4 no-underline focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-accent"
                    >
                      <span class="min-w-0 flex-1"
                        ><TheaterName
                          :name="theaterDisplayName(row.theater)"
                          :provider="row.theater.provider"
                          class="block text-base font-black leading-tight tracking-[-0.02em] text-ink group-hover:text-primary sm:text-lg"
                        /><span
                          class="mt-2 block text-sm font-medium leading-relaxed text-ink"
                          ><template v-if="row.theater.address"
                            >{{ row.theater.address }},
                          </template>{{ row.theater.postal_code }}
                          {{ row.theater.city }}</span
                        ></span
                      >
                      <ArrowRight
                        :size="22"
                        class="mt-0.5 shrink-0 text-ink group-hover:text-primary"
                        aria-hidden="true"
                      />
                    </NuxtLink>
                  </div>
                </div>
              </div>

              <div v-else class="space-y-8">
                <section
                  v-for="group in section.groups"
                  :key="group.citySlug"
                  class="city-section border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]"
                >
                  <header
                    class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 border-b-2 border-ink bg-[#f1efe8] p-4 sm:p-5"
                  >
                    <div class="min-w-0">
                      <h4
                        class="text-2xl font-black uppercase tracking-[-0.045em] sm:text-3xl"
                      >
                        <NuxtLink
                          :to="`/ville/${encodeURIComponent(group.citySlug)}/cinemas`"
                          class="inline-flex min-h-11 max-w-full items-center [overflow-wrap:anywhere] underline decoration-2 underline-offset-4 hover:text-primary"
                          >{{
                            group.city
                          }}</NuxtLink
                        >
                      </h4>
                    </div>
                    <label
                      v-if="group.theaters.length > 1"
                      class="city-group-selection flex size-11 shrink-0 cursor-pointer items-center justify-center has-disabled:cursor-not-allowed has-disabled:opacity-40 lg:hidden"
                    >
                      <input
                        type="checkbox"
                        class="peer sr-only"
                        :aria-label="`${groupSelectionState(group.theaters) === 'all' ? 'Désélectionner' : 'Sélectionner'} les cinémas affichés du groupe ${group.city}`"
                        :checked="groupSelectionState(group.theaters) === 'all'"
                        :indeterminate.prop="groupSelectionState(group.theaters) === 'some'"
                        :disabled="!preferencesReady || writesBlocked"
                        @change="updateGroup(group.theaters, groupSelectionState(group.theaters) !== 'all')"
                      >
                      <span
                        class="grid size-7 place-items-center border-2 border-ink bg-surface text-ink peer-checked:bg-ink peer-checked:text-white peer-checked:shadow-[3px_3px_0_var(--color-highlight)] peer-indeterminate:bg-ink peer-indeterminate:text-white peer-indeterminate:shadow-[3px_3px_0_var(--color-highlight)] peer-focus-visible:outline-3 peer-focus-visible:outline-offset-3 peer-focus-visible:outline-accent"
                        aria-hidden="true"
                      >
                        <Check
                          v-if="groupSelectionState(group.theaters) === 'all'"
                          :size="18"
                          stroke-width="3"
                        />
                        <Minus
                          v-else-if="groupSelectionState(group.theaters) === 'some'"
                          :size="18"
                          stroke-width="3"
                        />
                      </span>
                    </label>
                    <div
                      class="hidden gap-2 lg:flex"
                      role="group"
                      :aria-label="`Modifier mes cinémas à ${group.city}`"
                    >
                      <ClientOnly>
                        <button
                          type="button"
                          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white enabled:hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40"
                          :disabled="!preferencesReady || writesBlocked || group.theaters.every((theater) => selectedIds.has(theater.id))"
                          @click="updateGroup(group.theaters, true)"
                        >
                          Tout sélectionner
                        </button>
                        <button
                          type="button"
                          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink enabled:hover:bg-[#e8e6de] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40"
                          :disabled="!preferencesReady || writesBlocked || group.theaters.every((theater) => !selectedIds.has(theater.id))"
                          @click="updateGroup(group.theaters, false)"
                        >
                          Désélectionner
                        </button>
                        <template #fallback>
                          <button
                            type="button"
                            class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white disabled:cursor-not-allowed disabled:opacity-40"
                            disabled
                          >
                            Tout sélectionner
                          </button>
                          <button
                            type="button"
                            class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-ink disabled:cursor-not-allowed disabled:opacity-40"
                            disabled
                          >
                            Désélectionner
                          </button>
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
                      <div class="flex items-start gap-4">
                        <label
                          class="flex min-h-11 shrink-0 cursor-pointer items-start pt-0.5"
                        >
                          <input
                            type="checkbox"
                            class="peer sr-only"
                            :aria-label="`Sélectionner ${theaterDisplayName(theater)}`"
                            :checked="selectedIds.has(theater.id)"
                            :disabled="writesBlocked"
                            @change="toggleTheater(theater.id, $event)"
                          >
                          <span
                            class="theater-check grid size-7 place-items-center border-2 border-ink bg-surface peer-focus-visible:outline-3 peer-focus-visible:outline-offset-3 peer-focus-visible:outline-accent"
                            aria-hidden="true"
                            ><Check
                              v-if="selectedIds.has(theater.id)"
                              :size="18"
                              stroke-width="3"
                            /></span
                          >
                        </label>
                        <NuxtLink
                          :to="`/cinema/${encodeURIComponent(theater.slug)}`"
                          :aria-label="`Voir les séances : ${theaterDisplayName(theater)}`"
                          class="group flex min-h-11 min-w-0 flex-1 items-start gap-4 no-underline focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-accent"
                        >
                          <span class="min-w-0 flex-1"
                            ><TheaterName
                              :name="theaterDisplayName(theater)"
                              :provider="theater.provider"
                              class="block text-base font-black leading-tight tracking-[-0.02em] text-ink group-hover:text-primary sm:text-lg"
                            /><span
                              class="mt-2 block text-sm font-medium leading-relaxed text-ink"
                              ><template v-if="theater.address"
                                >{{ theater.address }},
                              </template>{{ theater.postal_code }}
                              {{ theater.city }}</span
                            ></span
                          >
                          <ArrowRight
                            :size="22"
                            class="mt-0.5 shrink-0 text-ink group-hover:text-primary"
                            aria-hidden="true"
                          />
                        </NuxtLink>
                      </div>
                    </div>
                  </div>
                </section>
              </div>
            </section>
          </div>

          <NuxtErrorBoundary v-else>
            <LazyCinemaTheaterMap
              :selection-disabled="writesBlocked"
              :theaters="displayedTheaters"
              :favorite-theater-ids="draftFavoriteTheaterIds"
              :user-position="userPosition"
              @show-list="showList"
              @toggle-favorite="toggleTheater"
            />
            <template #error="{ clearError }">
              <div
                class="flex flex-wrap items-center justify-between gap-4 border-2 border-primary bg-primary-soft p-4 text-primary-hover shadow-[4px_4px_0_#991b1b]"
                role="alert"
              >
                <strong>La carte ne peut pas être affichée.</strong>
                <button
                  type="button"
                  class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.8rem] py-[0.6rem] font-mono text-[0.62rem] font-black uppercase tracking-[0.08em] text-white hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
                  @click="recoverMapBoundary(clearError)"
                >
                  Afficher la liste
                </button>
              </div>
            </template>
          </NuxtErrorBoundary>
        </div>
      </div>
    </section>
    <dialog
      id="cinema-settings"
      ref="settingsDialog"
      aria-labelledby="cinema-settings-title"
      aria-modal="true"
      class="fixed inset-x-0 bottom-0 top-auto m-0 max-h-[calc(100dvh-1rem)] w-full max-w-none overflow-y-auto overscroll-contain rounded-none border-2 border-b-0 border-ink bg-[#f8f7f2] px-4 pt-4 pb-[max(1.5rem,env(safe-area-inset-bottom))] text-ink shadow-[0_-8px_0_#27272a] backdrop:bg-black/60 sm:px-6"
      @cancel.prevent="closeSettings()"
      @close="!settingsDialog?.open && closeSettings()"
      @click="handleSettingsBackdrop"
      @keydown="handleSettingsKeydown"
    >
      <div class="mb-6 flex items-center gap-2.5 border-b-2 border-ink pb-4">
        <SlidersHorizontal :size="18" aria-hidden="true" />
        <h2
          id="cinema-settings-title"
          class="min-w-0 flex-1 text-xl font-black leading-tight tracking-[-0.035em]"
        >
          Réglages des cinémas
        </h2>
        <button
          ref="settingsCloseButton"
          type="button"
          class="editorial-button-outline ml-auto size-11 shrink-0 p-0"
          aria-label="Fermer les réglages"
          @click="closeSettings()"
        >
          <X :size="20" aria-hidden="true" />
        </button>
      </div>
      <div
        id="cinema-settings-location"
        :class="locationMode === 'nearby' ? 'mb-5' : ''"
      ></div>
      <div id="cinema-settings-controls"></div>
      <div id="cinema-settings-feedback"></div>
    </dialog>
  </main>
</template>
