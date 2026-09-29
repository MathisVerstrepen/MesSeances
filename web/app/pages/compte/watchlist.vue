<script setup lang="ts">
import { ArrowDownUp, List, Tags, X } from '@lucide/vue'
import { accountDestination } from '~/utils/accountState'
import { groupWatchlistItems } from '~/utils/watchlistGrouping'
import { sortWatchlistItems, watchlistSortOptions } from '~/utils/watchlistSort'
import { sortWatchlistTags } from '~/utils/watchlistTags'

definePageMeta({ middleware: 'account-auth' })
useHead({ title: 'Watchlist - MesSeances' })
const account = useAccountSession()
const watchlist = useWatchlist()
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
const selectedTag = ref('')
const displayMode = ref<'list' | 'tags'>('list')
const openTagEditor = ref('')
const tagScope = ref(0)
const tagFilter = useTemplateRef('tagFilter')
let tagInteraction = 0
function interactWithTags() {
  tagInteraction++
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
    : [{ id: 'list', name: '', items: sortedItems.value }],
)
const rowKey = (sectionId: string, slug: string) => `${sectionId}:${slug}`
watch([selectedTag, displayMode], () => {
  openTagEditor.value = ''
  tagInteraction++
})
watch(tags, () => {
  if (
    selectedTag.value &&
    !tags.value.some((tag) => tag.id === selectedTag.value)
  )
    selectedTag.value = ''
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
  if (!input.isConnected) tagFilter.value?.focus({ preventScroll: true })
  else if (input.isConnected && !input.disabled)
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
    document.activeElement === document.body
  )
    select.focus({ preventScroll: true })
}
const searchArea = useTemplateRef('searchArea')
const searchInput = useTemplateRef('searchInput')
const resultsPanel = useTemplateRef('resultsPanel')
const catalogTab = useTemplateRef('catalogTab')
const externalTab = useTemplateRef('externalTab')
const resultsScroll = useTemplateRef('resultsScroll')
const panelOpen = ref(false)
const activeTab = ref<'catalog' | 'external'>('catalog')
const panelHeight = ref(448)
let interaction = 0

function positionPanel() {
  if (!panelOpen.value || !resultsPanel.value) return
  const viewport = window.visualViewport
  const bottom = viewport
    ? viewport.offsetTop + viewport.height
    : window.innerHeight
  panelHeight.value = Math.max(
    0,
    Math.min(448, bottom - resultsPanel.value.getBoundingClientRect().top - 16),
  )
}

function dismiss(restoreFocus = false) {
  interaction++
  panelOpen.value = false
  watchlist.dismissSearch()
  if (restoreFocus) searchInput.value?.focus({ preventScroll: true })
}

async function submitSearch() {
  if (writesBlocked.value || searching.value) return
  interaction++
  panelOpen.value = true
  activeTab.value = 'catalog'
  void watchlist.search()
  await nextTick()
  if (!panelOpen.value) return
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
    if (resultsScroll.value) resultsScroll.value.scrollTop = 0
    return
  }
  watchlist.clearSearch()
  dismiss(true)
}

function outsidePointer(event: PointerEvent) {
  if (
    panelOpen.value &&
    event.target instanceof Node &&
    !searchArea.value?.contains(event.target)
  )
    dismiss()
}

function focusOut(event: FocusEvent) {
  if (
    panelOpen.value &&
    event.relatedTarget instanceof Node &&
    !searchArea.value?.contains(event.relatedTarget)
  )
    dismiss()
}

function escape(event: KeyboardEvent) {
  if (!panelOpen.value || event.key !== 'Escape') return
  event.preventDefault()
  dismiss(true)
}

function clearPageSearch() {
  interaction++
  panelOpen.value = false
  watchlist.clearSearch()
  selectedTag.value = ''
  displayMode.value = 'list'
  openTagEditor.value = ''
  tagScope.value++
}

