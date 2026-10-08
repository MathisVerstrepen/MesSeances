<script setup lang="ts">
import { Plus, Settings2, X } from '@lucide/vue'
import type { WatchlistViewMode } from '~/types/watchlist'
import { accountDestination } from '~/utils/accountState'
import { groupWatchlistItems } from '~/utils/watchlistGrouping'
import { sortWatchlistItems, watchlistSortOptions } from '~/utils/watchlistSort'
import { sortWatchlistTags, watchlistTagPalette } from '~/utils/watchlistTags'

definePageMeta({ middleware: 'account-auth' })
useHead({ title: 'Watchlist - MesSeances' })
const account = useAccountSession()
const watchlist = useWatchlist()
const screenings = useWatchlistScreenings()
const {
  query,
  searchResults,
  searchError,
  searching,
  items,
  tags,
  sortOrder,
  ready,
  writesBlocked,
  error,
  owner,
} = watchlist
const softRefresh = computed(
  () =>
    account.revalidating.value &&
    ready.value &&
    !watchlist.saving.value &&
    !searching.value &&
    !error.value &&
    !searchError.value,
)
const selectedTag = computed(() => watchlist.filterTagId.value ?? '')
const displayMode = watchlist.viewMode
const openTagEditor = ref('')
const tagScope = ref(0)
const preferences = useTemplateRef('preferences')
let tagInteraction = 0
// Unlike picker identity changes, our own committed pair must not cancel focus recovery.
let preferenceInteraction = 0
function interactWithTags() {
  tagInteraction++
  preferenceInteraction++
}
const sortedTags = computed(() => sortWatchlistTags(tags.value))
const sortedItems = computed(() =>
  sortOrder.value
    ? sortWatchlistItems(
        items.value.filter(
          (item) =>
            !selectedTag.value || item.tag_ids.includes(selectedTag.value),
        ),
        sortOrder.value,
      )
    : [],
)
const savedSections = computed(() =>
  displayMode.value === 'tags'
    ? groupWatchlistItems(
        sortedItems.value,
        sortedTags.value,
        selectedTag.value,
      )
    : [{ id: 'list', name: '', color: null, items: sortedItems.value }],
)
const rowKey = (sectionId: string, slug: string) => `${sectionId}:${slug}`
watch([selectedTag, displayMode], () => {
  openTagEditor.value = ''
  tagInteraction++
})
watch(savedSections, () => {
  if (
    !savedSections.value.some((section) =>
      section.items.some(
        (item) => rowKey(section.id, item.slug) === openTagEditor.value,
      ),
    )
  )
    openTagEditor.value = ''
})

async function assignTag(
  slug: string,
  tagId: string,
  assigned: boolean,
  input: HTMLInputElement,
) {
  const scope = tagScope.value
  const interaction = tagInteraction
  const focused = document.activeElement === input
  await watchlist.assignTag(slug, tagId, assigned)
  await nextTick()
  if (
    !focused ||
    scope !== tagScope.value ||
    interaction !== tagInteraction ||
    document.activeElement !== document.body
  )
    return
  if (!input.isConnected) {
    if (isDesktop.value) preferences.value?.focusFilter()
    else configurationTrigger.value?.focus({ preventScroll: true })
  } else if (input.isConnected && !input.disabled)
    input.focus({ preventScroll: true })
}

async function changeSort(event: Event) {
  const select = event.target
  if (!(select instanceof HTMLSelectElement)) return
  const requested = watchlistSortOptions.find(
    (option) => option.value === select.value,
  )
  // Restore synchronously even on rejection: Vue's bound value may not change.
  // No delayed DOM cleanup can touch a control belonging to a replacement owner.
  select.value = sortOrder.value ?? ''
  if (!requested) return
  const scope = watchlist.scopeKey.value
  const focused = document.activeElement === select
  await watchlist.saveSort(requested.value)
  await nextTick()
  // Disabling a native select drops keyboard focus. Restore only its own scope,
  // never steal focus after navigation, an owner change, or another interaction.
  if (
    focused &&
    scope === watchlist.scopeKey.value &&
    select.isConnected &&
    !select.disabled &&
    select.checkVisibility() &&
    document.activeElement === document.body
  )
    select.focus({ preventScroll: true })
}

