<script setup lang="ts">
import {
  ColumnApiModule,
  DateEditorModule,
  DateFilterModule,
  InfiniteRowModelModule,
  LocaleModule,
  NumberEditorModule,
  NumberFilterModule,
  PaginationModule,
  RenderApiModule,
  RowApiModule,
  TextEditorModule,
  TextFilterModule,
  themeQuartz,
  type ColDef,
  type FilterChangedEvent,
  type GridApi,
  type GridReadyEvent,
  type IDatasource,
  type IGetRowsParams,
  type PaginationChangedEvent,
  type SortChangedEvent,
  type ValueSetterParams
} from 'ag-grid-community'
import { AgGridVue } from 'ag-grid-vue3'
import { AlertTriangle, LoaderCircle, RefreshCw } from '@lucide/vue'
import AdminMoviesActionsCell from './AdminMoviesActionsCell.vue'
import AdminMoviesMetadataCell from './AdminMoviesMetadataCell.vue'
import type { AdminMovieField, AdminMovieItem } from '~/types/api'
import {
  ADMIN_MOVIE_PAGE_SIZE,
  ADMIN_MOVIE_ROUTE_KEYS,
  adminMovieFieldLabels,
  adminMovieFieldValue,
  adminMovieGridFilterModel,
  adminMovieQueryFromGrid,
  adminMovieRouteQuery,
  adminMovieRouteStateFromGrid,
  buildAdminMoviePatch,
  isAdminMovieDraftDirty,
  isAdminMovieFieldOverridden,
  parseAdminMovieRouteQuery,
  stageAdminMovieOverride,
  stageAdminMovieRestore,
  validateAdminMovieDraft,
  type AdminMovieDraft,
  type AdminMovieDraftValue,
  type AdminMovieRouteState,
  type AdminMovieValidationErrors
} from '~/utils/adminMovies'
import { mergeOwnedQuery, queriesEqual } from '~/utils/routeQuery'

const modules = [
  InfiniteRowModelModule,
  PaginationModule,
  TextFilterModule,
  NumberFilterModule,
  DateFilterModule,
  TextEditorModule,
  NumberEditorModule,
  DateEditorModule,
  LocaleModule,
  ColumnApiModule,
  RenderApiModule,
  RowApiModule
]

const NARROW_VIEWPORT_QUERY = '(max-width: 767px)'

const gridTheme = themeQuartz.withParams({
  accentColor: '#1F6F78',
  backgroundColor: '#FFFFFF',
  borderColor: '#E4E4E7',
  browserColorScheme: 'light',
  foregroundColor: '#27272A',
  headerBackgroundColor: '#F4F4F5',
  headerTextColor: '#27272A',
  oddRowBackgroundColor: '#FCFAF8',
  rowHoverColor: '#EFF7F8',
  selectedRowBackgroundColor: '#FEF2F2',
  fontFamily: 'ui-sans-serif, system-ui, sans-serif',
  fontSize: 13,
  spacing: 6,
  wrapperBorderRadius: 8
})

const localeText = {
  page: 'Page',
  more: 'Plus',
  to: 'à',
  of: 'sur',
  next: 'Suivant',
  last: 'Dernier',
  first: 'Premier',
  previous: 'Précédent',
  loadingOoo: 'Chargement…',
  noRowsToShow: 'Aucun film',
  filterOoo: 'Filtrer…',
  equals: 'Égal à',
  notEqual: 'Différent de',
  lessThan: 'Inférieur à',
  greaterThan: 'Supérieur à',
  inRange: 'Entre',
  contains: 'Contient',
  notContains: 'Ne contient pas',
  applyFilter: 'Appliquer',
  resetFilter: 'Réinitialiser',
  clearFilter: 'Effacer',
  blank: 'Vide',
  notBlank: 'Non vide'
}

const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()
const gridApi = shallowRef<GridApi<AdminMovieItem> | null>(null)
const routeState = ref<AdminMovieRouteState>(parseAdminMovieRouteQuery(route.query))
const searchInput = ref(routeState.value.q)
const overrideStatusInput = ref(routeState.value.override_status)
const overrideFieldInput = ref<AdminMovieField | ''>(routeState.value.override_field ?? '')
const drafts = ref<Record<string, AdminMovieDraft>>({})
const rowErrors = ref<Record<string, string>>({})
const pendingRows = ref<Record<string, boolean>>({})
const validationErrors = ref<Record<string, AdminMovieValidationErrors>>({})
const selectedDetailsItem = ref<AdminMovieItem | null>(null)
const loadError = ref('')
const conflictMessage = ref('')
const initialLoading = ref(true)
let applyingGridState = false
let mounted = false
let routeDataFingerprint = dataFingerprint(routeState.value)
let requestSequence = 0
let activeRequest: AbortController | null = null
let searchTimer: ReturnType<typeof setTimeout> | undefined
let narrowViewportQuery: MediaQueryList | undefined

