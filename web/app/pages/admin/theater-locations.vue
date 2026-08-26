<script setup lang="ts">
import { AlertTriangle, ArrowLeft, Check, LoaderCircle, LogOut, MapPin, RefreshCw } from '@lucide/vue'
import type {
  AdminTheaterGeocodingFailureCode,
  AdminTheaterGeocodingJob,
  AdminTheaterGeocodingResponse,
  AdminTheaterLocation,
  AdminTheaterLocationsResponse,
  Provider
} from '~/types/api'
import {
  parseAdminTheaterLocationCoordinates,
  type AdminTheaterLocationCoordinateDraft,
  type AdminTheaterLocationCoordinateErrors
} from '~/utils/adminTheaterLocations'
import { mergeOwnedQuery, positiveSafeInteger, queriesEqual, singularQueryValue } from '~/utils/routeQuery'

definePageMeta({ middleware: 'admin-auth' })

type LocationAction = 'suggestion' | 'manual'

const PAGE_SIZE = 20
const POLL_DELAY = 2000
const OWNED_QUERY_KEYS = ['page'] as const
const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()

const result = ref<AdminTheaterLocationsResponse | null>(null)
const offset = ref(0)
const pending = ref(true)
const loadError = ref('')
const drafts = ref<Record<string, AdminTheaterLocationCoordinateDraft>>({})
const draftErrors = ref<Record<string, AdminTheaterLocationCoordinateErrors>>({})
const itemErrors = ref<Record<string, string>>({})
const pendingActions = ref<Record<string, LocationAction>>({})
const successMessage = ref('')
const geocodingStatus = ref<AdminTheaterGeocodingResponse | null>(null)
const geocodingInitialPending = ref(true)
const geocodingRequestPending = ref(false)
const geocodingStarting = ref(false)
const geocodingError = ref('')
const loggingOut = ref(false)
const logoutError = ref('')
let requestId = 0
let isMounted = false
let lastLoadPage = 0
let scrollAfterLoad = false
let pollTimer: ReturnType<typeof setTimeout> | undefined
let observedRunningJobId: string | null = null
const refreshedGeocodingJobs = new Set<string>()

const page = computed(() => Math.floor(offset.value / PAGE_SIZE) + 1)
const canGoNext = computed(() => (result.value?.items.length ?? 0) === PAGE_SIZE)
const geocodingJob = computed(() => geocodingStatus.value?.job ?? null)
const geocodingControlsDisabled = computed(() => geocodingInitialPending.value || geocodingStarting.value || geocodingStatus.value === null || geocodingJob.value?.state === 'running')

const providerLabels = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  cgr: 'CGR'
} satisfies Record<Provider, string>

const geocodingFailureLabels = {
  run_failed: 'Traitement IGN échoué',
  canceled: 'Exécution interrompue',
  internal_failure: 'Erreur interne'
} satisfies Record<AdminTheaterGeocodingFailureCode, string>

const dateTimeFormatter = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Europe/Paris'
})

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? 'Date indisponible' : dateTimeFormatter.format(date)
}

function geocodingStateLabel(job: AdminTheaterGeocodingJob): string {
  if (job.state === 'succeeded') return 'Terminé avec succès'
  if (job.error_code === 'canceled') return 'Interrompu'
  return 'Échec'
}

function locationKey(item: AdminTheaterLocation): string {
  return `${item.provider}:${item.provider_theater_id}`
}

function domKey(item: AdminTheaterLocation): string {
  return encodeURIComponent(locationKey(item)).replaceAll('%', '-')
}

function ensureDrafts(items: readonly AdminTheaterLocation[]) {
  for (const item of items) {
    const key = locationKey(item)
    drafts.value[key] ??= { latitude: '', longitude: '' }
  }
}

function draftFor(item: AdminTheaterLocation): AdminTheaterLocationCoordinateDraft {
  const key = locationKey(item)
  return drafts.value[key] ?? (drafts.value[key] = { latitude: '', longitude: '' })
}