async function changePreferences(
  mode: WatchlistViewMode,
  tagId: string | null,
  control: HTMLSelectElement | HTMLButtonElement,
) {
  if (
    writesBlocked.value ||
    (mode === displayMode.value && tagId === watchlist.filterTagId.value)
  )
    return
  const scope = tagScope.value
  const interaction = preferenceInteraction
  const focused = document.activeElement === control
  await watchlist.savePreferences(mode, tagId)
  await nextTick()
  if (
    focused &&
    scope === tagScope.value &&
    interaction === preferenceInteraction &&
    control.isConnected &&
    !control.disabled &&
    control.checkVisibility() &&
    document.activeElement === document.body
  )
    control.focus({ preventScroll: true })
}

function changeFilter(event: Event) {
  const select = event.target
  if (!(select instanceof HTMLSelectElement)) return
  const requested = select.value || null
  select.value = selectedTag.value
  const mode = displayMode.value
  if (!mode) return
  return changePreferences(mode, requested, select)
}

function changeDisplay(mode: WatchlistViewMode, event: Event) {
  const button = event.currentTarget
  const tagId = watchlist.filterTagId.value
  if (!(button instanceof HTMLButtonElement) || tagId === undefined) return
  return changePreferences(mode, tagId, button)
}

function clearFilter(event: Event) {
  const button = event.currentTarget
  const mode = displayMode.value
  if (!(button instanceof HTMLButtonElement) || !mode) return
  return changePreferences(mode, null, button)
}
const searchInput = useTemplateRef('searchInput')
const searchDialog = useTemplateRef('searchDialog')
const addTrigger = useTemplateRef('addTrigger')
const configurationDialog = useTemplateRef('configurationDialog')
const configurationTrigger = useTemplateRef('configurationTrigger')
const configurationClose = useTemplateRef('configurationClose')
const removalDialog = useTemplateRef('removalDialog')
const removalCancel = useTemplateRef('removalCancel')
const removalTarget = ref<{
  slug: string
  title: string
  scope: number
  username: string
} | null>(null)
const removing = ref(false)
const removalAttempted = ref(false)
let removalOpener: HTMLButtonElement | null = null
let removalVersion = 0
const catalogTab = useTemplateRef('catalogTab')
const externalTab = useTemplateRef('externalTab')
const resultsScroll = useTemplateRef('resultsScroll')
const panelOpen = ref(false)
const configurationOpen = ref(false)
const isDesktop = ref(true)
const activeTab = ref<'catalog' | 'external'>('catalog')
const panelHeight = ref(0)
const panelTop = ref(0)
const panelBottom = ref(0)
let interaction = 0
let active = true
let bodyOverflow: string | null = null
let desktop: MediaQueryList | null = null

function positionPanel() {
  panelHeight.value = window.visualViewport?.height ?? window.innerHeight
  panelTop.value = window.visualViewport?.offsetTop ?? 0
  panelBottom.value = Math.max(
    0,
    window.innerHeight - panelTop.value - panelHeight.value,
  )
}

function lockScroll() {
  if (bodyOverflow !== null) return
  bodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
}

function unlockScroll() {
  if (bodyOverflow === null) return
  document.body.style.overflow = bodyOverflow
  bodyOverflow = null
}

function restoreTrigger(trigger: HTMLButtonElement | null, current: number) {
  void nextTick(() => {
    if (
      active &&
      current === interaction &&
      trigger?.isConnected &&
      !trigger.disabled &&
      trigger.checkVisibility()
    )
      trigger.focus({ preventScroll: true })
  })
}

function closeRemoval(restoreFocus = true) {
  if (!removalTarget.value) return
  const opener = removalOpener
  const current = ++interaction
  removalVersion++
  // Removing the dialog on privacy/navigation boundaries must not run native focus restoration.
  if (restoreFocus) removalDialog.value?.close()
  removalTarget.value = null
  removalOpener = null
  removing.value = false
  removalAttempted.value = false
  unlockScroll()
  if (restoreFocus) {
    void nextTick(() => {
      const control =
        opener?.isConnected && !opener.disabled && opener.checkVisibility()
          ? opener
          : (document.querySelector<HTMLButtonElement>(
              '[data-watchlist-remove]:not(:disabled)',
            ) ?? addTrigger.value)
      restoreTrigger(control, current)
    })
  }
}