const fieldOptions = adminMovieFieldLabels
const selectedDetailsErrors = computed(() => selectedDetailsItem.value ? validationErrors.value[selectedDetailsItem.value.id] ?? {} : {})

const context = {
  formatFieldValue,
  isFieldOverridden: (item: AdminMovieItem, field: AdminMovieField) => isAdminMovieFieldOverridden(item, drafts.value[item.id], field),
  restoreField,
  detailsId,
  isDetailsOpen: (item: AdminMovieItem) => selectedDetailsItem.value?.id === item.id,
  isDirty: (item: AdminMovieItem) => isAdminMovieDraftDirty(drafts.value[item.id]),
  isPending: (item: AdminMovieItem) => Boolean(pendingRows.value[item.id]),
  rowError: (item: AdminMovieItem) => rowErrors.value[item.id] ?? '',
  toggleDetails,
  saveMovie: (item: AdminMovieItem) => void saveMovie(item),
  cancelMovie
}

function inlineColumn(field: 'title' | 'runtime_minutes' | 'release_date', options: Partial<ColDef<AdminMovieItem>> = {}): ColDef<AdminMovieItem> {
  const editor = field === 'runtime_minutes' ? 'agNumberCellEditor' : field === 'release_date' ? 'agDateStringCellEditor' : 'agTextCellEditor'
  return {
    colId: field,
    headerName: adminMovieFieldLabels[field],
    editable: (params) => !pendingRows.value[params.data?.id ?? ''],
    cellEditor: editor,
    cellDataType: field === 'runtime_minutes' ? 'number' : field === 'release_date' ? 'dateString' : 'text',
    valueGetter: (params) => params.data ? adminMovieFieldValue(params.data, drafts.value[params.data.id], field) : null,
    valueSetter: (params) => stageInlineValue(params, field),
    cellRenderer: AdminMoviesMetadataCell,
    cellRendererParams: { field },
    sortable: true,
    ...options
  }
}

const columnDefs: ColDef<AdminMovieItem>[] = [
  inlineColumn('title', { minWidth: 330, flex: 1, pinned: 'left', lockPinned: true }),
  inlineColumn('runtime_minutes', {
    width: 175,
    filter: 'agNumberColumnFilter',
    filterParams: { filterOptions: ['inRange'], defaultOption: 'inRange', maxNumConditions: 1, buttons: ['apply', 'reset'], closeOnApply: true }
  }),
  inlineColumn('release_date', {
    width: 190,
    filter: 'agDateColumnFilter',
    filterParams: { filterOptions: ['inRange'], defaultOption: 'inRange', maxNumConditions: 1, buttons: ['apply', 'reset'], closeOnApply: true }
  }),
  {
    colId: 'genres', headerName: 'Genres', minWidth: 210,
    valueGetter: (params) => params.data ? adminMovieFieldValue(params.data, drafts.value[params.data.id], 'genres') : [],
    valueFormatter: (params) => Array.isArray(params.value) ? params.value.join(', ') : '',
    filter: 'agTextColumnFilter',
    filterParams: { filterOptions: ['contains'], defaultOption: 'contains', maxNumConditions: 1, buttons: ['apply', 'reset'], closeOnApply: true },
    sortable: false
  },
  { colId: 'updated_at', headerName: 'Actualisé', width: 155, valueGetter: (params) => formatDateTime(params.data?.updated_at), sortable: true },
  { colId: 'id', headerName: 'ID', width: 115, field: 'id', sortable: true },
  { colId: 'actions', headerName: 'Actions', width: 350, pinned: 'right', lockPinned: true, sortable: false, filter: false, editable: false, cellRenderer: AdminMoviesActionsCell }
]

