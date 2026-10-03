<script setup lang="ts">
import { AlertTriangle, ArrowLeft, Image, RefreshCw } from '@lucide/vue'
import type {
  AdminTheater,
  AdminTheaterImageResult,
  AdminTheatersResponse,
  Provider,
} from '~/types/api'
import {
  cinemaImageErrorMessage,
  cinemaImagePreviewURL,
  cinemaImageURLError,
  clearCinemaImageDraft,
  fetchAdminCinemaImage,
  newCinemaImageDraft,
  selectCinemaImageFile,
  type CinemaImageDraft,
} from '~/utils/adminCinemaImages'
import {
  ADMIN_CINEMA_PAGE_SIZE as PAGE_SIZE,
  ADMIN_CINEMA_SEARCH_DELAY,
  cinemaApiQuery,
  cinemaProviderLabels as providerLabels,
  cinemaRouteQuery,
  normalizeCinemaSearch,
  parseCinemaProvider,
  parseCinemaRoute,
} from '~/utils/adminCinemaFilters'
import { queriesEqual } from '~/utils/routeQuery'

definePageMeta({ middleware: 'admin-auth' })

interface Editor extends CinemaImageDraft {
  pending: 'upload' | 'import' | 'remove' | null
  progress: number | null
  error: string
  confirmation: boolean
  inputVersion: number
  preview: string
  previewRevision: number
  previewState: 'empty' | 'loading' | 'ready' | 'error'
}

const api = useMesSeancesApi()
const config = useRuntimeConfig()
const route = useRoute()
const router = useRouter()
const result = ref<AdminTheatersResponse | null>(null)
const editors = ref<Record<string, Editor>>({})
const pending = ref(true)
const refreshing = ref(false)
const loadError = ref('')
const successMessage = ref('')
const initialFilters = parseCinemaRoute(route.query)
const page = ref(initialFilters.page)
const q = ref(initialFilters.q)
const provider = ref<Provider | ''>(initialFilters.provider)
const search = ref(initialFilters.q)
const filters = computed(() => ({
  page: page.value,
  q: q.value,
  provider: provider.value,
}))
const hasFilters = computed(() => Boolean(q.value || provider.value))
const offset = computed(() => (page.value - 1) * PAGE_SIZE)
const pageCount = computed(() =>
  Math.max(1, Math.ceil((result.value?.total ?? 0) / PAGE_SIZE)),
)
const canGoNext = computed(() => page.value < pageCount.value)
const previewRequests = new Map<string, AbortController>()
const mutations = new Map<string, AbortController>()
let listRequest: AbortController | null = null
let mounted = false
let epoch = 0
let loadedIdentity = ''
let searchTimer: ReturnType<typeof setTimeout> | undefined

function key(item: AdminTheater): string {
  return `${item.provider}:${item.provider_theater_id}`
}

function domKey(item: AdminTheater): string {
  return encodeURIComponent(key(item))
}

function editor(item: AdminTheater): Editor {
  return (editors.value[key(item)] ??= {
    ...newCinemaImageDraft(),
    pending: null,
    progress: null,
    error: '',
    confirmation: false,
    inputVersion: 0,
    preview: '',
    previewRevision: 0,
    previewState: 'empty',
  })
}

function clearCandidate(state: Editor) {
  clearCinemaImageDraft(state)
  state.inputVersion++
}

function clearPreview(state: Editor) {
  if (state.preview) URL.revokeObjectURL(state.preview)
  state.preview = ''
}

function resetPage() {
  epoch++
  listRequest?.abort()
  for (const request of mutations.values()) request.abort()
  for (const request of previewRequests.values()) request.abort()
  mutations.clear()
  previewRequests.clear()
  for (const state of Object.values(editors.value)) {
    clearCandidate(state)
    clearPreview(state)
  }
  editors.value = {}
  result.value = null
  refreshing.value = false
  loadError.value = ''
  successMessage.value = ''
}