async function openRemoval(
  movie: { slug: string; title: string },
  event: Event,
) {
  const button = event.currentTarget
  if (
    !active ||
    writesBlocked.value ||
    !owner.value ||
    !(button instanceof HTMLButtonElement) ||
    !items.value.some((item) => item.slug === movie.slug)
  )
    return
  closeRemoval(false)
  dismiss()
  closeConfiguration(false)
  openTagEditor.value = ''
  tagScope.value++
  const current = ++interaction
  const version = ++removalVersion
  removalOpener = button
  removalTarget.value = {
    slug: movie.slug,
    title: movie.title,
    scope: watchlist.scopeKey.value,
    username: owner.value,
  }
  removalAttempted.value = false
  await nextTick()
  if (
    !active ||
    current !== interaction ||
    version !== removalVersion ||
    !removalTarget.value
  )
    return
  positionPanel()
  removalDialog.value?.showModal()
  lockScroll()
  removalCancel.value?.focus({ preventScroll: true })
}

async function confirmRemoval() {
  const target = removalTarget.value
  if (
    !active ||
    !target ||
    removing.value ||
    removalAttempted.value ||
    writesBlocked.value ||
    target.scope !== watchlist.scopeKey.value ||
    target.username !== owner.value ||
    !items.value.some((item) => item.slug === target.slug)
  )
    return
  const version = removalVersion
  removing.value = true
  removalAttempted.value = true
  const result = await watchlist.save(target.slug, false)
  if (
    !active ||
    version !== removalVersion ||
    removalTarget.value !== target ||
    target.scope !== watchlist.scopeKey.value ||
    target.username !== owner.value
  )
    return
  removing.value = false
  if (result && !items.value.some((item) => item.slug === target.slug))
    closeRemoval()
  else {
    await nextTick()
    if (active && version === removalVersion)
      removalCancel.value?.focus({ preventScroll: true })
  }
}

watch(items, () => {
  if (
    removalTarget.value &&
    !removing.value &&
    !removalAttempted.value &&
    !items.value.some((item) => item.slug === removalTarget.value?.slug)
  )
    closeRemoval()
})

async function openSearch() {
  if (writesBlocked.value || !owner.value || panelOpen.value) return
  closeRemoval(false)
  closeConfiguration(false)
  openTagEditor.value = ''
  const current = ++interaction
  panelOpen.value = true
  activeTab.value = 'catalog'
  await nextTick()
  if (!active || current !== interaction || !panelOpen.value) return
  positionPanel()
  searchDialog.value?.showModal()
  lockScroll()
  searchInput.value?.focus({ preventScroll: true })
}

async function openConfiguration() {
  if (desktop?.matches || !owner.value || configurationOpen.value) return
  closeRemoval(false)
  dismiss()
  openTagEditor.value = ''
  const current = ++interaction
  configurationOpen.value = true
  await nextTick()
  if (!active || current !== interaction || !configurationOpen.value) return
  positionPanel()
  configurationDialog.value?.showModal()
  lockScroll()
  configurationClose.value?.focus({ preventScroll: true })
}

function closeConfiguration(restoreFocus = true) {
  if (!configurationOpen.value) return
  interaction++
  configurationOpen.value = false
  configurationDialog.value?.close()
  unlockScroll()
  if (restoreFocus) restoreTrigger(configurationTrigger.value, interaction)
}

function breakpointChanged() {
  const changed = isDesktop.value !== desktop?.matches
  isDesktop.value = desktop?.matches ?? true
  if (desktop?.matches) closeConfiguration(false)
  if (changed) {
    tagScope.value++
    openTagEditor.value = ''
  }
}

function backdrop(event: MouseEvent, close: () => void) {
  const dialog = event.currentTarget
  if (!(dialog instanceof HTMLDialogElement) || event.target !== dialog) return
  const rect = dialog.getBoundingClientRect()
  if (
    event.clientX < rect.left ||
    event.clientX > rect.right ||
    event.clientY < rect.top ||
    event.clientY > rect.bottom
  )
    close()
}

function dismiss(restoreFocus = false) {
  interaction++
  panelOpen.value = false
  searchDialog.value?.close()
  watchlist.dismissSearch()
  unlockScroll()
  if (restoreFocus) restoreTrigger(addTrigger.value, interaction)
}

async function submitSearch() {
  if (!panelOpen.value || writesBlocked.value || searching.value) return
  const current = ++interaction
  activeTab.value = 'catalog'
  void watchlist.search()
  await nextTick()
  if (!active || current !== interaction || !panelOpen.value) return
  positionPanel()
  catalogTab.value?.focus({ preventScroll: true })
}

function selectTab(tab: 'catalog' | 'external') {
  activeTab.value = tab
  if (resultsScroll.value) resultsScroll.value.scrollTop = 0
}

function tabKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const tab =
    event.key === 'Home'
      ? 'catalog'
      : event.key === 'End'
        ? 'external'
        : activeTab.value === 'catalog'
          ? 'external'
          : 'catalog'
  selectTab(tab)
  ;(tab === 'catalog' ? catalogTab : externalTab).value?.focus()
}

async function addMovie(movie: { slug: string } | { tmdb_id: string }) {
  const current = interaction
  const text = query.value
  const result =
    'slug' in movie
      ? await watchlist.save(movie.slug, true)
      : await watchlist.importMovie(movie.tmdb_id)
  if (current !== interaction || query.value !== text) return
  if (!result) {
    await nextTick()
    if (!active || current !== interaction || query.value !== text) return
    if (resultsScroll.value) resultsScroll.value.scrollTop = 0
    return
  }
  watchlist.clearSearch()
  dismiss(true)
}

function clearPageSearch() {
  closeRemoval(false)
  interaction++
  panelOpen.value = false
  configurationOpen.value = false
  searchDialog.value?.close()
  configurationDialog.value?.close()
  unlockScroll()
  watchlist.clearSearch()
  openTagEditor.value = ''
  tagScope.value++
}

watch(watchlist.scopeKey, clearPageSearch, { flush: 'sync' })
onMounted(() => {
  window.addEventListener('pagehide', clearPageSearch)
  document.addEventListener('pointerdown', interactWithTags)
  document.addEventListener('keydown', interactWithTags)
  desktop = window.matchMedia('(min-width: 1024px)')
  breakpointChanged()
  desktop.addEventListener('change', breakpointChanged)
  window.addEventListener('resize', positionPanel)
  window.addEventListener('scroll', positionPanel, true)
  window.visualViewport?.addEventListener('resize', positionPanel)
  window.visualViewport?.addEventListener('scroll', positionPanel)
})
onBeforeUnmount(() => {
  active = false
  clearPageSearch()
  window.removeEventListener('pagehide', clearPageSearch)
  document.removeEventListener('pointerdown', interactWithTags)
  document.removeEventListener('keydown', interactWithTags)
  desktop?.removeEventListener('change', breakpointChanged)
  window.removeEventListener('resize', positionPanel)
  window.removeEventListener('scroll', positionPanel, true)
  window.visualViewport?.removeEventListener('resize', positionPanel)
  window.visualViewport?.removeEventListener('scroll', positionPanel)
})
onBeforeRouteLeave(clearPageSearch)
</script>