const datasource: IDatasource = {
  getRows(params: IGetRowsParams<AdminMovieItem>) {
    activeRequest?.abort()
    const controller = new AbortController()
    activeRequest = controller
    const sequence = ++requestSequence
    loadError.value = ''
    const query = adminMovieQueryFromGrid(routeState.value, params)
    void api.adminMovies(query, controller.signal).then((response) => {
      if (!mounted || controller.signal.aborted || sequence !== requestSequence) return
      for (const item of response.items) {
        if (selectedDetailsItem.value?.id === item.id) selectedDetailsItem.value = item
      }
      initialLoading.value = false
      params.successCallback(response.items, response.total)
    }).catch((error) => {
      if (!mounted || controller.signal.aborted || sequence !== requestSequence) return
      initialLoading.value = false
      loadError.value = getFrenchAdminApiError(Object(error))
      params.failCallback()
    })
  },
  destroy() {
    activeRequest?.abort()
  }
}

function stageInlineValue(params: ValueSetterParams<AdminMovieItem>, field: AdminMovieField): boolean {
  if (!params.data) return false
  let value: AdminMovieDraftValue = params.newValue
  if (field === 'runtime_minutes') value = Number(value)
  if (field === 'release_date' && (value === '' || value === undefined)) value = null
  updateDraft(params.data, field, value)
  return true
}

function updateDraft(item: AdminMovieItem, field: AdminMovieField, value: AdminMovieDraftValue) {
  const draft = stageAdminMovieOverride(item, drafts.value[item.id], field, value)
  setDraft(item, draft)
}

function restoreField(item: AdminMovieItem, field: AdminMovieField) {
  setDraft(item, stageAdminMovieRestore(item, drafts.value[item.id], field))
}

function setDraft(item: AdminMovieItem, draft: AdminMovieDraft) {
  if (isAdminMovieDraftDirty(draft)) drafts.value[item.id] = draft
  else delete drafts.value[item.id]
  validationErrors.value[item.id] = validateAdminMovieDraft(item, drafts.value[item.id])
  delete rowErrors.value[item.id]
  gridApi.value?.refreshCells({ rowNodes: gridApi.value.getRowNode(item.id) ? [gridApi.value.getRowNode(item.id)!] : [], force: true })
}

async function saveMovie(item: AdminMovieItem) {
  if (pendingRows.value[item.id]) return
  const errors = validateAdminMovieDraft(item, drafts.value[item.id])
  validationErrors.value[item.id] = errors
  const firstError = Object.values(errors)[0]
  if (firstError) {
    rowErrors.value[item.id] = firstError
    gridApi.value?.refreshCells({ force: true })
    return
  }
  const patch = buildAdminMoviePatch(item, drafts.value[item.id])
  if (!patch) return
  pendingRows.value[item.id] = true
  delete rowErrors.value[item.id]
  conflictMessage.value = ''
  gridApi.value?.refreshCells({ force: true })
  try {
    const updated = await api.adminUpdateMovie(item.id, patch)
    delete drafts.value[item.id]
    delete validationErrors.value[item.id]
    if (selectedDetailsItem.value?.id === item.id) selectedDetailsItem.value = updated
    gridApi.value?.getRowNode(item.id)?.setData(updated)
  } catch (error) {
    const failure = Object(error)
    if (getApiErrorStatus(failure) === 409) {
      delete drafts.value[item.id]
      delete validationErrors.value[item.id]
      if (selectedDetailsItem.value?.id === item.id) selectedDetailsItem.value = null
      conflictMessage.value = 'Ce film a changé. La liste a été actualisée.'
      gridApi.value?.purgeInfiniteCache()
    } else {
      rowErrors.value[item.id] = getFrenchAdminApiError(failure)
    }
  } finally {
    delete pendingRows.value[item.id]
    gridApi.value?.refreshCells({ force: true })
  }
}

function cancelMovie(item: AdminMovieItem) {
  delete drafts.value[item.id]
  delete validationErrors.value[item.id]
  delete rowErrors.value[item.id]
  gridApi.value?.refreshCells({ force: true })
}

function toggleDetails(item: AdminMovieItem) {
  selectedDetailsItem.value = selectedDetailsItem.value?.id === item.id ? null : item
}

function detailsId(item: AdminMovieItem): string {
  return `admin-movie-details-${item.id}`
}

function detailValue(field: AdminMovieField): string {
  const item = selectedDetailsItem.value
  if (!item) return ''
  const value = adminMovieFieldValue(item, drafts.value[item.id], field)
  if (Array.isArray(value)) return value.join('\n')
  return value === null ? '' : String(value)
}