function errorMessage(cause: unknown): string {
  // Upload errors carry status/code directly; fetch errors use the existing envelope.
  const code =
    cause instanceof Error && 'code' in cause
      ? String(cause.code)
      : getApiErrorCode(cause)
  return cinemaImageErrorMessage(getApiErrorStatus(cause), code)
}

async function loadPreview(item: AdminTheater, retry = false) {
  const state = editor(item)
  if (
    !retry &&
    state.previewRevision === item.image_revision &&
    (state.previewState === 'ready' || state.previewState === 'loading')
  )
    return
  const itemKey = key(item)
  previewRequests.get(itemKey)?.abort()
  clearPreview(state)
  state.previewRevision = item.image_revision
  state.previewState = item.image ? 'loading' : 'empty'
  if (!item.image) return
  const url = cinemaImagePreviewURL(
    config.public.apiBase,
    item.image.url,
    item.provider,
    item.provider_theater_id,
    item.image_revision,
    window.location.origin,
  )
  if (!url) {
    state.previewState = 'error'
    return
  }
  const request = new AbortController()
  previewRequests.set(itemKey, request)
  const currentEpoch = epoch
  const active = () =>
    mounted &&
    epoch === currentEpoch &&
    !request.signal.aborted &&
    previewRequests.get(itemKey) === request
  try {
    const blob = await fetchAdminCinemaImage(
      config.public.apiBase,
      url,
      request.signal,
    )
    if (!active()) return
    state.preview = URL.createObjectURL(blob)
    state.previewState = 'ready'
  } catch (cause) {
    if (!active()) return
    state.previewState = 'error'
    if (getApiErrorStatus(cause) === 401) await navigateTo('/admin/login')
  } finally {
    if (previewRequests.get(itemKey) === request)
      previewRequests.delete(itemKey)
  }
}

async function loadInventory(background = false): Promise<boolean> {
  listRequest?.abort()
  const request = new AbortController()
  listRequest = request
  const currentEpoch = epoch
  const active = () =>
    mounted &&
    epoch === currentEpoch &&
    !request.signal.aborted &&
    listRequest === request
  refreshing.value = background
  if (!background) pending.value = true
  loadError.value = ''
  try {
    const response = await api.adminTheaters(
      cinemaApiQuery(filters.value),
      request.signal,
    )
    if (!active()) return false
    if (offset.value > 0 && offset.value >= response.total) {
      await router.replace({
        query: pageQuery(Math.max(1, Math.ceil(response.total / PAGE_SIZE))),
      })
      return false
    }
    // A background list response must not roll back a concurrent successful edit.
    const existing = new Map(
      result.value?.items.map((item) => [key(item), item]),
    )
    response.items = response.items.map((item) => {
      const prior = existing.get(key(item))
      return prior && prior.image_revision > item.image_revision
        ? { ...item, image_revision: prior.image_revision, image: prior.image }
        : item
    })
    result.value = response
    const currentKeys = new Set(response.items.map(key))
    for (const [itemKey, state] of Object.entries(editors.value)) {
      if (currentKeys.has(itemKey)) continue
      mutations.get(itemKey)?.abort()
      previewRequests.get(itemKey)?.abort()
      clearCandidate(state)
      clearPreview(state)
      delete editors.value[itemKey]
    }
    for (const item of response.items) {
      const state = editor(item)
      if (!response.imports_enabled && state.source === 'url') {
        // Retain failed input but force a deliberate source change before submitting.
        state.error = cinemaImageErrorMessage(
          503,
          'cinema_image_import_unavailable',
        )
      }
      void loadPreview(item)
    }
    return true
  } catch (cause) {
    if (active()) loadError.value = errorMessage(cause)
    return false
  } finally {
    if (active()) {
      pending.value = false
      refreshing.value = false
    }
  }
}

function pageQuery(nextPage: number) {
  return cinemaRouteQuery({ ...filters.value, page: nextPage }, route.query)
}

