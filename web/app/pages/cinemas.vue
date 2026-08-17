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

function toggleTheater(id: string) {
  if (!toggleFavoriteTheater(id)) {
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
  <main class="mx-auto max-w-[1100px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <div class="flex flex-col justify-between gap-5 sm:flex-row sm:items-end">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Mes cinémas</h1>
        <p class="mt-2 text-sm text-muted">
          {{ favoriteTheaterIds.length }} cinéma{{ favoriteTheaterIds.length > 1 ? 's' : '' }} sélectionné{{ favoriteTheaterIds.length > 1 ? 's' : '' }}
        </p>
      </div>

      <label class="block w-full text-sm font-medium text-ink sm:max-w-sm">
        <span class="mb-1.5 block">Rechercher un cinéma ou une ville</span>
        <span class="relative block">
          <Search :size="17" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" aria-hidden="true" />
          <input :value="search" type="search" class="field pl-10" autocomplete="off" @input="updateSearch" />
        </span>
      </label>
    </div>

    <p class="sr-only" aria-live="polite">{{ statusMessage }}</p>
    <p v-if="validationMessage" class="mt-5 flex items-center gap-2 text-sm font-medium text-red-700" role="alert">
      <AlertTriangle :size="17" aria-hidden="true" />
      {{ validationMessage }}
    </p>

    <div v-if="!isReady || isLoading" class="state-panel mt-8" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement des cinémas…</p>
    </div>

    <div v-else-if="error" class="state-panel mt-8" role="alert">
      <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
      <p class="max-w-lg">{{ error }}</p>
      <button type="button" class="button-primary" @click="loadPreferences">
        <RefreshCw :size="17" aria-hidden="true" /> Réessayer
      </button>
    </div>

    <div v-else-if="theaters.length === 0" class="state-panel mt-8">
      <Building2 :size="30" class="text-muted" aria-hidden="true" />
      <p>Aucun cinéma disponible.</p>
    </div>

    <div v-else-if="visibleTheaterCount === 0" class="state-panel mt-8">
      <Search :size="28" class="text-muted" aria-hidden="true" />
      <p>Aucun cinéma ne correspond à votre recherche.</p>
    </div>

    <div v-else class="mt-8 space-y-8">
      <section v-for="group in visibleGroups" :key="group.city">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-3">
          <h2 class="text-base font-semibold text-ink">{{ group.city }}</h2>
          <div class="flex items-center gap-3 text-sm">
            <button
              type="button"
              class="font-medium text-accent transition hover:text-orange-800 disabled:cursor-not-allowed disabled:text-muted"
              :disabled="group.theaters.every((theater) => selectedIds.has(theater.id))"
              @click="updateGroup(group.theaters, true)"
            >
              Tout sélectionner
            </button>
            <button
              type="button"
              class="font-medium text-muted transition hover:text-ink disabled:cursor-not-allowed disabled:opacity-50"
              :disabled="group.theaters.every((theater) => !selectedIds.has(theater.id))"
              @click="updateGroup(group.theaters, false)"
            >
              Désélectionner
            </button>
          </div>
        </div>

        <div class="grid gap-2 sm:grid-cols-2">
          <label
            v-for="theater in group.theaters"
            :key="theater.id"
            class="flex cursor-pointer items-start gap-3 rounded-md border bg-surface p-4 transition hover:border-stone-400 focus-within:ring-2 focus-within:ring-accent focus-within:ring-offset-2 focus-within:ring-offset-canvas"
            :class="selectedIds.has(theater.id) ? 'border-accent' : 'border-line'"
          >
            <span class="mt-0.5 grid size-5 shrink-0 place-items-center rounded border" :class="selectedIds.has(theater.id) ? 'border-accent bg-accent text-white' : 'border-line'">
              <Check v-if="selectedIds.has(theater.id)" :size="14" aria-hidden="true" />
            </span>
            <input
              type="checkbox"
              class="sr-only"
              :checked="selectedIds.has(theater.id)"
              @change="toggleTheater(theater.id)"
            />
            <span class="min-w-0">
              <BrandedText :text="theater.name" class="block text-sm font-semibold text-ink" />
              <span class="mt-1 block text-sm text-muted">{{ theater.address }}, {{ theater.postal_code }} {{ theater.city }}</span>
            </span>
          </label>
        </div>
      </section>
    </div>
  </main>
</template>