function updateDetail(field: AdminMovieField, event: Event) {
  const item = selectedDetailsItem.value
  // SAFETY: updateDetail is attached only to input and textarea input events.
  const target = event.target as HTMLInputElement | HTMLTextAreaElement
  if (!item) return
  const raw = target.value
  const value: AdminMovieDraftValue = field === 'genres'
    ? raw.split('\n').map((genre) => genre.trim()).filter(Boolean)
    : raw === '' ? null : raw
  updateDraft(item, field, value)
}

function automaticValue(item: AdminMovieItem, field: AdminMovieField): string {
  return formatFieldValue(field, item.automatic[field]) || 'Non renseigné'
}

function formatFieldValue(field: AdminMovieField, value: AdminMovieDraftValue): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) return value.join(', ')
  if (field === 'runtime_minutes') return `${value} min`
  return String(value)
}

function formatDateTime(value: string | undefined): string {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('fr-FR', { dateStyle: 'short', timeStyle: 'short', timeZone: 'Europe/Paris' }).format(date)
}

function onGridReady(event: GridReadyEvent<AdminMovieItem>) {
  gridApi.value = event.api
  applyGridState(routeState.value)
  applyResponsiveColumnPinning(narrowViewportQuery?.matches ?? window.matchMedia(NARROW_VIEWPORT_QUERY).matches)
  event.api.setGridOption('datasource', datasource)
  event.api.paginationGoToPage(routeState.value.page - 1)
  nextTick(() => { applyingGridState = false })
}

function applyResponsiveColumnPinning(narrow: boolean) {
  gridApi.value?.applyColumnState({
    state: [
      { colId: 'title', pinned: narrow ? null : 'left' },
      { colId: 'actions', pinned: narrow ? null : 'right' }
    ]
  })
}

function onViewportWidthChange(event: MediaQueryListEvent) {
  applyResponsiveColumnPinning(event.matches)
}

function applyGridState(state: AdminMovieRouteState) {
  const currentApi = gridApi.value
  if (!currentApi) return
  applyingGridState = true
  currentApi.applyColumnState({
    defaultState: { sort: null },
    state: [{ colId: state.sort, sort: state.direction }]
  })
  currentApi.setFilterModel(adminMovieGridFilterModel(state))
}

function onSortOrFilterChanged(_event: SortChangedEvent<AdminMovieItem> | FilterChangedEvent<AdminMovieItem>) {
  if (applyingGridState || !gridApi.value) return
  const next = adminMovieRouteStateFromGrid(routeState.value, gridApi.value.getColumnState().flatMap((column) => column.sort ? [{ colId: column.colId, sort: column.sort }] : []), gridApi.value.getFilterModel())
  void replaceRoute(next)
}

function onPaginationChanged(event: PaginationChangedEvent<AdminMovieItem>) {
  if (applyingGridState || !event.newPage || !gridApi.value) return
  const nextPage = gridApi.value.paginationGetCurrentPage() + 1
  if (nextPage !== routeState.value.page) void pushRoute({ ...routeState.value, page: nextPage })
}

function updateSearch() {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    void replaceRoute({ ...routeState.value, q: searchInput.value.trim(), page: 1 })
  }, 350)
}

function updateOverrides() {
  const status = overrideStatusInput.value
  if (status === 'automatic') overrideFieldInput.value = ''
  void replaceRoute({
    ...routeState.value,
    override_status: status,
    override_field: status === 'automatic' ? undefined : overrideFieldInput.value || undefined,
    page: 1
  })
}

function routeQuery(state: AdminMovieRouteState) {
  const ownedValues = Object.fromEntries(Object.entries(adminMovieRouteQuery(state)))
  return mergeOwnedQuery(route.query, ADMIN_MOVIE_ROUTE_KEYS, ownedValues)
}

async function replaceRoute(state: AdminMovieRouteState) {
  const query = routeQuery(state)
  if (!queriesEqual(route.query, query)) await router.replace({ query })
}

async function pushRoute(state: AdminMovieRouteState) {
  const query = routeQuery(state)
  if (!queriesEqual(route.query, query)) await router.push({ query })
}

