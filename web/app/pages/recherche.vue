<script setup lang="ts">
import { AlertTriangle, CalendarSearch, LoaderCircle, Search, SlidersHorizontal } from '@lucide/vue'
import type { Language, SlotResult } from '~/types/api'
import { createServiceTimeOptions, formatLongDate, todayInParis } from '~/utils/date'

const api = useMovieFlowApi()
const { favoriteTheaterIds, favoriteTheaters, isInitialized, isLoading, error: preferencesError, initialize } = useCinemaPreferences()
const form = reactive({
  date: todayInParis(),
  startAfter: '12:00',
  finishBefore: '15:00',
  language: 'ALL' as Language,
  includeAds: true
})
const timeOptions = createServiceTimeOptions()
const results = ref<SlotResult[] | null>(null)
const pending = ref(false)
const errorMessage = ref('')
const searchedDate = ref('')
const selectedTheaterIds = ref<string[]>([])
const theaterValidationMessage = ref('')
let previousFavoriteIds: string[] = []

watch(
  favoriteTheaterIds,
  (favoriteIds) => {
    const favorites = new Set(favoriteIds)
    const previousFavorites = new Set(previousFavoriteIds)
    const retainedIds = selectedTheaterIds.value.filter((id) => favorites.has(id))
    const newlyFavoritedIds = favoriteIds.filter((id) => !previousFavorites.has(id))

    selectedTheaterIds.value = favoriteIds.filter((id) => retainedIds.includes(id) || newlyFavoritedIds.includes(id))
    previousFavoriteIds = [...favoriteIds]
    if (selectedTheaterIds.value.length > 0) theaterValidationMessage.value = ''
  },
  { immediate: true }
)

watch(selectedTheaterIds, (ids) => {
  if (ids.length > 0) theaterValidationMessage.value = ''
})

onMounted(initialize)