<template>
  <AccountShell
    title="Watchlist"
    account-area
    back-to-account
    hide-logout
    hide-explore
    :class="{ 'soft-refresh': softRefresh }"
  >
    <template #title-actions>
      <button
        v-if="owner"
        ref="addTrigger"
        type="button"
        class="account-primary shrink-0"
        :disabled="writesBlocked"
        aria-haspopup="dialog"
        aria-controls="watchlist-add"
        :aria-expanded="panelOpen"
        @click="openSearch"
      >
        <Plus :size="18" aria-hidden="true" />
        Ajouter
      </button>
    </template>
    <div v-if="owner" class="space-y-6 sm:space-y-10">
      <div
        v-if="error && !panelOpen && !configurationOpen && !removalTarget && !openTagEditor"
        role="alert"
        class="account-alert"
      >
        <p>{{ error }}</p>
        <button
          type="button"
          class="account-link mt-2"
          :disabled="watchlist.saving.value"
          @click="watchlist.retry"
        >
          Réessayer
        </button>
      </div>
      <dialog
        v-if="panelOpen"
        id="watchlist-add"
        ref="searchDialog"
        aria-labelledby="watchlist-add-heading"
        class="m-auto flex w-[calc(100%-2rem)] max-w-2xl flex-col overflow-hidden border-2 border-ink bg-surface p-0 text-ink shadow-lg backdrop:bg-black/60"
        :style="{ maxHeight: panelHeight ? `${panelHeight - 32}px` : 'calc(100dvh - 2rem)', top: `${panelTop + panelHeight / 2}px`, bottom: 'auto', transform: 'translateY(-50%)' }"
        @cancel.prevent="dismiss(true)"
        @click="backdrop($event, () => dismiss(true))"
      >
        <div
          class="flex shrink-0 items-center justify-between gap-3 border-b border-ink/20 p-4"
        >
          <h2 id="watchlist-add-heading" class="account-heading">
            Ajouter un film
          </h2>
          <button
            type="button"
            class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle"
            aria-label="Fermer l’ajout de film"
            @click="dismiss(true)"
          >
            <X :size="20" aria-hidden="true" />
          </button>
        </div>
        <div
          ref="resultsScroll"
          class="min-h-0 overflow-y-auto overscroll-contain"
        >
          <form
            class="p-4"
            :aria-busy="searching"
            @submit.prevent="submitSearch"
          >
            <label for="watchlist-query" class="account-label"
              >Rechercher un film</label
            >
            <div class="flex flex-wrap gap-3">
              <input
                id="watchlist-query"
                ref="searchInput"
                v-model="query"
                type="search"
                autocomplete="off"
                class="account-input min-w-0 flex-1"
                required
                :disabled="!ready"
              >
              <button
                type="submit"
                class="account-primary"
                :disabled="writesBlocked || searching"
              >
                Rechercher
              </button>
            </div>
          </form>
          <div id="watchlist-results" class="flex flex-col">
            <div
              class="flex shrink-0 items-center gap-2 border-b border-ink/20 px-3"
            >
              <div
                role="tablist"
                aria-label="Sources des films"
                class="flex min-w-0 flex-1 gap-2"
                @keydown="tabKeydown"
              >
                <button
                  id="watchlist-catalog-tab"
                  ref="catalogTab"
                  type="button"
                  role="tab"
                  aria-controls="watchlist-catalog-panel"
                  :aria-selected="activeTab === 'catalog'"
                  :tabindex="activeTab === 'catalog' ? 0 : -1"
                  class="min-h-12 border-b-2 px-2 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2"
                  :class="activeTab === 'catalog' ? 'border-primary text-primary' : 'border-transparent text-muted hover:text-ink'"
                  @click="selectTab('catalog')"
                >
                  Catalogue
                </button>
                <button
                  id="watchlist-external-tab"
                  ref="externalTab"
                  type="button"
                  role="tab"
                  aria-controls="watchlist-external-panel"
                  :aria-selected="activeTab === 'external'"
                  :tabindex="activeTab === 'external' ? 0 : -1"
                  class="min-h-12 border-b-2 px-2 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2"
                  :class="activeTab === 'external' ? 'border-primary text-primary' : 'border-transparent text-muted hover:text-ink'"
                  @click="selectTab('external')"
                >
                  Autres films
                </button>
              </div>
            </div>
            <div class="px-4 pb-4" :aria-busy="searching">
              <div v-if="error" role="alert" class="account-alert mt-3">
                <p>{{ error }}</p>
                <button
                  type="button"
                  class="account-link mt-2"
                  :disabled="watchlist.saving.value"
                  @click="watchlist.retry"
                >
                  Réessayer
                </button>
              </div>
              <p v-if="searchError" role="alert" class="account-alert mt-3">
                {{ searchError }}
              </p>
              <p
                v-else-if="!searching && !searchResults"
                role="status"
                class="mt-3 text-sm"
              >
                Recherchez un titre pour ajouter un film.
              </p>
              <p
                v-if="searchResults && (activeTab === 'catalog' || searchResults.external_status === 'ready')"
                role="status"
                class="sr-only"
              >
                {{
                  activeTab === 'catalog' ? searchResults.catalog.length : searchResults.external.length
                }} films trouvés.
              </p>
              <div
                v-if="searching"
                role="status"
                class="mt-3 space-y-3 motion-safe:animate-pulse"
              >
                <span class="sr-only">Recherche des films…</span>
                <div class="h-24 bg-subtle" />
              </div>
              <section
                v-show="activeTab === 'catalog'"
                id="watchlist-catalog-panel"
                role="tabpanel"
                aria-labelledby="watchlist-catalog-tab"
                tabindex="0"
                class="focus-visible:outline-2 focus-visible:outline-offset-2"
              >
                <template v-if="searchResults">
                  <ul v-if="searchResults.catalog.length">
                    <WatchlistMovieRow
                      v-for="movie in searchResults.catalog"
                      :key="movie.slug"
                      :title="movie.title"
                      :slug="movie.slug"
                      :poster-url="movie.poster_url"
                      :release-date="movie.release_date"
                    >
                      <button
                        type="button"
                        class="account-secondary shrink-0"
                        :disabled="writesBlocked || watchlist.slugs.value.has(movie.slug)"
                        :aria-label="watchlist.slugs.value.has(movie.slug) && ready ? `${movie.title} déjà dans la watchlist` : `Ajouter ${movie.title} à la watchlist`"
                        @click="addMovie(movie)"
                      >
                        {{
                          watchlist.slugs.value.has(movie.slug) && ready ? 'Déjà ajouté' : 'Ajouter'
                        }}
                      </button>
                    </WatchlistMovieRow>
                  </ul>
                  <p v-else class="mt-3 text-sm">
                    Aucun film du catalogue. Essayez un autre titre.
                  </p>
                  <p v-if="searchResults.catalog_has_more" class="mt-3 text-sm">
                    D’autres films correspondent. Précisez le titre pour les
                    retrouver.
                  </p>
                </template>
              </section>
              <section
                v-show="activeTab === 'external'"
                id="watchlist-external-panel"
                role="tabpanel"
                aria-labelledby="watchlist-external-tab"
                tabindex="0"
                class="focus-visible:outline-2 focus-visible:outline-offset-2"
              >
                <template v-if="searchResults">
                  <p
                    v-if="searchResults.external_status === 'unavailable'"
                    role="status"
                    class="mt-3 text-sm"
                  >
                    La recherche externe est indisponible. Les films du
                    catalogue restent disponibles. Relancez la recherche plus
                    tard.
                  </p>
                  <p
                    v-else-if="searchResults.external_status === 'disabled'"
                    class="mt-3 text-sm"
                  >
                    La recherche externe n’est pas activée. Choisissez un film
                    du catalogue.
                  </p>
                  <template v-else>
                    <p
                      v-if="searchResults.external.length"
                      class="mt-3 text-sm"
                    >
                      Ajouter un film crée sa fiche publique. Votre watchlist
                      reste privée.
                    </p>
                    <p v-else class="mt-3 text-sm">
                      Aucun autre film trouvé. Essayez un autre titre.
                    </p>
                    <ul>
                      <WatchlistMovieRow
                        v-for="movie in searchResults.external"
                        :key="movie.tmdb_id"
                        :tmdb-id="movie.tmdb_id"
                        :title="movie.title"
                        :poster-url="movie.poster_url"
                        :release-date="movie.release_date"
                      >
                        <button
                          type="button"
                          class="account-secondary shrink-0"
                          :disabled="writesBlocked"
                          :aria-label="`Ajouter ${movie.title} à la watchlist`"
                          @click="addMovie(movie)"
                        >
                          Ajouter
                        </button>
                      </WatchlistMovieRow>
                    </ul>
                  </template>
                </template>
              </section>
            </div>
          </div>
        </div>
      </dialog>
      <dialog
        v-if="removalTarget"
        id="watchlist-remove"
        ref="removalDialog"
        aria-labelledby="watchlist-remove-heading"
        aria-describedby="watchlist-remove-title"
        class="m-auto flex w-[calc(100%-2rem)] max-w-2xl flex-col overflow-hidden border-2 border-ink bg-surface p-0 text-ink shadow-lg backdrop:bg-black/60"
        :style="{ maxHeight: panelHeight ? `${panelHeight - 32}px` : 'calc(100dvh - 2rem)', top: `${panelTop + panelHeight / 2}px`, bottom: 'auto', transform: 'translateY(-50%)' }"
        @cancel.prevent="closeRemoval()"
        @click="backdrop($event, closeRemoval)"
      >
        <div
          class="flex shrink-0 items-center justify-between gap-3 border-b border-ink/20 p-4"
        >
          <h2 id="watchlist-remove-heading" class="account-heading">
            Retirer de la watchlist
          </h2>
          <button
            type="button"
            class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle"
            aria-label="Fermer la confirmation"
            @click="closeRemoval()"
          >
            <X :size="20" aria-hidden="true" />
          </button>
        </div>
        <div class="min-h-0 overflow-y-auto overscroll-contain p-4">
          <p
            id="watchlist-remove-title"
            class="font-bold [overflow-wrap:anywhere]"
          >
            {{ removalTarget.title }}
          </p>
          <div v-if="error" role="alert" class="account-alert mt-4">
            <p>{{ error }}</p>
            <button
              type="button"
              class="account-link mt-2"
              :disabled="watchlist.saving.value || watchlist.loading.value"
              @click="watchlist.retry"
            >
              Réessayer
            </button>
          </div>
          <p v-if="removalAttempted && !removing" class="mt-4 text-sm">
            Vérifiez la watchlist avant de confirmer à nouveau. Fermez cette
            fenêtre pour reprendre.
          </p>
          <div class="mt-5 flex flex-wrap justify-end gap-3">
            <button
              ref="removalCancel"
              type="button"
              class="account-secondary"
              @click="closeRemoval()"
            >
              Annuler
            </button>
            <button
              type="button"
              class="account-primary"
              :disabled="writesBlocked || removing || removalAttempted"
              :aria-busy="removing"
              @click="confirmRemoval"
            >
              Retirer
            </button>
          </div>
        </div>
      </dialog>
      <section aria-labelledby="saved-heading">
        <div class="flex items-center justify-between gap-2 lg:hidden">
          <h2 id="saved-heading" class="shrink-0 text-xl font-bold">
            Mes films
          </h2>
          <div class="flex shrink-0 items-center gap-2">
            <button
              ref="configurationTrigger"
              type="button"
              class="account-secondary size-12 p-0!"
              aria-label="Configuration"
              aria-haspopup="dialog"
              aria-controls="watchlist-configuration"
              :aria-expanded="configurationOpen"
              @click="openConfiguration"
            >
              <Settings2 :size="18" aria-hidden="true" />
            </button>
            <WatchlistTagManager
              v-if="!isDesktop"
              compact-trigger
              :key="`mobile-${tagScope}`"
              :tags="tags"
              :ready="ready"
              :blocked="writesBlocked"
            />
          </div>
        </div>
        <WatchlistPreferences
          ref="preferences"
          class="hidden lg:grid"
          :mode="displayMode"
          :filter="selectedTag"
          :sort="sortOrder"
          :tags="sortedTags"
          :ready="ready"
          :blocked="writesBlocked"
          :error="error"
          @display="changeDisplay"
          @filter="changeFilter"
          @sort="changeSort"
        >
          <template #heading
            ><h2 id="saved-desktop-heading" class="text-xl font-bold">
              Mes films
            </h2></template
          >
          <WatchlistTagManager
            v-if="isDesktop"
            :key="tagScope"
            :tags="tags"
            :ready="ready"
            :blocked="writesBlocked"
          />
        </WatchlistPreferences>
        <dialog
          v-if="configurationOpen"
          id="watchlist-configuration"
          ref="configurationDialog"
          aria-labelledby="watchlist-configuration-heading"
          class="fixed inset-x-0 bottom-0 top-auto m-0 w-full max-w-none overflow-y-auto overscroll-contain border-2 border-ink bg-surface p-5 pb-[max(1.25rem,env(safe-area-inset-bottom))] text-ink shadow-lg backdrop:bg-black/60"
          :style="{ maxHeight: panelHeight ? `${panelHeight - 16}px` : 'calc(100dvh - 1rem)', bottom: `${panelBottom}px` }"
          @cancel.prevent="closeConfiguration()"
          @click="backdrop($event, closeConfiguration)"
        >
          <div class="mb-5 flex items-center justify-between gap-3">
            <h2 id="watchlist-configuration-heading" class="account-heading">
              Configuration
            </h2>
            <button
              ref="configurationClose"
              type="button"
              class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle"
              aria-label="Fermer la configuration"
              @click="closeConfiguration()"
            >
              <X :size="20" aria-hidden="true" />
            </button>
          </div>
          <WatchlistPreferences
            mobile
            :mode="displayMode"
            :filter="selectedTag"
            :sort="sortOrder"
            :tags="sortedTags"
            :ready="ready"
            :blocked="writesBlocked"
            :error="error"
            @display="changeDisplay"
            @filter="changeFilter"
            @sort="changeSort"
          />
          <div v-if="error" role="alert" class="account-alert mt-5">
            <p>{{ error }}</p>
            <button
              type="button"
              class="account-link mt-2"
              :disabled="watchlist.saving.value"
              @click="watchlist.retry"
            >
              Réessayer
            </button>
          </div>
        </dialog>
        <p v-if="screenings.loading.value" role="status" class="sr-only">
          Chargement des séances…
        </p>
        <div v-if="screenings.error.value" role="status" class="mt-3 text-sm">
          <p>Séances indisponibles.</p>
          <button
            type="button"
            class="account-link min-h-11"
            @click="screenings.retry"
          >
            Réessayer les séances
          </button>
        </div>
        <div
          v-if="!ready && !error && !openTagEditor"
          role="status"
          class="mt-3 space-y-3 motion-safe:animate-pulse"
        >
          <span class="sr-only">Chargement de la watchlist…</span>
          <div class="h-24 bg-subtle" />
          <div class="h-24 bg-subtle" />
        </div>
        <p v-else-if="ready && !items.length" class="mt-3 text-sm">
          Votre watchlist est vide. Recherchez un film pour l’ajouter.
        </p>
        <div
          v-else-if="ready && !sortedItems.length"
          class="mt-3 text-sm"
          role="status"
        >
          <p>Aucun film avec ce tag.</p>
          <button
            type="button"
            class="account-link min-h-11"
            :disabled="writesBlocked"
            @click="clearFilter"
          >
            Voir tous les films
          </button>
        </div>
        <!-- Keep the committed rows mounted while an open picker reconciles a write.
             Shared write guards stay active; scope invalidation clears the picker. -->
        <div
          v-else-if="ready || openTagEditor"
          :class="displayMode === 'tags' ? 'mt-4 space-y-6 sm:mt-6 sm:space-y-8' : ''"
        >
          <component
            :is="displayMode === 'tags' ? 'section' : 'div'"
            v-for="section in savedSections"
            :key="section.id"
            :aria-labelledby="displayMode === 'tags' ? `watchlist-group-${section.id}` : undefined"
          >
            <h3
              v-if="displayMode === 'tags'"
              :id="`watchlist-group-${section.id}`"
              class="flex items-baseline gap-2 text-lg font-bold"
            >
              <span
                v-if="section.color"
                aria-hidden="true"
                class="size-2.5 shrink-0 rounded-full"
                :style="{ backgroundColor: watchlistTagPalette[section.color].borderColor }"
              />
              <span class="min-w-0 [overflow-wrap:anywhere]">{{
                section.name
              }}</span>
              <span class="shrink-0 text-sm font-normal text-muted"
                >({{ section.items.length }})</span
              >
            </h3>
            <ul>
              <WatchlistMovieRow
                v-for="movie in section.items"
                :key="movie.slug"
                :title="movie.title"
                :slug="movie.slug"
                :poster-url="movie.poster_url"
                :french-release-date="movie.french_release_date"
              >
                <template #content>
                  <div
                    v-if="screenings.forMovie(movie.slug)?.inTheaters"
                    data-watchlist-screenings
                    class="mt-2 flex flex-wrap gap-2"
                  >
                    <span
                      class="watchlist-tag-chip border-accent bg-accent-soft text-accent"
                      >En salle</span
                    >
                    <span
                      class="watchlist-tag-chip border-ink/20 bg-subtle text-ink"
                      :aria-label="screenings.forMovie(movie.slug)?.average.label"
                      >{{
                        screenings.forMovie(movie.slug)?.average.text
                      }}</span
                    >
                  </div>
                  <WatchlistItemTags
                    :title="movie.title"
                    :tags="sortedTags"
                    :tag-ids="movie.tag_ids"
                    :context-tag-id="displayMode === 'tags' && section.id.startsWith('tag-') ? section.id.slice(4) : undefined"
                    :open="openTagEditor === rowKey(section.id, movie.slug)"
                    :blocked="writesBlocked"
                    :error="error"
                    :retry-blocked="watchlist.saving.value || watchlist.loading.value"
                    @toggle="openTagEditor = openTagEditor === rowKey(section.id, movie.slug) ? '' : rowKey(section.id, movie.slug)"
                    @close="openTagEditor = ''"
                    @retry="watchlist.retry"
                    @assign="(tagId, assigned, input) => assignTag(movie.slug, tagId, assigned, input)"
                  />
                </template>
                <button
                  type="button"
                  data-watchlist-remove
                  class="flex size-11 shrink-0 self-center items-center justify-center text-ink hover:bg-subtle focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-50"
                  :disabled="writesBlocked"
                  aria-label="Retirer de la watchlist"
                  aria-haspopup="dialog"
                  aria-controls="watchlist-remove"
                  @click="openRemoval(movie, $event)"
                >
                  <X :size="20" aria-hidden="true" focusable="false" />
                </button>
              </WatchlistMovieRow>
            </ul>
          </component>
        </div>
      </section>
    </div>
    <NuxtLink
      v-else
      :to="account.session.value ? accountDestination(account.session.value) : '/connexion'"
      :prefetch="false"
      class="account-link"
      >Reprendre la connexion</NuxtLink
    >
  </AccountShell>
</template>

<style scoped>
@reference "../../assets/css/main.css";

/* Only authority-blocked controls, not invalid or consumed dialog actions. */
.soft-refresh :deep(button:disabled:not(dialog button)),
.soft-refresh :deep(select:disabled:not(dialog select)),
.soft-refresh :deep(label:has(input:disabled):not(dialog label)),
.soft-refresh :deep(#watchlist-configuration button:disabled),
.soft-refresh :deep(#watchlist-configuration select:disabled),
.soft-refresh :deep(#watchlist-add button[type="submit"]:disabled) {
  @apply opacity-100!;
}
</style>
