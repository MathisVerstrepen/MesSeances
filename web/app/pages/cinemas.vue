<script setup lang="ts">
import { AlertTriangle, Building2, Check, LoaderCircle, RefreshCw, Search } from '@lucide/vue'
import { mergeOwnedQuery, queriesEqual, singularQueryValue } from '~/utils/routeQuery'

const route = useRoute()
const router = useRouter()
const OWNED_QUERY_KEYS = ['q'] as const

const {
  theaters,
  favoriteTheaterIds,
  isLoading,
  error,
  initialize,
  setFavoriteTheaterIds,
  toggleFavoriteTheater
} = useCinemaPreferences()

type PreferenceTheater = (typeof theaters.value)[number]

const search = ref('')
const isReady = ref(false)
const statusMessage = ref('')
const validationMessage = ref('')

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

const selectedIds = computed(() => new Set(favoriteTheaterIds.value))
const normalizedSearch = computed(() => search.value.trim().toLocaleLowerCase('fr-FR'))

const visibleGroups = computed(() => {
  const groups = new Map<string, PreferenceTheater[]>()

  for (const theater of theaters.value) {
    const searchable = `${theater.name} ${theater.city}`.toLocaleLowerCase('fr-FR')
    if (normalizedSearch.value && !searchable.includes(normalizedSearch.value)) continue

    const group = groups.get(theater.city) ?? []
    group.push(theater)
    groups.set(theater.city, group)
  }

  return [...groups.entries()].map(([city, cityTheaters]) => ({ city, theaters: cityTheaters }))
})

const visibleTheaterCount = computed(() => visibleGroups.value.reduce((total, group) => total + group.theaters.length, 0))

function reportSaved() {
  validationMessage.value = ''
  const count = favoriteTheaterIds.value.length
  statusMessage.value = `${count} cinéma${count > 1 ? 's' : ''} enregistré${count > 1 ? 's' : ''}.`
}

function reportRequiredSelection() {
  statusMessage.value = ''
  validationMessage.value = 'Conservez au moins un cinéma dans vos favoris.'
}

function toggleTheater(id: string, event: Event) {
  if (!toggleFavoriteTheater(id)) {
    if (event.currentTarget instanceof HTMLInputElement) event.currentTarget.checked = true
    reportRequiredSelection()
    return
  }
  reportSaved()
}

function updateGroup(groupTheaters: readonly PreferenceTheater[], select: boolean) {
  const nextIds = new Set(favoriteTheaterIds.value)
  for (const theater of groupTheaters) {
    if (select) nextIds.add(theater.id)
    else nextIds.delete(theater.id)
  }

  if (!setFavoriteTheaterIds([...nextIds])) {
    reportRequiredSelection()
    return
  }
  reportSaved()
}