function statusLabel(item: AdminTheaterLocation): string {
  return item.status === 'ambiguous' ? 'Ambiguë' : 'Introuvable'
}

function formatScore(score: number): string {
  return Number.isFinite(score) ? `${Math.round(score * 100)} %` : 'Non renseigné'
}

function pageQuery(nextPage = page.value) {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    page: nextPage === 1 ? undefined : String(nextPage)
  })
}

function hydrateRoute() {
  const requestedPage = positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1
  const nextOffset = (requestedPage - 1) * PAGE_SIZE
  const safePage = Number.isSafeInteger(nextOffset) ? requestedPage : 1
  offset.value = (safePage - 1) * PAGE_SIZE
  return pageQuery(safePage)
}

async function loadLocations(background = false): Promise<boolean> {
  const currentRequest = ++requestId
  if (!background) pending.value = true
  loadError.value = ''
  try {
    const response = await api.adminTheaterLocations(PAGE_SIZE, offset.value)
    if (!isMounted || currentRequest !== requestId) return false
    ensureDrafts(response.items)
    result.value = response
    if (scrollAfterLoad) {
      scrollAfterLoad = false
      window.scrollTo({ top: 0, behavior: 'smooth' })
    }
    return true
  } catch (error) {
    if (isMounted && currentRequest === requestId) {
      if (!background) result.value = null
      loadError.value = getFrenchAdminApiError(error)
    }
    return false
  } finally {
    if (isMounted && !background && currentRequest === requestId) pending.value = false
  }
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  if (page.value !== lastLoadPage) {
    lastLoadPage = page.value
    await loadLocations()
  }
}

function setItemError(key: string, message: string) {
  itemErrors.value[key] = message
}

function clearDraftError(key: string, field: keyof AdminTheaterLocationCoordinateErrors) {
  const errors = draftErrors.value[key]
  if (errors) delete errors[field]
}

function clearSuccessfulDraft(key: string) {
  delete drafts.value[key]
  delete draftErrors.value[key]
  delete itemErrors.value[key]
}

async function refreshAfterSuccess(key: string) {
  clearSuccessfulDraft(key)
  successMessage.value = 'Coordonnées enregistrées'
  const loaded = await loadLocations(true)
  if (loaded && offset.value > 0 && result.value?.items.length === 0) {
    await router.replace({ query: pageQuery(page.value - 1) })
  }
}

async function refreshLocationsAfterGeocoding() {
  const loaded = await loadLocations(true)
  if (loaded && offset.value > 0 && result.value?.items.length === 0) {
    await router.replace({ query: pageQuery(page.value - 1) })
  }
}

function clearGeocodingPolling() {
  if (pollTimer !== undefined) {
    clearTimeout(pollTimer)
    pollTimer = undefined
  }
}

function scheduleGeocodingPolling() {
  clearGeocodingPolling()
  if (!isMounted || geocodingJob.value?.state !== 'running') return
  pollTimer = setTimeout(() => {
    pollTimer = undefined
    void loadGeocodingStatus(true)
  }, POLL_DELAY)
}

async function applyGeocodingStatus(response: AdminTheaterGeocodingResponse) {
  const nextJob = response.job
  const shouldRefresh = nextJob?.state === 'succeeded'
    && nextJob.id === observedRunningJobId
    && !refreshedGeocodingJobs.has(nextJob.id)

  geocodingStatus.value = response
  if (nextJob?.state === 'running') observedRunningJobId = nextJob.id
  if (!shouldRefresh || !nextJob) return

  refreshedGeocodingJobs.add(nextJob.id)
  observedRunningJobId = null
  await refreshLocationsAfterGeocoding()
}