async function applyRoute() {
  const nextState = parseAdminMovieRouteQuery(route.query)
  const ownedValues = Object.fromEntries(Object.entries(adminMovieRouteQuery(nextState)))
  const canonical = mergeOwnedQuery(route.query, ADMIN_MOVIE_ROUTE_KEYS, ownedValues)
  if (!queriesEqual(route.query, canonical)) {
    await router.replace({ query: canonical })
    return
  }
  const previousDataFingerprint = routeDataFingerprint
  routeState.value = nextState
  routeDataFingerprint = dataFingerprint(nextState)
  searchInput.value = nextState.q
  overrideStatusInput.value = nextState.override_status
  overrideFieldInput.value = nextState.override_field ?? ''
  if (!gridApi.value) return
  applyGridState(nextState)
  if (previousDataFingerprint !== routeDataFingerprint) gridApi.value.purgeInfiniteCache()
  gridApi.value.paginationGoToPage(nextState.page - 1)
  nextTick(() => { applyingGridState = false })
}

function retryLoad() {
  loadError.value = ''
  initialLoading.value = true
  gridApi.value?.purgeInfiniteCache()
}

function dataFingerprint(state: AdminMovieRouteState): string {
  return JSON.stringify({ ...state, page: 1 })
}

watch(() => route.query, () => {
  if (mounted) void applyRoute()
})

onMounted(() => {
  mounted = true
  narrowViewportQuery = window.matchMedia(NARROW_VIEWPORT_QUERY)
  narrowViewportQuery.addEventListener('change', onViewportWidthChange)
  applyResponsiveColumnPinning(narrowViewportQuery.matches)
  void applyRoute()
})

onBeforeUnmount(() => {
  mounted = false
  narrowViewportQuery?.removeEventListener('change', onViewportWidthChange)
  activeRequest?.abort()
  if (searchTimer !== undefined) clearTimeout(searchTimer)
})
</script>