async function applyRoute() {
  cancelSearch()
  const next = parseCinemaRoute(route.query)
  search.value = next.q
  const identity = JSON.stringify(next)
  if (identity !== loadedIdentity) {
    resetPage()
    page.value = next.page
    q.value = next.q
    provider.value = next.provider
    loadedIdentity = identity
    pending.value = true
  }
  const canonical = cinemaRouteQuery(next, route.query)
  if (!queriesEqual(route.query, canonical)) {
    await router.replace({ query: canonical })
    return
  }
  if (result.value === null) await loadInventory()
}

function cancelSearch() {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
  searchTimer = undefined
}

async function selectFilters(nextSearch: string, nextProvider: Provider | '') {
  cancelSearch()
  const normalized = normalizeCinemaSearch(nextSearch)
  search.value = normalized
  const query = cinemaRouteQuery(
    { q: normalized, provider: nextProvider, page: 1 },
    route.query,
  )
  if (queriesEqual(route.query, query)) {
    if (result.value === null) await loadInventory()
  } else await router.replace({ query })
}

function changeSearch() {
  cancelSearch()
  // Invalidate immediately, before debounce: departed editors and late responses
  // must not remain actionable while a different inventory is being requested.
  resetPage()
  pending.value = true
  searchTimer = setTimeout(() => {
    searchTimer = undefined
    if (mounted) void selectFilters(search.value, provider.value)
  }, ADMIN_CINEMA_SEARCH_DELAY)
}

function changeProvider(event: Event) {
  if (!(event.target instanceof HTMLSelectElement)) return
  void selectFilters(search.value, parseCinemaProvider(event.target.value))
}

function resetFilters() {
  void selectFilters('', '')
}

function switchSource(item: AdminTheater, source: 'file' | 'url') {
  const state = editor(item)
  if (
    state.pending ||
    (source === 'url' && !result.value?.imports_enabled) ||
    state.source === source
  )
    return
  clearCandidate(state)
  state.source = source
  state.error = ''
  state.confirmation = false
}

function chooseFile(item: AdminTheater, event: Event) {
  const input = event.target
  if (!(input instanceof HTMLInputElement)) return
  const state = editor(item)
  if (state.pending || !mounted) return
  state.error = selectCinemaImageFile(state, input.files?.[0] ?? null)
  if (state.error) input.value = ''
  state.confirmation = false
}

function validateURL(item: AdminTheater) {
  const state = editor(item)
  if (state.url) state.error = cinemaImageURLError(state.url)
}

async function setRemoveConfirmation(item: AdminTheater, visible: boolean) {
  const state = editor(item)
  if (state.pending) return
  state.confirmation = visible
  await nextTick()
  if (mounted && editors.value[key(item)] === state)
    document
      .getElementById(
        `${visible ? 'confirm-remove' : 'remove'}-${domKey(item)}`,
      )
      ?.focus()
}