watch(watchlist.scopeKey, clearPageSearch, { flush: 'sync' })
onMounted(() => {
  window.addEventListener('pagehide', clearPageSearch)
  document.addEventListener('pointerdown', interactWithTags)
  document.addEventListener('keydown', interactWithTags)
  document.addEventListener('pointerdown', outsidePointer)
  document.addEventListener('keydown', escape)
  window.addEventListener('resize', positionPanel)
  window.addEventListener('scroll', positionPanel, true)
  window.visualViewport?.addEventListener('resize', positionPanel)
  window.visualViewport?.addEventListener('scroll', positionPanel)
})
onBeforeUnmount(() => {
  clearPageSearch()
  window.removeEventListener('pagehide', clearPageSearch)
  document.removeEventListener('pointerdown', interactWithTags)
  document.removeEventListener('keydown', interactWithTags)
  document.removeEventListener('pointerdown', outsidePointer)
  document.removeEventListener('keydown', escape)
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
  >
    <div v-if="owner" class="space-y-10">
      <div
        v-if="error && !panelOpen && !openTagEditor"
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
      <form
        ref="searchArea"
        class="relative max-w-2xl"
        :aria-busy="searching"
        @submit.prevent="submitSearch"
        @focusout="focusOut"
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
            aria-haspopup="dialog"
            :aria-expanded="panelOpen"
            :aria-controls="panelOpen ? 'watchlist-results' : undefined"
          >
          <button
            type="submit"
            class="account-primary"
            :disabled="writesBlocked || searching"
          >
            Rechercher
          </button>
        </div>
        <div
          v-if="panelOpen"
          id="watchlist-results"
          ref="resultsPanel"
          role="dialog"
          aria-label="Résultats de recherche de films"
          class="absolute inset-x-0 top-full z-30 mt-2 flex flex-col overflow-hidden border-2 border-ink bg-surface shadow-lg"
          :style="{ maxHeight: `${panelHeight}px` }"
        >
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
            <button
              type="button"
              class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-2"
              aria-label="Fermer les résultats"
              @click="dismiss(true)"
            >
              <X :size="20" aria-hidden="true" />
            </button>
          </div>
          <div
            ref="resultsScroll"
            class="min-h-0 overflow-y-auto overscroll-contain px-4 pb-4"
            :aria-busy="searching"
          >
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
              La recherche a été interrompue. Relancez-la.
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
                  La recherche externe est indisponible. Les films du catalogue
                  restent disponibles. Relancez la recherche plus tard.
                </p>
                <p
                  v-else-if="searchResults.external_status === 'disabled'"
                  class="mt-3 text-sm"
                >
                  La recherche externe n’est pas activée. Choisissez un film du
                  catalogue.
                </p>
                <template v-else>
                  <p v-if="searchResults.external.length" class="mt-3 text-sm">
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
      </form>
      <section aria-labelledby="saved-heading">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 id="saved-heading" class="text-xl font-bold">Mes films</h2>
          <div
            role="group"
            aria-label="Affichage des films"
            class="inline-flex shrink-0 border-2 border-ink bg-surface"
          >
            <button
              v-for="mode in ([{ value: 'list', label: 'Liste' }, { value: 'tags', label: 'Par tag' }] as const)"
              :key="mode.value"
              type="button"
              :aria-pressed="displayMode === mode.value"
              :disabled="!ready"
              class="relative inline-flex min-h-12 min-w-27 items-center justify-center gap-2 px-4 font-mono text-xs font-black uppercase tracking-[0.08em] not-first:border-l-2 not-first:border-ink focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ink focus-visible:ring-offset-0 disabled:opacity-50"
              :class="displayMode === mode.value ? 'bg-ink text-surface shadow-[inset_0_-4px_0_var(--color-highlight)]' : 'bg-surface text-ink enabled:hover:bg-subtle'"
              @click="displayMode = mode.value"
            >
              <component
                :is="mode.value === 'list' ? List : Tags"
                :size="18"
                class="shrink-0"
                aria-hidden="true"
                focusable="false"
              />
              {{ mode.label }}
            </button>
          </div>
        </div>
        <div
          class="mt-4 grid min-w-0 gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end"
        >
          <div class="min-w-0">
            <label for="watchlist-tag-filter" class="account-label"
              >Filtrer par tag</label
            >
            <select
              id="watchlist-tag-filter"
              ref="tagFilter"
              v-model="selectedTag"
              class="account-input min-h-11 w-full min-w-0"
              :disabled="!ready"
            >
              <option value="">Tous les films</option>
              <option
                v-for="tag in (ready ? sortedTags : [])"
                :key="tag.id"
                :value="tag.id"
              >
                {{ tag.name }}
              </option>
            </select>
          </div>
          <div class="relative min-w-0 self-end">
            <label for="watchlist-sort" class="sr-only">Trier par</label>
            <ArrowDownUp
              :size="20"
              class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink"
              aria-hidden="true"
            />
            <select
              id="watchlist-sort"
              class="account-input min-h-11 w-full min-w-0 pl-10!"
              :value="ready ? sortOrder : ''"
              :disabled="!ready || writesBlocked"
              @change="changeSort"
            >
              <option v-if="!ready" value="" disabled>
                {{ error ? 'Tri indisponible' : 'Trier par' }}
              </option>
              <option
                v-for="option in watchlistSortOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </option>
            </select>
          </div>
          <WatchlistTagManager
            :key="tagScope"
            :tags="tags"
            :ready="ready"
            :blocked="writesBlocked"
          />
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
            @click="selectedTag = ''"
          >
            Voir tous les films
          </button>
        </div>
        <!-- Keep the committed rows mounted while an open picker reconciles a write.
             Shared write guards stay active; scope invalidation clears the picker. -->
        <div
          v-else-if="ready || openTagEditor"
          :class="displayMode === 'tags' ? 'mt-6 space-y-8' : ''"
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
              <span class="min-w-0 [overflow-wrap:anywhere]">{{
                section.name
              }}</span>
              <span class="text-sm font-normal text-muted"
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
                  <WatchlistItemTags
                    :title="movie.title"
                    :tags="sortedTags"
                    :tag-ids="movie.tag_ids"
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
                <WatchlistButton :slug="movie.slug" :show-error="false" />
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