<template>
  <section aria-label="Éditeur des métadonnées de films">
    <div class="sticky top-0 z-20 mb-3 flex flex-wrap items-end gap-3 border-y border-line bg-canvas/95 py-3 backdrop-blur">
      <label class="min-w-64 flex-1 text-sm font-semibold text-ink">
        Rechercher un titre
        <input v-model="searchInput" class="field mt-1.5" type="search" autocomplete="off" @input="updateSearch">
      </label>
      <label class="text-sm font-semibold text-ink">
        État
        <select v-model="overrideStatusInput" class="field mt-1.5 min-w-40" @change="updateOverrides">
          <option value="all">Tous</option>
          <option value="overridden">Avec valeur manuelle</option>
          <option value="automatic">Entièrement automatiques</option>
        </select>
      </label>
      <label class="text-sm font-semibold text-ink">
        Champ modifié
        <select v-model="overrideFieldInput" class="field mt-1.5 min-w-48" :disabled="overrideStatusInput === 'automatic'" @change="updateOverrides">
          <option value="">Tous les champs</option>
          <option v-for="(label, field) in fieldOptions" :key="field" :value="field">{{ label }}</option>
        </select>
      </label>
    </div>

    <div v-if="conflictMessage" class="mb-3 flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900" role="alert">
      <AlertTriangle :size="18" class="shrink-0" aria-hidden="true" />
      <p>{{ conflictMessage }}</p>
    </div>
    <div v-if="loadError" class="mb-3 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
      <AlertTriangle :size="18" class="shrink-0" aria-hidden="true" />
      <div><p>{{ loadError }}</p><button type="button" class="mt-2 inline-flex items-center gap-1 font-semibold underline" @click="retryLoad"><RefreshCw :size="15" aria-hidden="true" /> Réessayer</button></div>
    </div>
    <div v-if="initialLoading" class="mb-3 flex min-h-12 items-center justify-center gap-2 text-sm text-muted" role="status" aria-live="polite"><LoaderCircle :size="18" class="animate-spin text-accent" aria-hidden="true" /> Chargement des films…</div>

    <div class="h-[620px] min-w-0 overflow-hidden rounded-lg border border-line bg-surface shadow-sm">
      <AgGridVue
        class="size-full"
        :theme="gridTheme"
        :modules="modules"
        :column-defs="columnDefs"
        :context="context"
        :locale-text="localeText"
        row-model-type="infinite"
        :cache-block-size="ADMIN_MOVIE_PAGE_SIZE"
        :pagination-page-size="ADMIN_MOVIE_PAGE_SIZE"
        :max-blocks-in-cache="4"
        :max-concurrent-datasource-requests="1"
        :pagination="true"
        :pagination-page-size-selector="false"
        :row-height="42"
        :header-height="40"
        :suppress-multi-sort="true"
        :multi-sort-key="undefined"
        :get-row-id="({ data }: { data: AdminMovieItem }) => data.id"
        :enable-cell-text-selection="true"
        @grid-ready="onGridReady"
        @sort-changed="onSortOrFilterChanged"
        @filter-changed="onSortOrFilterChanged"
        @pagination-changed="onPaginationChanged"
      />
    </div>

    <section v-if="selectedDetailsItem" :id="detailsId(selectedDetailsItem)" class="mt-4 border-y border-line bg-surface py-5" :aria-labelledby="`${detailsId(selectedDetailsItem)}-title`">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 :id="`${detailsId(selectedDetailsItem)}-title`" class="text-lg font-semibold text-ink">Détails - {{ selectedDetailsItem.values.title }}</h2>
        <div class="flex gap-2">
          <button type="button" class="button-primary" :disabled="!isAdminMovieDraftDirty(drafts[selectedDetailsItem.id]) || pendingRows[selectedDetailsItem.id]" @click="saveMovie(selectedDetailsItem)">{{ pendingRows[selectedDetailsItem.id] ? 'Enregistrement…' : 'Enregistrer' }}</button>
          <button type="button" class="h-10 rounded-md border border-line px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="!isAdminMovieDraftDirty(drafts[selectedDetailsItem.id]) || pendingRows[selectedDetailsItem.id]" @click="cancelMovie(selectedDetailsItem)">Annuler</button>
        </div>
      </div>
      <p v-if="rowErrors[selectedDetailsItem.id]" class="mt-3 text-sm font-semibold text-red-700" role="alert">{{ rowErrors[selectedDetailsItem.id] }}</p>

      <div class="mt-4 grid gap-5 lg:grid-cols-2">
        <div v-for="field in (['genres', 'overview', 'poster_url', 'backdrop_url', 'trailer_vf_youtube_key', 'trailer_vo_youtube_key'] as AdminMovieField[])" :key="field" :class="field === 'overview' ? 'lg:col-span-2' : ''">
          <div class="mb-1.5 flex min-h-8 flex-wrap items-center gap-2">
            <label :for="`${detailsId(selectedDetailsItem)}-${field}`" class="text-sm font-semibold text-ink">{{ adminMovieFieldLabels[field] }}</label>
            <span v-if="isAdminMovieFieldOverridden(selectedDetailsItem, drafts[selectedDetailsItem.id], field)" class="inline-flex items-center gap-1 text-xs font-semibold text-primary"><span class="size-2 rounded-full bg-primary" aria-hidden="true" /> Valeur manuelle</span>
            <button v-if="isAdminMovieFieldOverridden(selectedDetailsItem, drafts[selectedDetailsItem.id], field)" type="button" class="ml-auto text-xs font-semibold text-accent underline underline-offset-2" @click="restoreField(selectedDetailsItem, field)">Restaurer la valeur automatique</button>
          </div>
          <textarea v-if="field === 'genres' || field === 'overview'" :id="`${detailsId(selectedDetailsItem)}-${field}`" class="field h-auto min-h-28 py-2" :maxlength="field === 'overview' ? 10000 : undefined" :value="detailValue(field)" :aria-invalid="selectedDetailsErrors[field] ? 'true' : undefined" @input="updateDetail(field, $event)" />
          <input v-else :id="`${detailsId(selectedDetailsItem)}-${field}`" class="field" type="text" :maxlength="field.includes('youtube') ? 11 : 4096" :value="detailValue(field)" :aria-invalid="selectedDetailsErrors[field] ? 'true' : undefined" autocomplete="off" spellcheck="false" @input="updateDetail(field, $event)">
          <p v-if="selectedDetailsErrors[field]" class="mt-1 text-sm font-semibold text-red-700" role="alert">{{ selectedDetailsErrors[field] }}</p>
          <p v-if="isAdminMovieFieldOverridden(selectedDetailsItem, drafts[selectedDetailsItem.id], field)" class="mt-1 text-xs text-muted">Automatique : {{ automaticValue(selectedDetailsItem, field) }}</p>
        </div>
      </div>
    </section>
  </section>
</template>