async function submitSearch() {
  const selectedIds = favoriteTheaterIds.value.filter((id) => selectedTheaterIds.value.includes(id))
  if (selectedIds.length === 0) {
    theaterValidationMessage.value = 'Sélectionnez au moins un cinéma favori.'
    return
  }

  pending.value = true
  errorMessage.value = ''
  theaterValidationMessage.value = ''
  results.value = null
  try {
    results.value = await api.searchSlot({
      theaters: selectedIds.join(','),
      date: form.date,
      start_after: form.startAfter,
      finish_before: form.finishBefore,
      buffer_ads: form.includeAds ? 20 : 0,
      language: form.language
    })
    searchedDate.value = form.date
  } catch (error) {
    errorMessage.value = getFrenchApiError(error)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <main class="mx-auto max-w-[1280px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Trouver une séance</h1>

    <div class="mt-7 grid gap-8 lg:grid-cols-[320px_minmax(0,1fr)] lg:gap-10 lg:items-start">
      <form class="lg:sticky lg:top-6 lg:border-r lg:border-line lg:pr-8" @submit.prevent="submitSearch">
        <div class="mb-5 flex items-center gap-2.5 border-b border-line pb-4">
          <SlidersHorizontal :size="18" class="text-accent" aria-hidden="true" />
          <h2 class="font-semibold text-ink">Votre disponibilité</h2>
        </div>

        <div class="space-y-5">
          <fieldset :aria-invalid="theaterValidationMessage || preferencesError ? 'true' : undefined" :aria-describedby="theaterValidationMessage || preferencesError ? 'theater-selection-message' : undefined">
            <legend class="float-left mb-1.5 text-sm font-medium text-ink">Cinémas</legend>
            <NuxtLink to="/cinemas" class="float-right mb-1.5 text-sm font-medium text-accent underline-offset-4 hover:underline">Gérer mes favoris</NuxtLink>
            <div v-if="preferencesError && !isInitialized" id="theater-selection-message" class="clear-both rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">
              <p>{{ preferencesError }}</p>
              <button type="button" class="mt-3 font-semibold underline underline-offset-4" @click="initialize">Réessayer</button>
            </div>
            <div v-else-if="isLoading || !isInitialized" class="clear-both flex min-h-10 items-center gap-2 rounded-md border border-line px-3 text-sm text-muted">
              <LoaderCircle :size="16" class="animate-spin" aria-hidden="true" /> Chargement des cinémas…
            </div>
            <div v-else-if="favoriteTheaters.length" class="clear-both max-h-44 space-y-1 overflow-y-auto rounded-md border border-line bg-surface p-2">
              <label v-for="theater in favoriteTheaters" :key="theater.id" class="flex cursor-pointer items-start gap-2.5 rounded px-2 py-2 text-sm text-ink hover:bg-subtle">
                <input v-model="selectedTheaterIds" type="checkbox" :value="theater.id" class="mt-0.5 size-4 accent-accent" />
                <span><BrandedText :text="theater.name" /> <span class="text-muted">· {{ theater.city }}</span></span>
              </label>
            </div>
            <p v-else class="clear-both rounded-md border border-line bg-subtle px-3 py-2 text-sm text-muted">Aucun cinéma favori.</p>
            <p v-if="theaterValidationMessage" id="theater-selection-message" class="mt-1.5 text-sm text-red-700" role="alert">{{ theaterValidationMessage }}</p>
          </fieldset>

          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Date de la séance</span>
            <input v-model="form.date" required type="date" class="field" />
          </label>

          <div class="grid grid-cols-2 gap-3">
            <label class="block text-sm font-medium text-ink">
              <span class="mb-1.5 block">À partir de</span>
              <select v-model="form.startAfter" class="field">
                <option v-for="option in timeOptions" :key="`start-${option.value}`" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
            <label class="block text-sm font-medium text-ink">
              <span class="mb-1.5 block">Terminé avant</span>
              <select v-model="form.finishBefore" class="field">
                <option v-for="option in timeOptions" :key="`finish-${option.value}`" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
          </div>

          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Langue</span>
            <select v-model="form.language" class="field">
              <option value="ALL">Toutes</option>
              <option value="VOSTFR">VOSTFR</option>
              <option value="VF">VF</option>
            </select>
          </label>

          <label class="flex cursor-pointer items-start gap-3 rounded-md border border-line bg-subtle p-3 text-sm text-ink">
            <input v-model="form.includeAds" type="checkbox" class="mt-0.5 size-4 accent-accent" />
            <span>Inclure les publicités (+20 min)</span>
          </label>

          <button type="submit" class="button-primary w-full" :disabled="pending || isLoading || !isInitialized">
            <LoaderCircle v-if="pending" :size="18" class="animate-spin" aria-hidden="true" />
            <Search v-else :size="18" aria-hidden="true" />
            {{ pending ? 'Recherche…' : 'Trouver une séance' }}
          </button>
        </div>
      </form>

      <section aria-live="polite" aria-label="Résultats de recherche">
        <div class="mb-5 flex items-end justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-muted">Résultats</p>
            <h2 class="mt-1 text-xl font-semibold capitalize text-ink">{{ searchedDate ? formatLongDate(searchedDate) : 'Lancez votre recherche' }}</h2>
          </div>
          <span v-if="results" class="text-sm text-muted">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</span>
        </div>

        <div v-if="pending" class="state-panel" role="status">
          <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
          <p>Recherche des séances compatibles…</p>
        </div>
        <div v-else-if="errorMessage" class="state-panel" role="alert">
          <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
          <p class="max-w-lg">{{ errorMessage }}</p>
        </div>
        <div v-else-if="results?.length === 0" class="state-panel">
          <CalendarSearch :size="30" class="text-muted" aria-hidden="true" />
          <p>Aucune séance ne tient entièrement dans ce créneau.</p>
        </div>
        <div v-else-if="results" class="divide-y divide-line rounded-lg border border-line bg-surface">
          <SlotResultCard v-for="result in results" :key="result.showtime.id" :result="result" />
        </div>
        <div v-else class="state-panel">
          <CalendarSearch :size="30" class="text-accent" aria-hidden="true" />
          <p>Définissez votre créneau pour voir les séances compatibles.</p>
        </div>
      </section>
    </div>

  </main>
</template>