async function loadGeocodingStatus(fromPolling = false) {
  if (geocodingRequestPending.value) return
  if (!fromPolling) clearGeocodingPolling()
  if (!fromPolling && geocodingStatus.value === null) geocodingInitialPending.value = true
  if (!fromPolling) geocodingError.value = ''
  geocodingRequestPending.value = true
  try {
    const response = await api.adminTheaterGeocodingStatus()
    if (!isMounted) return
    await applyGeocodingStatus(response)
    geocodingError.value = ''
  } catch (error) {
    if (isMounted) geocodingError.value = getFrenchAdminApiError(error)
  } finally {
    if (isMounted) {
      geocodingRequestPending.value = false
      geocodingInitialPending.value = false
      scheduleGeocodingPolling()
    }
  }
}

async function startGeocoding() {
  if (geocodingControlsDisabled.value) return
  clearGeocodingPolling()
  geocodingStarting.value = true
  geocodingError.value = ''
  try {
    const response = await api.adminStartTheaterGeocoding()
    if (!isMounted) return
    await applyGeocodingStatus(response)
  } catch (error) {
    if (!isMounted) return
    if (getApiErrorStatus(error) === 409) {
      await loadGeocodingStatus()
      return
    }
    geocodingError.value = getFrenchAdminApiError(error)
  } finally {
    if (isMounted) {
      geocodingStarting.value = false
      scheduleGeocodingPolling()
    }
  }
}

async function handleMutationError(cause: unknown, key: string) {
  if (getApiErrorStatus(cause) === 409) {
    setItemError(key, 'Cette localisation a changé ou la suggestion n’est plus disponible. La liste a été actualisée. Vérifiez les données, puis réessayez.')
    await loadLocations(true)
    return
  }
  setItemError(key, getFrenchAdminApiError(cause))
}

async function acceptSuggestion(item: AdminTheaterLocation) {
  const key = locationKey(item)
  if (pendingActions.value[key] || !item.can_accept_suggestion) return
  pendingActions.value[key] = 'suggestion'
  setItemError(key, '')
  successMessage.value = ''
  try {
    await api.adminAcceptTheaterLocationSuggestion(item.provider, item.provider_theater_id, {
      expected_updated_at: item.updated_at
    })
    await refreshAfterSuccess(key)
  } catch (error) {
    await handleMutationError(error, key)
  } finally {
    delete pendingActions.value[key]
  }
}

async function saveManualCoordinates(item: AdminTheaterLocation) {
  const key = locationKey(item)
  if (pendingActions.value[key]) return
  const validation = parseAdminTheaterLocationCoordinates(drafts.value[key] ?? { latitude: '', longitude: '' })
  draftErrors.value[key] = validation.errors
  setItemError(key, '')
  successMessage.value = ''
  if (!validation.coordinates) return

  pendingActions.value[key] = 'manual'
  try {
    await api.adminSetManualTheaterLocation(item.provider, item.provider_theater_id, {
      expected_updated_at: item.updated_at,
      ...validation.coordinates
    })
    await refreshAfterSuccess(key)
  } catch (error) {
    await handleMutationError(error, key)
  } finally {
    delete pendingActions.value[key]
  }
}

function changePage(nextOffset: number) {
  if (pending.value || nextOffset < 0 || nextOffset === offset.value) return
  scrollAfterLoad = true
  void router.push({ query: pageQuery(Math.floor(nextOffset / PAGE_SIZE) + 1) })
}

async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  logoutError.value = ''
  try {
    await api.adminLogout()
    await navigateTo('/admin/login')
  } catch (error) {
    logoutError.value = getFrenchAdminApiError(error)
  } finally {
    loggingOut.value = false
  }
}

watch(() => route.query, () => {
  if (isMounted) void applyRoute()
})
onMounted(() => {
  isMounted = true
  void applyRoute()
  void loadGeocodingStatus()
})
onBeforeUnmount(() => {
  isMounted = false
  clearGeocodingPolling()
  requestId += 1
})