async function mutate(item: AdminTheater, remove = false) {
  const state = editor(item)
  if (!mounted || state.pending) return
  state.error = ''
  successMessage.value = ''
  if (!remove) {
    if (state.source === 'file' && !state.file)
      state.error = 'Choisissez une image JPEG, PNG ou WebP de 5 Mio maximum.'
    if (state.source === 'url')
      state.error = result.value?.imports_enabled
        ? cinemaImageURLError(state.url)
        : cinemaImageErrorMessage(503, 'cinema_image_import_unavailable')
    if (state.error) return
  } else if (!state.confirmation || !item.image) return
  const itemKey = key(item)
  const request = new AbortController()
  mutations.set(itemKey, request)
  const currentEpoch = epoch
  const active = () =>
    mounted &&
    epoch === currentEpoch &&
    !request.signal.aborted &&
    mutations.get(itemKey) === request
  state.pending = remove
    ? 'remove'
    : state.source === 'file'
      ? 'upload'
      : 'import'
  state.progress = null
  try {
    let response: AdminTheaterImageResult
    if (remove)
      response = await api.adminRemoveTheaterImage(
        item.provider,
        item.provider_theater_id,
        { expected_revision: item.image_revision },
        request.signal,
      )
    else if (state.source === 'file')
      response = await api.adminUploadTheaterImage(
        item.provider,
        item.provider_theater_id,
        state.file!,
        item.image_revision,
        request.signal,
        (percent) => {
          if (active()) state.progress = percent
        },
      )
    else
      response = await api.adminImportTheaterImage(
        item.provider,
        item.provider_theater_id,
        { expected_revision: item.image_revision, url: state.url },
        request.signal,
      )
    if (!active()) return
    const current = result.value?.items.find((row) => key(row) === itemKey)
    if (!current) return
    Object.assign(current, response)
    clearCandidate(state)
    state.confirmation = false
    successMessage.value = `${remove ? 'Image supprimée' : 'Image enregistrée'} : ${current.name}.`
    void loadPreview(current, true)
  } catch (cause) {
    if (!active()) return
    const code =
      cause instanceof Error && 'code' in cause
        ? String(cause.code)
        : getApiErrorCode(cause)
    if (code === 'cinema_image_conflict') {
      const refreshed = await loadInventory(true)
      if (!active()) return
      state.error = refreshed
        ? 'Une autre modification a changé cette image. La liste a été actualisée. Votre sélection est conservée ; vérifiez l’image puis réessayez.'
        : 'Une autre modification a changé cette image. Actualisation impossible. Votre sélection est conservée ; actualisez la liste avant de réessayer.'
      state.confirmation = false
    } else state.error = errorMessage(cause)
  } finally {
    if (active()) {
      state.pending = null
      mutations.delete(itemKey)
    }
  }
}

function changePage(nextPage: number) {
  if (pending.value || nextPage < 1 || nextPage > pageCount.value) return
  cancelSearch()
  void router.push({ query: pageQuery(nextPage) })
}