async function loadPreferences() {
  isReady.value = false
  try {
    await initialize()
  } finally {
    isReady.value = true
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
})
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
          <strong>{{ favoriteTheaterIds.length }}</strong>
          <span>cinéma{{ favoriteTheaterIds.length > 1 ? 's' : '' }} sélectionné{{ favoriteTheaterIds.length > 1 ? 's' : '' }}</span>
        </div>
        <span class="title-mark" aria-hidden="true"></span>
      </div>
    </section>

    <section class="cinemas-canvas border-b-2 border-ink" aria-label="Sélection des cinémas favoris">
      <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-12">
        <div class="search-workspace border-2 border-ink bg-[#f1efe8] p-4 shadow-[7px_7px_0_#27272a] sm:p-6">
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
        </div>

        <p class="sr-only" aria-live="polite">{{ statusMessage }}</p>
        <p v-if="validationMessage" class="validation-alert mt-7" role="alert">
          <AlertTriangle :size="19" aria-hidden="true" />
          {{ validationMessage }}
        </p>

        <div v-if="!isReady || isLoading" class="cinema-state" role="status" aria-live="polite">
          <LoaderCircle :size="34" class="cinema-spinner animate-spin" aria-hidden="true" />
          <p>Chargement des cinémas…</p>
        </div>

        <div v-else-if="error" class="cinema-state" role="alert">
          <AlertTriangle :size="34" class="text-primary" aria-hidden="true" />
          <p class="max-w-lg">{{ error }}</p>
          <button type="button" class="state-button" @click="loadPreferences">
            <RefreshCw :size="17" aria-hidden="true" /> Réessayer
          </button>
        </div>

        <div v-else-if="theaters.length === 0" class="cinema-state">
          <Building2 :size="36" aria-hidden="true" />
          <p>Aucun cinéma disponible.</p>
        </div>

        <div v-else-if="visibleTheaterCount === 0" class="cinema-state">
          <Search :size="34" aria-hidden="true" />
          <p>Aucun cinéma ne correspond à votre recherche.</p>
        </div>

        <div v-else class="mt-10">
          <div class="mb-7 flex flex-col gap-2 border-y-2 border-ink py-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
            <h2 class="text-xl font-black tracking-[-0.035em] sm:text-2xl">Cinémas disponibles</h2>
            <p class="utility-label">{{ visibleTheaterCount }} cinéma{{ visibleTheaterCount > 1 ? 's' : '' }}</p>
          </div>

          <div class="space-y-8">
            <section v-for="group in visibleGroups" :key="group.city" class="city-section border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
              <header class="grid gap-4 border-b-2 border-ink bg-[#f1efe8] p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:p-5">
                <div class="min-w-0">
                  <h3 class="text-2xl font-black uppercase tracking-[-0.045em] sm:text-3xl">
                    <NuxtLink :to="`/ville/${encodeURIComponent(group.theaters[0]!.city_slug)}/cinemas`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary">{{ group.city }}</NuxtLink>
                  </h3>
                  <p class="utility-label mt-1">{{ group.theaters.length }} cinéma{{ group.theaters.length > 1 ? 's' : '' }}</p>
                </div>
                <div class="grid grid-cols-2 gap-2 sm:flex" role="group" :aria-label="`Modifier les favoris à ${group.city}`">
                  <button
                    type="button"
                    class="group-action"
                    :disabled="group.theaters.every((theater) => selectedIds.has(theater.id))"
                    @click="updateGroup(group.theaters, true)"
                  >
                    Tout sélectionner
                  </button>
                  <button
                    type="button"
                    class="group-action group-action--secondary"
                    :disabled="group.theaters.every((theater) => !selectedIds.has(theater.id))"
                    @click="updateGroup(group.theaters, false)"
                  >
                    Désélectionner
                  </button>
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
                    <input type="checkbox" class="peer sr-only" :checked="selectedIds.has(theater.id)" @change="toggleTheater(theater.id, $event)" />
                    <span class="theater-check mt-0.5 grid size-7 shrink-0 place-items-center border-2 border-ink bg-surface" aria-hidden="true"><Check v-if="selectedIds.has(theater.id)" :size="18" stroke-width="3" /></span>
                    <span class="min-w-0"><BrandedText :text="theater.name" class="block text-base font-black leading-tight tracking-[-0.02em] text-ink sm:text-lg" /><span class="mt-2 block text-sm font-medium leading-relaxed text-ink"><template v-if="theater.address">{{ theater.address }}, </template>{{ theater.postal_code }} {{ theater.city }}</span></span>
                  </label>
                  <NuxtLink :to="`/cinema/${encodeURIComponent(theater.slug)}`" class="mt-3 inline-flex min-h-11 items-center font-mono text-[11px] font-black uppercase underline decoration-2 underline-offset-4 hover:text-primary">Voir les séances</NuxtLink>
                </div>
              </div>
            </section>
          </div>
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
.group-action:focus-visible {
  outline: 3px solid #27272a;
  outline-offset: 3px;
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

.cinema-state {
  margin: 4rem auto 1rem;
  display: flex;
  min-height: 24rem;
  max-width: 48rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 2rem;
  text-align: center;
  font-weight: 800;
  box-shadow: 8px 8px 0 #27272a;
}

.state-button,
.group-action {
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
.group-action:hover:not(:disabled) {
  background: #991b1b;
}

.group-action--secondary {
  background: #fff;
  color: #27272a;
}

.group-action--secondary:hover:not(:disabled) {
  background: #e8e6de;
}

.group-action:disabled {
  cursor: not-allowed;
  opacity: 0.4;
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

  .cinema-state {
    min-height: 19rem;
    margin-top: 2.5rem;
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