useHead({ title: 'Localisations des cinémas - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
    <div class="flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <NuxtLink to="/admin" class="mb-1 inline-flex min-h-11 items-center gap-1 text-sm font-semibold text-muted hover:text-accent">
          <ArrowLeft :size="16" aria-hidden="true" /> Administration
        </NuxtLink>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Localisations des cinémas</h1>
      </div>
      <button type="button" class="inline-flex h-11 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-line-hover disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
        <LoaderCircle v-if="loggingOut" :size="17" class="animate-spin" aria-hidden="true" />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <p class="sr-only" role="status" aria-live="polite">{{ successMessage }}</p>

    <div v-if="logoutError" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <p>{{ logoutError }}</p>
    </div>

    <section class="mt-6 rounded-lg border border-line bg-surface p-5 shadow-sm sm:p-6" aria-labelledby="geocoding-title" :aria-busy="geocodingJob?.state === 'running' || geocodingStarting ? 'true' : undefined">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 id="geocoding-title" class="text-lg font-semibold text-ink">Géocodage IGN</h2>
        <button type="button" class="button-primary h-auto min-h-11" :disabled="geocodingControlsDisabled" @click="startGeocoding">
          <LoaderCircle v-if="geocodingStarting || geocodingJob?.state === 'running'" :size="17" class="animate-spin" aria-hidden="true" />
          <RefreshCw v-else :size="17" aria-hidden="true" />
          Lancer le géocodage
        </button>
      </div>

      <div v-if="geocodingError" class="mt-4 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
        <AlertTriangle :size="19" class="shrink-0" aria-hidden="true" />
        <div class="flex-1">
          <p>{{ geocodingError }}</p>
          <button type="button" class="mt-2 inline-flex min-h-11 items-center gap-2 font-semibold underline underline-offset-2 disabled:opacity-50" :disabled="geocodingRequestPending || geocodingStarting" @click="loadGeocodingStatus()">
            <RefreshCw :size="16" aria-hidden="true" /> Actualiser l’état
          </button>
        </div>
      </div>

      <div v-if="geocodingInitialPending" class="mt-4 flex min-h-11 items-center gap-2 text-sm text-muted" role="status" aria-live="polite">
        <LoaderCircle :size="18" class="animate-spin text-accent" aria-hidden="true" />
        Chargement de l’état du géocodage…
      </div>

      <div v-else-if="geocodingJob?.state === 'running'" class="mt-4 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm" role="status" aria-live="polite">
        <span class="inline-flex items-center gap-2 font-semibold text-amber-800">
          <LoaderCircle :size="18" class="animate-spin" aria-hidden="true" /> Géocodage en cours
        </span>
        <span class="text-muted">Démarré le {{ formatDateTime(geocodingJob.started_at) }}</span>
      </div>

      <div v-else-if="geocodingJob" class="mt-4">
        <div class="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm" role="status" aria-live="polite">
          <span class="font-semibold" :class="geocodingJob.state === 'succeeded' ? 'text-green-700' : 'text-red-700'">{{ geocodingStateLabel(geocodingJob) }}</span>
          <span v-if="geocodingJob.finished_at" class="text-muted">Terminé le {{ formatDateTime(geocodingJob.finished_at) }}</span>
          <span v-if="geocodingJob.state === 'failed' && geocodingJob.error_code" class="font-medium text-red-700">{{ geocodingFailureLabels[geocodingJob.error_code] }}</span>
        </div>
        <dl v-if="geocodingJob.summary" class="mt-4 grid grid-cols-2 gap-x-5 gap-y-3 text-sm sm:grid-cols-4 lg:grid-cols-7">
          <div><dt class="text-muted">Sélectionnés</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.selected }}</dd></div>
          <div><dt class="text-muted">Ignorés</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.skipped }}</dd></div>
          <div><dt class="text-muted">Correspondances</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.matched }}</dd></div>
          <div><dt class="text-muted">Ambigus</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.ambiguous }}</dd></div>
          <div><dt class="text-muted">Introuvables</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.not_found }}</dd></div>
          <div><dt class="text-muted">Échecs</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.failed }}</dd></div>
          <div><dt class="text-muted">Enregistrés</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ geocodingJob.summary.written }}</dd></div>
        </dl>
      </div>

      <p v-else-if="!geocodingError" class="mt-4 text-sm text-muted">Aucun géocodage enregistré.</p>
    </section>

    <div v-if="loadError" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <div class="flex-1">
        <p>{{ loadError }}</p>
        <button type="button" class="mt-2 inline-flex min-h-11 items-center gap-2 font-semibold underline underline-offset-2 disabled:opacity-50" :disabled="pending" @click="loadLocations()">
          <RefreshCw :size="16" aria-hidden="true" /> Réessayer
        </button>
      </div>
    </div>

    <div v-if="pending" class="state-panel mt-6" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement des localisations…</p>
    </div>

    <template v-else-if="result">
      <div v-if="!result.items.length" class="state-panel mt-6">
        <Check :size="30" class="text-accent" aria-hidden="true" />
        <p>Aucune localisation à traiter.</p>
      </div>

      <ul v-else class="mt-6 space-y-5" aria-label="Localisations de cinémas à traiter">
        <li v-for="item in result.items" :key="locationKey(item)" class="rounded-lg border border-line bg-surface p-5 shadow-sm sm:p-6">
          <article :aria-labelledby="`theater-${domKey(item)}`">
            <div class="flex flex-wrap items-start justify-between gap-3 border-b border-line pb-4">
              <div class="min-w-0">
                <p class="text-xs font-semibold uppercase tracking-wide text-muted">{{ providerLabels[item.provider] }} · {{ item.provider_theater_id }}</p>
                <h2 :id="`theater-${domKey(item)}`" class="mt-1 text-lg font-semibold text-ink">{{ item.name }}</h2>
                <p class="mt-1 flex items-start gap-2 text-sm text-muted">
                  <MapPin :size="17" class="mt-0.5 shrink-0" aria-hidden="true" />
                  <span>{{ item.address }}, {{ item.postal_code }} {{ item.city }}</span>
                </p>
              </div>
              <span class="rounded-full px-2.5 py-1 text-xs font-semibold" :class="item.status === 'ambiguous' ? 'bg-amber-100 text-amber-800' : 'bg-subtle text-muted'">{{ statusLabel(item) }}</span>
            </div>

            <section class="border-b border-line py-4" :aria-labelledby="`suggestion-${domKey(item)}`">
              <h3 :id="`suggestion-${domKey(item)}`" class="text-sm font-semibold text-ink">Suggestion IGN</h3>
              <template v-if="item.suggestion">
                <p class="mt-2 text-sm text-ink">{{ item.suggestion.label }}</p>
                <dl class="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-3">
                  <div><dt class="text-muted">Score</dt><dd class="font-semibold tabular-nums text-ink">{{ formatScore(item.suggestion.score) }}</dd></div>
                  <div v-if="item.suggestion.latitude !== null"><dt class="text-muted">Latitude</dt><dd class="font-semibold tabular-nums text-ink">{{ item.suggestion.latitude }}</dd></div>
                  <div v-if="item.suggestion.longitude !== null"><dt class="text-muted">Longitude</dt><dd class="font-semibold tabular-nums text-ink">{{ item.suggestion.longitude }}</dd></div>
                  <div v-if="item.suggestion.postal_code"><dt class="text-muted">Code postal</dt><dd class="font-semibold text-ink">{{ item.suggestion.postal_code }}</dd></div>
                  <div v-if="item.suggestion.city"><dt class="text-muted">Ville</dt><dd class="font-semibold text-ink">{{ item.suggestion.city }}</dd></div>
                  <div v-if="item.suggestion.type"><dt class="text-muted">Type</dt><dd class="font-semibold text-ink">{{ item.suggestion.type }}</dd></div>
                </dl>
                <button type="button" class="button-primary mt-4 h-auto min-h-11" :disabled="!item.can_accept_suggestion || Boolean(pendingActions[locationKey(item)])" @click="acceptSuggestion(item)">
                  <LoaderCircle v-if="pendingActions[locationKey(item)] === 'suggestion'" :size="17" class="animate-spin" aria-hidden="true" />
                  <Check v-else :size="17" aria-hidden="true" />
                  {{ pendingActions[locationKey(item)] === 'suggestion' ? 'Enregistrement…' : 'Accepter la suggestion' }}
                </button>
                <p v-if="!item.can_accept_suggestion" class="mt-2 text-sm text-muted">Cette suggestion ne contient pas de coordonnées acceptables.</p>
              </template>
              <p v-else class="mt-2 text-sm text-muted">Aucune suggestion enregistrée.</p>
            </section>

            <form class="pt-4" novalidate @submit.prevent="saveManualCoordinates(item)">
              <fieldset :disabled="Boolean(pendingActions[locationKey(item)])">
                <legend class="text-sm font-semibold text-ink">Coordonnées manuelles</legend>
                <div class="mt-3 grid gap-4 sm:grid-cols-2">
                  <div>
                    <label :for="`latitude-${domKey(item)}`" class="mb-1.5 block text-sm font-semibold text-ink">Latitude</label>
                    <input :id="`latitude-${domKey(item)}`" v-model="draftFor(item).latitude" class="field h-11 min-h-11" type="text" inputmode="decimal" autocomplete="off" spellcheck="false" :aria-invalid="draftErrors[locationKey(item)]?.latitude ? 'true' : undefined" :aria-describedby="draftErrors[locationKey(item)]?.latitude ? `latitude-error-${domKey(item)}` : undefined" @input="clearDraftError(locationKey(item), 'latitude')" />
                    <p v-if="draftErrors[locationKey(item)]?.latitude" :id="`latitude-error-${domKey(item)}`" class="mt-1.5 text-sm font-medium text-red-700" role="alert">{{ draftErrors[locationKey(item)]?.latitude }}</p>
                  </div>
                  <div>
                    <label :for="`longitude-${domKey(item)}`" class="mb-1.5 block text-sm font-semibold text-ink">Longitude</label>
                    <input :id="`longitude-${domKey(item)}`" v-model="draftFor(item).longitude" class="field h-11 min-h-11" type="text" inputmode="decimal" autocomplete="off" spellcheck="false" :aria-invalid="draftErrors[locationKey(item)]?.longitude ? 'true' : undefined" :aria-describedby="draftErrors[locationKey(item)]?.longitude ? `longitude-error-${domKey(item)}` : undefined" @input="clearDraftError(locationKey(item), 'longitude')" />
                    <p v-if="draftErrors[locationKey(item)]?.longitude" :id="`longitude-error-${domKey(item)}`" class="mt-1.5 text-sm font-medium text-red-700" role="alert">{{ draftErrors[locationKey(item)]?.longitude }}</p>
                  </div>
                </div>
                <button type="submit" class="button-primary mt-4 h-auto min-h-11" :disabled="Boolean(pendingActions[locationKey(item)])">
                  <LoaderCircle v-if="pendingActions[locationKey(item)] === 'manual'" :size="17" class="animate-spin" aria-hidden="true" />
                  <MapPin v-else :size="17" aria-hidden="true" />
                  {{ pendingActions[locationKey(item)] === 'manual' ? 'Enregistrement…' : 'Enregistrer les coordonnées' }}
                </button>
              </fieldset>
            </form>

            <div v-if="itemErrors[locationKey(item)]" class="mt-4 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
              <AlertTriangle :size="18" class="shrink-0" aria-hidden="true" />
              <p>{{ itemErrors[locationKey(item)] }}</p>
            </div>
          </article>
        </li>
      </ul>

      <nav v-if="offset > 0 || canGoNext" class="mt-8 flex items-center justify-center gap-4 border-t border-line pt-6" aria-label="Pagination des localisations de cinémas">
        <button type="button" class="h-11 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="offset === 0 || pending" @click="changePage(offset - PAGE_SIZE)">Précédent</button>
        <span class="text-sm text-muted" aria-live="polite">Page {{ page }}</span>
        <button type="button" class="h-11 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="!canGoNext || pending" @click="changePage(offset + PAGE_SIZE)">Suivant</button>
      </nav>
    </template>
  </main>
</template>