watch(
  () => route.query,
  () => {
    if (mounted) void applyRoute()
  },
  { flush: 'sync' },
)
onMounted(() => {
  mounted = true
  void applyRoute()
})
onBeforeRouteLeave(() => {
  mounted = false
  cancelSearch()
  resetPage()
})
onBeforeUnmount(() => {
  mounted = false
  cancelSearch()
  resetPage()
})
useHead({ title: 'Images des cinémas - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-10 lg:px-8">
    <header class="border-b-2 border-ink pb-6">
      <NuxtLink
        to="/admin"
        class="mb-1 inline-flex min-h-11 items-center gap-1 font-mono text-xs font-bold text-ink underline underline-offset-4 hover:text-primary"
      >
        <ArrowLeft :size="16" aria-hidden="true" />
        Administration
      </NuxtLink>
      <h1 class="editorial-title">Images des cinémas</h1>
    </header>

    <div class="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-3">
      <div class="sm:col-span-2">
        <label
          for="cinema-search"
          class="mb-2 block text-sm font-semibold text-ink"
          >Rechercher un cinéma</label
        >
        <input
          id="cinema-search"
          v-model="search"
          type="search"
          maxlength="1024"
          placeholder="Nom ou ville"
          class="editorial-field min-h-11"
          @input="changeSearch"
        >
      </div>
      <div>
        <label
          for="cinema-provider"
          class="mb-2 block text-sm font-semibold text-ink"
          >Enseigne</label
        >
        <select
          id="cinema-provider"
          :value="provider"
          class="editorial-field min-h-11"
          @change="changeProvider"
        >
          <option value="">Toutes les enseignes</option>
          <option
            v-for="(label, value) in providerLabels"
            :key="value"
            :value="value"
          >
            {{ label }}
          </option>
        </select>
      </div>
    </div>
    <button
      v-if="hasFilters && (pending || !result || result.items.length)"
      type="button"
      class="editorial-button-outline mt-4"
      @click="resetFilters"
    >
      Réinitialiser les filtres
    </button>

    <p
      v-if="successMessage"
      class="mt-4 text-sm font-semibold text-accent"
      role="status"
      aria-live="polite"
    >
      {{ successMessage }}
    </p>
    <div
      v-if="loadError"
      class="editorial-alert mt-6 flex items-start gap-3 p-4"
      role="alert"
    >
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <div>
        <p>{{ loadError }}</p>
        <button
          type="button"
          class="mt-2 inline-flex min-h-11 items-center gap-2 font-semibold underline underline-offset-2 disabled:opacity-50"
          :disabled="pending || refreshing"
          @click="loadInventory(Boolean(result))"
        >
          <RefreshCw :size="16" aria-hidden="true" />
          Réessayer
        </button>
      </div>
    </div>

    <section
      v-if="pending"
      class="mt-6 space-y-5"
      aria-label="Chargement des cinémas"
      aria-busy="true"
      role="status"
    >
      <p class="sr-only">Chargement des cinémas…</p>
      <div
        v-for="n in 3"
        :key="n"
        class="grid gap-5 border-2 border-ink bg-surface p-5 sm:grid-cols-[220px_1fr]"
        aria-hidden="true"
      >
        <div class="h-40 bg-subtle motion-safe:animate-pulse" />
        <div class="space-y-4">
          <div class="h-6 w-2/3 bg-subtle" />
          <div class="h-4 w-1/2 bg-subtle" />
          <div class="h-11 bg-subtle" />
        </div>
      </div>
    </section>

    <template v-else-if="result">
      <div class="mt-6 flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm text-muted" role="status" aria-live="polite">
          {{ result.total }} cinéma{{ result.total > 1 ? 's' : '' }}
          {{ hasFilters ? (result.total > 1 ? 'trouvés' : 'trouvé') : '' }}
        </p>
        <button
          type="button"
          class="editorial-button-outline"
          :disabled="refreshing"
          :aria-busy="refreshing"
          @click="loadInventory(true)"
        >
          <RefreshCw :size="16" aria-hidden="true" />
          {{ refreshing ? 'Actualisation…' : 'Actualiser la liste' }}
        </button>
      </div>
      <p v-if="!result.imports_enabled" class="mt-4 text-sm text-ink">
        Import par lien indisponible. Utilisez un fichier.
      </p>
      <EditorialStatePanel
        v-if="!result.items.length"
        class="mt-6"
        size="compact"
        shadow="small"
      >
        <Image :size="30" aria-hidden="true" />
        <p>
          {{
            hasFilters ? 'Aucun cinéma ne correspond aux filtres.' : 'Aucun cinéma disponible.'
          }}
        </p>
        <button
          v-if="hasFilters"
          type="button"
          class="editorial-button-outline"
          @click="resetFilters"
        >
          Réinitialiser les filtres
        </button>
      </EditorialStatePanel>
      <ul v-else class="mt-5 space-y-5" aria-label="Images des cinémas">
        <li
          v-for="item in result.items"
          :key="key(item)"
          class="border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] sm:p-6"
        >
          <article
            :aria-labelledby="`cinema-${domKey(item)}`"
            :aria-busy="Boolean(editor(item).pending)"
          >
            <header class="mb-5 border-b border-ink/30 pb-4">
              <h2
                :id="`cinema-${domKey(item)}`"
                class="editorial-heading wrap-anywhere"
              >
                {{ item.name }}
              </h2>
              <p class="mt-1 text-sm font-semibold">
                {{ providerLabels[item.provider] }}
              </p>
              <p class="mt-1 text-sm text-muted wrap-anywhere">
                {{ item.address }}<span v-if="item.address">, </span>
                {{ item.postal_code }} {{ item.city }}
              </p>
            </header>
            <div class="grid gap-5 sm:grid-cols-[220px_minmax(0,1fr)]">
              <div class="min-w-0">
                <div
                  class="flex h-44 items-center justify-center bg-subtle p-2 text-center text-sm text-muted"
                >
                  <img
                    v-if="editor(item).previewState === 'ready'"
                    :src="editor(item).preview"
                    :alt="`Image enregistrée de ${item.name}`"
                    class="h-full w-full object-contain"
                    @error="editor(item).previewState = 'error'"
                  >
                  <div
                    v-else-if="editor(item).previewState === 'loading'"
                    class="h-full w-full bg-line motion-safe:animate-pulse"
                    role="status"
                  >
                    <span class="sr-only">Chargement de l’image…</span>
                  </div>
                  <p
                    v-else-if="editor(item).previewState === 'error'"
                    role="status"
                  >
                    Image indisponible.
                  </p>
                  <p v-else>Aucune image</p>
                </div>
                <p
                  v-if="item.image"
                  class="mt-2 text-xs text-muted tabular-nums"
                >
                  WebP, {{ item.image.width }} × {{ item.image.height }} px,
                  {{ Math.ceil(item.image.size_bytes / 1024) }} Kio
                </p>
                <button
                  v-if="editor(item).previewState === 'error'"
                  type="button"
                  class="mt-2 min-h-11 text-sm font-semibold underline underline-offset-2"
                  @click="loadPreview(item, true)"
                >
                  Recharger l’image
                </button>
              </div>

              <div class="min-w-0">
                <form novalidate @submit.prevent="mutate(item)">
                  <fieldset :disabled="Boolean(editor(item).pending)">
                    <legend class="sr-only">Image de {{ item.name }}</legend>
                    <div class="mb-4 flex flex-wrap gap-2">
                      <button
                        type="button"
                        :class="editor(item).source === 'file' ? 'editorial-button' : 'editorial-button-outline'"
                        :aria-pressed="editor(item).source === 'file'"
                        @click="switchSource(item, 'file')"
                      >
                        Fichier
                      </button>
                      <button
                        type="button"
                        :class="editor(item).source === 'url' ? 'editorial-button' : 'editorial-button-outline'"
                        :aria-pressed="editor(item).source === 'url'"
                        :disabled="!result.imports_enabled"
                        @click="switchSource(item, 'url')"
                      >
                        Lien
                      </button>
                    </div>
                    <label
                      :for="`source-${domKey(item)}`"
                      class="mb-1.5 block text-sm font-semibold"
                      >{{
                        editor(item).source === 'file' ? 'Fichier image' : 'Lien de l’image'
                      }}</label
                    >
                    <input
                      v-if="editor(item).source === 'file'"
                      :id="`source-${domKey(item)}`"
                      :key="editor(item).inputVersion"
                      class="editorial-field"
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      :aria-describedby="`constraints-${domKey(item)} error-${domKey(item)}`"
                      :aria-invalid="Boolean(editor(item).error)"
                      @change="chooseFile(item, $event)"
                    >
                    <input
                      v-else
                      :id="`source-${domKey(item)}`"
                      v-model="editor(item).url"
                      class="editorial-field"
                      type="url"
                      inputmode="url"
                      autocomplete="off"
                      spellcheck="false"
                      :aria-describedby="`constraints-${domKey(item)} error-${domKey(item)}`"
                      :aria-invalid="Boolean(editor(item).error)"
                      @input="editor(item).error = ''"
                      @blur="validateURL(item)"
                    >
                    <p
                      :id="`constraints-${domKey(item)}`"
                      class="mt-2 text-xs text-muted"
                    >
                      JPEG, PNG ou WebP non animé, 5 Mio maximum<span
                        v-if="editor(item).source === 'url'"
                        >. Lien HTTPS direct ; aperçu après enregistrement</span
                      >.
                    </p>
                    <figure v-if="editor(item).candidate" class="mt-4">
                      <img
                        :src="editor(item).candidate"
                        alt="Aperçu du fichier sélectionné"
                        class="max-h-44 w-full object-contain"
                        @error="editor(item).error = 'Aperçu impossible. Vérifiez le fichier ou choisissez une autre image.'"
                      >
                      <figcaption class="mt-1 text-xs text-muted">
                        Fichier sélectionné, non enregistré
                      </figcaption>
                    </figure>
                    <button
                      type="submit"
                      class="editorial-button mt-4 w-full sm:w-auto"
                    >
                      {{
                        item.image ? 'Remplacer l’image' : 'Enregistrer l’image'
                      }}
                    </button>
                  </fieldset>
                </form>

                <div
                  v-if="editor(item).pending"
                  class="mt-4 text-sm"
                  role="status"
                  aria-live="polite"
                >
                  <template
                    v-if="editor(item).pending === 'upload' && editor(item).progress !== 100"
                  >
                    <label :for="`progress-${domKey(item)}`"
                      >Envoi du fichier<span
                        v-if="editor(item).progress !== null"
                      >
                        : {{ editor(item).progress }} %</span
                      >…</label
                    >
                    <progress
                      :id="`progress-${domKey(item)}`"
                      class="mt-2 h-3 w-full accent-accent"
                      :value="editor(item).progress ?? undefined"
                      max="100"
                    />
                  </template>
                  <p v-else>
                    {{
                      editor(item).pending === 'import' ? 'Téléchargement et optimisation…' : editor(item).pending === 'remove' ? 'Suppression…' : 'Optimisation…'
                    }}
                  </p>
                </div>

                <div v-if="item.image" class="mt-4">
                  <div
                    v-if="editor(item).confirmation"
                    class="flex flex-wrap items-center gap-2"
                    @keydown.esc="setRemoveConfirmation(item, false)"
                  >
                    <p class="w-full text-sm font-semibold">
                      Supprimer l’image enregistrée ?
                    </p>
                    <button
                      type="button"
                      :id="`confirm-remove-${domKey(item)}`"
                      class="editorial-button-danger"
                      :disabled="Boolean(editor(item).pending)"
                      @click="mutate(item, true)"
                    >
                      Supprimer
                    </button>
                    <button
                      type="button"
                      class="editorial-button-outline"
                      :disabled="Boolean(editor(item).pending)"
                      @click="setRemoveConfirmation(item, false)"
                    >
                      Annuler
                    </button>
                  </div>
                  <button
                    v-else
                    :id="`remove-${domKey(item)}`"
                    type="button"
                    class="min-h-11 text-sm font-semibold text-primary underline underline-offset-4 disabled:opacity-50"
                    :disabled="Boolean(editor(item).pending)"
                    @click="setRemoveConfirmation(item, true)"
                  >
                    Supprimer l’image
                  </button>
                </div>
                <div :id="`error-${domKey(item)}`">
                  <div
                    v-if="editor(item).error"
                    class="editorial-alert mt-4 flex items-start gap-2 p-3"
                    role="alert"
                  >
                    <AlertTriangle
                      :size="18"
                      class="shrink-0"
                      aria-hidden="true"
                    />
                    <p>{{ editor(item).error }}</p>
                  </div>
                </div>
              </div>
            </div>
          </article>
        </li>
      </ul>

      <nav
        v-if="result.total > PAGE_SIZE"
        class="mt-8 grid grid-cols-2 items-center justify-center gap-4 border-t-2 border-ink pt-6 sm:flex"
        aria-label="Pagination des cinémas"
      >
        <button
          type="button"
          class="editorial-button-outline"
          :disabled="page === 1 || pending"
          @click="changePage(page - 1)"
        >
          Précédent
        </button>
        <span
          class="order-first col-span-2 text-center text-sm text-muted sm:order-none"
          aria-live="polite"
          >Page {{ page }} sur {{ pageCount }}</span
        >
        <button
          type="button"
          class="editorial-button-outline"
          :disabled="!canGoNext || pending"
          @click="changePage(page + 1)"
        >
          Suivant
        </button>
      </nav>
    </template>
  </main>
</template>
